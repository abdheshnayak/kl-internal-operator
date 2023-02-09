package infra

import (
	"context"
	"fmt"
	"strconv"
	"time"

	infrav1 "github.com/kloudlite/internal_operator_v2/apis/infra/v1"
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	"github.com/kloudlite/internal_operator_v2/lib/functions"
	"github.com/kloudlite/internal_operator_v2/lib/kresource"
	"github.com/kloudlite/internal_operator_v2/lib/logging"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	stepResult "github.com/kloudlite/internal_operator_v2/lib/operator.v2/step-result"
	"github.com/kloudlite/internal_operator_v2/lib/rcalculate"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type NodePoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logging.Logger
	Name   string
}

func (r *NodePoolReconciler) GetName() string {
	return r.Name
}

const (
	ReconcilationPeriod time.Duration = 30
)

const (
	WorkerNodesReady   string = "worker-nodes-ready"
	WorkerNodesDeleted string = "worker-nodes-deleted"
)

// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=nodepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=nodepools/finalizers,verbs=update

func (r *NodePoolReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	req, err := rApi.NewRequest(context.WithValue(ctx, constants.LoggerConst, r.logger), r.Client, request.NamespacedName, &infrav1.NodePool{})
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

	if step := r.reconWorkerNodes(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *NodePoolReconciler) finalize(req *rApi.Request[*infrav1.NodePool]) stepResult.Result {
	ctx, obj := req.Context(), req.Object

	check := rApi.Check{Generation: obj.Generation}

	var workerNodes infrav1.WorkerNodeList
	if err := r.Client.List(
		ctx, &workerNodes, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				map[string]string{constants.RegionKey: obj.Spec.EdgeRef},
			),
		},
	); err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(WorkerNodesDeleted, check, err.Error())
		}
		return req.CheckFailed(WorkerNodesDeleted, check, err.Error())
	}

	if len(workerNodes.Items) != 0 {
		return req.Done()
	}

	// if len(accountNodes.Items) >= 1 {
	// 	if err := r.DeleteAllOf(
	// 		ctx, &infrav1.AccountNode{}, &client.DeleteAllOfOptions{
	// 			ListOptions: client.ListOptions{
	// 				LabelSelector: apiLabels.SelectorFromValidatedSet(
	// 					map[string]string{
	// 						constants.RegionKey: obj.Spec.EdgeRef,
	// 					},
	// 				),
	// 			},
	// 		},
	// 	); err != nil {
	// 		return req.CheckFailed(AccountNodesDeleted, check, err.Error())
	// 	}
	// 	checks[AccountNodesDeleted] = check
	// 	return req.UpdateStatus()
	// }

	// if len(accountNodes.Items) != 0 {
	// 	return req.Done()
	// }

	// TODO: (FIXED) (watch for all nodepools to be deleted, prior to releasing finalizers)
	return req.Finalize()
}

func (r *NodePoolReconciler) reconWorkerNodes(req *rApi.Request[*infrav1.NodePool]) stepResult.Result {
	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks

	check := rApi.Check{Generation: obj.Generation}

	var workerNodes infrav1.WorkerNodeList
	if err := r.List(
		ctx, &workerNodes, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				apiLabels.Set{
					constants.RegionKey: obj.Spec.EdgeRef,
				},
			),
		},
	); err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(WorkerNodesReady, check, err.Error())
		}
	}

	for _, an := range workerNodes.Items {
		if an.DeletionTimestamp != nil || !an.Status.IsReady {
			return req.CheckFailed(WorkerNodesReady, check, "waiting for deletion/creation of nodes to complete...")
		}
	}

	// totalAvailableRes, err := kresource.GetTotalResource(
	// 	map[string]string{
	// 		constants.NodePoolKey: req.Object.Name,
	// 	},
	// )
	// if err != nil {
	// 	return req.CheckFailed(AccountNodesReady, check, err.Error())
	// }

	var nodes corev1.NodeList
	if err := r.List(
		ctx, &nodes, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(
				apiLabels.Set{
					constants.RegionKey: obj.Spec.EdgeRef,
				},
			),
		},
	); err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.CheckFailed(WorkerNodesReady, check, err.Error())
		}
	}

	totalUsedRes, err := kresource.GetTotalPodRequest(
		map[string]string{
			constants.RegionKey: obj.Spec.EdgeRef,
		}, "requests",
	)
	if err != nil {
		return req.CheckFailed(WorkerNodesReady, check, err.Error())
	}

	totalStatefulUsedRes, err := kresource.GetTotalPodRequest(
		map[string]string{
			constants.RegionKey:          obj.Spec.EdgeRef,
			"kloudlite.io/stateful-node": "true",
		}, "requests",
	)
	if err != nil {
		return req.CheckFailed(WorkerNodesReady, check, err.Error())
	}

	i := rcalculate.Input{
		MinNode: obj.Spec.Min,
		MaxNode: obj.Spec.Max,
		Nodes: func() []rcalculate.Node {
			rk := make([]rcalculate.Node, 0)
			for _, n := range nodes.Items {
				rk = append(
					rk, rcalculate.Node{
						Name:     n.Name,
						Stateful: n.GetLabels()["kloudlite.io/stateful-node"] == "true",
						Size: rcalculate.Size{
							Memory: n.Status.Allocatable.Memory().String(),
							Cpu:    n.Status.Allocatable.Cpu().String(),
						},
					},
				)
			}
			return rk
		}(),
		StatefulUsed: totalStatefulUsedRes.Memory,
		TotalUsed:    totalUsedRes.Memory,
		Threshold:    80,
	}

	fmt.Println("Scale-> ", obj.Name)
	action, msg, err := i.Calculate()
	if err != nil {
		return req.CheckFailed(WorkerNodesReady, check, err.Error())
	}

	r.logger.Infof("\n\n\n%d: %s (%s)\n\n\n", action, *msg, req.Object.Name)

	if true {
		switch action {
		case rcalculate.ADD_NODE:
			{
				if err := r.Client.Create(
					ctx, &infrav1.WorkerNode{
						ObjectMeta: metav1.ObjectMeta{
							Name: string(uuid.NewUUID()),
							Labels: apiLabels.Set{
								constants.RegionKey: obj.Spec.EdgeRef,
							},
							OwnerReferences: []metav1.OwnerReference{functions.AsOwner(obj, true)},
						},
						Spec: infrav1.WorkerNodeSpec{
							ProviderRef: obj.Spec.ProviderRef,
							AccountRef:  obj.Spec.AccountRef,
							EdgeRef:     obj.Spec.EdgeRef,
							Provider:    obj.Spec.Provider,
							Config:      obj.Spec.Config,
							Region:      obj.Spec.Region,
							Pool:        obj.Name,
							Index: func() int {
								ind := len(nodes.Items)
								for i := 0; i < ind; i++ {
									found := false
									for _, n := range nodes.Items {
										index, ok := n.GetLabels()["kloudlite.io/node-index"]
										if !ok {
											continue
										}
										in, err := strconv.ParseInt(index, 10, 32)
										if err != nil {
											continue
										}
										if i == int(in) {
											found = true
											break
										}
									}
									if !found {
										return i
									}
								}
								return ind
							}(),
						},
					},
				); err != nil {
					return req.CheckFailed(WorkerNodesReady, check, err.Error())
				}
				return req.Done()
			}

		case rcalculate.DEL_NODE:
			{
				// find last node and delete

				if len(workerNodes.Items) > 0 {

					last := 0
					for _, n := range workerNodes.Items {
						if last < n.Spec.Index {
							last = n.Spec.Index
						}
					}

					for _, n := range workerNodes.Items {
						if n.Spec.Index == last {
							if err := r.Delete(
								ctx, &infrav1.WorkerNode{
									ObjectMeta: metav1.ObjectMeta{
										Name: n.Name,
										// Namespace: n.Namespace,
									},
								},
							); err != nil {
								return req.CheckFailed(WorkerNodesReady, check, err.Error())
							}
							break
						}
					}
					return req.Done()
				}
			}
		case rcalculate.ADD_STATEFUL:
			{
				cnt := i.GetStatefulCount()
				for _, n := range nodes.Items {
					if n.GetLabels()["kloudlite.io/node-index"] == fmt.Sprint(cnt) {
						ctrl.CreateOrUpdate(
							ctx, r.Client, &n, func() error {
								l := n.GetLabels()
								l["kloudlite.io/stateful-node"] = "true"
								n.SetLabels(l)
								return nil
							},
						)

						break
					}
				}
			}
		case 0:
			{
				req.Logger.Infof("workerNodes in sync...")
				check.Status = true
			}
		}
	}

	check.Status = true
	if check != checks[WorkerNodesReady] {
		checks[WorkerNodesReady] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *NodePoolReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.logger = logger.WithName(r.Name)
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()

	builder := ctrl.NewControllerManagedBy(mgr).For(&infrav1.NodePool{})
	builder.Owns(&infrav1.WorkerNode{})

	watchList := []client.Object{
		&corev1.Node{},
		&corev1.Pod{},
	}

	for i := range watchList {
		builder.Watches(
			&source.Kind{Type: watchList[i]}, handler.EnqueueRequestsFromMapFunc(
				func(o client.Object) []reconcile.Request {
					l, ok := o.GetLabels()[constants.RegionKey]
					if !ok {
						return nil
					}
					return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: l}}}
				},
			),
		)
	}

	return builder.Complete(r)
}
