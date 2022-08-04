package infra

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	// "k8s.io/apimachinery/pkg/types"
	"operators.kloudlite.io/lib/conditions"
	rApi "operators.kloudlite.io/lib/operator"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// corev1 "k8s.io/api/core/v1"
	// apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	infrav1 "operators.kloudlite.io/apis/infra/v1"
)

// AccountNodeReconciler reconciles a AccountNode object
type AccountNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AccountNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *AccountNodeReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &infrav1.AccountNode{})

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

func (r *AccountNodeReconciler) finalize(req *rApi.Request[*infrav1.AccountNode]) rApi.StepResult {
	return req.Finalize()
}

func (r *AccountNodeReconciler) reconcileStatus(req *rApi.Request[*infrav1.AccountNode]) rApi.StepResult {

	// actions (action depends on provider) (best if create seperate crdfor diffrent providers)
	// check if provider present and ready
	// check if node present
	// check status of node
	// create if node not present
	// update node if values changed
	// update node if provider values changed
	// drain and delete node if crd deleted

	req.Object.Status.DisplayVars.Reset()
	var cs []metav1.Condition
	isReady := true
	retry := false

	// template
	if err := func() error {
		// _, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
		// 	Name: "wg-" + req.Object.Name,
		// }, &corev1.Namespace{})

		// if err != nil {
		// 	if !apiErrors.IsNotFound(err) {
		// 		return err
		// 	}
		// 	isReady = false
		// 	cs = append(cs,
		// 		conditions.New(
		// 			"WGNamespaceNotFound",
		// 			false,
		// 			"NotFound",
		// 			"WG namespace not found",
		// 		),
		// 	)
		// }

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

func (r *AccountNodeReconciler) reconcileOperations(req *rApi.Request[*infrav1.AccountNode]) rApi.StepResult {

	// do some task here
	if err := func() error {
		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AccountNode{}).
		Complete(r)
}
