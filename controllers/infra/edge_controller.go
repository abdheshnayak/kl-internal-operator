package infra

import (
	"context"
	"fmt"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	// "k8s.io/apimachinery/pkg/types"
	"operators.kloudlite.io/lib/conditions"
	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/functions"
	"operators.kloudlite.io/lib/templates"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	managementv1 "operators.kloudlite.io/apis/management/v1"
	rApi "operators.kloudlite.io/lib/operator"
	ctrl "sigs.k8s.io/controller-runtime"

	infrav1 "operators.kloudlite.io/apis/infra/v1"
)

// EdgeReconciler reconciles a Edge object
type EdgeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Edge object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile

func (r *EdgeReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &infrav1.Edge{})

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

func (r *EdgeReconciler) finalize(req *rApi.Request[*infrav1.Edge]) rApi.StepResult {
	// check and delete region
	if err, done := func() (error, bool) {

		_, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Name: req.Object.Name,
		}, &managementv1.Region{})

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err, false
			}
			return err, true
		}

		if _, err = functions.ExecCmd(
			fmt.Sprintf("kubectl delete region/%s", req.Object.Name),
			""); err != nil {
			return err, false
		}

		return nil, false
	}(); err != nil {
		return req.FailWithStatusError(err)
	} else if done {
		return req.Finalize()
	}

	// check is pool present
	if err, done := func() (error, bool) {

		_, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Name: req.Object.Name,
		}, &infrav1.NodePool{})

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err, false
			}
			return err, true
		}

		_, err = functions.ExecCmd(fmt.Sprintf("kubectl delete nodepool -l kloudlite.io/edge-ref", req.Object.Name), "")
		if err != nil {
			return err, false
		}

		return nil, false
	}(); err != nil {
		return req.FailWithStatusError(err)
	} else if done {
		return req.Finalize()
	}

	return req.Done()
}

func (r *EdgeReconciler) reconcileStatus(req *rApi.Request[*infrav1.Edge]) rApi.StepResult {

	// actions
	// if possible check if credentials valid
	// delete all the nodes under this provider if deleted
	// if credentials updated update the version of config and trigger all the nodes to be updated

	req.Object.Status.DisplayVars.Reset()
	var cs []metav1.Condition
	isReady := true
	// retry := false

	// check if region created
	if err := func() error {

		r, err := rApi.Get(req.Context(), r.Client, types.NamespacedName{
			Name: req.Object.Name,
		}, &managementv1.Region{})

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false

			cs = append(cs,
				conditions.New(
					"RegionFound",
					false,
					"NotFound",
					"Region Not created yet",
				),
			)
			return nil
		}

		if r.Spec.Account != req.Object.Spec.AccountId {
			isReady = false
			cs = append(cs,
				conditions.New(
					"RegionFound",
					false,
					"NotFound",
					"Region Not created yet",
				),
			)
			return nil
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check is pool created
	if err := func() error {

		var nodePools infrav1.NodePoolList
		err := r.Client.List(req.Context(), &nodePools, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"kloudlite.io/edge-ref": req.Object.Name,
			}),
		})

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false
			cs = append(cs,
				conditions.New(
					"PoolUpToDate",
					false,
					"NotFound",
					"Node pools are on old Version",
				),
			)
			return nil
		} else if len(nodePools.Items) != len(req.Object.Spec.Pools) {

			isReady = false
			cs = append(cs,
				conditions.New(
					"PoolUpToDate",
					false,
					"NotFound",
					"Node pools are on old Version",
				),
			)
			return nil
		}

		for _, p := range req.Object.Spec.Pools {
			matched := false
			for _, np := range nodePools.Items {
				fmt.Println(p.Name, np.Name)
				fmt.Println(p.Min, np.Spec.Min)
				fmt.Println(p.Max, np.Spec.Max)
				if fmt.Sprintf("%s-%s", req.Object.Name, p.Name) == np.Name &&
					p.Min == np.Spec.Min &&
					p.Max == np.Spec.Max {
					matched = true
					break
				}
			}

			if !matched {

				isReady = false
				cs = append(cs,
					conditions.New(
						"PoolUpToDate",
						false,
						"NotFound",
						"Node pools are on old Version",
					),
				)
				return nil

			}

		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// if retry {
	// 	if err := r.Status().Update(req.Context(), req.Object); err != nil {
	// 		return req.FailWithStatusError(err)
	// 	}
	// 	return req.Done(&ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5})
	// }

	newConditions, hasUpdated, err := conditions.Patch(req.Object.Status.Conditions, cs)

	if err != nil {
		return req.FailWithStatusError(err)
	}

	if !hasUpdated && isReady == req.Object.Status.IsReady {
		return req.Next()
	}

	req.Object.Status.Conditions = newConditions
	req.Object.Status.IsReady = isReady
	if err := r.Status().Update(req.Context(), req.Object); err != nil {
		return req.FailWithStatusError(err)
	}

	return req.Done()

}

func (r *EdgeReconciler) reconcileOperations(req *rApi.Request[*infrav1.Edge]) rApi.StepResult {
	// if region not created create
	if err := func() error {

		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "RegionFound") {
			return nil
		}

		if b, err := templates.Parse(templates.Region, map[string]any{
			"name":     req.Object.Name,
			"account":  req.Object.Spec.AccountId,
			"provider": req.Object.Spec.Provider,
		}); err != nil {
			return err
		} else if _, err = functions.KubectlApplyExec(b); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// do some task here
	if err := func() error {

		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "PoolUpToDate") {
			return nil
		}

		b, err := templates.Parse(templates.NodePools, map[string]any{
			"pools": func() []infrav1.NodePool {
				pls := make([]infrav1.NodePool, 0)
				for _, p := range req.Object.Spec.Pools {

					pls = append(pls, infrav1.NodePool{
						ObjectMeta: metav1.ObjectMeta{
							Name:            fmt.Sprintf("%s-%s", req.Object.Name, p.Name),
							OwnerReferences: []metav1.OwnerReference{functions.AsOwner(req.Object, true)},
							Labels:          req.Object.GetEnsuredLabels(),
						},
						Spec: infrav1.NodePoolSpec{
							AccountRef: req.Object.Spec.AccountId,
							EdgeRef:    req.Object.Name,
							Provider:   req.Object.Spec.Provider,
							Region:     req.Object.Spec.Region,
							Config:     p.Config,
							Min:        p.Min,
							Max:        p.Max,
						},
					})
				}
				return pls
			}(),
		})

		if err != nil {
			return err
		}

		// fmt.Println(string(b))

		if _, err = functions.KubectlApplyExec(b); err != nil {
			return err
		}

		return nil

	}(); err != nil {
		return req.FailWithOpError(err)
	}

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *EdgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.Edge{}).
		Owns(&infrav1.NodePool{}).
		Complete(r)
}
