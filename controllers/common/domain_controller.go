package commoncontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	managementv1 "github.com/kloudlite/internal_operator_v2/apis/management/v1"
	"github.com/kloudlite/internal_operator_v2/env"

	"github.com/kloudlite/internal_operator_v2/lib/constants"
	"github.com/kloudlite/internal_operator_v2/lib/logging"
	"github.com/kloudlite/internal_operator_v2/lib/nameserver"

	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	stepResult "github.com/kloudlite/internal_operator_v2/lib/operator.v2/step-result"
)

// DomainReconciler reconciles a Domain object
type DomainReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logging.Logger
	Name   string
	Env    *env.Env
}

const (
	ReconcilationPeriod time.Duration = 30
)

const (
	RecoardUpToDate string = "record-up-to-date"
)

// +kubebuilder:rbac:groups=management.kloudlite.io,resources=domains,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=domains/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=domains/finalizers,verbs=update

func (r *DomainReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	req, err := rApi.NewRequest(context.WithValue(ctx, "logger", r.logger), r.Client, request.NamespacedName, &managementv1.Domain{})
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if step := req.EnsureChecks(RecoardUpToDate); !step.ShouldProceed() {
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

	if step := r.reconUpdateRecord(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func (r *DomainReconciler) reconUpdateRecord(req *rApi.Request[*managementv1.Domain]) stepResult.Result {
	obj, checks := req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}

	if _, ok := req.Object.GetLabels()["kloudlite.io/wg-domain"]; !ok {
		return req.CheckFailed(RecoardUpToDate, check, "wg-domain not provided in labels of")
	}

	ns := nameserver.NewClient(r.Env.NameserverEndpoint, r.Env.NameserverUser, r.Env.NameserverPassword)

	res, err := ns.GetRecord(req.Object.Spec.Name)

	if err != nil {
		return req.CheckFailed(RecoardUpToDate, check, err.Error())
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return req.CheckFailed(RecoardUpToDate, check, err.Error())
	}

	var ips struct {
		Answers []string `json:"answers"`
	}

	if err = json.Unmarshal(body, &ips); err != nil {
		fmt.Println("cant find parse the ips from the response of nameserver")
	}

	c_ips := ips.Answers

	sort.Slice(
		c_ips, func(i, j int) bool {
			return c_ips[i] > c_ips[j]
		},
	)

	sort.Slice(
		req.Object.Spec.Ips, func(i, j int) bool {
			return req.Object.Spec.Ips[i] > req.Object.Spec.Ips[j]
		},
	)

	notMatched := false

	for i, v := range req.Object.Spec.Ips {
		if len(c_ips) <= i {
			notMatched = true
			break
		}
		if v != c_ips[i] {
			notMatched = true
			break
		}
	}

	if notMatched {

		dns := nameserver.NewClient(r.Env.NameserverEndpoint, r.Env.NameserverUser, r.Env.NameserverPassword)

		if err = dns.UpsertDomain(req.Object.Spec.Name, req.Object.Spec.Ips); err != nil {
			return req.CheckFailed(RecoardUpToDate, check, err.Error())
		}

	}

	check.Status = true
	if check != checks[RecoardUpToDate] {
		checks[RecoardUpToDate] = check
		return req.UpdateStatus()
	}

	return req.Next()
}

func (r *DomainReconciler) finalize(req *rApi.Request[*managementv1.Domain]) stepResult.Result {

	dns := nameserver.NewClient(r.Env.NameserverEndpoint, r.Env.NameserverUser, r.Env.NameserverPassword)
	if err := dns.DeleteDomain(req.Object.Spec.Name); err != nil {
		return req.FailWithStatusError(err)
	}

	return req.Finalize()
}

// SetupWithManager sets up the controller with the Manager.
func (r *DomainReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	r.logger = logger.WithName(r.Name)
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Domain{}).
		Complete(r)
}
