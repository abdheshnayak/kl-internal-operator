package infra

import (
	"context"
	"fmt"
	"time"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
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
	RegionReady     string = "region-ready"
	PoolReady       string = "pool-ready"
	DefaultsPatched string = "defaults-patched"
)

// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=edges/finalizers,verbs=update

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

	if step := r.PatchDefaults(req); !step.ShouldProceed() {
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

func (r *EdgeReconciler) PatchDefaults(req *rApi.Request[*infrav1.Edge]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	cloudProvider, err := rApi.Get(ctx, r.Client, functions.NN("", obj.Spec.CredentialsRef.SecretName), &infrav1.CloudProvider{})
	if err != nil {
		return req.CheckFailed(DefaultsPatched, check, err.Error()).Err(nil)
	}

	if !functions.IsOwner(obj, functions.AsOwner(cloudProvider)) {
		obj.SetOwnerReferences(append(obj.GetOwnerReferences(), functions.AsOwner(cloudProvider, true)))
		if err := r.Update(ctx, obj); err != nil {
			return req.CheckFailed(DefaultsPatched, check, err.Error())
		}
		return req.Done().RequeueAfter(1 * time.Second)
	}

	check.Status = true
	if check != checks[DefaultsPatched] {
		checks[DefaultsPatched] = check
		return req.UpdateStatus()
	}
	return req.Next()
}

func (r *EdgeReconciler) reconRegion(req *rApi.Request[*infrav1.Edge]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	reg, err := rApi.Get(ctx, r.Client, types.NamespacedName{Name: req.Object.Name}, &managementv1.Region{})

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
		if err := r.applyRegion(req); err != nil {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
	} else if reg.Spec.Account != req.Object.Spec.AccountId {
		if err := r.applyRegion(req); err != nil {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
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
	err := r.Client.List(
		ctx, &nodePools, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				apiLabels.Set{
					"kloudlite.io/edge-ref": req.Object.Name,
				},
			),
		},
	)

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(RegionReady, check, err.Error())
		}
		if err := r.UpdatePool(req); err != nil {
			return req.CheckFailed(PoolReady, check, err.Error())
		}
	} else if len(nodePools.Items) != len(req.Object.Spec.Pools) {

		if err := r.UpdatePool(req); err != nil {
			return req.CheckFailed(PoolReady, check, err.Error())
		}
	} else {

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

	}

	check.Status = true
	if check != checks[PoolReady] {
		checks[PoolReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *EdgeReconciler) applyRegion(req *rApi.Request[*infrav1.Edge]) error {

	if b, err := templates.Parse(
		templates.Region, map[string]any{
			"name":     req.Object.Name,
			"account":  req.Object.Spec.AccountId,
			"provider": req.Object.Spec.Provider,
		},
	); err != nil {
		return err
	} else if _, err = functions.KubectlApplyExec(b); err != nil {
		return err
	}

	return nil
}

func (r *EdgeReconciler) UpdatePool(req *rApi.Request[*infrav1.Edge]) error {
	b, err := templates.Parse(
		templates.NodePools, map[string]any{"pools": func() []infrav1.NodePool {
			pls := make([]infrav1.NodePool, 0)
			for _, p := range req.Object.Spec.Pools {

				pls = append(
					pls, infrav1.NodePool{
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
					},
				)
			}
			return pls
		}(),
		},
	)

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
	// if err := func() error {
	// 	_, err := rApi.Get(
	// 		req.Context(), r.Client, types.NamespacedName{
	// 			Name: req.Object.Name,
	// 		}, &managementv1.Region{},
	// 	)
	//
	// 	if err != nil {
	// 		if !apiErrors.IsNotFound(err) {
	// 			return err
	// 		}
	// 		return nil
	// 	}
	//
	// 	return r.Delete(req.Context(), req.Object)
	//
	// }(); err != nil {
	// 	return req.FailWithStatusError(err)
	// }

	checkName := "NodePoolsDeleted"

	check := rApi.Check{Generation: req.Object.Generation}

	var nodePools infrav1.NodePoolList
	if err := r.List(
		req.Context(), &nodePools, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				map[string]string{
					constants.EdgeRef: req.Object.Name,
				},
			),
		},
	); err != nil {
		return req.CheckFailed(checkName, check, err.Error())
	}

	if len(nodePools.Items) != 0 {
		return req.Done()
	}

	// np, err := rApi.Get(
	// 	req.Context(), r.Client, types.NamespacedName{
	// 		Name: req.Object.Name,
	// 	}, &infrav1.NodePool{},
	// )
	// if err != nil {
	// 	if apiErrors.IsNotFound(err) {
	// 		return req.Finalize()
	// 	}
	// }
	//
	// if err := r.Delete(ctx, np); err != nil {
	// 	return req.CheckFailed("NodePoolDeleted", rApi.Check{Generation: req.Object.Generation}, err.Error())
	// }

	// check is pool present
	// if err := func() error {
	// 	_, err := rApi.Get(
	// 		req.Context(), r.Client, types.NamespacedName{
	// 			Name: req.Object.Name,
	// 		}, &infrav1.NodePool{},
	// 	)
	//
	// 	if err != nil {
	// 		if !apiErrors.IsNotFound(err) {
	// 			return err
	// 		}
	// 		return nil
	// 	}
	//
	// 	_, err = functions.ExecCmd(fmt.Sprintf("kubectl delete nodepool -l kloudlite.io/edge-ref", req.Object.Name), "")
	// 	return err
	// }(); err != nil {
	// 	return req.FailWithStatusError(err)
	// }

	// TODO: (watch for all nodepools to be deleted, prior to releasing finalizers)
	return req.Finalize()
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
