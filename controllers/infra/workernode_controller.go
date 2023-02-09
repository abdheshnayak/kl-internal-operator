package infra

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/kloudlite/internal_operator_v2/apis/infra/v1"
	"github.com/kloudlite/internal_operator_v2/env"
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	"github.com/kloudlite/internal_operator_v2/lib/logging"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	stepResult "github.com/kloudlite/internal_operator_v2/lib/operator.v2/step-result"
)

/*
# actions needs to be performed
1. check if node created
2. if not created create
3. check if k3s installed
4. if not installed install
5. if deletion timestamp present
	- delete node from the cluster
	- delete the actual master node
*/

// WorkerNodeReconciler reconciles a WorkerNode object
type WorkerNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logging.Logger
	Name   string
	Env    *env.Env
}

func (r *WorkerNodeReconciler) GetName() string {
	return r.Name
}

const (
	SampleReady string = "sample-ready"
)

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=workernodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=workernodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=workernodes/finalizers,verbs=update

func (r *WorkerNodeReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	req, err := rApi.NewRequest(context.WithValue(ctx, constants.LoggerConst, r.logger), r.Client, request.NamespacedName, &infrav1.WorkerNode{})
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if step := req.EnsureChecks(WorkerNodesReady, WorkerNodesDeleted); !step.ShouldProceed() {
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

	// if step := r.reconWorkerNodes(req); !step.ShouldProceed() {
	// 	return step.ReconcilerResponse()
	// }

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *WorkerNodeReconciler) finalize(req *rApi.Request[*infrav1.WorkerNode]) stepResult.Result {
	return req.Finalize()
}

func (r *WorkerNodeReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.logger = logger.WithName(r.Name)
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()

	builder := ctrl.NewControllerManagedBy(mgr).For(&infrav1.WorkerNode{})
	return builder.Complete(r)
}
