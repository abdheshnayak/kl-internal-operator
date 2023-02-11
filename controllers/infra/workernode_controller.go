package infra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/kloudlite/internal_operator_v2/lib/templates"
	"gopkg.in/yaml.v2"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/kloudlite/internal_operator_v2/apis/infra/v1"
	"github.com/kloudlite/internal_operator_v2/env"
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	"github.com/kloudlite/internal_operator_v2/lib/functions"
	"github.com/kloudlite/internal_operator_v2/lib/logging"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	stepResult "github.com/kloudlite/internal_operator_v2/lib/operator.v2/step-result"
	"github.com/kloudlite/internal_operator_v2/lib/terraform"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

/*
# actions needs to be performed
1. check if node created
2. if not created create
3. check if node attached
4. if not attached
5. if deletion timestamp present
	- delete node from the cluster
*/

// WorkerNodeReconciler reconciles a WorkerNode object
type WorkerNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	logger logging.Logger
	Name   string
	Env    *env.Env
}

func (r *WorkerNodeReconciler) GetName() string {
	return r.Name
}

const (
	NodeCreated  string = "node-created"
	NodeAttached string = "node-attached"
)

//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=workernodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=workernodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infra.kloudlite.io,resources=workernodes/finalizers,verbs=update

func (r *WorkerNodeReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	req, err := rApi.NewRequest(rApi.NewReconcilerCtx(ctx, r.logger), r.Client, request.NamespacedName, &infrav1.WorkerNode{})
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if step := req.EnsureChecks(NodeCreated, NodeAttached); !step.ShouldProceed() {
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

	// if step := r.reconWorkerNodes(req); !step.ShouldProceed() {
	// 	return step.ReconcilerResponse()
	// }

	if step := r.fetchRequired(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.ensureNodeCreated(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	if step := r.ensureNodeAttached(req); !step.ShouldProceed() {
		return step.ReconcilerResponse()
	}

	req.Object.Status.IsReady = true
	req.Logger.Infof("RECONCILATION COMPLETE")
	return ctrl.Result{RequeueAfter: ReconcilationPeriod * time.Second}, r.Status().Update(ctx, req.Object)
}

func mNode(name string) string {
	return fmt.Sprintf("kl-byoc-%s", name)
}

func (r *WorkerNodeReconciler) getTFPath(obj *infrav1.WorkerNode) string {
	// eg -> /path/acc_id/do/blr1/node_id/do
	// eg -> /path/acc_id/aws/ap-south-1/node_id/aws
	return path.Join(r.Env.StorePath, obj.Spec.AccountName, obj.Spec.Provider, obj.Spec.Region, mNode(obj.Name), obj.Spec.Provider)
}

func (r *WorkerNodeReconciler) getJobCrd(req *rApi.Request[*infrav1.WorkerNode], create bool) ([]byte, error) {

	ctx, obj := req.Context(), req.Object

	providerSec, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Name:      obj.Spec.ProviderName,
			Namespace: constants.MainNs,
		},
		&corev1.Secret{},
	)

	if err != nil {
		return nil, err
	}

	klConfig := KLConf{
		Version: "v1",
		Values: KLConfValues{
			StorePath:   r.Env.StorePath,
			TfTemplates: r.Env.TFTemplatesPath,
			SSHPath:     r.Env.SSHPath,
		},
	}

	klConfigJsonBytes, err := yaml.Marshal(klConfig)
	if err != nil {
		return nil, err
	}

	var config string
	switch obj.Spec.Provider {
	case "aws":

		accessKey, ok := providerSec.Data["accessKey"]
		if !ok {
			return nil, fmt.Errorf("AccessKey not provided in provider secret")
		}

		accessSecret, ok := providerSec.Data["accessSecret"]
		if !ok {
			return nil, fmt.Errorf("AccessSecret not provided in provider secret")
		}

		var awsNodeConfig awsNode
		if err = json.Unmarshal(
			[]byte(req.Object.Spec.Config),
			&awsNodeConfig,
		); err != nil {
			return nil, err
		}

		nodeConfig := awsConfig{
			Version: "v1",
			Action: func() string {
				if create {
					return "create"
				}

				return "delete"
			}(),
			Provider: "aws",
			Spec: awsSpec{
				Provider: awsProvider{
					AccessKey:    string(accessKey),
					AccessSecret: string(accessSecret),
					AccountName:  obj.Spec.AccountName,
				},
				Node: awsNode{
					NodeId:       mNode(obj.Name),
					Region:       obj.Spec.Region,
					InstanceType: awsNodeConfig.InstanceType,
					VPC:          awsNodeConfig.VPC,
				},
			},
		}

		cYaml, e := yaml.Marshal(nodeConfig)
		if e != nil {
			return nil, e
		}
		config = base64.StdEncoding.EncodeToString(cYaml)

	case "do":

		apiToken, ok := providerSec.Data["apiToken"]
		if !ok {
			return nil, fmt.Errorf("apiToken not provided in provider secret")
		}

		var doNodeConfig doNode
		if e := json.Unmarshal(
			[]byte(req.Object.Spec.Config),
			&doNodeConfig,
		); e != nil {
			return nil, e
		}

		nodeConfig := doConfig{
			Version: "v1",
			Action: func() string {
				if create {
					return "create"
				}

				return "delete"
			}(),
			Provider: req.Object.Spec.Provider,
			Spec: doSpec{
				Provider: doProvider{
					ApiToken:    string(apiToken),
					AccountName: req.Object.Spec.AccountName,
				},
				Node: doNode{
					NodeId:  mNode(obj.Name),
					Region:  req.Object.Spec.Region,
					Size:    doNodeConfig.Size,
					ImageId: doNodeConfig.ImageId,
				},
			},
		}

		cYaml, e := yaml.Marshal(nodeConfig)
		if e != nil {
			return nil, e
		}
		config = base64.StdEncoding.EncodeToString(cYaml)

	default:
		return nil, fmt.Errorf("unknown provider %s found", obj.Spec.Provider)
	}

	jobOut, err := templates.Parse(templates.NodeJob, map[string]any{
		"name": func() string {
			if create {
				return fmt.Sprintf("create-node-%s", obj.Name)
			}
			return fmt.Sprintf("delete-node-%s", obj.Name)
		}(),
		"namespace":  constants.MainNs,
		"nodeConfig": config,
		"klConfig":   base64.StdEncoding.EncodeToString(klConfigJsonBytes),
		"provider":   obj.Spec.Provider,
		"ownerRefs":  []metav1.OwnerReference{functions.AsOwner(obj, true)},
	})

	if err != nil {
		return nil, err
	}

	return jobOut, nil
}

func (r *WorkerNodeReconciler) createNode(req *rApi.Request[*infrav1.WorkerNode]) error {

	ctx, obj := req.Context(), req.Object

	_, err := rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Name:      fmt.Sprintf("create-node-%s", obj.Name),
			Namespace: constants.MainNs,
		},
		&batchv1.Job{},
	)

	if err == nil {
		return fmt.Errorf("creation of node is in progress")
	}

	jobOut, err := r.getJobCrd(req, true)
	if err != nil {
		return err
	}

	if _, err = functions.KubectlApplyExec(jobOut); err != nil {
		return err
	}

	r.logger.Debugf("node scheduled to create")
	return nil
}

func (r *WorkerNodeReconciler) ensureNodeCreated(req *rApi.Request[*infrav1.WorkerNode]) stepResult.Result {

	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(NodeCreated, check, err.Error())
	}

	fmt.Println(r.getTFPath(obj))

	_, err := terraform.GetOutputs(r.getTFPath(obj))
	if err != nil {
		// node is created // needs to check its status
		if err := r.createNode(req); err != nil {
			return failed(err)
		}
	}

	_, err = rApi.Get(
		ctx, r.Client, types.NamespacedName{
			Name:      fmt.Sprintf("create-node-%s", obj.Name),
			Namespace: constants.MainNs,
		},
		&batchv1.Job{},
	)

	if err == nil {
		if err := r.Client.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("create-node-%s", obj.Name),
				Namespace: constants.MainNs,
			},
		}); err != nil {
			return failed(err)
		}
	}

	// TODO: ensure it is running

	check.Status = true
	if check != checks[NodeCreated] {
		checks[NodeCreated] = check
		return req.UpdateStatus()
	}
	return req.Next()
}

func (r *WorkerNodeReconciler) fetchRequired(req *rApi.Request[*infrav1.WorkerNode]) stepResult.Result {
	ctx, obj := req.Context(), req.Object

	// fetching kubeConfig
	if err := func() error {

		kubeConfig, err := rApi.Get(
			ctx, r.Client, types.NamespacedName{
				Name:      fmt.Sprintf("cluster-kubeconfig-%s", obj.Spec.ClusterName),
				Namespace: constants.MainNs,
			},
			&corev1.Secret{},
		)

		if err != nil {
			return err
		}

		rApi.SetLocal(req, "kubeconfig-sec", kubeConfig)

		return nil
	}(); err != nil {
		r.logger.Warnf(err.Error())
	}

	return req.Next()
}

// ensure it's attached to the cluster

func (r *WorkerNodeReconciler) attachNode(req *rApi.Request[*infrav1.WorkerNode]) error {
	obj := req.Object

	// get node token
	kubeConfigSec, ok := rApi.GetLocal[*corev1.Secret](req, "kubeconfig-sec")
	if !ok {
		return fmt.Errorf("cluster config not found in secret")
	}

	token := kubeConfigSec.Data["node-token"]
	serverIp := kubeConfigSec.Data["master-node-ip"]
	fmt.Println(string(token), string(serverIp), "*****************************************888")

	ip, err := terraform.GetOutput(r.getTFPath(obj), "node-ip")
	if err != nil {
		return err
	}

	labels := func() []string {
		l := []string{}
		for k, v := range map[string]string{
			"kloudlite.io/region":     obj.Spec.EdgeName,
			"kloudlite.io/node.index": fmt.Sprint(obj.Spec.Index),
		} {
			l = append(l, fmt.Sprintf("--node-label %s=%s", k, v))
		}
		l = append(l, fmt.Sprintf("--node-label %s=%s", "kloudlite.io/public-ip", string(ip)))
		return l
	}()

	cmd := fmt.Sprintf(
		"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i %s root@%s sudo sh /tmp/k3s-install.sh agent --token=%s --server https://%s:6443 --node-external-ip %s --node-name %s %s",
		r.Env.SSHPath,
		ip,
		strings.TrimSpace(string(token)),
		serverIp,
		ip,
		mNode(obj.Name),
		strings.Join(labels, " "),
	)

	// fmt.Println("********************************************************************", cmd)

	_, err = functions.ExecCmd(cmd, "")
	if err != nil {
		return err
	}

	return nil
}

func (r *WorkerNodeReconciler) ensureNodeAttached(req *rApi.Request[*infrav1.WorkerNode]) stepResult.Result {

	ctx, obj, checks := req.Context(), req.Object, req.Object.Status.Checks
	check := rApi.Check{Generation: obj.Generation}
	failed := func(err error) stepResult.Result {
		return req.CheckFailed(NodeAttached, check, err.Error())
	}

	node, err := rApi.Get(ctx, r.Client, types.NamespacedName{
		Name: mNode(obj.Name),
	}, &corev1.Node{})

	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return failed(err)
		}

		if err := r.attachNode(req); err != nil {
			return failed(err)
		}

	}

	if isStateful, ok := node.Labels["kloudlite.io/stateful-node"]; !ok || isStateful != fmt.Sprint(obj.Spec.Stateful) {
		node.Labels["kloudlite.io/stateful-node"] = fmt.Sprint(obj.Spec.Stateful)
		if err = r.Client.Update(ctx, node); err != nil {
			return failed(err)
		}
	}

	// attached
	// if err := r.attachNode(req); err != nil {
	// 	return failed(err)
	// }

	check.Status = true
	if check != checks[NodeAttached] {
		checks[NodeAttached] = check
		return req.UpdateStatus()
	}
	return req.Next()
}

func (r *WorkerNodeReconciler) finalize(req *rApi.Request[*infrav1.WorkerNode]) stepResult.Result {
	return req.Finalize()
}

func (r *WorkerNodeReconciler) SetupWithManager(mgr ctrl.Manager, logger logging.Logger) error {
	r.logger = logger.WithName(r.Name)
	r.Client = mgr.GetClient()
	r.Scheme = mgr.GetScheme()

	builder := ctrl.NewControllerManagedBy(mgr).For(&infrav1.WorkerNode{})
	builder.Owns(&batchv1.Job{})

	return builder.Complete(r)
}
