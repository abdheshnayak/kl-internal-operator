package commoncontroller

import (
	"context"
	"fmt"

	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rApi "operators.kloudlite.io/lib/operator"
	ctrl "sigs.k8s.io/controller-runtime"

	managementv1 "operators.kloudlite.io/apis/management/v1"
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
	Scheme *runtime.Scheme
}

type configService struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	ServicePort int32  `json:"servicePort"`
	ProxyPort   int32  `json:"proxyPort"`
}

//+kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=management.kloudlite.io,resources=accounts/finalizers,verbs=update

func (r *AccountReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &managementv1.Account{})

	if req == nil {
		return ctrl.Result{}, nil
	}

	req.Logger.Info("##################### NEW RECONCILATION------------------")

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

func (r *AccountReconciler) finalize(req *rApi.Request[*managementv1.Account]) rApi.StepResult {
	return req.Finalize()
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Account{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Watches(&source.Kind{Type: &managementv1.Device{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
			if object.GetLabels() == nil {
				return nil
			}
			account, ok := object.GetLabels()["kloudlite.io/account-ref"]
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
		})).
		Watches(&source.Kind{Type: &managementv1.Region{}}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {

			if object.GetLabels() == nil {
				return nil
			}

			l := object.GetLabels()
			accountId := l["kloudlite.io/account-ref"]

			var accounts managementv1.AccountList
			results := []reconcile.Request{}
			ctx := context.TODO()

			if accountId == "" {
				err := r.Client.List(ctx, &accounts, &client.ListOptions{
					LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{}),
				})
				if err != nil {
					return nil
				}

				for _, account := range accounts.Items {
					results = append(results, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: account.Name,
						},
					})
				}
			} else {

				account, err := rApi.Get(ctx, r.Client,
					types.NamespacedName{
						Name: accountId,
					}, &managementv1.Account{},
				)
				if err != nil {
					return nil
				}

				results = append(results, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: account.Name,
					},
				})
			}
			if len(results) == 0 {
				return nil
			}

			return results
		})).
		Complete(r)
}
