package commoncontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/kloudlite/internal_operator_v2/env"
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	"github.com/kloudlite/internal_operator_v2/lib/functions"
	"github.com/kloudlite/internal_operator_v2/lib/logging"
	stepResult "github.com/kloudlite/internal_operator_v2/lib/operator.v2/step-result"
	"github.com/kloudlite/internal_operator_v2/lib/templates"
	"github.com/kloudlite/internal_operator_v2/lib/wireguard"

	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	"github.com/seancfoley/ipaddress-go/ipaddr"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	managementv1 "github.com/kloudlite/internal_operator_v2/apis/management/v1"
)

/*
# actions
	- generate service of own
	- watch config
	- generate wg-keys
	- generate wg-config for diffrent regions
*/

// DeviceReconciler reconciles a Device object
type DeviceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Env    *env.Env
	logger logging.Logger
	Name   string
}

func (r *DeviceReconciler) GetName() string {
	return r.Name
}

const (
	WgKeysReady           string = "wireguard-keys-ready"
	ServicesSynced        string = "services-syncd"
	DnsRewriteRulesSynced string = "dns-rewrite-rules-syncd"
	deviceConfigSynced    string = "device-config-syncd"
)

// +kubebuilder:rbac:groups=management.kloudlite.io,resources=devices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=devices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=devices/finalizers,verbs=update

func (r *DeviceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	req, err := rApi.NewRequest(context.WithValue(ctx,constants.LoggerConst, r.logger), r.Client, request.NamespacedName, &managementv1.Device{})
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if step := req.EnsureChecks(
		WgKeysReady,
		ServicesSynced,
		DnsRewriteRulesSynced,
		deviceConfigSynced,
	); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	// if req.Object.Name != "kl-core" {
	// 	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
	// }

	if req.Object.GetDeletionTimestamp() != nil {
		if x := r.finalize(req); !x.ShouldProceed() {
			return x.ReconcilerResponse()
		}
		return ctrl.Result{}, nil
	}

	req.Logger.Infof("NEW RECONCILATION")

	if step := req.ClearStatusIfAnnotated(); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := req.RestartIfAnnotated(); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := req.EnsureLabelsAndAnnotations(); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := req.EnsureFinalizers(constants.ForegroundFinalizer, constants.CommonFinalizer); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.fetchRequired(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconWgKeys(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconSyncServices(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconDnsRewriteRules(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconDeviceConfig(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *DeviceReconciler) fetchRequired(req *rApi.Request[*managementv1.Device]) stepResult.Result {

	ctx, obj := req.Context(), req.Object
	namespace := "wg-" + obj.Spec.AccountId

	// fetching regions
	if err := func() error {

		var regions managementv1.RegionList

		err := r.List(ctx, &regions)
		if err != nil || len(regions.Items) == 0 {
			if !apiErrors.IsNotFound(err) {
				return err
			}

			return fmt.Errorf("regions not found")
		}

		rApi.SetLocal(req, "regions", regions)

		return nil
	}(); err != nil {
		r.logger.Debugf(err.Error())
	}

	// fetching dns config
	if err := func() error {

		var CheckErr error
		dnsConf, err := rApi.Get(
			ctx, r.Client, types.NamespacedName{
				Namespace: namespace,
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
			ctx, r.Client, types.NamespacedName{
				Namespace: namespace,
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

			return fmt.Errorf("dns config not found")
		}

		return nil
	}(); err != nil {
		req.Logger.Debugf(err.Error())
	}

	return req.Next()
}

func (r *DeviceReconciler) updateServices(req *rApi.Request[*managementv1.Device]) error {

	ctx, obj := req.Context(), req.Object

	config, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Namespace: "wg-" + obj.Spec.AccountId,
			Name:      "device-proxy-config",
		}, &corev1.ConfigMap{},
	)

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return err
		}

		return fmt.Errorf("proxy config not generated yet")

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
	for _, v := range obj.Spec.Ports {

		proxyPort, err := getPort(configData, fmt.Sprint(obj.Name, "-", v.Port))
		if err != nil {
			return err
		}

		sPorts = append(
			sPorts, corev1.ServicePort{
				Name: fmt.Sprint(obj.Name, "-", v.Port),
				Port: v.Port,
				TargetPort: intstr.IntOrString{
					Type:   0,
					IntVal: proxyPort,
				},
			},
		)
	}

	deviceServices := corev1.Service{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      obj.Name,
			Namespace: "wg-" + obj.Spec.AccountId,
			Labels: map[string]string{
				"proxy-device-service":    "true",
				"kloudlite.io/device-ref": obj.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				functions.AsOwner(obj, true),
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
				"region": obj.Spec.ActiveRegion,
			},
		},
	}

	out, err := templates.Parse(templates.ProxyService, deviceServices)
	if err != nil {
		return err
	}

	// fmt.Println(string(b))
	_, err = functions.KubectlApplyExec(out)
	if err != nil {
		return err
	}

	return fmt.Errorf("services being updated")
}

func (r *DeviceReconciler) reconSyncServices(req *rApi.Request[*managementv1.Device]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(ServicesSynced, check, err.Error())
	}

	service, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Namespace: "wg-" + obj.Spec.AccountId,
			Name:      obj.Name,
		}, &corev1.Service{},
	)
	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}

		// update services
		if err := r.updateServices(req); err != nil {
			return failed(err)
		}
	}

	if service.Spec.Selector["region"] != obj.Spec.ActiveRegion || checkPortsDiffer(service.Spec.Ports, obj.Spec.Ports) {

		// update services
		if err := r.updateServices(req); err != nil {
			return failed(err)
		}

	}
	// check if region updated

	check.Status = true
	if check != checks[ServicesSynced] {
		checks[ServicesSynced] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *DeviceReconciler) reconWgKeys(req *rApi.Request[*managementv1.Device]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	namespace := fmt.Sprintf("wg-%s", obj.Spec.AccountId)
	name := fmt.Sprintf("wg-device-keys-%s", obj.GetName())

	failed := func(err error) stepResult.Result {
		return req.CheckFailed(WgKeysReady, check, err.Error())
	}

	secret, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, &corev1.Secret{},
	)
	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}

		// generate keys and create as secret here
		pub, pv, err := wireguard.GenerateWgKeys()
		if err != nil {
			return req.FailWithOpError(err)
		}

		ip, err := getRemoteDeviceIp(int64(obj.Spec.Offset))
		if err != nil {
			fmt.Println(err)
			return req.FailWithOpError(err)
		}

		if err = functions.KubectlApply(
			ctx, r.Client,
			functions.ParseSecret(
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "wg-" + obj.Spec.AccountId,
						Name:      fmt.Sprintf("wg-device-keys-%s", obj.Name),
						Labels: map[string]string{
							"kloudlite.io/is-wg-key":  "true",
							"kloudlite.io/device-ref": obj.Name,
						},
						OwnerReferences: []metav1.OwnerReference{
							functions.AsOwner(obj, true),
						},
					},
					Data: map[string][]byte{
						"private-key": []byte(pv),
						"public-key":  []byte(pub),
						"ip":          []byte(ip.String()),
					},
				},
			),
		); err != nil {
			return failed(err)
		}

		return failed(fmt.Errorf("generating wg keys"))
	}

	rApi.SetLocal(req, "device-ip", string(secret.Data["ip"]))
	rApi.SetLocal(req, "device-publickey", string(secret.Data["public-key"]))
	rApi.SetLocal(req, "device-privatekey", string(secret.Data["private-key"]))

	check.Status = true
	if check != checks[WgKeysReady] {
		checks[WgKeysReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *DeviceReconciler) reconDeviceConfig(req *rApi.Request[*managementv1.Device]) stepResult.Result {

	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	failed := func(err error) stepResult.Result {
		return req.CheckFailed(deviceConfigSynced, check, err.Error())
	}

	if !rApi.HasLocal(req, "device-ip") || !rApi.HasLocal(req, "device-privatekey") || !rApi.HasLocal(req, "device-publickey") {
		return failed(fmt.Errorf("all configs not found"))
	}

	dnsIp, ok := rApi.GetLocal[string](req, "dns-ip")
	if !ok {
		return failed(fmt.Errorf("can't get dns"))
	}

	regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")

	if !ok {
		return failed(fmt.Errorf("regions not found"))
	}

	currentConfig, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Namespace: "wg-" + obj.Spec.AccountId,
			Name:      fmt.Sprintf("wg-device-config-%s", obj.Name),
		}, &corev1.Secret{},
	)

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}

		r.logger.Debugf("wg config not found")
	} else {
		rApi.SetLocal(req, "WGConfig", currentConfig)
	}

	privateKey, ok := rApi.GetLocal[string](req, "device-privatekey")
	if !ok {
		return failed(fmt.Errorf("PRIVATE KEY NOT FOUND"))
	}

	deviceIp, ok := rApi.GetLocal[string](req, "device-ip")
	if !ok {
		return failed(fmt.Errorf("DEVICE IP NOT FOUND"))
	}

	account, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Name: obj.Spec.AccountId,
		}, &managementv1.Account{},
	)

	if err != nil {
		return failed(err)
	}

	wgPublicKey, ok := account.Status.DisplayVars.GetString("wg-public-key")

	if !ok {
		return failed(fmt.Errorf("wg public key not available"))
	}

	// wgDomain, ok := account.Status.GeneratedVars.GetString("wg-domain")

	// if !ok {
	// 	return fmt.Errorf("wg domain not available")
	// }

	var accountServerConfigs []struct {
		Region    string
		Endpoint  string
		PublicKey string
	}

	// if wgDomain == "" {
	// 	return fmt.Errorf(("CAN'T find WG_DOMAIN in environment"))
	// }

	for _, region := range regions.Items {

		wgNodePort, ok := account.Status.DisplayVars.GetString("wg-nodeport-" + region.Name)

		if !ok {
			r.logger.Debugf("nodeport not found")
			wgNodePort = "nodeport_not_available"
			continue
		}

		// ns := nameserver.NewClient(r.Env.NameserverEndpoint, r.Env.NameserverUser, r.Env.NameserverPassword)

		// res, e := ns.GetRegionDomain(region.Spec.AccountId, region.Name)

		// if e != nil {
		// 	r.logger.Infof(e.Error())
		// 	continue
		// }

		// regionDomain, e := ioutil.ReadAll(res.Body)
		// if e != nil {
		// 	r.logger.Warnf(e.Error())
		// 	continue
		// }
		//
		// if res.StatusCode != 200 {
		// 	r.logger.Warnf(string(regionDomain))
		// }

		accountServerConfigs = append(
			accountServerConfigs, struct {
				Region    string
				Endpoint  string
				PublicKey string
			}{
				Region: region.Name,
				Endpoint: func() string {
					if region.Spec.IsMaster {
						return fmt.Sprintf("%s.wg.khost.dev:%s", obj.Name, wgNodePort)
					} else {
						return fmt.Sprintf("%s.%s.wg.khost.dev:%s", region.Name, region.Spec.AccountId, wgNodePort)
					}
					// if res.StatusCode == 200 {
					// 	return fmt.Sprintf("%s:%s", string(regionDomain), wgNodePort)
					// }
					// return "endpoint_not_found"
				}(),
				PublicKey: wgPublicKey,
			},
		)
	}

	wConfigs := map[string][]byte{}

	for _, asc := range accountServerConfigs {
		out, errr := templates.Parse(
			templates.WireGuardDeviceConfig, struct {
				DeviceIp        string
				DevicePvtKey    string
				ServerPublicKey string
				ServerEndpoint  string
				RewriteRules    string
				PodCidr         string
				SvcCidr         string
			}{
				DeviceIp:        deviceIp,
				DevicePvtKey:    privateKey,
				ServerPublicKey: asc.PublicKey,
				ServerEndpoint:  asc.Endpoint,
				RewriteRules:    dnsIp,
				PodCidr:         r.Env.PodCidr,
				SvcCidr:         r.Env.SvcCidr,
			},
		)

		// fmt.Println(string(b))

		if errr != nil {
			fmt.Println("Error with templating WG Config", errr)
			continue
		}

		wConfigs["config-"+asc.Region] = out

	}

	if err = functions.KubectlApply(
		ctx, r.Client, functions.ParseSecret(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("wg-device-config-%s", obj.Name),
					Namespace: "wg-" + obj.Spec.AccountId,
					Labels: map[string]string{
						"kloudlite.io/wg-device-config": "true",
						"kloudlite.io/account-ref":      obj.Spec.AccountId,
						"kloudlite.io/device-ref":       obj.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						functions.AsOwner(obj, true),
					},
				},
				Data:       wConfigs,
				StringData: map[string]string{},
			},
		),
	); err != nil {
		return failed(err)
	}

	check.Status = true
	if check != checks[deviceConfigSynced] {
		checks[deviceConfigSynced] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *DeviceReconciler) reconDnsRewriteRules(req *rApi.Request[*managementv1.Device]) stepResult.Result {

	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	failed := func(err error) stepResult.Result {
		return req.CheckFailed(DnsRewriteRulesSynced, check, err.Error())
	}

	dnsDevices, ok := rApi.GetLocal[string](req, "dns-devices")
	if !ok {
		dnsDevices = "[]"
	}

	var devices managementv1.DeviceList
	err := r.List(
		ctx, &devices,
		&client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				apiLabels.Set{
					"kloudlite.io/account-ref": obj.Spec.AccountId,
				},
			),
		},
	)
	if err != nil || len(devices.Items) == 0 {
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}
		return failed(fmt.Errorf("device not found"))
	}
	rApi.SetLocal(req, "devices", devices)
	d := []string{}

	for _, device := range devices.Items {
		d = append(d, device.Name)
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
		if err := func() error {

			kubeDns, err := rApi.Get(ctx, r.Client, types.NamespacedName{
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
				d = append(d, device.Name)
				if device.Name == "" {
					continue
				}
				rewriteRules += fmt.Sprintf(
					"rewrite name %s.%s %s.wg-%s.svc.cluster.local\n        ",
					device.Name,
					"kl.local",
					device.Name,
					device.Spec.AccountId,
				)
			}

			account, err := rApi.Get(
				ctx, r.Client, types.NamespacedName{
					Name: obj.Spec.AccountId,
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
			_, err = functions.Kubectl("-n", fmt.Sprintf("wg-%s", obj.Spec.AccountId), "rollout", "restart", "deployment/coredns")

			if err != nil {
				return err
			}

			return nil
		}(); err != nil {
			return failed(err)
		}
	}

	check.Status = true
	if check != checks[DnsRewriteRulesSynced] {
		checks[DnsRewriteRulesSynced] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *DeviceReconciler) finalize(req *rApi.Request[*managementv1.Device]) stepResult.Result {
	return req.Finalize()
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

func checkPortsDiffer(target []corev1.ServicePort, source []managementv1.Port) bool {
	if len(target) != len(source) {
		return true
	}

	m := make(map[int32]int32, len(source))
	for i := range source {
		m[source[i].Port] = source[i].TargetPort
	}

	for i := range target {
		if target[i].Port != source[i].Port {
			return true
		}
	}

	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeviceReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.logger = logger.WithName(r.Name)

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Device{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{})

	builder.
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
		)

	return builder.Complete(r)
}
