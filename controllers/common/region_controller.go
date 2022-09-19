package commoncontroller

import (
	"context"
	"fmt"
	"os"

	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/nameserver"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1 "k8s.io/api/core/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	rApi "operators.kloudlite.io/lib/operator"
	ctrl "sigs.k8s.io/controller-runtime"

	managementv1 "operators.kloudlite.io/apis/management/v1"
)

// RegionReconciler reconciles a Region object
type RegionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=management.kloudlite.io,resources=regions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=management.kloudlite.io,resources=regions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=management.kloudlite.io,resources=regions/finalizers,verbs=update

func (r *RegionReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {
	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &managementv1.Region{})

	if req == nil {
		return ctrl.Result{}, nil
	}

	if req.Object.GetDeletionTimestamp() != nil {
		if x := r.finalize(req); !x.ShouldProceed() {
			return x.Result(), x.Err()
		}
	}

	if x := req.EnsureFinilizer(constants.CommonFinalizer); !x.ShouldProceed() {
		// fmt.Println("EnsureFinilizer", x.Err())
		return x.Result(), x.Err()
	}

	req.Logger.Info("-------------------- NEW RECONCILATION------------------")

	if x := req.EnsureLabels(); !x.ShouldProceed() {
		fmt.Println(x.Err())
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

func (r *RegionReconciler) finalize(req *rApi.Request[*managementv1.Region]) rApi.StepResult {
	return req.Done()

}

func (r *RegionReconciler) reconcileStatus(req *rApi.Request[*managementv1.Region]) rApi.StepResult {

	isReady := true
	// check is fetch all nodes of current node
	if err := func() error {
		var nodes corev1.NodeList
		err := r.Client.List(req.Context(), &nodes, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"kloudlite.io/region": req.Object.Name,
			}),
		})

		if err != nil {
			return err
		}

		ips := []string{}
		for _, node := range nodes.Items {
			annotations := node.GetAnnotations()
			if annotations == nil || annotations["k3s.io/internal-ip"] == "" {
				continue
			}
			ips = append(ips, annotations["k3s.io/internal-ip"])
		}

		req.Object.Status.DisplayVars.Set("kloudlite.io/node-ips", ips)

		endpoint := os.Getenv("NAMESERVER_ENDPOINT")

		if endpoint == "" {
			return fmt.Errorf("NAMESERVER_ENDPOINT not found in environment")
		}

		dns := nameserver.NewClient(endpoint)

		if err = dns.UpsertNodeIps(req.Object.Name, ips); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	if req.Object.Status.IsReady != isReady {

		req.Object.Status.IsReady = isReady

		if err := r.Status().Update(req.Context(), req.Object); err != nil {
			return req.FailWithStatusError(err)
		}

	}

	return req.Done()
}

func (r *RegionReconciler) reconcileOperations(req *rApi.Request[*managementv1.Region]) rApi.StepResult {
	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *RegionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Region{}).
		Watches(&source.Kind{Type: &corev1.Node{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			if object.GetLabels() == nil {
				return nil
			}
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
		})).
		Complete(r)
}
