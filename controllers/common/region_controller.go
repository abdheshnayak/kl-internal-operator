package commoncontroller

import (
	"context"
	"time"

	"operators.kloudlite.io/env"
	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/logging"
	"operators.kloudlite.io/lib/nameserver"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1 "k8s.io/api/core/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"

	managementv1 "operators.kloudlite.io/apis/management/v1"

	rApi "operators.kloudlite.io/lib/operator.v2"
	stepResult "operators.kloudlite.io/lib/operator.v2/step-result"
)

// RegionReconciler reconciles a Region object
type RegionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logging.Logger
	Name   string
	Env    *env.Env
}

const (
	DomainReady     string = "domain-ready"
	DefaultsPatched string = "defaults-patched"
)

const (
	DefaultAccountName = "kl-core"
	ClusterName        = "CLUSTER_NAME"
	ClusterConfigName  = "cluster-config"
)

// +kubebuilder:rbac:groups=management.kloudlite.io,resources=regions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=regions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=regions/finalizers,verbs=update

func (r *RegionReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	req, err := rApi.NewRequest(context.WithValue(ctx, "logger", r.logger), r.Client, request.NamespacedName, &managementv1.Region{})
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if step := req.EnsureChecks(RecoardUpToDate); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

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

	if step := r.reconDefaults(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconUpdateRecord(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *RegionReconciler) finalize(req *rApi.Request[*managementv1.Region]) stepResult.Result {
	return req.Done()

}

func (r *RegionReconciler) reconDefaults(req *rApi.Request[*managementv1.Region]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	if obj.Spec.Account == "" {
		obj.Spec.Account = DefaultAccountName

		ann := obj.GetAnnotations()
		ann[constants.AccountRef] = obj.Spec.Account
		obj.SetAnnotations(ann)

		err := r.Update(ctx, obj)
		return req.Done().RequeueAfter(2 * time.Second).Err(err)
	}

	check.Status = true
	if check != checks[DefaultsPatched] {
		checks[DefaultsPatched] = check
		return req.UpdateStatus()
	}
	return req.Next()
}

func (r *RegionReconciler) reconUpdateRecord(req *rApi.Request[*managementv1.Region]) stepResult.Result {
	obj, checks := req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	var nodes corev1.NodeList
	err := r.Client.List(
		req.Context(), &nodes, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				apiLabels.Set{
					"kloudlite.io/region": req.Object.Name,
				},
			),
		},
	)

	if err != nil {
		return req.CheckFailed(DomainReady, check, err.Error())
	}

	ips := []string{}
	for _, node := range nodes.Items {
		if _, ok := node.GetAnnotations()["k3s.io/external-ip"]; !ok {
			if _, ok := node.GetLabels()["kloudlite.io/public-ip"]; !ok {
				continue
			} else {
				ips = append(ips, node.GetLabels()["kloudlite.io/public-ip"])
			}
		} else {
			ips = append(ips, node.GetAnnotations()["k3s.io/external-ip"])
		}
	}

	req.Object.Status.DisplayVars.Set("kloudlite.io/node-ips", ips)

	dns := nameserver.NewClient(r.Env.NameserverEndpoint, r.Env.NameserverUser, r.Env.NameserverPassword)

	// clusterConfig, err := rApi.Get(req.Context(), r.Client, fn.NN("kube-system", ClusterConfigName), &corev1.ConfigMap{})
	// if err != nil {
	// 	return req.CheckFailed(DomainReady, check, err.Error()).Err(nil)
	// }

	if err = dns.UpsertNodeIps(req.Object.Name, req.Object.Spec.Account, r.Env.ClusterId, ips); err != nil {
		return req.CheckFailed(DomainReady, check, err.Error())
	}

	check.Status = true
	if check != checks[RecoardUpToDate] {
		checks[RecoardUpToDate] = check
		return req.UpdateStatus()
	}

	return req.Next()

}

// SetupWithManager sets up the controller with the Manager.
func (r *RegionReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.logger = logger.WithName(r.Name)
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Region{}).
		Watches(
			&source.Kind{Type: &corev1.Node{}}, handler.EnqueueRequestsFromMapFunc(
				func(object client.Object) []reconcile.Request {
					region, ok := object.GetLabels()["kloudlite.io/region"]
					if !ok {
						return nil
					}
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name: region,
							},
						},
					}
				},
			),
		).
		Complete(r)
}
