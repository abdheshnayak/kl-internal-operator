package commoncontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/templates"

	"github.com/seancfoley/ipaddress-go/ipaddr"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"operators.kloudlite.io/lib/conditions"
	"operators.kloudlite.io/lib/functions"
	rApi "operators.kloudlite.io/lib/operator"
	"operators.kloudlite.io/lib/wireguard"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	managementv1 "operators.kloudlite.io/apis/management/v1"
)

// DeviceReconciler reconciles a Device object
type DeviceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// generate service of own
// watch config
// generate wg-keys
// generate wg-config for diffrent regions

// +kubebuilder:rbac:groups=management.kloudlite.io,resources=devices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=devices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=devices/finalizers,verbs=update

func (r *DeviceReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &managementv1.Device{})
	if req == nil {
		return ctrl.Result{}, nil
	}

	if req.Object.GetDeletionTimestamp() != nil {
		if x := r.finalize(req); !x.ShouldProceed() {
			return x.Result(), x.Err()
		}
	}

	if x := req.EnsureFinilizer(constants.CommonFinalizer); !x.ShouldProceed() {
		fmt.Println("EnsureFinilizer", x.Err())
		return x.Result(), x.Err()
	}

	req.Logger.Info("-------------------- NEW RECONCILATION------------------")

	if x := req.EnsureLabels(); !x.ShouldProceed() {
		return x.Result(), x.Err()
	}

	if x := r.reconcileStatus(req); !x.ShouldProceed() {
		return x.Result(), x.Err()
	}

	if x := r.reconcileOperations(req); !x.ShouldProceed() {
		return x.Result(), x.Err()
	}

	return ctrl.Result{}, nil

}

func (r *DeviceReconciler) finalize(req *rApi.Request[*managementv1.Device]) rApi.StepResult {
	return req.Finalize()
}

func (r *DeviceReconciler) reconcileStatus(req *rApi.Request[*managementv1.Device]) rApi.StepResult {

	fmt.Println("reconcileStatus")

	var cs []metav1.Condition
	isReady := true
	retry := false

	// check if wg keys generated
	if err := func() error {
		secret, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Namespace: "wg-" + req.Object.Spec.Account,
				Name:      fmt.Sprintf("wg-device-keys-%s", req.Object.GetName()),
			}, &corev1.Secret{},
		)

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false

			cs = append(
				cs, conditions.New(
					"WGKeysGenerated",
					false,
					"NotFound",
					"WG keys not generated",
				),
			)
			return nil
		}

		rApi.SetLocal(req, "device-ip", string(secret.Data["ip"]))
		rApi.SetLocal(req, "device-publickey", string(secret.Data["public-key"]))
		rApi.SetLocal(req, "device-privatekey", string(secret.Data["private-key"]))

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

			cs = append(
				cs,
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

	// fetching domains
	if err := func() error {

		var domains managementv1.DomainList

		err := r.List(req.Context(), &domains)
		if err != nil || len(domains.Items) == 0 {
			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false

			cs = append(
				cs,
				conditions.New(
					"WGServerDomainsFound",
					false,
					"NotFound",
					"WG Server domains Not found",
				),
			)
			return nil
		}

		rApi.SetLocal(req, "wg-domains", domains)

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	if err := func() error {

		var CheckErr error
		dnsConf, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Namespace: "wg-" + req.Object.Spec.Account,
				Name:      "coredns",
			}, &corev1.ConfigMap{},
		)

		if err != nil {
			CheckErr = err
		}

		if err == nil {
			rApi.SetLocal(req, "dns-devices", string(dnsConf.Data["devices"]))
		}

		dnsSvc, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Namespace: "wg-" + req.Object.Spec.Account,
				Name:      "coredns",
			}, &corev1.Service{},
		)

		if err != nil {
			CheckErr = err
		}

		if err == nil {
			rApi.SetLocal(req, "dns-ip", dnsSvc.Spec.ClusterIP)
		}

		if CheckErr != nil {
			fmt.Println(CheckErr)

			if !apiErrors.IsNotFound(CheckErr) {
				return CheckErr
			}

			isReady = false
			cs = append(
				cs,
				conditions.New(
					"DNSServerReady",
					false,
					"NotFound",
					"DNS server is not ready",
				),
			)

			return nil
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check if anythin update in service
	if err := func() error {
		service, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Namespace: "wg-" + req.Object.Spec.Account,
				Name:      req.Object.Name,
			}, &corev1.Service{},
		)
		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false
			cs = append(
				cs,
				conditions.New(
					"DeviceServiceChanged",
					false,
					"Updted",
					"Device Service is not created yet",
				),
			)

			return nil
		}

		var region string

		if service.Spec.Selector != nil && service.Spec.Selector["region"] != "" {
			region = service.Spec.Selector["region"]
			if region != req.Object.Spec.ActiveRegion {
				isReady = false
				cs = append(
					cs,
					conditions.New(
						"DeviceServiceChanged",
						false,
						"Updted",
						"Device Service is not created yet",
					),
				)

				return nil
			}
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// if region updated update service
	if err := func() error {

		if !meta.IsStatusConditionFalse(cs, "DeviceServiceChanged") {
			return nil
		}

		accountId := req.Object.Labels["kloudlite.io/account-ref"]

		config, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Namespace: "wg-" + accountId,
				Name:      "device-proxy-config",
			}, &corev1.ConfigMap{},
		)

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false
			cs = append(
				cs,
				conditions.New(
					"ProxyConfigGenerated",
					false,
					"NotFound",
					"Proxy Config not created yet",
				),
			)

			return nil
		}

		type configService struct {
			Id          string `json:"id"`
			Name        string `json:"name"`
			ServicePort int32  `json:"servicePort"`
			ProxyPort   int32  `json:"proxyPort"`
		}

		configData := []configService{}

		oConfMap := map[string][]configService{}
		err = json.Unmarshal([]byte(config.Data["config.json"]), &oConfMap)

		if oConfMap["services"] != nil {
			configData = oConfMap["services"]
		}
		if err != nil {
			return err
		}

		// method to check either the port exists int the config
		getPort := func(svce []configService, id string) (int32, error) {
			for _, s := range svce {
				if s.Id == id {
					return s.ProxyPort, nil
				}
			}
			return 0, errors.New("proxy port not found in proxy config")
		}

		sPorts := []corev1.ServicePort{}
		for _, v := range req.Object.Spec.Ports {

			proxyPort, err := getPort(configData, fmt.Sprint(req.Object.Name, "-", v.Port))
			if err != nil {
				return err
			}

			sPorts = append(
				sPorts, corev1.ServicePort{
					Name: fmt.Sprint(req.Object.Name, "-", v.Port),
					Port: v.Port,
					TargetPort: intstr.IntOrString{
						Type:   0,
						IntVal: proxyPort,
					},
				},
			)
		}

		svc := corev1.Service{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      req.Object.Name,
				Namespace: "wg-" + accountId,
				Labels: map[string]string{
					"proxy-device-service":    "true",
					"kloudlite.io/device-ref": req.Object.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					functions.AsOwner(req.Object),
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: func() []corev1.ServicePort {
					if len(sPorts) == 0 {
						sPorts = append(
							sPorts, corev1.ServicePort{
								Name: "temp",
								Port: 80,
								TargetPort: intstr.IntOrString{
									Type:   0,
									IntVal: 0,
								},
							},
						)
					}
					return sPorts
				}(),
				Selector: map[string]string{
					"region": req.Object.Spec.ActiveRegion,
				},
			},
		}

		rApi.SetLocal(req, "device-service", svc)

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// checking dns rewrite-rules changed
	if err := func() error {
		dnsDevices, ok := rApi.GetLocal[string](req, "dns-devices")
		if !ok {
			dnsDevices = "[]"
		}
		var devices managementv1.DeviceList
		err := r.List(
			req.Context(), &devices,
			&client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(
					apiLabels.Set{
						"kloudlite.io/account-ref": req.Object.Spec.Account,
					},
				),
			},
		)
		if err != nil || len(devices.Items) == 0 {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false

			cs = append(
				cs,
				conditions.New(
					"DeviceFound",
					false,
					"NotFound",
					"Devices Not found",
				),
			)
			return nil
		}
		rApi.SetLocal(req, "devices", devices)
		d := []string{}

		for _, device := range devices.Items {
			d = append(d, device.Spec.DeviceName)
		}

		sort.Strings(d)
		var oldDevices []string
		json.Unmarshal([]byte(dnsDevices), &oldDevices)
		sort.Strings(oldDevices)

		ok = func() bool {
			if len(oldDevices) != len(d) {
				return false
			}
			for i := 0; i < len(d); i++ {
				if d[i] != oldDevices[i] {
					return false
				}
			}
			return true
		}()

		if !ok {
			isReady = false
			cs = append(
				cs,
				conditions.New(
					"DevicesUpToDate",
					false,
					"DeviceChanged",
					"Devices has been updated",
				),
			)
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// checkDevice WGConfig Changed
	if err := func() error {
		if !rApi.HasLocal(req, "device-ip") || !rApi.HasLocal(req, "device-privatekey") || !rApi.HasLocal(req, "device-publickey") {
			fmt.Println("not found")
			return nil
		}

		dnsIp, ok := rApi.GetLocal[string](req, "dns-ip")
		if !ok {
			return fmt.Errorf("CAN'T GET DNS")
		}

		regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")

		if !ok {
			return fmt.Errorf("CAN'T FETCH REGIONS")
		}

		currentConfig, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Namespace: "wg-" + req.Object.Spec.Account,
				Name:      fmt.Sprintf("wg-device-config-%s", req.Object.Name),
			}, &corev1.Secret{},
		)

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false
			cs = append(
				cs,
				conditions.New(
					"WGConfigFound",
					false,
					"NotFound",
					"Wg config not found",
				),
			)

		} else {
			rApi.SetLocal(req, "WGConfig", currentConfig)
		}

		privateKey, ok := rApi.GetLocal[string](req, "device-privatekey")
		if !ok {
			return fmt.Errorf("PRIVATE KEY NOT FOUND")
		}

		deviceIp, ok := rApi.GetLocal[string](req, "device-ip")
		if !ok {
			return fmt.Errorf("DEVICE IP NOT FOUND")
		}

		account, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Name: req.Object.Spec.Account,
			}, &managementv1.Account{},
		)

		if err != nil {
			return err
		}

		wgPublicKey, ok := account.Status.DisplayVars.GetString("wg-public-key")

		if !ok {
			return fmt.Errorf("wg public key not available")
		}

		wgDomain, ok := account.Status.GeneratedVars.GetString("wg-domain")

		if !ok {
			return fmt.Errorf("wg domain not available")
		}

		var accountServerConfigs []struct {
			Region    string
			Endpoint  string
			PublicKey string
		}

		wgBaseDomain := os.Getenv("WG_DOMAIN")

		if wgDomain == "" {
			return fmt.Errorf(("CAN'T find WG_DOMAIN in environment"))
		}

		for _, region := range regions.Items {

			wgNodePort, ok := account.Status.DisplayVars.GetString("wg-nodeport-" + region.Name)

			if !ok {
				// fmt.Println(req.Object.Spec.Account, "wg-nodeport-"+region.Name)
				// fmt.Println("node port not available")
				continue
			}

			accountServerConfigs = append(
				accountServerConfigs, struct {
					Region    string
					Endpoint  string
					PublicKey string
				}{
					Region:    region.Name,
					Endpoint:  fmt.Sprintf("%s.%s.wg.%s:%s", region.Name, wgDomain, wgBaseDomain, wgNodePort),
					PublicKey: wgPublicKey,
				},
			)

		}

		wConfigs := map[string][]byte{}

		for _, asc := range accountServerConfigs {
			b, errr := templates.Parse(
				templates.WireGuardDeviceConfig, struct {
					DeviceIp        string
					DevicePvtKey    string
					ServerPublicKey string
					ServerEndpoint  string
					RewriteRules    string
				}{
					DeviceIp:        deviceIp,
					DevicePvtKey:    privateKey,
					ServerPublicKey: asc.PublicKey,
					ServerEndpoint:  asc.Endpoint,
					RewriteRules:    dnsIp,
				},
			)

			// fmt.Println(string(b))

			if errr != nil {
				fmt.Println("Error with templating WG Config", errr)
				continue
			}

			wConfigs["config-"+asc.Region] = b

		}

		if err = functions.KubectlApply(
			req.Context(), r.Client, functions.ParseSecret(
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("wg-device-config-%s", req.Object.Name),
						Namespace: "wg-" + req.Object.Spec.Account,
						Labels: map[string]string{
							"kloudlite.io/wg-device-config": "true",
							"kloudlite.io/account-ref":      req.Object.Spec.Account,
							"kloudlite.io/device-ref":       req.Object.Name,
						},
						OwnerReferences: []metav1.OwnerReference{
							functions.AsOwner(req.Object),
						},
					},
					Data:       wConfigs,
					StringData: map[string]string{},
				},
			),
		); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	newConditions, hasUpdated, err := conditions.Patch(req.Object.Status.Conditions, cs)

	if err != nil {
		return req.FailWithStatusError(err)
	}

	if !retry && !hasUpdated {
		return req.Next()
	}

	req.Object.Status.Conditions = newConditions

	req.Object.Status.IsReady = isReady

	if err := r.Status().Update(req.Context(), req.Object); err != nil {
		return req.FailWithStatusError(err)
	}

	if retry {
		return req.Done(&ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5})
	}

	return req.Done()

}

func getRemoteDeviceIp(deviceOffcet int64) (*ipaddr.IPAddressString, error) {
	deviceRange := ipaddr.NewIPAddressString("10.13.0.0/16")

	if address, addressError := deviceRange.ToAddress(); addressError == nil {
		increment := address.Increment(deviceOffcet + 2)
		return ipaddr.NewIPAddressString(increment.GetNetIP().String()), nil
	} else {
		return nil, addressError
	}
}

func (r *DeviceReconciler) reconcileOperations(req *rApi.Request[*managementv1.Device]) rApi.StepResult {

	if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "DevicesUpToDate") {

		if err := func() error {

			kubeDns, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
				Name:      "kube-dns",
				Namespace: "kube-system",
			}, &corev1.Service{})

			if err != nil {
				return err
			}

			devices, ok := rApi.GetLocal[managementv1.DeviceList](req, "devices")

			if !ok {
				return fmt.Errorf("devices not found")
			}

			rewriteRules := ""
			d := []string{}

			for _, device := range devices.Items {
				d = append(d, device.Spec.DeviceName)
				if device.Spec.DeviceName == "" {
					continue
				}
				rewriteRules += fmt.Sprintf(
					"rewrite name %s.%s %s.wg-%s.svc.cluster.local\n        ",
					device.Spec.DeviceName,
					"kl.local",
					device.Name,
					device.Spec.Account,
				)
			}

			account, err := rApi.Get(
				req.Context(), r.Client, types.NamespacedName{
					Name: req.Object.Spec.Account,
				}, &managementv1.Account{},
			)
			if err != nil {
				return err
			}

			parse, err := templates.Parse(
				templates.DNSConfig, map[string]any{
					"object":        account,
					"devices":       d,
					"rewrite-rules": rewriteRules,
					"dns-ip":        kubeDns.Spec.ClusterIP,
				},
			)

			if err != nil {
				return err
			}
			// fmt.Println(string(parse))

			_, err = functions.KubectlApplyExec(parse)

			if err != nil {
				return err
			}

			// fmt.Println(op)
			_, err = functions.Kubectl("-n", fmt.Sprintf("wg-%s", req.Object.Spec.Account), "rollout", "restart", "deployment/coredns")

			if err != nil {
				return err
			}

			return nil
		}(); err != nil {
			return req.FailWithOpError(err)
		}

	}

	if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGKeysGenerated") {

		pub, pv, err := wireguard.GenerateWgKeys()
		if err != nil {
			return req.FailWithOpError(err)
		}

		ip, err := getRemoteDeviceIp(int64(req.Object.Spec.Offset))
		if err != nil {
			fmt.Println(err)
			return req.FailWithOpError(err)
		}

		err = functions.KubectlApply(
			req.Context(), r.Client,
			functions.ParseSecret(
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "wg-" + req.Object.Spec.Account,
						Name:      fmt.Sprintf("wg-device-keys-%s", req.Object.Name),
						Labels: map[string]string{
							"kloudlite.io/is-wg-key":  "true",
							"kloudlite.io/device-ref": req.Object.Name,
						},
						OwnerReferences: []metav1.OwnerReference{
							functions.AsOwner(req.Object, true),
						},
					},
					Data: map[string][]byte{
						"private-key": []byte(pv),
						"public-key":  []byte(pub),
						"ip":          []byte(ip.String()),
					},
				},
			),
		)

		if err != nil {
			return req.FailWithOpError(err)
		}

	}

	if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "IpFoundInAnnotations") {

		ip, ok := rApi.GetLocal[string](req, "device-ip")
		pub, ok2 := rApi.GetLocal[string](req, "device-publickey")

		if !ok {
			return req.FailWithOpError(fmt.Errorf("CAN'T FIND DEVICE IP"))
		}

		if !ok2 {
			return req.FailWithOpError(fmt.Errorf("CAN'T FIND DEVICE PUBLICKEY"))
		}

		annotations := req.Object.GetAnnotations()
		if annotations == nil {
			req.Object.Annotations = make(map[string]string)
		}

		req.Object.ObjectMeta.Annotations["kloudlite.io/device-ip"] = ip
		req.Object.ObjectMeta.Annotations["kloudlite.io/device-publickey"] = pub

		err := r.Update(req.Context(), req.Object)
		if err != nil {
			return req.FailWithOpError(err)
		}

	}

	if err := func() error {

		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "DeviceServiceChanged") {
			return nil
		}

		deviceService, ok := rApi.GetLocal[corev1.Service](req, "device-service")
		if !ok {
			return errors.New("Can't find generated service")
		}

		b, err := templates.Parse(templates.ProxyService, deviceService)
		if err != nil {
			return err
		}
		// fmt.Println(string(b))
		_, err = functions.KubectlApplyExec(b)
		if err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeviceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Device{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Watches(
			&source.Kind{Type: &corev1.Service{}}, handler.EnqueueRequestsFromMapFunc(
				func(object client.Object) []reconcile.Request {
					if object.GetLabels() == nil {
						return nil
					}

					l := object.GetLabels()
					deviceId := l["kloudlite.io/device-ref"]
					if deviceId == "" {
						return nil
					}

					results := []reconcile.Request{}

					results = append(
						results, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name: deviceId,
							},
						},
					)

					return results
				},
			),
		).
		Complete(r)
}
