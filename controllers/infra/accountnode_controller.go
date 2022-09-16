package infra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	// "k8s.io/apimachinery/pkg/types"
	"operators.kloudlite.io/lib/conditions"
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

	// corev1 "k8s.io/api/core/v1"
	// apiErrors "k8s.io/apimachinery/pkg/api/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	infrav1 "operators.kloudlite.io/apis/infra/v1"
)

// AccountNodeReconciler reconciles a AccountNode object
type AccountNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=accountnodes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AccountNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *AccountNodeReconciler) Reconcile(ctx context.Context, oReq ctrl.Request) (ctrl.Result, error) {

	req := rApi.NewRequest(ctx, r.Client, oReq.NamespacedName, &infrav1.AccountNode{})

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

	var cs []metav1.Condition

	// ensure creation job deleted
	if err := func() error {
		functions.KubectlDelete("kl-core", fmt.Sprintf("job/%s", req.Object.Name))

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check if deletion job created, if created and completed remove finalizers
	if err, done := func() (error, bool) {

		job, err := rApi.Get(req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      fmt.Sprintf("delete-%s", req.Object.Name),
				Namespace: "kl-core",
			},
			&batchv1.Job{},
		)

		if err != nil {
			cs = append(cs,
				conditions.New(
					"DeleteJobFound",
					false,
					"NotFound",
					"JoinSecret not found to attach",
				))
			return nil, false
		}

		for _, jc := range job.Status.Conditions {
			if jc.Type == "Complete" && jc.Status == "True" {
				return nil, true
			}
		}

		out, err := functions.ExecCmd(fmt.Sprintf("kubectl -n kl-core logs job/delete-%s", req.Object.Name), "")
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
		return req.Finalize()
	}

	// finding provider
	if err := func() error {
		provider, err := rApi.Get(req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      req.Object.Spec.ProviderRef,
				Namespace: "kl-core",
			},
			&infrav1.AccountProvider{},
		)

		if err != nil {
			if !apiErrors.IsNotFound(err) {
				return err
			}

			return fmt.Errorf("provider not found")
		}

		rApi.SetLocal(req, "provider", provider)
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// get provider secrets
	if err := func() error {
		if !rApi.HasLocal(req, "provider") {
			return nil
		}

		provider, ok := rApi.GetLocal[*infrav1.AccountProvider](req, "provider")
		if !ok {
			fmt.Println("error fetching provider")
			return nil
		}

		secret, err := rApi.Get(req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      provider.Spec.CredentialsRef.SecretName,
				Namespace: provider.Spec.CredentialsRef.Namespace,
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

		if !meta.IsStatusConditionFalse(cs, "DeleteJobFound") {
			return nil
		}

		secret, ok := rApi.GetLocal[*corev1.Secret](req, "secret")
		if !ok {
			return fmt.Errorf("can't fetch secret from local")
		}

		switch req.Object.Spec.Provider {
		case "do":

			apiToken, ok := secret.Data["apiToken"]
			if !ok {
				return fmt.Errorf("apiToken not provided in provider secret")
			}

			var doNodeConfig struct {
				Region  string `json:"region"`
				Size    string `json:"size"`
				ImageId string `json:"imageId"`
			}

			if err := json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&doNodeConfig,
			); err != nil {
				return err
			}

			klConfig := doKLConf{
				Version: "v1",
				Values: doKLConfValues{
					ServerUrl:   "not_required_to_delete",
					SshKeyPath:  os.Getenv("SSH_KEY_PATH"),
					StorePath:   os.Getenv("STORE_PATH"),
					TfTemplates: os.Getenv("TF_TEMPLATES_PATH"),
					JoinToken:   "not_required_to_delete",
				},
			}

			// if any of the environment not provided panic
			if klConfig.Values.SshKeyPath == "" ||
				klConfig.Values.StorePath == "" ||
				klConfig.Values.TfTemplates == "" {
				return fmt.Errorf("no all environments available to delete job")
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
						Region:  doNodeConfig.Region,
						Size:    doNodeConfig.Size,
						ImageId: doNodeConfig.ImageId,
						NodeId:  req.Object.Name,
						// NodeId:  req.Object.Name,
					},
				},
			}

			cYaml, err := yaml.Marshal(nodeConfig)
			if err != nil {
				return err
			}
			base64C := base64.StdEncoding.EncodeToString(cYaml)

			kYaml, err := yaml.Marshal(klConfig)
			if err != nil {
				return err
			}
			base64K := base64.StdEncoding.EncodeToString(kYaml)

			b, err := templates.Parse(templates.CreateNode, map[string]any{
				"name":       fmt.Sprintf("delete-%s", req.Object.Name),
				"namespace":  "kl-core",
				"labels":     req.Object.GetEnsuredLabels(),
				"nodeConfig": base64C,
				"klConfig":   base64K,
				"provider":   "do",
				"owner-refs": []metav1.OwnerReference{functions.AsOwner(req.Object, true)},
			})

			// fmt.Println(string(b))

			if err != nil {
				return err
			}

			_, err = functions.KubectlApplyExec(b)

			if err != nil {
				fmt.Println(err)
				return err
			}

		default:
			return fmt.Errorf("unknown provider")
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

	// finding Join Token
	if err := func() error {
		secret, err := rApi.Get(req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      "join-token",
				Namespace: "kl-core",
			},
			&corev1.Secret{},
		)

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false

			cs = append(cs,
				conditions.New(
					"JoinSecretFound",
					false,
					"NotFound",
					"JoinSecret not found to attach",
				),
			)

			return nil
		}

		joinToken := secret.Data["join-token"]
		if string(joinToken) == "" {
			isReady = false

			cs = append(cs,
				conditions.New(
					"JoinTokenFound",
					false,
					"NotFound",
					"JoinSecret not found to attach",
				),
			)
			return nil
		}

		serverUrl := secret.Data["serverUrl"]
		if string(serverUrl) == "" {
			isReady = false

			cs = append(cs,
				conditions.New(
					"ServerUrlFound",
					false,
					"NotFound",
					"ServerUrl not found to attach",
				),
			)
			return nil
		}

		rApi.SetLocal(req, "join-token", string(joinToken))
		rApi.SetLocal(req, "serverUrl", string(serverUrl))
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// finding provider
	if err := func() error {
		provider, err := rApi.Get(req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      req.Object.Spec.ProviderRef,
				Namespace: "kl-core",
			},
			&infrav1.AccountProvider{},
		)

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false

			cs = append(cs,
				conditions.New(
					"ProviderFound",
					false,
					"NotFound",
					"Provider not found",
				),
			)

			return nil
		}

		rApi.SetLocal(req, "provider", provider)
		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// get provider secrets
	if err := func() error {
		if !rApi.HasLocal(req, "provider") {
			return nil
		}

		provider, ok := rApi.GetLocal[*infrav1.AccountProvider](req, "provider")
		if !ok {
			fmt.Println("error fetching provider")
			return nil
		}

		secret, err := rApi.Get(req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      provider.Spec.CredentialsRef.SecretName,
				Namespace: provider.Spec.CredentialsRef.Namespace,
			},
			&corev1.Secret{},
		)

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			isReady = false

			cs = append(cs,
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
		if !rApi.HasLocal(req, "provider") || !rApi.HasLocal(req, "secret") {
			return nil
		}

		_, err := rApi.Get(req.Context(),
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

			cs = append(cs,
				conditions.New(
					"NodeFound",
					false,
					"NotFound",
					"node not created yet",
				),
			)

			return nil
		}

		return nil
	}(); err != nil {
		return req.FailWithStatusError(err)
	}

	// check if job created
	if err := func() error {

		if !rApi.HasLocal(req, "provider") || !rApi.HasLocal(req, "secret") {
			return nil
		}

		job, err := rApi.Get(req.Context(),
			r.Client,
			types.NamespacedName{
				Name:      req.Object.Name,
				Namespace: "kl-core",
			},
			&batchv1.Job{},
		)

		if err != nil {

			if !apiErrors.IsNotFound(err) {
				return err
			}

			if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "NodeFound") {
				return nil
			}

			isReady = false

			cs = append(cs,
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
				return functions.KubectlDelete("kl-core",
					fmt.Sprintf("job/%s", req.Object.Name))
			}
		}

		out, err := functions.ExecCmd(fmt.Sprintf("kubectl -n kl-core logs job/%s", req.Object.Name), "")
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
			var doNodeConfig struct {
				Region  string `json:"region"`
				Size    string `json:"size"`
				ImageId string `json:"imageId"`
			}

			if err := json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&doNodeConfig,
			); err != nil {
				return err
			}

			if doNodeConfig.Region == "" ||
				doNodeConfig.Size == "" ||
				doNodeConfig.ImageId == "" {
				isReady = false

				cs = append(cs,
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

func (r *AccountNodeReconciler) reconcileOperations(req *rApi.Request[*infrav1.AccountNode]) rApi.StepResult {

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

		joinToken, ok := rApi.GetLocal[string](req, "join-token")
		if !ok {
			return fmt.Errorf("join token not found")
		}

		serverUrl, ok := rApi.GetLocal[string](req, "serverUrl")
		if !ok {
			return fmt.Errorf("serverUrl not found")
		}

		secret, ok := rApi.GetLocal[*corev1.Secret](req, "secret")
		if !ok {
			return fmt.Errorf("can't fetch secret from local")
		}

		switch req.Object.Spec.Provider {
		case "do":

			apiToken := secret.Data["apiToken"]
			if string(apiToken) == "" {
				return fmt.Errorf("apiToken not provided in provider secret")
			}

			var doNodeConfig struct {
				Region  string `json:"region"`
				Size    string `json:"size"`
				ImageId string `json:"imageId"`
			}

			if err := json.Unmarshal(
				[]byte(req.Object.Spec.Config),
				&doNodeConfig,
			); err != nil {
				return err
			}

			klConfig := doKLConf{
				Version: "v1",
				Values: doKLConfValues{
					ServerUrl:   serverUrl,
					SshKeyPath:  os.Getenv("SSH_KEY_PATH"),
					StorePath:   os.Getenv("STORE_PATH"),
					TfTemplates: os.Getenv("TF_TEMPLATES_PATH"),
					JoinToken:   joinToken,
				},
			}

			// if any of the environment not provided panic
			if klConfig.Values.ServerUrl == "" ||
				klConfig.Values.SshKeyPath == "" ||
				klConfig.Values.StorePath == "" ||
				klConfig.Values.TfTemplates == "" ||
				klConfig.Values.JoinToken == "" {
				return fmt.Errorf("no all environments available to create job")
			}

			nodeConfig := doConfig{
				Version:  "v1",
				Action:   "create",
				Provider: "do",
				Spec: doSpec{
					Provider: doProvider{
						ApiToken:  string(apiToken),
						AccountId: req.Object.Spec.AccountRef,
					},
					Node: doNode{
						Region:  doNodeConfig.Region,
						Size:    doNodeConfig.Size,
						ImageId: doNodeConfig.ImageId,
						NodeId:  req.Object.Name,
					},
				},
			}

			cYaml, err := yaml.Marshal(nodeConfig)
			if err != nil {
				return err
			}
			base64C := base64.StdEncoding.EncodeToString(cYaml)

			kYaml, err := yaml.Marshal(klConfig)
			if err != nil {
				return err
			}
			base64K := base64.StdEncoding.EncodeToString(kYaml)

			b, err := templates.Parse(templates.CreateNode, map[string]any{
				"name":       req.Object.Name,
				"namespace":  "kl-core",
				"labels":     req.Object.GetEnsuredLabels(),
				"nodeConfig": base64C,
				"klConfig":   base64K,
				"provider":   "do",
				"owner-refs": []metav1.OwnerReference{functions.AsOwner(req.Object, true)},
			})

			// fmt.Println(string(b))

			if err != nil {
				return err
			}

			_, err = functions.KubectlApplyExec(b)

			if err != nil {
				fmt.Println(err)
				return err
			}

		default:
			return fmt.Errorf("unknown provider")
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// do some task here
	if err := func() error {
		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	return req.Done()
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AccountNode{}).
		Owns(&batchv1.Job{}).
		Watches(&source.Kind{Type: &corev1.Node{}}, handler.EnqueueRequestsFromMapFunc(
			func(o client.Object) []reconcile.Request {
				l, ok := o.GetLabels()["kloudlite.io/account-node.name"]
				if !ok {
					return nil
				}
				return []reconcile.Request{{NamespacedName: functions.NN("kl-core", l)}}
			},
		)).
		Complete(r)
}

// terraform destroy -auto-approve -var=accountId="kl-core" -var=do-image-id="ubuntu-18-04-x64" -var=nodeId="kl-sample-node" -var=cluster-id="kl" -var=keys-path="/home/nonroot/ssh" -var=do-token="dop_v1_c70d104e3383f73a3bf0becf5119a615cb069b927752afd504a7c7f56e87c458"
