package commoncontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	managementv1 "operators.kloudlite.io/apis/management/v1"

	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/nameserver"
	rApi "operators.kloudlite.io/lib/operator"
)

// DomainReconciler reconciles a Domain object
type DomainReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=management.kloudlite.io,resources=domains,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=domains/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=management.kloudlite.io,resources=domains/finalizers,verbs=update

func (r *DomainReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &managementv1.Domain{})

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

func (r *DomainReconciler) finalize(req *rApi.Request[*managementv1.Domain]) rApi.StepResult {

	fmt.Println("finalize")

	// deleting domain record
	if err := func() error {
		endpoint := os.Getenv("NAMESERVER_ENDPOINT")

		if endpoint == "" {
			return fmt.Errorf("NAMESERVER_ENDPOINT not found in environment")
		}

		dns := nameserver.NewClient(endpoint)

		err := dns.DeleteDomain(req.Object.Spec.Name)

		return err

	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	return req.Finalize()
}

// domain management
func (r *DomainReconciler) reconcileStatus(req *rApi.Request[*managementv1.Domain]) rApi.StepResult {
	fmt.Println("DOMAIN RECONCILATION------------------")

	isReady := true

	// check if record present
	if err, notMatched := func() (error, bool) {

		endpoint := os.Getenv("NAMESERVER_ENDPOINT")

		if endpoint == "" {
			return fmt.Errorf("NAMESERVER_ENDPOINT not found in environment"), false
		}

		labels := req.Object.GetLabels()

		if labels == nil || labels["kloudlite.io/wg-domain"] == "" {
			return nil, false
		}

		res, err := http.Get(fmt.Sprintf("%s/get-records/%s", endpoint, req.Object.Spec.Name))

		if err != nil {
			return err, false
		}

		body, err := ioutil.ReadAll(res.Body)

		if err != nil {
			return err, false
		}

		var ips struct {
			Answers []string `json:"answers"`
		}

		if err = json.Unmarshal(body, &ips); err != nil {
			fmt.Println("cant find parse the ips from the response of nameserver")
		}

		// if len(ips.Answers) == 0 {
		// 	return err
		// }

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

		dns := nameserver.NewClient(endpoint)

		if err = dns.UpsertDomain(req.Object.Spec.Name, req.Object.Spec.Ips); err != nil {
			return err, notMatched
		}

		return nil, notMatched
	}(); err != nil {
		req.FailWithStatusError(err)
	} else if notMatched {
		fmt.Println("Domain IPS not updated on server")
		return req.Done(&ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5})
	}

	if req.Object.Status.IsReady != isReady {

		req.Object.Status.IsReady = isReady

		if err := r.Status().Update(req.Context(), req.Object); err != nil {
			return req.FailWithStatusError(err)
		}

	}

	return req.Done()
}

func (r *DomainReconciler) reconcileOperations(req *rApi.Request[*managementv1.Domain]) rApi.StepResult {
	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *DomainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		For(&managementv1.Domain{}).
		Complete(r)
}
