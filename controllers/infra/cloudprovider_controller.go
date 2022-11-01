package infra

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "operators.kloudlite.io/apis/infra/v1"
	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/logging"
	rApi "operators.kloudlite.io/lib/operator.v2"
	stepResult "operators.kloudlite.io/lib/operator.v2/step-result"
)

// CloudProviderReconciler reconciles a CloudProvider object
type CloudProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	logger logging.Logger
	Name   string
}

const (
	EdgesDeleted  string = "edges-deleted"
	SecretDeleted string = "secret-deleted"
)

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=cloudproviders,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=cloudproviders/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=cloudproviders/finalizers,verbs=update

func (r *CloudProviderReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	req, err := rApi.NewRequest(context.WithValue(ctx, "logger", r.logger), r.Client, request.NamespacedName, &infrav1.CloudProvider{})
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if step := req.EnsureChecks(RegionReady, PoolReady); !step.ShouldProceed() {
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

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *CloudProviderReconciler) finalize(req *rApi.Request[*infrav1.CloudProvider]) stepResult.Result {

	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks

	check := rApi.Check{Generation: obj.Generation}

	var Edges infrav1.EdgeList
	if err := r.Client.List(
		ctx, &Edges, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				map[string]string{constants.ProviderRef: obj.Name},
			),
		},
	); err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(EdgesDeleted, check, err.Error())
		}
	}

	if len(Edges.Items) >= 1 {
		if err := r.DeleteAllOf(
			ctx, &infrav1.Edge{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: apiLabels.SelectorFromValidatedSet(
						map[string]string{
							constants.ProviderRef: obj.Name,
						},
					),
				},
			},
		); err != nil {
			return req.CheckFailed(EdgesDeleted, check, err.Error())
		}
		checks[EdgesDeleted] = check
		return req.UpdateStatus()
	}

	if len(Edges.Items) != 0 {
		// r.Get(ctx,)
		if _, err := rApi.Get(ctx, r.Client, types.NamespacedName{
			Name: obj.Name,
		}, &corev1.Secret{}); err != nil {
			if !apiErrors.IsNotFound(err) {
				return req.CheckFailed(SecretDeleted, check, err.Error())
			}
			return req.Done()
		}

		if err := r.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: obj.Name,
			},
		}); err != nil {
			return req.CheckFailed(SecretDeleted, check, err.Error())
		}
		checks[SecretDeleted] = check
		return req.UpdateStatus()
	}

	// check and everything is deleted and delete secret
	return req.Finalize()
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudProviderReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {

	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.logger = logger.WithName(r.Name)

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.CloudProvider{}).
		Complete(r)
}
