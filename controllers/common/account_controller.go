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
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"operators.kloudlite.io/lib/errors"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	managementv1 "operators.kloudlite.io/apis/management/v1"
	"operators.kloudlite.io/lib/conditions"
	"operators.kloudlite.io/lib/functions"
	rApi "operators.kloudlite.io/lib/operator"
	"operators.kloudlite.io/lib/templates"
	"operators.kloudlite.io/lib/wireguard"
)

// create namespace
// wg-config namespace
// wg-deployment(wg,proxy) namespace
// proxy services of devices
// fetch nodeport
// dns domain (generation/fetching)

// AccountReconciler reconciles a Account object
type AccountReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type configService struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	ServicePort int32  `json:"servicePort"`
	ProxyPort   int32  `json:"proxyPort"`
}

//+kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts/finalizers,verbs=update

func (r *AccountReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &managementv1.Account{})

	if req == nil {
		return ctrl.Result{}, nil
	}

	req.Logger.Info("##################### NEW RECONCILATION------------------")

	// fmt.Printf("reconcile: %+v\n", req.Object)

	if req == nil {
		return ctrl.Result{}, nil
	}

	if req.Object.GetDeletionTimestamp() != nil {
		if x := r.finalize(req); !x.ShouldProceed() {
			return x.Result(), x.Err()
		}
	}

	if x := req.EnsureLabels(); !x.ShouldProceed() {
		fmt.Println(x.Err())
		return x.Result(), x.Err()
	}

	fmt.Println("reconcileStatus")

	if x := r.reconcileStatus(req); !x.ShouldProceed() {
		return x.Result(), x.Err()
	}

	fmt.Println("reconcileOperations")
	if x := r.reconcileOperations(req); !x.ShouldProceed() {
		return x.Result(), x.Err()
	}

	return ctrl.Result{}, nil

}

func (r *AccountReconciler) finalize(req *rApi.Request[*managementv1.Account]) rApi.StepResult {
	return req.Finalize()
}

func (r *AccountReconciler) reconcileStatus(req *rApi.Request[*managementv1.Account]) rApi.StepResult {
	req.Object.Status.DisplayVars.Reset()
	var cs []metav1.Condition
	isReady := true
	retry := false

	// check wg namespace
	if err := func() error {
		_, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Name: "wg-" + req.Object.Name,
		}, &corev1.Namespace{})

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false
			cs = append(cs,
				conditions.New(
					"WGNamespaceNotFound",
					false,
					"NotFound",
					"WG namespace not found",
				),
			)
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// checking dns-server ready
	if err := func() error {
		_, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Namespace: "wg-" + req.Object.Name,
			Name:      "coredns",
		}, &appsv1.Deployment{})

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false
			cs = append(cs,
				conditions.New(
					"DNSServerReady",
					false,
					"NotFound",
					"DNS server is not ready",
				),
			)

		}
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check wg public key
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
					"WG public key not found",
				),
			)
			return nil
		}

		rApi.SetLocal(req, "accountWgServerKeys", wgSecrets)
		existingPublicKey, ok := req.Object.Status.DisplayVars.Get("WGPublicKey")
		if ok && string(wgSecrets.Data["public-key"]) != existingPublicKey {
			retry = true
		}

		req.Object.Status.DisplayVars.Set("WGPublicKey", string(wgSecrets.Data["public-key"]))

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// Generating Services
	if err := func() error {
		oldConfig, configFetchError := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Namespace: "wg-" + req.Object.Name,
			Name:      "device-proxy-config",
		}, &corev1.ConfigMap{})

		cServices := []configService{}
		configData := []configService{}

		if configFetchError == nil {
			oConfMap := map[string][]configService{}

			fmt.Println("reading")

			err := json.Unmarshal([]byte(oldConfig.Data["config.json"]), &oConfMap)

			if oConfMap["services"] != nil {
				configData = oConfMap["services"]
			}

			if err != nil {
				return err
			}

		}

		isContains := func(svce []configService, port int32) bool {
			for _, s := range svce {
				if s.ServicePort == port {
					return true
				}
			}
			return false
		}

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

		err := r.List(req.Context(), &devices,
			&client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
					"kloudlite.io/account-id": req.Object.Spec.AccountId,
				}),
			},
		)

		sort.Slice(devices.Items, func(i, j int) bool {
			return devices.Items[i].Name < devices.Items[j].Name
		})

		// fmt.Printf("devices: %v\n", devices.Items, req.Object.Spec.AccountId)

		if err != nil {
			return err
		}

		svcs := []corev1.Service{}

		// fmt.Println("devices", devices)
		for _, d := range devices.Items {
			type portStruct struct {
				Name       string `json:"name"`
				Port       int32  `json:"port"`
				TargetPort int32  `json:"targetPort"`
				Protocol   string `json:"protocol"`
			}

			ports := []corev1.ServicePort{}

			for _, port := range d.Spec.Ports {
				tempPort := getTempPort(configData, fmt.Sprint(d.Name, "-", port))

				ports = append(ports, corev1.ServicePort{
					Name: fmt.Sprint(d.Name, "-", port),
					Port: port,
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

				cServices = append(cServices, configService{
					Id: fmt.Sprint(d.Name, "-", port),
					// Name:        d.Annotations["kloudlite.io/device-ip"],

					Name:        dIp.String(),
					ServicePort: port,
					ProxyPort:   tempPort,
				})
			}

			svcs = append(svcs, corev1.Service{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      d.Name,
					Namespace: "wg-" + req.Object.Name,
					Labels: map[string]string{
						"proxy-device-service": "true",
					},
					Annotations: map[string]string{
						"proxy-device-service": "true",
					},
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

		c, err := json.Marshal(map[string][]configService{
			"services": cServices,
		})

		if err != nil {
			return err
		}

		equal := false

		if configFetchError == nil {
			equal, err = functions.JSONStringsEqual(oldConfig.Data["config.json"], string(c))

			if err != nil {
				fmt.Println(err)
				return err
			}

			if len(cServices) == 0 {
				equal = true
			}
		}

		if !equal {
			isReady = false
			cs = append(cs,
				conditions.New(
					"DevicePorxyConfigMatching",
					false,
					"NotFound",
					"Devices are updated",
				),
			)
		}
		rApi.SetLocal(req, "device-proxy-config", cServices)
		rApi.SetLocal(req, "device-proxy-services", svcs)
		if err != nil {
			return err
		}
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// Generate WG Account Server Config
	if err := func() error {
		if accountServerKeys, ok := rApi.GetLocal[*corev1.Secret](req, "accountWgServerKeys"); ok {
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
			data.AccountWireguardPvtKey = string(accountServerKeys.Data["private-key"])

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

				// fmt.Println("\nChanged\nOLD----------------------------------------------------\n", string(existingConfig.Data["data"]), "\nNew\n-----------------------------------------\n", string(parse))

				isReady = false
				cs = append(cs,
					conditions.New(
						"WGServerConfigMatching",
						false,
						"NotMatching",
						"WG Server Config not matching "+"\nChanged\nOLD----------------------------------------------------\n"+string(existingConfig.Data["data"])+"\nNew\n-----------------------------------------\n"+string(parse),
					),
				)
			}
		}
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// fetching regions
	if err := func() error {

		var regions managementv1.RegionList

		err := r.List(req.Context(), &regions)
		if err != nil || len(regions.Items) == 0 {
			if !apiErrors.IsNotFound(err) {
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

		rApi.SetLocal(req, "regions", regions)

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check wg deployed or not
	if err := func() error {

		if meta.IsStatusConditionFalse(cs, "RegionsFound") {
			return nil
		}

		regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")

		if !ok {
			return fmt.Errorf("CAN'T FETCH REGIONS")
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

		// svc, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
		// 	Namespace: "wg-" + req.Object.Name,
		// 	Name:      "wireguard-service",
		// }, &corev1.Service{})

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

	if !hasUpdated {
		return req.Next()
	}

	req.Object.Status.Conditions = newConditions
	req.Object.Status.IsReady = isReady
	if err := r.Status().Update(req.Context(), req.Object); err != nil {
		return req.FailWithStatusError(err)
	}

	return req.Done()
}

func (r *AccountReconciler) reconcileOperations(req *rApi.Request[*managementv1.Account]) rApi.StepResult {

	if err := func() error {

		var regions managementv1.RegionList

		err := r.List(req.Context(), &regions)
		if err != nil {
			return err
		}

		for _, region := range regions.Items {

			wgDomain, ok := rApi.GetLocal[string](req, "wg-domain")

			if !ok {
				return fmt.Errorf("CAN'T WG DOMAINS")
			}

			ipsAny, ok := region.Status.DisplayVars.Get("kloudlite.io/node-ips")

			if !ok {
				continue
			}

			err = functions.KubectlApply(req.Context(), r.Client, &managementv1.Domain{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "management.kloudlite.io/v1",
					Kind:       "Domain",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("wg-%s-%s", region.Name, req.Object.Name),
					Labels: map[string]string{
						"kloudlite.io/wg-domain":         wgDomain,
						"kloudlite.io/wg-region":         region.Name,
						"kloudlite.io/wg-domain-account": req.Object.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						functions.AsOwner(req.Object),
					},
				},
				Spec: managementv1.DomainSpec{
					Name: fmt.Sprintf("%s.%s.wg.dev.kloudlite.io", region.Name, wgDomain),
					Ips: func() []string {

						// fmt.Println(ipsAny)
						ips := []string{}
						for _, ip := range ipsAny.([]interface{}) {
							ips = append(ips, ip.(string))
						}
						return ips

					}(),
				},
			})

			if err != nil {
				fmt.Println(err)
				return err
			}
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	wgNotReady := func() bool {
		return meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGNamespaceNotFound") ||
			meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGNodePortNotReady") ||
			meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGServiceFound") ||
			meta.IsStatusConditionFalse(req.Object.Status.Conditions, "DNSServerReady") ||
			meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGDeploymentExists")
	}()

	// creating wireguard deployment service and namespace
	if wgNotReady {
		var regions managementv1.RegionList

		err := r.Client.List(req.Context(), &regions)
		if err != nil {
			return req.FailWithOpError(err)
		}

		for _, region := range regions.Items {

			// fmt.Println(".................................")
			corednsConfigExists := true
			deviceProxyConfigExists := true
			if _, err := rApi.Get(req.Context(), r.Client, functions.NN("wg-"+req.Object.Name, "coredns"), &corev1.ConfigMap{}); err != nil {
				corednsConfigExists = false
			}

			if _, err := rApi.Get(req.Context(), r.Client, functions.NN("wg-"+req.Object.Name, "device-proxy-config"), &corev1.ConfigMap{}); err != nil {
				corednsConfigExists = false
			}

			b, err := templates.Parse(templates.WireGuard, map[string]any{
				"obj":                        req.Object,
				"owner-refs":                 functions.AsOwner(req.Object, true),
				"region-owner-refs":          functions.AsOwner(&region),
				"region":                     region.Name,
				"coredns-config-exists":      corednsConfigExists,
				"device-proxy-config-exists": deviceProxyConfigExists,
			})

			// fmt.Printf("template: %s\n", string(b))

			if err != nil {
				return req.FailWithOpError(err)
			}

			// fmt.Println(string(b))

			_, err = functions.KubectlApplyExec(b)

			// fmt.Println(o.String())

			if err != nil {
				return req.FailWithOpError(err)
			}

		}

		req.Done(&ctrl.Result{Requeue: true})
	}

	// generating wireguard server config
	if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGSecretNotFound") {
		pub, priv, err := wireguard.GenerateWgKeys()

		if err != nil {
			fmt.Println("kk", err)
			return req.FailWithOpError(err)
		}

		err = functions.KubectlApply(req.Context(), r.Client,
			functions.ParseSecret(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-keys",
					Namespace: "wg-" + req.Object.Name,
					OwnerReferences: []metav1.OwnerReference{
						functions.AsOwner(req.Object, true),
					},
				},
				Data: map[string][]byte{
					"private-key": []byte(priv),
					"public-key":  []byte(pub),
				},
			}))

		if err != nil {
			fmt.Println("err", err)
			return req.FailWithOpError(err)
		}

	}

	// storing wg server config into secret
	if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGServerConfigExists") {
		serverConfig, ok := rApi.GetLocal[string](req, "serverWgConfig")
		if !ok {
			return req.FailWithOpError(errors.New("serverWgConfig not found"))
		}
		err := r.Client.Create(req.Context(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wg-server-config",
				Namespace: "wg-" + req.Object.Name,
				OwnerReferences: []metav1.OwnerReference{
					functions.AsOwner(req.Object),
				},
			},
			Data: map[string][]byte{
				"data": []byte(serverConfig),
			},
		})
		if err != nil {
			return req.FailWithOpError(err)
		}
	}

	// updating wireguard server config
	if err := func() error {

		if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGServerConfigMatching") {
			serverConfig, ok := rApi.GetLocal[string](req, "serverWgConfig")
			if !ok {
				return errors.New("serverWgConfig not found")
			}
			err := r.Client.Update(req.Context(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-config",
					Namespace: "wg-" + req.Object.Name,
				},
				Data: map[string][]byte{
					"data": []byte(serverConfig),
				},
			})
			if err != nil {
				return err
			}
			// TODO don't restart
			functions.Kubectl("-n", fmt.Sprintf("wg-%s", req.Object.Name), "rollout", "restart", "deployments", "-l", "wireguard-deployment=true")
			// Ignoring error of rollout
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// updating device proxy config and services
	if err := func() error {
		if !rApi.HasLocal(req, "device-proxy-config") || !rApi.HasLocal(req, "device-proxy-services") {
			return fmt.Errorf("DeviceConfig & ServiceConfig Not found2")
		}

		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "DevicePorxyConfigMatching") {
			return nil
		}

		configs, ok := rApi.GetLocal[[]configService](req, "device-proxy-config")

		if !ok {
			return fmt.Errorf("failed to fetch device-proxy-config")
		}

		services, ok := rApi.GetLocal[[]corev1.Service](req, "device-proxy-services")

		if !ok {
			return fmt.Errorf("failed to fetch device-proxy-services")
		}

		// for _, c := range configs {
		// 	fmt.Println(c.Id)
		// }

		// fmt.Printf("config,services: %+v\n%+v\n", configs, services)
		b, err := templates.Parse(templates.ProxyDevice, map[string]any{
			"services": services,
			"configmap": map[string][]configService{
				"services": configs,
			},
			"namespace": "wg-" + req.Object.Name,
			"account-refs": []metav1.OwnerReference{
				functions.AsOwner(req.Object),
			},
		})

		if err != nil {
			fmt.Println(err)
			return err
		}

		// fmt.Println(string(b))
		_, err = functions.KubectlApplyExec(b)

		if err != nil {
			fmt.Println("hree:", err)
			return err
		}

		configJson, err := json.Marshal(map[string][]configService{
			"services": configs,
		})

		if err != nil {
			fmt.Println(err)
			return err
		}

		regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
		if !ok {
			return errors.New("Regions not found")
		}

		var updateError error
		for _, region := range regions.Items {
			_, err = http.Post(fmt.Sprintf("http://proxy-service-%s.wg-%s.svc.cluster.local/post", region.Name, req.Object.Name), "application/json", bytes.NewBuffer(configJson))
			// fmt.Println(resp)
			if err != nil {
				fmt.Println(region.Name, ":", err)
				updateError = err
			}
			// fmt.Println(fmt.Sprintf("proxy-service-%s.wg-%s/post", region.Name, req.Object.Name))

		}

		if updateError != nil {
			return err
		}
		// regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
		// if !ok {
		// 	return errors.New("Regions not found")
		// }

		// var restartError error
		// for _, region := range regions.Items {
		// functions.Kubectl("-n", fmt.Sprintf("wg-%s", req.Object.Name), "rollout", "restart", fmt.Sprintf("deployment/%v", "wireguard-deployment"))

		// _, err = functions.Kubectl("-n", fmt.Sprintf("wg-%s", req.Object.Name), "rollout", "restart", fmt.Sprintf("deployment/%v-%s-k", "wireguard-deployment", region.Name))

		// if err != nil {
		// 	fmt.Println(err)
		// 	restartError = err
		// }

		// if restartError != nil {
		// 	return restartError
		// }

		// }

		return nil
	}(); err != nil {
		return rApi.NewStepResult(&ctrl.Result{
			Requeue:      true,
			RequeueAfter: 5,
		}, err)
	}

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Account{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Watches(&source.Kind{Type: &managementv1.Device{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			if object.GetLabels() == nil {
				return nil
			}
			account, ok := object.GetLabels()["kloudlite.io/account-id"]
			if !ok {
				return nil
			}
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: account,
					},
				},
			}
		})).
		Watches(&source.Kind{Type: &managementv1.Region{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			if object.GetLabels() == nil {
				return nil
			}
			var accounts managementv1.AccountList

			err := r.Client.List(context.TODO(), &accounts, &client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{}),
			})

			if err != nil {
				return nil
			}

			results := []reconcile.Request{}
			for _, account := range accounts.Items {
				results = append(results, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: account.Name,
					},
				})
			}

			return results
		})).
		Complete(r)
}
