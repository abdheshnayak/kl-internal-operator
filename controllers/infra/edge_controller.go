package infra

import (
	"context"
	"fmt"
	"time"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/functions"
	"operators.kloudlite.io/lib/logging"
	"operators.kloudlite.io/lib/templates"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	managementv1 "operators.kloudlite.io/apis/management/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	infrav1 "operators.kloudlite.io/apis/infra/v1"

	rApi "operators.kloudlite.io/lib/operator.v2"
	stepResult "operators.kloudlite.io/lib/operator.v2/step-result"
)

// EdgeReconciler reconciles a Edge object
type EdgeReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	logger logging.Logger
	Name   string
}

const (
	RegionReady string = "region-ready"
	PoolReady   string = "pool-ready"
)

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges/finalizers,verbs=update

func (r *EdgeReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	req, err := rApi.NewRequest(context.WithValue(ctx, "logger", r.logger), r.Client, request.NamespacedName, &infrav1.Edge{})
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

	if step := r.reconRegion(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.reconPool(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *EdgeReconciler) reconRegion(req *rApi.Request[*infrav1.Edge]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	reg, err := rApi.Get(ctx, r.Client, types.NamespacedName{
		Name: req.Object.Name,
	}, &managementv1.Region{})

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
		if err := r.applyRegion(req); err != nil {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
		return nil
	}

	if reg.Spec.Account != req.Object.Spec.AccountId {

		if err := r.applyRegion(req); err != nil {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
		return nil
	}

	check.Status = true
	if check != checks[RegionReady] {
		checks[RegionReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *EdgeReconciler) reconPool(req *rApi.Request[*infrav1.Edge]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	var nodePools infrav1.NodePoolList
	err := r.Client.List(ctx, &nodePools, &client.ListOptions{
		LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
			"kloudlite.io/edge-ref": req.Object.Name,
		}),
	})

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
		if err := r.UpdatePool(req); err != nil {
			return req.CheckFailed(PoolReady, check, err.Error())
		}
		return nil
	} else if len(nodePools.Items) != len(req.Object.Spec.Pools) {

		if err := r.UpdatePool(req); err != nil {
			return req.CheckFailed(PoolReady, check, err.Error())
		}
		return nil
	}

	for _, p := range req.Object.Spec.Pools {
		matched := false
		for _, np := range nodePools.Items {
			if fmt.Sprintf("%s-%s", req.Object.Name, p.Name) == np.Name &&
				p.Min == np.Spec.Min &&
				p.Max == np.Spec.Max &&
				p.Config == np.Spec.Config {
				matched = true
				break
			}
		}

		if !matched {
			if err := r.UpdatePool(req); err != nil {
				return req.CheckFailed(PoolReady, check, err.Error())
			}
		}

	}

	check.Status = true
	if check != checks[PoolReady] {
		checks[PoolReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *EdgeReconciler) applyRegion(req *rApi.Request[*infrav1.Edge]) error {

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
}

func (r *EdgeReconciler) UpdatePool(req *rApi.Request[*infrav1.Edge]) error {

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
						ProviderRef: req.Object.Spec.CredentialsRef.SecretName,
						AccountRef:  req.Object.Spec.AccountId,
						EdgeRef:     req.Object.Name,
						Provider:    req.Object.Spec.Provider,
						Region:      req.Object.Spec.Region,
						Config:      p.Config,
						Min:         p.Min,
						Max:         p.Max,
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
}

func (r *EdgeReconciler) finalize(req *rApi.Request[*infrav1.Edge]) stepResult.Result {
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

func (r *EdgeReconciler) reconcileOperations(req *rApi.Request[*infrav1.Edge]) stepResult.Result {
	// do some task here
	if err := func() error {

		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "PoolUpToDate") {
			return nil
		}

		return nil

	}(); err != nil {
		return req.FailWithOpError(err)
	}

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *EdgeReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.logger = logger.WithName(r.Name)

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.Edge{}).
		Owns(&infrav1.NodePool{}).
		Complete(r)
}
