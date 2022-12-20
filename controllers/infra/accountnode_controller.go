package infra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	apiLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"operators.kloudlite.io/lib/conditions"
	"operators.kloudlite.io/lib/constants"
	"operators.kloudlite.io/lib/functions"
	rApi "operators.kloudlite.io/lib/operator"
	"operators.kloudlite.io/lib/templates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	infrav1 "operators.kloudlite.io/apis/infra/v1"
	"operators.kloudlite.io/env"
)

// AccountNodeReconciler reconciles a AccountNode object
type AccountNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Env    *env.Env
}

const (
	JOB_NS   = "kl-core"
	TOKEN_NS = "kl-core"
)

const (
	KloudliteNs = "kl-core"
)

// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes/finalizers,verbs=update

func (r *AccountNodeReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &infrav1.AccountNode{})

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
		return x.Result(), x.Err()
	}

	if !controllerutil.ContainsFinalizer(req.Object, "klouldite-finalizer") {
		controllerutil.AddFinalizer(req.Object, "klouldite-finalizer")
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

func (r *AccountNodeReconciler) finalize(req *rApi.Request[*infrav1.AccountNode]) rApi.StepResult {
	ctx, obj := req.Context(), req.Object
	var cs []metav1.Condition

	createJob, err := rApi.Get(
		req.Context(), r.Client, types.NamespacedName{
			Name:      req.Object.Name,
			Namespace: JOB_NS,
		}, &batchv1.Job{},
	)
	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return req.FailWithStatusError(err)
		}
		createJob = nil
	}

	if createJob != nil {
		if createJob.Status.Active > 0 {
			obj.Status.Conditions = append(
				obj.Status.Conditions,
				conditions.New(
					"CreateJobFound",
					true,
					"WaitingToFinish",
					"Creation job already exists, waiting to create deletion job",
				),
			)
			if err := r.Update(ctx, obj); err != nil {
				return req.FailWithStatusError(err)
			}
			return req.Done()
		}
	}

	// check if deletion job created, if created and completed remove finalizers
	if err, done := func() (error, bool) {
		job, err := rApi.Get(
			req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      fmt.Sprintf("delete-%s", req.Object.Name),
				Namespace: JOB_NS,
			},
			&batchv1.Job{},
		)

		if err != nil {
			cs = append(
				cs,
				conditions.New(
					"DeleteJobFound",
					false,
					"NotFound",
					"Delete Job not created yet",
				),
			)
			return nil, false
		}

		for _, jc := range job.Status.Conditions {
			if jc.Type == "Complete" && jc.Status == "True" {
				return nil, true
			}
		}

		out, err := functions.ExecCmd(fmt.Sprintf("kubectl -n %s logs job/delete-%s", JOB_NS, req.Object.Name), "")
		if err != nil {
			fmt.Println(err.Error())
		}

		if strings.TrimSpace(string(out)) != "" {

			req.Object.Status.Message = string(out)

			err := r.Status().Update(req.Context(), req.Object)
			return err, false
		}

		return nil, false

	}(); err != nil {
		return req.FailWithStatusError(err)
	} else if done {
		fmt.Println("waiting 5 seconds before cleaning up")
		time.Sleep(time.Second * 5)
		return req.Finalize()
	}

	// finding edge
	// if err := func() error {
	// 	edge, err := rApi.Get(
	// 		req.Context(),
	// 		r.Client,
	// 		types.NamespacedName{
	// 			Name: req.Object.Spec.EdgeRef,
	// 		},
	// 		&infrav1.Edge{},
	// 	)
	//
	// 	if err != nil {
	// 		if !apiErrors.IsNotFound(err) {
	// 			return err
	// 		}
	// 		return fmt.Errorf("edge not found")
	// 	}
	//
	// 	rApi.SetLocal(req, "edge", edge)
	// 	return nil
	// }(); err != nil {
	// 	return req.FailWithStatusError(err)
	// }

	// get provider secrets
	if err := func() error {
		// if !rApi.HasLocal(req, "provider") {
		// 	return nil
		// }
		//
		// provider, ok := rApi.GetLocal[*infrav1.Edge](req, "provider")
		// if !ok {
		// 	fmt.Println("error fetching provider")
		// 	return nil
		// }

		secret, err := rApi.Get(
			req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      obj.Spec.ProviderRef,
				Namespace: KloudliteNS,
			},
			&corev1.Secret{},
		)

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			return fmt.Errorf("provider secret not found")
		}

		rApi.SetLocal(req, "secret", secret)

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// if deletion job not created create it
	if err := func() error {

		if _, err := rApi.Get(
			req.Context(), r.Client, types.NamespacedName{
				Name: fmt.Sprintf("kl-byoc-%s", req.Object.Name),
			}, &corev1.Node{},
		); err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
		}

		if !meta.IsStatusConditionFalse(cs, "DeleteJobFound") {
			return nil
		}

		secret, ok := rApi.GetLocal[*corev1.Secret](req, "secret")
		if !ok {
			return fmt.Errorf("can't fetch secret from local")
		}

		klConfig := KLConf{
			Version: "v1",
			Values: KLConfValues{
				StorePath:   os.Getenv("STORE_PATH"),
				TfTemplates: os.Getenv("TF_TEMPLATES_PATH"),
				Secrets:     "not_required_to_delete",
				SSHPath:     r.Env.SSHPath,
			},
		}

		// if any of the environment not provided panic
		if klConfig.Values.StorePath == "" ||
			klConfig.Values.TfTemplates == "" {
			return fmt.Errorf("no all environments available to delete job")

		}

		var b []byte
		var base64C string

		kYaml, err := yaml.Marshal(klConfig)
		if err != nil {
			return err
		}
		base64K := base64.StdEncoding.EncodeToString(kYaml)

		switch req.Object.Spec.Provider {
		case "do":

			apiToken, ok := secret.Data["apiToken"]
			if !ok {
				return fmt.Errorf("apiToken not provided in provider secret")
			}

			var doNodeConfig doNode

			if err = json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&doNodeConfig,
			); err != nil {
				return err
			}

			nodeConfig := doConfig{
				Version:  "v1",
				Action:   "delete",
				Provider: "do",
				Spec: doSpec{
					Provider: doProvider{
						ApiToken:  string(apiToken),
						AccountId: req.Object.Spec.AccountRef,
					},
					Node: doNode{
						Region:  req.Object.Spec.Region,
						Size:    doNodeConfig.Size,
						ImageId: doNodeConfig.ImageId,
						NodeId:  fmt.Sprintf("kl-byoc-%s", req.Object.Name),
					},
				},
			}

			cYaml, e := yaml.Marshal(nodeConfig)
			if e != nil {
				return e
			}
			base64C = base64.StdEncoding.EncodeToString(cYaml)

		case "aws":

			accessKey, ok := secret.Data["accessKey"]
			if !ok {
				return fmt.Errorf("AccessKey not provided in provider secret")
			}
			accessSecret, ok := secret.Data["accessSecret"]
			if !ok {
				return fmt.Errorf("AccessSecret not provided in provider secret")
			}

			var awsNodeConfig awsNode

			if err = json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&awsNodeConfig,
			); err != nil {
				return err
			}

			nodeConfig := awsConfig{
				Version:  "v1",
				Action:   "delete",
				Provider: "aws",
				Spec: awsSpec{
					Provider: awsProvider{
						AccessKey:    string(accessKey),
						AccessSecret: string(accessSecret),
						AccountId:    req.Object.Spec.AccountRef,
					},
					Node: awsNode{
						NodeId:       fmt.Sprintf("kl-byoc-%s", req.Object.Name),
						Region:       req.Object.Spec.Region,
						InstanceType: awsNodeConfig.InstanceType,
						VPC:          awsNodeConfig.VPC,
					},
				},
			}

			cYaml, e := yaml.Marshal(nodeConfig)
			if e != nil {
				return e
			}
			base64C = base64.StdEncoding.EncodeToString(cYaml)

		default:
			return fmt.Errorf("unknown provider")

		}

		b, err = templates.Parse(
			templates.CreateNode, map[string]any{
				"name":       fmt.Sprintf("delete-%s", req.Object.Name),
				"namespace":  JOB_NS,
				"labels":     req.Object.GetEnsuredLabels(),
				"taints":     []string{},
				"nodeConfig": base64C,
				"klConfig":   base64K,
				"provider":   req.Object.Spec.Provider,
				"owner-refs": []metav1.OwnerReference{functions.AsOwner(req.Object, true)},
			},
		)

		if err != nil {
			return err
		}

		// fmt.Println(string(b))

		if _, err = functions.KubectlApplyExec(b); err != nil {
			// fmt.Println(err)
			return err
		}

		return nil

	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	return req.Done()
}

func (r *AccountNodeReconciler) reconcileStatus(req *rApi.Request[*infrav1.AccountNode]) rApi.StepResult {

	// actions (action depends on provider) (best if create seperate crdfor diffrent providers)
	// check if provider present and ready
	// check if node present
	// check status of node
	// create if node not present
	// update node if values changed
	// update node if provider values changed
	// drain and delete node if crd deleted

	req.Object.Status.DisplayVars.Reset()
	var cs []metav1.Condition
	isReady := true
	// retry := false

	// finding Cluster Secret
	if err := func() error {
		secret, err := rApi.Get(
			req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      "cluster-secret",
				Namespace: TOKEN_NS,
			},
			&corev1.Secret{},
		)

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false

			cs = append(
				cs,
				conditions.New(
					"ClusterSecretFound",
					false,
					"NotFound",
					"Clsuter Secret not found to attach",
				),
			)

			return nil
		}

		clusterSecret := secret.Data["secrets.yml"]

		if string(clusterSecret) == "" {
			isReady = false

			cs = append(
				cs,
				conditions.New(
					"ClsutercSecretFound",
					false,
					"NotFound",
					"ClusterSecret not found to Create and Attach",
				),
			)
			return nil
		}

		rApi.SetLocal(req, "cluster-secret", string(clusterSecret))
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// // finding provider
	// if err := func() error {
	// 	provider, err := rApi.Get(
	// 		req.Context(),
	// 		r.Client,
	// 		types.NamespacedName{
	// 			Name: req.Object.Spec.EdgeRef,
	// 		},
	// 		&infrav1.Edge{},
	// 	)
	//
	// 	if err != nil {
	//
	// 		if !apiErrors.IsNotFound(err) {
	// 			return err
	// 		}
	//
	// 		isReady = false
	//
	// 		cs = append(
	// 			cs,
	// 			conditions.New(
	// 				"ProviderFound",
	// 				false,
	// 				"NotFound",
	// 				"Provider not found",
	// 			),
	// 		)
	//
	// 		return nil
	// 	}

	// rApi.SetLocal(req, "provider", provider)
	// 	return nil
	// }(); err != nil {
	// 	return req.FailWithStatusError(err)
	// }

	// get provider secrets
	if err := func() error {
		// if !rApi.HasLocal(req, "provider") {
		// 	return nil
		// }

		// provider, ok := rApi.GetLocal[*infrav1.Edge](req, "provider")
		// if !ok {
		// 	fmt.Println("error fetching provider")
		// 	return nil
		// }

		secret, err := rApi.Get(
			req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      req.Object.Spec.ProviderRef,
				Namespace: KloudliteNS,
			},
			&corev1.Secret{},
		)

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
			isReady = false
			cs = append(
				cs,
				conditions.New(
					"SecretFound",
					false,
					"NotFound",
					"Secret not found",
				),
			)

			return nil
		}

		rApi.SetLocal(req, "secret", secret)

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check node present and ready TODO: Ready check not implemented now
	if err := func() error {
		if !rApi.HasLocal(req, "secret") {
			return nil
		}

		_, err := rApi.Get(
			req.Context(),
			r.Client,
			types.NamespacedName{
				Name: fmt.Sprintf("kl-byoc-%s", req.Object.Name),
			},
			&corev1.Node{},
		)

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false

			cs = append(
				cs,
				conditions.New(
					"NodeFound",
					false,
					"NotFound",
					"node not created yet",
				),
			)

			return nil
		}

		if _, err := functions.ExecCmd(
			fmt.Sprintf(
				"kubectl taint nodes kl-byoc-%s kloudlite.io/region=%s:NoExecute --overwrite",
				req.Object.Name,
				req.Object.Spec.EdgeRef,
			), "",
		); err != nil {
			return err
		}

		// if _, err := functions.ExecCmd(fmt.Sprintf("kubectl taint nodes kl-byoc-%s kloudlite.io/provider-ref=%s:NoExecute --overwrite", req.Object.Name, req.Object.Spec.ProviderRef), ""); err != nil {
		// 	return err
		// }

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check if job created
	if err := func() error {

		if !rApi.HasLocal(req, "secret") {
			return nil
		}

		job, err := rApi.Get(
			req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      req.Object.Name,
				Namespace: JOB_NS,
			},
			&batchv1.Job{},
		)

		// fmt.Println(job)

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "NodeFound") {
				return nil
			}

			isReady = false

			cs = append(
				cs,
				conditions.New(
					"JobFound",
					false,
					"NotFound",
					"job not created yet",
				),
			)

			return nil
		}

		for _, jc := range job.Status.Conditions {
			if jc.Type == "Complete" && jc.Status == "True" {
				functions.ExecCmd(fmt.Sprintf("kubectl delete -n %s job/%s --grace-period=0 --force", JOB_NS, req.Object.Name), "")
			}
		}

		out, err := functions.ExecCmd(fmt.Sprintf("kubectl -n %s logs job/%s", JOB_NS, req.Object.Name), "")
		if err != nil {
			fmt.Println(err)
		}

		if strings.TrimSpace(string(out)) != "" {

			req.Object.Status.Message = string(out)

			return r.Status().Update(req.Context(), req.Object)
		}

		return nil

	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check if all data needed to create node provided
	if err := func() error {

		switch req.Object.Spec.Provider {
		case "do":
			var doNodeConfig doNode

			if err := json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&doNodeConfig,
			); err != nil {
				return err
			}

			if doNodeConfig.Size == "" {
				isReady = false

				cs = append(
					cs,
					conditions.New(
						"ConfigsAvailable",
						false,
						"NotFound",
						"All configs not provided to create do node",
					),
				)

				return nil
			}

		case "aws":

			var awsNodeConfig awsNode

			if err := json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&awsNodeConfig,
			); err != nil {
				return err
			}

			if awsNodeConfig.InstanceType == "" {
				isReady = false

				cs = append(
					cs,
					conditions.New(
						"ConfigsAvailable",
						false,
						"NotFound",
						"All configs not provided to create do node",
					),
				)

				return nil
			}

		default:
			return fmt.Errorf("unknown provider %q", req.Object.Spec.Provider)
		}
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

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

func (r *AccountNodeReconciler) reconcileOperations(req *rApi.Request[*infrav1.AccountNode]) rApi.StepResult {
	ctx := req.Context()

	// if job not created create one
	if err := func() error {

		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "NodeFound") {
			fmt.Println("node already created")
			return nil
		} else if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "JobFound") {
			fmt.Println("job created for node creation")
			return nil
		}
		fmt.Println("nither node found nor job created")

		clusterSecret, ok := rApi.GetLocal[string](req, "cluster-secret")
		if !ok {
			return fmt.Errorf("cluster secret not found")
		}

		secret, ok := rApi.GetLocal[*corev1.Secret](req, "secret")
		if !ok {
			return fmt.Errorf("can't fetch secret from local")
		}

		base64Secret := base64.StdEncoding.EncodeToString([]byte(clusterSecret))

		klConfig := KLConf{
			Version: "v1",
			Values: KLConfValues{
				StorePath:   os.Getenv("STORE_PATH"),
				TfTemplates: os.Getenv("TF_TEMPLATES_PATH"),
				Secrets:     base64Secret,
				SSHPath:     r.Env.SSHPath,
			},
		}

		// if any of the environment not provided panic
		if klConfig.Values.StorePath == "" ||
			klConfig.Values.TfTemplates == "" {
			return fmt.Errorf("no all environments available to create job")
		}

		kYaml, err := yaml.Marshal(klConfig)
		if err != nil {
			return err
		}
		base64K := base64.StdEncoding.EncodeToString(kYaml)

		taints := func() []string {
			s := make([]string, 0)
			// s = append(s, fmt.Sprintf("kloudlite.io/region=%s:NoExecute", doNodeConfig.Region))
			// s = append(s, fmt.Sprintf("kloudlite.io/account=%s:NoExecute", req.Object.Spec.AccountRef))
			// s = append(s, fmt.Sprintf("kloudlite.io/provider=%s:NoExecute", req.Object.Spec.Provider))

			s = append(
				s,
				fmt.Sprintf(
					"kloudlite.io/acc-edge-ref=%s:NoExecute",
					req.Object.Spec.EdgeRef,
				),
			)
			return s
		}()

		var base64C string

		switch req.Object.Spec.Provider {
		case "do":

			apiToken, ok := secret.Data["apiToken"]
			if !ok {
				return fmt.Errorf("apiToken not provided in provider secret")
			}

			var doNodeConfig doNode
			if e := json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&doNodeConfig,
			); e != nil {
				return e
			}

			nodeConfig := doConfig{
				Version:  "v1",
				Action:   "create",
				Provider: req.Object.Spec.Provider,
				Spec: doSpec{
					Provider: doProvider{
						ApiToken:  string(apiToken),
						AccountId: req.Object.Spec.AccountRef,
					},
					Node: doNode{
						Region:  req.Object.Spec.Region,
						Size:    doNodeConfig.Size,
						ImageId: doNodeConfig.ImageId,
						NodeId:  fmt.Sprintf("kl-byoc-%s", req.Object.Name),
					},
				},
			}

			cYaml, e := yaml.Marshal(nodeConfig)
			if e != nil {
				return e
			}
			base64C = base64.StdEncoding.EncodeToString(cYaml)

		case "aws":

			accessKey, ok := secret.Data["accessKey"]
			if !ok {
				return fmt.Errorf("AccessKey not provided in provider secret")
			}
			accessSecret, ok := secret.Data["accessSecret"]
			if !ok {
				return fmt.Errorf("AccessSecret not provided in provider secret")
			}

			var awsNodeConfig awsNode
			if err = json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&awsNodeConfig,
			); err != nil {
				return err
			}

			nodeConfig := awsConfig{
				Version:  "v1",
				Action:   "create",
				Provider: req.Object.Spec.Provider,
				Spec: awsSpec{
					Provider: awsProvider{
						AccessKey:    string(accessKey),
						AccessSecret: string(accessSecret),
						AccountId:    req.Object.Spec.AccountRef,
					},
					Node: awsNode{
						NodeId:       fmt.Sprintf("kl-byoc-%s", req.Object.Name),
						Region:       req.Object.Spec.Region,
						InstanceType: awsNodeConfig.InstanceType,
						VPC:          awsNodeConfig.VPC,
					},
				},
			}

			cYaml, e := yaml.Marshal(nodeConfig)
			if e != nil {
				return e
			}
			base64C = base64.StdEncoding.EncodeToString(cYaml)

		default:
			return fmt.Errorf("unknown provider")
		}

		var nodes corev1.NodeList
		if err = r.List(
			ctx, &nodes, &client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(
					apiLabels.Set{
						constants.NodePoolKey: req.Object.Name,
					},
				),
			},
		); err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}
		}

		l := req.Object.GetEnsuredLabels()

		l["kloudlite.io/node-index"] = fmt.Sprintf("%d", req.Object.Spec.Index)

		b, err := templates.Parse(
			templates.CreateNode, map[string]any{
				"name":       req.Object.Name,
				"namespace":  "kl-core",
				"labels":     req.Object.GetEnsuredLabels(),
				"taints":     taints,
				"nodeConfig": base64C,
				"klConfig":   base64K,
				"provider":   req.Object.Spec.Provider,
				"owner-refs": []metav1.OwnerReference{functions.AsOwner(req.Object, true)},
			},
		)

		// fmt.Println(string(b))

		if err != nil {
			return err
		}

		if _, err = functions.KubectlApplyExec(b); err != nil {
			fmt.Println(err)
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// do some task here
	// if err := func() error {
	// 	return nil
	// }(); err != nil {
	// 	return req.FailWithOpError(err)
	// }

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AccountNode{}).
		Owns(&batchv1.Job{}).
		Watches(
			&source.Kind{Type: &corev1.Node{}}, handler.EnqueueRequestsFromMapFunc(
				func(o client.Object) []reconcile.Request {
					l, ok := o.GetLabels()["kloudlite.io/account-node.name"]
					if !ok {
						return nil
					}

					return []reconcile.Request{{NamespacedName: types.NamespacedName{
						Name: l,
					}}}
				},
			),
		).
		Complete(r)
}
