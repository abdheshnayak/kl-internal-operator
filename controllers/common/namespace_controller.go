package commoncontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	managementv1 "operators.kloudlite.io/apis/management/v1"
	rApi "operators.kloudlite.io/lib/operator"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// NamespaceReconciler reconciles a Bridger object
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=device-cluster.kloudlite.io,resources=bridgers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=device-cluster.kloudlite.io,resources=bridgers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=device-cluster.kloudlite.io,resources=bridgers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Bridger object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fmt.Println("Reconcile Namespace")

	ns, e := rApi.Get(ctx, r.Client, types.NamespacedName{
		Name: req.Name,
	}, &corev1.Namespace{})

	// Checking Root CA
	if e != nil {
		if !apiErrors.IsNotFound(e) {
			return ctrl.Result{}, e
		}
		return ctrl.Result{}, nil
	}

	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}

	ns.Annotations["linkerd.io/inject"] = "enabled"

	fmt.Println("Everything is fine")
	e = r.Update(ctx, ns)
	if e != nil {
		fmt.Println("Error updating namespace")
		return ctrl.Result{}, e
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Account{}).
		Watches(
			&source.Kind{Type: &corev1.Namespace{}},
			handler.EnqueueRequestsFromMapFunc(
				func(c client.Object) []reconcile.Request {
					// fmt.Println("Enqueue Requests From Map Func")
					matchedNamespaces := []reconcile.Request{}
					labels := c.GetLabels()
					annotaitons := c.GetAnnotations()
					if labels["mirror.linkerd.io/mirrored-service"] == "true" && annotaitons["linkerd.io/inject"] != "enabled" {
						fmt.Println("Matched")
						matchedNamespaces = append(matchedNamespaces, reconcile.Request{NamespacedName: client.ObjectKey{Name: c.GetName()}})
					}
					return matchedNamespaces
				},
			),
		).
		Complete(r)
}
