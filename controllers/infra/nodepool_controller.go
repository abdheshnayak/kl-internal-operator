package infra

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"operators.kloudlite.io/lib/conditions"
	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/functions"
	"operators.kloudlite.io/lib/kresource"
	"operators.kloudlite.io/lib/rcalculate"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	rApi "operators.kloudlite.io/lib/operator"
	ctrl "sigs.k8s.io/controller-runtime"

	infrav1 "operators.kloudlite.io/apis/infra/v1"
)

// NodePoolReconciler reconciles a NodePool object
type NodePoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=nodepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=nodepools/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NodePool object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile

func (r *NodePoolReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &infrav1.NodePool{})

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

	if !controllerutil.ContainsFinalizer(req.Object, constants.KlFinalizer) {
		controllerutil.AddFinalizer(req.Object, constants.KlFinalizer)
		return ctrl.Result{}, r.Update(ctx, req.Object)
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

func (r *NodePoolReconciler) finalize(req *rApi.Request[*infrav1.NodePool]) rApi.StepResult {
	// TODO: delete all the nodes

	if err, done := func() (error, bool) {

		var accountNodes infrav1.AccountNodeList
		if err := r.Client.List(req.Context(), &accountNodes, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"kloudlite.io/node-pool": req.Object.Name,
			}),
		}); err != nil {

			if !apiErrors.IsNotFound(err) {
				return err, false
			}

		}

		if len(accountNodes.Items) >= 1 {
			if _, e := functions.ExecCmd(fmt.Sprintf("kubectl delete accountnode -l %s=%s", "kloudlite.io/node-pool", req.Object.Name), ""); e != nil {
				return e, false
			}
		} else {
			return nil, true
		}

		return nil, false
	}(); err != nil {
		return req.FailWithStatusError(err)
	} else if done {
		return req.Finalize()
	}

	return req.Done()
}

func (r *NodePoolReconciler) reconcileStatus(req *rApi.Request[*infrav1.NodePool]) rApi.StepResult {

	req.Object.Status.DisplayVars.Reset()
	var cs []metav1.Condition
	isReady := true

	// get all the accountnodes on this pool
	/*
		# Actions performed by this block
		- fetch list of account nodes
		- GetTotal Resource available in the current pool
		- GetTotal usage of resource in the current pool
		- Calculate action
	*/

	if err := func() error {
		var accountNodes infrav1.AccountNodeList

		totalAvailableRes, err := kresource.GetTotalResource(map[string]string{
			"kloudlite.io/node-pool": req.Object.Name,
		})
		if err != nil {
			return err
		}

		totalUsedRes, err := kresource.GetTotalPodRequest(map[string]string{
			"kloudlite.io/node-pool": req.Object.Name,
		}, "requests")
		if err != nil {
			return err
		}

		// get all the nodes in this pool if not create one
		if err = r.Client.List(req.Context(), &accountNodes, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"kloudlite.io/node-pool": req.Object.Name,
			}),
		}); err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false
			cs = append(cs,
				conditions.New(
					"AddNode",
					true,
					"NotFound",
					"Node pool not created yet",
				),
			)

		}

		for _, an := range accountNodes.Items {
			if an.DeletionTimestamp != nil {

				isReady = false
				cs = append(cs,
					conditions.New(
						"Deleting",
						true,
						"Updating",
						"one node being deleted",
					),
				)
				return nil

			} else if !an.Status.IsReady {

				isReady = false
				cs = append(cs,
					conditions.New(
						"Adding",
						true,
						"Updating",
						"one node being added",
					),
				)
				return nil

			}
		}

		i := rcalculate.Input{
			MinNode:          req.Object.Spec.Min,
			MaxNode:          req.Object.Spec.Max,
			CurrentNodeCount: len(accountNodes.Items),
			TotalCapacity:    totalAvailableRes.Memory,
			Used:             totalUsedRes.Memory,
		}

		action, msg, err := i.Calculate()
		if err != nil {
			return err
		}

		fmt.Println(msg)

		// return nil

		if action == 1 {

			isReady = false
			cs = append(cs,
				conditions.New(
					"AddNode",
					true,
					"NodeNeeded",
					msg,
				),
			)

			return nil

		} else if action == -1 {

			isReady = false
			cs = append(cs,
				conditions.New(
					"DelNode",
					true,
					"ExtraFound",
					msg,
				),
			)

			if len(accountNodes.Items) >= 1 {
				rApi.SetLocal(req, "del-account", accountNodes.Items[0].Name)
			} else {
				return fmt.Errorf("needs to delete but no nodes found")
			}

			return nil

		} else {
			isReady = true
			return nil
		}

	}(); err != nil {
		return req.FailWithStatusError(err)
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

func (r *NodePoolReconciler) reconcileOperations(req *rApi.Request[*infrav1.NodePool]) rApi.StepResult {
	if err := func() error {

		if !meta.IsStatusConditionTrue(req.Object.Status.Conditions, "AddNode") {
			return nil
		}

		if err := r.Client.Create(req.Context(), &infrav1.AccountNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: string(uuid.NewUUID()),
				Labels: map[string]string{
					"kloudlite.io/node-pool": req.Object.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					functions.AsOwner(req.Object, true),
				},
			},
			Spec: infrav1.AccountNodeSpec{
				AccountRef:  req.Object.Spec.AccountRef,
				ProviderRef: req.Object.Spec.ProviderRef,
				Provider:    req.Object.Spec.Provider,
				Config:      req.Object.Spec.Config,
				Pool:        req.Object.Name,
			},
		}); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	if err := func() error {

		if !meta.IsStatusConditionTrue(req.Object.Status.Conditions, "DelNode") {
			return nil
		}

		accountName, ok := rApi.GetLocal[string](req, "del-account")

		if !ok {
			return fmt.Errorf("can't find account name  to delete")
		}

		if _, e := functions.ExecCmd(fmt.Sprintf("kubectl delete accountnode/%s", accountName), ""); e != nil {
			return e
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.NodePool{}).
		Owns(&infrav1.AccountNode{}).
		Watches(&source.Kind{Type: &corev1.Node{}}, handler.EnqueueRequestsFromMapFunc(
			func(o client.Object) []reconcile.Request {
				l, ok := o.GetLabels()["kloudlite.io/node-pool"]
				if !ok {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name: l,
				}}}
			},
		)).
		Complete(r)
}
