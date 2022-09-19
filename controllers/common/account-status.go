package commoncontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"time"

	"github.com/goombaio/namegenerator"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/strings/slices"
	"operators.kloudlite.io/lib/conditions"
	"operators.kloudlite.io/lib/errors"
	"operators.kloudlite.io/lib/functions"
	rApi "operators.kloudlite.io/lib/operator"
	"operators.kloudlite.io/lib/templates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	managementv1 "operators.kloudlite.io/apis/management/v1"
)

func (r *AccountReconciler) reconcileStatus(req *rApi.Request[*managementv1.Account]) rApi.StepResult {
	req.Object.Status.DisplayVars.Reset()
	var cs []metav1.Condition
	isReady := true
	retry := false

	// checking wg namespace if not present
	if err := func() error {
		if _, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Name: "wg-" + req.Object.Name,
		}, &corev1.Namespace{}); err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false

			cs = append(cs,
				conditions.New(
					"NamespaceFound",
					false,
					"NotFound",
					"wireguard namespace not found",
				),
			)
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}
	nsCreated := func() bool {
		return meta.IsStatusConditionFalse(cs, "NamespaceFound")
	}

	// checking dns-server present
	if err := func() error {
		if !nsCreated() {
			return nil
		}

		if _, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Namespace: "wg-" + req.Object.Name,
			Name:      "coredns",
		}, &appsv1.Deployment{}); err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false
			cs = append(cs,
				conditions.New(
					"DNSServerCreated",
					false,
					"NotFound",
					"DNS server is not created yet",
				),
			)

		}
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check accounts-wg public key generated
	if err := func() error {

		wgSecrets, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Namespace: "wg-" + req.Object.Name,
			Name:      "wg-server-keys",
		}, &corev1.Secret{})

		if err != nil {
			isReady = false
			cs = append(cs,
				conditions.New(
					"WGSecretNotFound",
					false,
					"NotFound",
					"wireguard server keys not generated yet",
				),
			)
			return nil
		}

		rApi.SetLocal(req, "wg-public-key", string(wgSecrets.Data["public-key"]))
		rApi.SetLocal(req, "wg-private-key", string(wgSecrets.Data["private-key"]))

		// check if publicKey is set to DisplayVars if not then set it (will be used by device)
		existingPublicKey, ok := req.Object.Status.DisplayVars.Get("wg-public-key")
		if ok && string(wgSecrets.Data["public-key"]) != existingPublicKey {
			retry = true
		}

		req.Object.Status.DisplayVars.Set("wg-public-key", string(wgSecrets.Data["public-key"]))
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// fetching regions and setting to local to fetch it later
	if err := func() error {

		var regions managementv1.RegionList
		var klRegions managementv1.RegionList
		err := r.List(req.Context(), &regions, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"kloudlite.io/account-ref": req.Object.Name,
			}),
		})

		err2 := r.List(req.Context(), &klRegions, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"kloudlite.io/account-ref": "",
			}),
		})

		regions.Items = append(regions.Items, klRegions.Items...)

		if err != nil || len(regions.Items) == 0 {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			if !apiErrors.IsNotFound(err2) {
				return err
			}

			isReady = false
			cs = append(cs,
				conditions.New(
					"RegionsFound",
					false,
					"NotFound",
					"Regions Not found",
				),
			)
			return nil
		}

		// filtring the regions with is related to current account
		// r2 := []managementv1.Region{}
		// for _, region := range regions.Items {
		// 	if region.Spec.Account == "" || region.Spec.Account == req.Object.Name {
		// 		r2 = append(r2, region)
		// 	}
		// }
		// regions.Items = r2

		rApi.SetLocal(req, "regions", regions)
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// needs to be optimized
	// Generating device Services and also device proxy config
	if statusErr, OpsError := func() (error, error) {
		configs := []configService{}
		services := []corev1.Service{}

		if err := func() error {

			oldConfig, configFetchError := rApi.Get(req.Context(), r.Client, types.NamespacedName{
				Namespace: "wg-" + req.Object.Name,
				Name:      "device-proxy-config",
			}, &corev1.ConfigMap{})

			configData := []configService{}

			// parsing the value from old config andd adding to the var configData
			if configFetchError == nil {
				oConfMap := map[string][]configService{}
				err := json.Unmarshal([]byte(oldConfig.Data["config.json"]), &oConfMap)

				if oConfMap["services"] != nil {
					configData = oConfMap["services"]
				}
				if err != nil {
					return err
				}
			}

			// method to check either the port exists int the config
			isContains := func(svce []configService, port int32) bool {
				for _, s := range svce {
					if s.ServicePort == port {
						return true
					}
				}
				return false
			}

			// method to finding the unique port to assign to needfull
			getTempPort := func(svce []configService, id string) int32 {
				for _, c := range svce {
					if c.Id == id {
						return c.ProxyPort
					}
				}
				return func() int32 {
					min, max := 3000, 6000

					count := 0
					var r int

					for {
						r = rand.Intn(max-min) + min
						if !isContains(configData, int32(r)) || count > max-min {
							break
						}

						count++
					}

					return int32(r)
				}()
			}

			var devices managementv1.DeviceList
			activeRegions := map[string]bool{}

			// fetching all the devices under the current account
			if err := r.List(req.Context(), &devices,
				&client.ListOptions{
					LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
						"kloudlite.io/account-ref": req.Object.Spec.AccountId,
					}),
				},
			); err != nil {
				return err
			}

			// sorting the fetched devices
			sort.Slice(devices.Items, func(i, j int) bool {
				return devices.Items[i].Name < devices.Items[j].Name
			})

			// fmt.Printf("devices: %v\n", devices.Items, req.Object.Spec.AccountId)

			// generating the latest services and will be applied at in operations sections
			for _, d := range devices.Items {
				activeRegions[d.Spec.ActiveRegion] = true

				type portStruct struct {
					Name       string `json:"name"`
					Port       int32  `json:"port"`
					TargetPort int32  `json:"targetPort"`
					Protocol   string `json:"protocol"`
				}

				ports := []corev1.ServicePort{}
				for _, port := range d.Spec.Ports {
					tempPort := getTempPort(configData, fmt.Sprint(d.Name, "-", port.Port))

					ports = append(ports, corev1.ServicePort{
						Name: fmt.Sprint(d.Name, "-", port.Port),
						Port: port.Port,
						TargetPort: intstr.IntOrString{
							Type:   0,
							IntVal: tempPort,
						},
					})

					dIp, e := getRemoteDeviceIp(int64(d.Spec.Offset))
					if e != nil {
						fmt.Println(e)
						continue
					}

					configs = append(configs, configService{
						Id:   fmt.Sprint(d.Name, "-", port.Port),
						Name: dIp.String(),
						ServicePort: func() int32 {
							if port.TargetPort != 0 {
								return port.TargetPort
							}
							return port.Port
						}(),
						ProxyPort: tempPort,
					})
				}
				if len(ports) == 0 {
					ports = append(ports, corev1.ServicePort{
						Name: "temp",
						Port: 3000,
						TargetPort: intstr.IntOrString{
							Type:   0,
							IntVal: 3000,
						},
					})
				}

				// finally generating the services
				services = append(services, corev1.Service{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:      d.Name,
						Namespace: "wg-" + req.Object.Name,
						Labels: map[string]string{
							"proxy-device-service":    "true",
							"kloudlite.io/device-ref": d.Name,
						},
						// Annotations not needed (if will run smooth delete this comment in future)
						// Annotations: map[string]string{
						// 	"proxy-device-service": "true",
						// },
						OwnerReferences: []metav1.OwnerReference{
							functions.AsOwner(&d),
						},
					},

					Spec: corev1.ServiceSpec{
						Ports: ports,
						Selector: map[string]string{
							"region": d.Spec.ActiveRegion,
						},
					},
				})
			}

			rApi.SetLocal(req, "active-regions", activeRegions)

			sort.Slice(configs, func(i, j int) bool {
				return configs[i].Name < configs[j].Name
			})

			c, err := json.Marshal(map[string][]configService{
				"services": configs,
			})

			if err != nil {
				return err
			}

			// checking either the new generated config is equal or not
			equal := false
			if configFetchError == nil {
				//fmt.Println(oldConfig.Data["config.json"], string(c))
				equal, err = functions.JSONStringsEqual(oldConfig.Data["config.json"], string(c))
				if err != nil {
					return err
				}

				if len(configs) == 0 {
					equal = true
				}
			}

			// if not equal set the error the status of ther resource
			if !equal {
				isReady = false
				cs = append(cs,
					conditions.New(
						"DeviceProxyConfigMatching",
						false,
						"NotFound",
						"Devices are updated",
					),
				)
			}

			regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
			if !ok {
				return fmt.Errorf("can't fetch regions")
			}

			var deploys appsv1.DeploymentList

			r.List(req.Context(), &deploys, &client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
					"wireguard-deployment": "true",
				}),
				Namespace: fmt.Sprintf("wg-%s", req.Object.Name),
			})

			deploysNames := func() []string {
				s := make([]string, 0)
				for _, d := range deploys.Items {
					s = append(s, d.Name)
				}
				return s
			}()

			for _, r := range regions.Items {

				if !activeRegions[r.Name] && slices.Contains(deploysNames, fmt.Sprintf("wireguard-deployment-%s", r.Name)) {
					functions.ExecCmd(fmt.Sprintf("kubectl delete deploy/wireguard-deployment-%s -n wg-%s", r.Name, req.Object.Name), "")
				}

			}

			return nil
		}(); err != nil {
			return err, nil
		}

		// updating device proxy config and services
		if err := func() error {
			if !meta.IsStatusConditionFalse(cs, "DeviceProxyConfigMatching") {
				return nil
			}

			sort.Slice(configs, func(i, j int) bool {
				return configs[i].Name < configs[j].Name
			})

			// fmt.Printf("config,services: %+v\n%+v\n", configs, services)
			if b, err := templates.Parse(templates.ProxyDevice, map[string]any{
				"services": services,
				"configmap": map[string][]configService{
					"services": configs,
				},
				"namespace": "wg-" + req.Object.Name,
				"account-refs": []metav1.OwnerReference{
					functions.AsOwner(req.Object),
				},
			}); err != nil {
				return err
			} else if _, err = functions.KubectlApplyExec(b); err != nil {
				return err
			}

			// fmt.Println(string(b))

			if configJson, err := json.Marshal(map[string][]configService{
				"services": configs,
			}); err != nil {
				return err
			} else {
				regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
				if !ok {
					return errors.New("regions not found")
				}

				errStr := ""

				for _, region := range regions.Items {
					if _, err = http.Post(
						fmt.Sprintf("http://wg-api-service-%s.wg-%s.svc.cluster.local:2999/post",
							region.Name, req.Object.Name),
						"application/json", bytes.NewBuffer(configJson)); err != nil {
						fmt.Println(region.Name, ":", err)
						errStr += err.Error() + "\n"
					}
				}

				if errStr != "" {
					return errors.New(errStr)
				}
			}
			return nil
		}(); err != nil {
			return nil, err
			// return rApi.NewStepResult(&ctrl.Result{
			// 	Requeue:      true,
			// 	RequeueAfter: 5,
			// }, err)
		}

		return nil, nil
	}(); statusErr != nil {
		return req.FailWithStatusError(statusErr)
	} else if OpsError != nil {
		// return rApi.NewStepResult(&ctrl.Result{
		// 	Requeue:      true,
		// 	RequeueAfter: 5,
		// }, OpsError)
		return req.FailWithOpError(OpsError)
	}

	// Generate WG Account Server Config
	if err := func() error {
		aPvKey, ok := rApi.GetLocal[string](req, "wg-private-key")
		if !ok {
			return errors.New("wireguard server private key not generated yet")
		}

		var deviceWgSecretList corev1.SecretList

		err := r.List(req.Context(), &deviceWgSecretList,
			&client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
					"kloudlite.io/is-wg-key": "true",
				}),
				Namespace: "wg-" + req.Object.Name,
			},
		)
		if err != nil {
			return err
		}

		var data struct {
			AccountWireguardIp     string
			AccountWireguardPvtKey string
			Peers                  []struct {
				PublicKey  string
				AllowedIps string
			}
		}

		data.AccountWireguardIp = "10.13.13.1/32"
		data.AccountWireguardPvtKey = aPvKey

		sort.Slice(deviceWgSecretList.Items, func(i, j int) bool {
			return deviceWgSecretList.Items[i].Name < deviceWgSecretList.Items[j].Name
		})

		for _, device := range deviceWgSecretList.Items {
			ip, ok := device.Data["ip"]
			if !ok {
				continue
			}
			publicKey, ok := device.Data["public-key"]
			if !ok {
				continue
			}
			data.Peers = append(data.Peers, struct {
				PublicKey  string
				AllowedIps string
			}{
				PublicKey:  string(publicKey),
				AllowedIps: fmt.Sprintf("%s/32", string(ip)),
			})
		}

		parse, err := templates.Parse(templates.WireGuardConfig, data)
		if err != nil {
			return err
		}

		rApi.SetLocal(req, "serverWgConfig", string(parse))

		existingConfig, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Namespace: "wg-" + req.Object.Name,
			Name:      "wg-server-config",
		}, &corev1.Secret{})

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false
			cs = append(cs,
				conditions.New(
					"WGServerConfigExists",
					false,
					"NotFound",
					"WG Server Config not found",
				),
			)
			return nil
		}

		if string(existingConfig.Data["data"]) != string(parse) {

			isReady = false
			cs = append(cs,
				conditions.New(
					"WGServerConfigMatching",
					false,
					"NotMatching",
					"WG Server Config not matching",
				),
			)
		}
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check either wg-deployment exists or not if exists the get svc and if svc present then fetch the nodeport and add it to DisplayVars
	if err := func() error {
		if meta.IsStatusConditionFalse(cs, "RegionsFound") {
			return nil
		}

		regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
		if !ok {
			return fmt.Errorf("can't fetch regions")
		}

		var deployments appsv1.DeploymentList
		err := r.Client.List(context.TODO(), &deployments, &client.ListOptions{
			Namespace: fmt.Sprintf("wg-%s", req.Object.Name),
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"wireguard-deployment": "true",
			}),
		})

		if err != nil || len(deployments.Items) == 0 || len(deployments.Items) != len(regions.Items) {
			if err != nil && !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false
			cs = append(cs, conditions.New("WGDeploymentExists", false, "NotFound", "Deployment not found"))
		}

		var svcs corev1.ServiceList
		err = r.Client.List(context.TODO(), &svcs, &client.ListOptions{
			Namespace: fmt.Sprintf("wg-%s", req.Object.Name),
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"wireguard-service": "true",
			}),
		})

		if err != nil || len(svcs.Items) == 0 || len(svcs.Items) != len(regions.Items) {
			if err != nil && !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false

			cs = append(cs,
				conditions.New(
					"WGServiceFound",
					false,
					"NotFound",
					"WG service not found",
				),
			)
			return nil
		}

		for _, svc := range svcs.Items {
			nodePort := svc.Spec.Ports[0].NodePort

			if nodePort == 0 {
				isReady = false
				cs = append(cs,
					conditions.New(
						"WGNodePortNotReady",
						false,
						"NotReady",
						"WG NodePort not available",
					),
				)
				continue
			}

			lbs := svc.GetLabels()
			if lbs == nil {
				continue
			}
			region := lbs["region"]
			req.Object.Status.DisplayVars.Set(fmt.Sprintf("WGNodePort-%s", region), fmt.Sprintf("%d", nodePort))
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// checking wg-domain if not present generate
	if err := func() error {
		dm, ok := req.Object.Status.GeneratedVars.Get("wg-domain")

		if !ok {
			fmt.Println("not found", dm)
			seed := time.Now().UTC().UnixNano()
			nameGenerator := namegenerator.NewNameGenerator(seed)

			domainName := func() string {

				for {
					name := nameGenerator.Generate()
					var domains managementv1.DomainList
					e := r.Client.List(context.TODO(), &domains, &client.ListOptions{
						LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
							"kloudlite.io/wg-domain": name,
						}),
					})
					if e != nil || len(domains.Items) == 0 {
						return name
					}
				}

			}()

			req.Object.Status.GeneratedVars.Set("wg-domain", domainName)

			return nil

		}

		rApi.SetLocal(req, "wg-domain", dm)

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	if retry {
		if err := r.Status().Update(req.Context(), req.Object); err != nil {
			return req.FailWithStatusError(err)
		}
		return req.Done(&ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5})
	}

	newConditions, hasUpdated, err := conditions.Patch(req.Object.Status.Conditions, cs)
	if err != nil {
		return req.FailWithStatusError(err)
	}

	if !hasUpdated && isReady == req.Object.Status.IsReady {
		return req.Next()
	}

	req.Object.Status.Conditions = newConditions
	req.Object.Status.IsReady = isReady
	if err := r.Status().Update(req.Context(), req.Object); err != nil {
		return req.FailWithStatusError(err)
	}

	return req.Done()
}
