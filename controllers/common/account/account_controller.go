package account

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
	"github.com/kloudlite/internal_operator_v2/env"
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	"github.com/kloudlite/internal_operator_v2/lib/errors"
	"github.com/kloudlite/internal_operator_v2/lib/functions"
	"github.com/kloudlite/internal_operator_v2/lib/logging"
	stepResult "github.com/kloudlite/internal_operator_v2/lib/operator.v2/step-result"
	"github.com/kloudlite/internal_operator_v2/lib/templates"
	"github.com/kloudlite/internal_operator_v2/lib/wireguard"
	"github.com/seancfoley/ipaddress-go/ipaddr"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	managementv1 "github.com/kloudlite/internal_operator_v2/apis/management/v1"
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
	Name   string
	logger logging.Logger
	Scheme *runtime.Scheme

	Env *env.Env
}

func (r *AccountReconciler) GetName() string {
	return r.Name
}

type configService struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	ServicePort int32  `json:"servicePort"`
	ProxyPort   int32  `json:"proxyPort"`
}

const (
	ReconcilationPeriod time.Duration = 30
)

const (
	AccountNSReady = "namespace-ready"
	// AccountNSDeleted      = "namespace-deleted"
	AccountWGSKeysReady   = "wg-server-keys-ready"
	AccountWGSConfigReady = "wg-server-config-ready"
	DeviceProxyReady      = "device-proxy-ready"
	WGDeploysReady        = "wg-deployments-ready"
	CorednsDeployReady    = "coredns-deployment-ready"
	DomainReady           = "domain-ready"
	// WgDomainReady         = "wg-domain-ready"
)

// +kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts/finalizers,verbs=update

func (r *AccountReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	req, err := rApi.NewRequest(rApi.NewReconcilerCtx(ctx, r.logger), r.Client, request.NamespacedName, &managementv1.Account{})
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if step := req.EnsureChecks(
		AccountNSReady,
		// AccountNSDeleted,
		AccountWGSKeysReady,
		AccountWGSConfigReady,
		DeviceProxyReady,
		WGDeploysReady,
		CorednsDeployReady,
		// DomainReady,
		// WgDomainReady,
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

	if step := r.reconNamespace(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.fetchRequired(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	// if step := r.reconWGDomains(req); !step.ShouldProceed() {
	// 	return step.ReconcilerResponse()
	// }

	if step := r.reconWGServerKey(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconWGServerConfig(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconDevProxyConfig(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconDeployments(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	// if step := r.reconDomain(req); !step.ShouldProceed() {
	// 	return step.ReconcilerResponse()
	// }

	if step := r.reconCoredns(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *AccountReconciler) reconNamespace(req *rApi.Request[*managementv1.Account]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	failed := func(err error) stepResult.Result {
		return req.CheckFailed(AccountNSReady, check, err.Error())
	}

	if _, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Name: "wg-" + obj.Name,
		}, &corev1.Namespace{},
	); err != nil {
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}

		if err = r.Client.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("wg-%s", req.Object.Name),
				Labels: apiLabels.Set{
					constants.AccountNameKey: req.Object.Name,
				},
				OwnerReferences: []metav1.OwnerReference{functions.AsOwner(obj, true)},
			},
		}); err != nil {
			return failed(err)
		}
		return failed(fmt.Errorf("namespacef for account is scheduled to create"))
	}

	check.Status = true
	if check != checks[AccountNSReady] {
		checks[AccountNSReady] = check
		req.UpdateStatus()
	}

	return req.Next()
}

func (r *AccountReconciler) fetchRequired(req *rApi.Request[*managementv1.Account]) stepResult.Result {

	ctx, obj := req.Context(), req.Object

	// fetching regions
	if err := func() error {
		var regions managementv1.RegionList
		var klRegions managementv1.RegionList
		if err := r.List(
			ctx, &regions, &client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(
					apiLabels.Set{
						constants.AccountNameKey: req.Object.Name,
					},
				),
			},
		); err != nil {
			req.Logger.Warnf(err.Error())
		}

		if err := r.List(
			ctx, &klRegions, &client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(
					apiLabels.Set{
						constants.AccountNameKey: "kl-core",
					},
				),
			},
		); err != nil {
			req.Logger.Warnf(err.Error())
		}

		if masterRegion, err := rApi.Get(
			ctx, r.Client, types.NamespacedName{
				Name: "master",
			}, &managementv1.Region{},
		); masterRegion != nil && err == nil {
			regions.Items = append(regions.Items, *masterRegion)
		}

		regions.Items = append(regions.Items, klRegions.Items...)

		// fmt.Println(regions.Items,"here...................")
		if len(regions.Items) == 0 {
			return fmt.Errorf("no regions found")
		}

		rApi.SetLocal(req, "regions", regions)

		return nil
	}(); err != nil {
		r.logger.Warnf(err.Error())
	}

	// fetching devices
	if err := func() error {
		var devices managementv1.DeviceList
		if err := r.List(
			req.Context(), &devices,
			&client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(
					apiLabels.Set{
						constants.AccountNameKey: obj.Name,
					},
				),
			},
		); err != nil {
			req.Logger.Warnf(err.Error())
		}

		rApi.SetLocal(req, "devices", devices)
		return nil
	}(); err != nil {
		r.logger.Warnf(err.Error())
	}

	// fetching deployments
	if err := func() error {

		var deploys appsv1.DeploymentList
		if err := r.List(
			req.Context(), &deploys, &client.ListOptions{
				Namespace: fmt.Sprintf("wg-%s", obj.Name),
				LabelSelector: apiLabels.SelectorFromValidatedSet(
					apiLabels.Set{
						constants.WgDeploy: "true",
					},
				),
			},
		); err != nil {
			req.Logger.Warnf(err.Error())
		}

		rApi.SetLocal(req, "wg-deployments", deploys)
		return nil
	}(); err != nil {
		r.logger.Warnf(err.Error())
	}

	return req.Next()
}

// reconcile wireguard unique domain for endpoint
func (r *AccountReconciler) reconWGDomain(req *rApi.Request[*managementv1.Account]) stepResult.Result {
	ctx, obj := req.Context(), req.Object

	dm, ok := obj.Status.GeneratedVars.GetString("wg-domain")

	if !ok {
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)

		domainName := func() string {
			for {
				name := nameGenerator.Generate()
				var domains managementv1.DomainList
				e := r.Client.List(
					ctx, &domains, &client.ListOptions{
						LabelSelector: apiLabels.SelectorFromValidatedSet(
							apiLabels.Set{
								constants.WgDomain: name,
							},
						),
					},
				)
				if e != nil || len(domains.Items) == 0 {
					return name
				}
			}
		}()

		obj.Status.GeneratedVars.Set("wg-domain", domainName)
		return req.UpdateStatus()
	}

	rApi.SetLocal(req, "wg-domain", dm)

	return req.Next()
}

// reconcile wireguard public,private keys and ensure created.
func (r *AccountReconciler) reconWGServerKey(req *rApi.Request[*managementv1.Account]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(AccountWGSKeysReady, check, err.Error())
	}

	namespace := fmt.Sprintf("wg-%s", obj.Name)

	// ensure keys generated, else crate
	wgSecrets, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Namespace: namespace,
			Name:      "wg-server-keys",
		}, &corev1.Secret{},
	)

	// if not found or got some error handle it
	if err != nil {
		// if err is something else return
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}

		// if err is not found means needs to create new

		pub, priv, err := wireguard.GenerateWgKeys()
		if err != nil {
			return failed(err)
		}

		if err = r.Client.Create(ctx,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-keys",
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						functions.AsOwner(req.Object, true),
					},
				},
				Data: map[string][]byte{
					"private-key": []byte(priv),
					"public-key":  []byte(pub),
				},
			},
		); err != nil {
			return failed(err)
		}

		req.Object.Status.DisplayVars.Set("wg-public-key", pub)
		req.UpdateStatus()
		return failed(fmt.Errorf("keys are being generated"))
	}

	// if found set to local so we can use it later
	rApi.SetLocal(req, "wg-private-key", string(wgSecrets.Data["private-key"]))

	// check if publicKey is set to DisplayVars if not then set it (will be used by device)
	if existingPublicKey, ok :=
		req.Object.Status.DisplayVars.GetString("wg-public-key"); ok &&
		string(wgSecrets.Data["public-key"]) != existingPublicKey {

		req.Object.Status.DisplayVars.Set("wg-public-key", string(wgSecrets.Data["public-key"]))
		req.UpdateStatus()
		return failed(fmt.Errorf("keys are being generated"))

	} else if !ok {
		req.Object.Status.DisplayVars.Set("wg-public-key", string(wgSecrets.Data["public-key"]))
		return failed(fmt.Errorf("keys are being generated"))
	}

	check.Status = true
	if check != checks[AccountWGSKeysReady] {
		checks[AccountWGSKeysReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

// reconcile wireguard server config generation
func (r *AccountReconciler) reconWGServerConfig(req *rApi.Request[*managementv1.Account]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	failed := func(err error) stepResult.Result {
		return req.CheckFailed(AccountWGSConfigReady, check, err.Error())
	}

	privateKey, ok := rApi.GetLocal[string](req, "wg-private-key")
	if !ok {
		return failed(fmt.Errorf("can't find wireguard private key"))
	}

	namespace := fmt.Sprintf("wg-%s", obj.Name)

	var deviceWgSecretList corev1.SecretList
	if err := r.List(
		ctx, &deviceWgSecretList,
		&client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				map[string]string{
					constants.DeviceWgKey: "true",
				},
			),
			Namespace: namespace,
		},
	); err != nil {
		return failed(err)
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
	data.AccountWireguardPvtKey = privateKey

	sort.Slice(
		deviceWgSecretList.Items, func(i, j int) bool {
			return deviceWgSecretList.Items[i].Name < deviceWgSecretList.Items[j].Name
		},
	)

	for _, device := range deviceWgSecretList.Items {
		ip, ok := device.Data["ip"]
		if !ok {
			continue
		}
		publicKey, ok := device.Data["public-key"]
		if !ok {
			continue
		}
		data.Peers = append(
			data.Peers, struct {
				PublicKey  string
				AllowedIps string
			}{
				PublicKey:  string(publicKey),
				AllowedIps: fmt.Sprintf("%s/32", string(ip)),
			},
		)
	}
	// fmt.Println(data.Peers)

	parse, err := templates.Parse(templates.WireGuardConfig, data)
	if err != nil {
		return failed(err)
	}

	serverConfig := parse

	existingConfig, err := rApi.Get(
		req.Context(), r.Client, types.NamespacedName{
			Namespace: "wg-" + req.Object.Name,
			Name:      "wg-server-config",
		}, &corev1.Secret{},
	)

	if err != nil {
		if e := r.Client.Create(
			ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-config",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"data": serverConfig,
				},
			},
		); e != nil {
			return failed(err)
		}
	}

	if existingConfig == nil || string(existingConfig.Data["data"]) != string(parse) {

		if e := r.Client.Update(
			req.Context(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-config",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"data": serverConfig,
				},
			},
		); e != nil {
			return failed(err)
		}

		regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
		if !ok {
			return failed(fmt.Errorf("regions not found"))
		}

		devices, ok := rApi.GetLocal[managementv1.DeviceList](req, "devices")
		if !ok {
			return failed(fmt.Errorf("devices not found"))
		}

		activeRegions := func() map[string]bool {
			r := map[string]bool{}
			for _, d := range devices.Items {
				if d.Spec.ActiveRegion != "" {
					r[d.Spec.ActiveRegion] = true
				}
			}
			return r
		}()

		for _, region := range regions.Items {

			if !activeRegions[region.Name] {
				continue
			}

			if _, err = http.Post(
				fmt.Sprintf("http://wg-api-service-%s.wg-%s.svc.cluster.local:2998/post", region.Name, req.Object.Name),
				"application/json",
				bytes.NewBuffer([]byte(serverConfig)),
			); err != nil {
				r.logger.Warnf(err.Error())
			}
		}

	}

	check.Status = true
	if check != checks[AccountWGSConfigReady] {
		checks[AccountWGSConfigReady] = check
		req.UpdateStatus()
	}

	return req.Next()
}

// update deployments
func (r *AccountReconciler) updateDeployments(req *rApi.Request[*managementv1.Account]) error {

	obj := req.Object
	namespace := fmt.Sprintf("wg-%s", obj.Name)

	regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
	if !ok {
		return errors.New("regions not found")
	}

	devices, ok := rApi.GetLocal[managementv1.DeviceList](req, "devices")
	if !ok {
		return errors.New("devices not found")
	}

	deploys, ok := rApi.GetLocal[appsv1.DeploymentList](req, "wg-deployments")
	if !ok {
		return errors.New("cant't find deployments")
	}

	deployments := func() map[string]bool {
		r := map[string]bool{}
		for _, d := range deploys.Items {
			r[d.Name] = true
		}
		return r
	}()

	name := func(n string) string {
		return fmt.Sprintf("wireguard-deployment-%s", n)
	}

	activeRegions := func() map[string]bool {
		r := map[string]bool{}
		for _, d := range devices.Items {
			if d.Spec.ActiveRegion != "" {
				r[d.Spec.ActiveRegion] = true
			}
		}
		return r
	}()

	for _, r2 := range regions.Items {

		if !activeRegions[r2.Name] {
			if deployments[name(r2.Name)] {
				if err := r.Delete(req.Context(), &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name(r2.Name),
						Namespace: namespace,
					},
				}); err != nil {
					return err
				}
			}

			continue
		}

		if deployments[name(r2.Name)] {
			continue
		}

		if b, err := templates.Parse(
			templates.WGDeploy, map[string]any{
				"obj":               req.Object,
				"region-owner-refs": functions.AsOwner(&r2),
				"region":            r2.Name,
				"isMaster":          r2.Spec.IsMaster,
			},
		); err != nil {
			return err
		} else if _, err = functions.KubectlApplyExec(b); err != nil {
			return err
		}
	}
	return nil
}

// reconcile coredns
func (r *AccountReconciler) reconCoredns(req *rApi.Request[*managementv1.Account]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	namespace := fmt.Sprintf("wg-%s", obj.Name)
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(CorednsDeployReady, check, err.Error())
	}

	_, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Namespace: namespace,
			Name:      "coredns",
		}, &appsv1.Deployment{},
	)

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}
		configExists := true

		if _, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Name:      "coredns",
				Namespace: namespace,
			}, &corev1.ConfigMap{},
		); err != nil {
			configExists = false
		}

		regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")

		if !ok {
			return failed(fmt.Errorf("regions not found"))
		}

		var klReg string
		for _, reg := range regions.Items {
			if reg.Spec.AccountName == "kl-core" {
				klReg = reg.Name
			}
		}

		// if klReg == "" {
		// 	// if len(regions.Items) > 0 {
		// 	// 	klReg = regions.Items[0].Name
		// 	// } else {
		// 	// 	return failed(fmt.Errorf("regions not found"))
		// 	// }
		// }

		if b, err := templates.Parse(
			templates.Coredns, map[string]any{
				"obj":                 req.Object,
				"corednsConfigExists": configExists,
				"owner-refs":          []metav1.OwnerReference{functions.AsOwner(obj, true)},
				"region":              klReg,
			},
		); err != nil {
			return failed(err)
		} else if _, err = functions.KubectlApplyExec(b); err != nil {
			return failed(err)
		}

	}

	check.Status = true
	if check != checks[CorednsDeployReady] {
		checks[CorednsDeployReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

// reconcile deployments at least master needs to be created
func (r *AccountReconciler) reconDeployments(req *rApi.Request[*managementv1.Account]) stepResult.Result {

	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(WGDeploysReady, check, err.Error())
	}

	regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
	if !ok {
		return failed(fmt.Errorf("regions not found"))
	}

	deployments, ok := rApi.GetLocal[appsv1.DeploymentList](req, "wg-deployments")
	if !ok || len(deployments.Items) == 0 || len(deployments.Items) != len(regions.Items) {
		// update deployment
		if err := r.updateDeployments(req); err != nil {
			return failed(err)
		}
	}

	var svcs corev1.ServiceList
	err := r.Client.List(
		ctx, &svcs, &client.ListOptions{
			Namespace: fmt.Sprintf("wg-%s", req.Object.Name),
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				apiLabels.Set{
					constants.WgService: "true",
				},
			),
		},
	)

	if err != nil || len(svcs.Items) == 0 || len(svcs.Items) != len(regions.Items) {
		if err != nil && !apiErrors.IsNotFound(err) {
			return failed(err)
		}
		// create service

		if err := r.updateDeployments(req); err != nil {
			return failed(err)
		}

	}

	displayVarsUpdated := false
	for _, svc := range svcs.Items {
		nodePort := svc.Spec.Ports[0].NodePort

		if nodePort == 0 {
			fmt.Println("node port not ready for service", svc.Name)
			continue
		}

		region, ok := svc.GetLabels()["region"]
		if !ok {
			continue
		}

		np, ok := req.Object.GetStatus().DisplayVars.GetString(fmt.Sprintf("wg-nodeport-%s", region))
		if !ok || np != fmt.Sprintf("%d", nodePort) {
			displayVarsUpdated = true
			req.Object.Status.DisplayVars.Set(fmt.Sprintf("wg-nodeport-%s", region), fmt.Sprintf("%d", nodePort))
		}
	}

	if displayVarsUpdated {
		req.UpdateStatus()
		return failed(fmt.Errorf("display vars are being updated"))
	}

	check.Status = true
	if check != checks[WGDeploysReady] {
		checks[WGDeploysReady] = check
		return req.UpdateStatus()
	}
	return req.Next()
}

// reconcile domains
func (r *AccountReconciler) reconDomain(req *rApi.Request[*managementv1.Account]) stepResult.Result {

	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(AccountWGSConfigReady, check, err.Error())
	}

	regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
	if !ok {
		return failed(fmt.Errorf("regions not found"))
	}

	for _, region := range regions.Items {

		wgDomain, ok := rApi.GetLocal[string](req, "wg-domain")
		if !ok {
			return failed(fmt.Errorf("can't get wg domain"))
		}

		var ipsAny []any
		if err := region.Status.DisplayVars.Get(constants.NodeIps, &ipsAny); err != nil {
			continue
		}

		if err := functions.KubectlApply(
			ctx, r.Client, &managementv1.Domain{
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
						functions.AsOwner(&region),
					},
				},
				Spec: managementv1.DomainSpec{
					Name: fmt.Sprintf("%s.%s.wg.%s", region.Name, wgDomain, r.Env.WgDomain),
					Ips: func() []string {

						// fmt.Println(ipsAny)
						ips := []string{}
						for _, ip := range ipsAny {
							ips = append(ips, ip.(string))
						}
						return ips

					}(),
				},
			},
		); err != nil {
			return failed(err)
		}
	}

	check.Status = true
	if check != checks[DomainReady] {
		checks[DomainReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

// generating device proxy config and updating services
func (r *AccountReconciler) reconDevProxyConfig(req *rApi.Request[*managementv1.Account]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(DeviceProxyReady, check, err.Error())
	}

	namespace := "wg-" + req.Object.Name
	name := "device-proxy-config"

	oldConfig, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, &corev1.ConfigMap{},
	)

	if err != nil {

		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}

		if e := r.Client.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Annotations: apiLabels.Set{
					constants.AccountNameKey: obj.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					functions.AsOwner(req.Object),
				},
			},
			Data: map[string]string{
				"config.json": `{"services":[]}`,
			},
		}); e != nil {
			return failed(e)
		}

		return failed(fmt.Errorf("services are not upto date"))
	}

	configs := []configService{}
	oConfigs := []configService{}
	configData := map[string]*configService{}

	// parsing the value from old config andd adding to the var configData
	if err == nil {
		oConfMap := map[string][]configService{}
		if e := json.Unmarshal([]byte(oldConfig.Data["config.json"]), &oConfMap); e != nil {
			return failed(fmt.Errorf("can't unmarshal config.json"))
		}

		oConfigs = []configService{}
		if oConfMap["services"] != nil {
			oConfigs = oConfMap["services"]
		}
	}

	for _, cs := range oConfigs {
		k := cs
		configData[cs.Id] = &k
	}

	// method to check either the port exists int the config
	isContains := func(svce map[string]*configService, port int32) bool {
		for _, s := range svce {
			if s.ServicePort == port {
				return true
			}
		}
		return false
	}

	// method to finding the unique port to assign to needfull
	getTempPort := func(svcs map[string]*configService, id string) int32 {
		if svcs[id] != nil {
			return svcs[id].ProxyPort
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

	devices, ok := rApi.GetLocal[managementv1.DeviceList](req, "devices")
	if !ok {
		return failed(fmt.Errorf("devices not found"))
	}

	// sorting the fetched devices
	sort.Slice(
		devices.Items, func(i, j int) bool {
			return devices.Items[i].Name < devices.Items[j].Name
		},
	)

	// generating the latest services and will be applied at in operations sections
	// services := []corev1.Service{}
	for _, d := range devices.Items {
		// type portStruct struct {
		// 	Name       string `json:"name"`
		// 	Port       int32  `json:"port"`
		// 	TargetPort int32  `json:"targetPort"`
		// 	Protocol   string `json:"protocol"`
		// }

		for _, port := range d.Spec.Ports {
			tempPort := getTempPort(configData, fmt.Sprint(d.Name, "-", port.Port))

			dIp, e := getRemoteDeviceIp(int64(d.Spec.Offset))
			if e != nil {
				fmt.Println(e)
				continue
			}

			configs = append(
				configs, configService{
					Id:   fmt.Sprint(d.Name, "-", port.Port),
					Name: dIp.String(),
					ServicePort: func() int32 {
						if port.TargetPort != 0 {
							return port.TargetPort
						}
						return port.Port
					}(),
					ProxyPort: tempPort,
				},
			)
		}
	}
	sort.Slice(
		configs, func(i, j int) bool {
			return configs[i].Name < configs[j].Name
		},
	)

	c, err := json.Marshal(
		map[string][]configService{
			"services": configs,
		},
	)
	if err != nil {
		return failed(err)
	}

	// checking either the new generated config is equal or not
	equal := false
	// fmt.Println(oldConfig.Data["config.json"], string(c))
	equal, err = functions.JSONStringsEqual(oldConfig.Data["config.json"], string(c))
	if err != nil {
		return failed(err)
	}
	if len(configs) == 0 {
		equal = true
	}
	if !equal {

		sort.Slice(
			configs, func(i, j int) bool {
				return configs[i].Name < configs[j].Name
			},
		)

		configJson, err := json.Marshal(map[string][]configService{
			"services": configs,
		})
		if err != nil {
			return failed(err)
		}

		if e := r.Client.Update(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Annotations: apiLabels.Set{
					constants.AccountNameKey: obj.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					functions.AsOwner(req.Object),
				},
			},
			Data: map[string]string{
				"config.json": string(configJson),
			},
		}); e != nil {
			return failed(err)
		}

		regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
		if !ok {
			return failed(fmt.Errorf("regions not found"))
		}

		activeRegions := func() map[string]bool {
			r := map[string]bool{}
			for _, d := range devices.Items {
				if d.Spec.ActiveRegion != "" {
					r[d.Spec.ActiveRegion] = true
				}
			}
			return r
		}()

		for _, region := range regions.Items {
			if !activeRegions[region.Name] {
				continue
			}

			if _, err = http.Post(
				fmt.Sprintf("http://wg-api-service-%s.wg-%s.svc.cluster.local:2999/post", region.Name, req.Object.Name),
				"application/json",
				bytes.NewBuffer(configJson),
			); err != nil {
				r.logger.Warnf(err.Error())
			}
		}

	}

	check.Status = true
	if check != checks[DeviceProxyReady] {
		checks[DeviceProxyReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

// finalize account delete
func (r *AccountReconciler) finalize(req *rApi.Request[*managementv1.Account]) stepResult.Result {
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

// SetupWithManager sets up the controller with the Manager.
func (r *AccountReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.logger = logger.WithName(r.Name)

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Account{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{})

	builder.Watches(
		&source.Kind{Type: &managementv1.Device{}}, handler.EnqueueRequestsFromMapFunc(
			func(object client.Object) []reconcile.Request {
				if object.GetLabels() == nil {
					return nil
				}
				account, ok := object.GetLabels()["kloudlite.io/account.name"]
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
			},
		),
	)

	builder.Watches(
		&source.Kind{Type: &managementv1.Region{}}, handler.EnqueueRequestsFromMapFunc(
			func(object client.Object) []reconcile.Request {

				if object.GetLabels() == nil {
					return nil
				}

				l := object.GetLabels()
				accountName := l["kloudlite.io/account.name"]

				var accounts managementv1.AccountList
				results := []reconcile.Request{}
				ctx := context.TODO()

				if accountName == "" {
					err := r.Client.List(
						ctx, &accounts, &client.ListOptions{
							LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{}),
						},
					)
					if err != nil {
						return nil
					}

					for _, account := range accounts.Items {
						results = append(
							results, reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: account.Name,
								},
							},
						)
					}
				} else {

					account, err := rApi.Get(
						ctx, r.Client,
						types.NamespacedName{
							Name: accountName,
						}, &managementv1.Account{},
					)

					if err != nil {
						return nil
					}

					results = append(
						results, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name: account.Name,
							},
						},
					)
				}
				if len(results) == 0 {
					return nil
				}

				return results
			},
		),
	)
	return builder.Complete(r)
}
