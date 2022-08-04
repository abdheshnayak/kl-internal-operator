package commoncontroller

import (
	"context"
	"os"

	"operators.kloudlite.io/lib/nameserver"
	rApi "operators.kloudlite.io/lib/operator"

	corev1 "k8s.io/api/core/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

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
	_ = log.FromContext(ctx)

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &managementv1.Region{})

	if req == nil {
		return ctrl.Result{}, nil
	}

	var nodes corev1.NodeList

	err := r.Client.List(ctx, &nodes, &client.ListOptions{
		LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
			"kloudlite.io/region": req.Object.Name,
		}),
	})

	if err != nil {
		x := rApi.NewStepResult(nil, err)
		return x.Result(), x.Err()
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

	endpoint := os.Getenv("nameserver_endpoint")

	dns := nameserver.NewClient(endpoint)

	err = dns.UpsertNodeIps(req.Object.Name, ips)

	if err != nil {
		x := rApi.NewStepResult(nil, err)
		return x.Result(), x.Err()
	}

	if err := r.Status().Update(req.Context(), req.Object); err != nil {
		x := rApi.NewStepResult(nil, err)
		return x.Result(), x.Err()
	}

	return ctrl.Result{}, nil
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
