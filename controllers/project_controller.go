package controllers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/kubernetes"
	crdsv1 "operators.kloudlite.io/api/v1"
	"operators.kloudlite.io/lib"
	"operators.kloudlite.io/lib/errors"
	"operators.kloudlite.io/lib/finalizers"
	reconcileResult "operators.kloudlite.io/lib/reconcile-result"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ProjectReconciler reconciles a Project object
type ProjectReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ClientSet      *kubernetes.Clientset
	SendMessage    func(key string, msg lib.MessageReply) error
	JobMgr         lib.Job
	logger         *zap.SugaredLogger
	HarborUserName string
	HarborPassword string
	resource       *crdsv1.Project
}

func (r *ProjectReconciler) apply(ctx context.Context, obj client.Object, fn ...controllerutil.MutateFn) error {
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if err := ctrl.SetControllerReference(r.resource, obj, r.Scheme); err != nil {
			r.logger.Infof("could not update controller reference")
			return err
		}
		if len(fn) > 0 {
			return fn[0]()
		}
		return nil
	})
	return err
}

//+kubebuilder:rbac:groups=crds.kloudlite.io,resources=projects,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=crds.kloudlite.io,resources=projects/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=crds.kloudlite.io,resources=projects/finalizers,verbs=update

func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.logger = GetLogger(req.NamespacedName)
	logger := r.logger.With("RECONCILE", "true")

	project := &crdsv1.Project{}
	if err := r.Get(ctx, req.NamespacedName, project); err != nil {
		if apiErrors.IsNotFound(err) {
			// INFO: might have been deleted
			return reconcileResult.OK()
		}
		return reconcileResult.Failed()
	}
	r.resource = project

	if project.HasToBeDeleted() {
		return r.finalizeProject(ctx, project)
	}

	logger.Infof("request received")

	// check for namespace existence
	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: project.Name}, &ns); err != nil {
		if !apiErrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	namespace := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: project.Name}}
	err := r.apply(ctx, &namespace)
	if err != nil {
		return reconcileResult.FailedE(err)
	}

	encAuthPass := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", r.HarborUserName, r.HarborPassword)))
	dockerConfigJson, err := json.Marshal(map[string]interface{}{
		"auths": map[string]interface{}{
			ImageRegistry: map[string]interface{}{
				"auth": encAuthPass,
			},
		},
	})
	if err != nil {
		e := errors.NewEf(err, "could not unmarshal dockerconfigjson")
		return reconcileResult.FailedE(e)
	}

	imgPullSecret := corev1.Secret{
		TypeMeta: TypeSecret,
		ObjectMeta: metav1.ObjectMeta{
			Namespace: project.Name,
			Name:      ImagePullSecretName,
		},
		Data: map[string][]byte{
			".dockerconfigjson": dockerConfigJson,
		},
		Type: corev1.SecretTypeDockerConfigJson,
	}

	err = r.apply(ctx, &imgPullSecret)
	if err != nil {
		return reconcileResult.FailedE(err)
	}

	// Role
	adminRole := rbacv1.Role{
		TypeMeta: TypeRole,
		ObjectMeta: metav1.ObjectMeta{
			Namespace: project.Name,
			Name:      NamespaceAdminRole,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"", "extensions", "apps"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs", "cronjobs"},
				Verbs:     []string{"*"},
			},
		},
	}

	err = r.apply(ctx, &adminRole)
	if err != nil {
		return reconcileResult.FailedE(err)
	}

	adminRoleBinding := rbacv1.RoleBinding{
		TypeMeta: TypeRoleBinding,
		ObjectMeta: metav1.ObjectMeta{
			Namespace: project.Name,
			Name:      NamespaceAdminRoleBinding,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      TypeSvcAccount.Kind,
				APIGroup:  "",
				Name:      SvcAccountName,
				Namespace: project.Name,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "",
			Kind:     TypeRole.Kind,
			Name:     NamespaceAdminRole,
		},
	}

	err = r.apply(ctx, &adminRoleBinding)
	if err != nil {
		return reconcileResult.FailedE(err)
	}

	// service account
	svcAccount := corev1.ServiceAccount{
		TypeMeta:         TypeSvcAccount,
		ObjectMeta:       metav1.ObjectMeta{Name: SvcAccountName, Namespace: project.Name},
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: ImagePullSecretName}},
	}

	r.apply(ctx, &svcAccount)
	if err != nil {
		return reconcileResult.FailedE(err)
	}

	// set conditions
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:   "Ready",
		Status: "True",
		Reason: "Reconcilation Successfull",
	})

	logger.Infof("Reconcile Completed")
	return reconcileResult.OK()
}

func (r *ProjectReconciler) finalizeProject(ctx context.Context, project *crdsv1.Project) (ctrl.Result, error) {
	logger := r.logger.With("FINALIZER", "true")
	logger.Debug("finalizing ...")

	if controllerutil.ContainsFinalizer(project, finalizers.Project.String()) {
		controllerutil.RemoveFinalizer(project, finalizers.Project.String())
		err := r.Update(ctx, project)
		if err != nil {
			return reconcileResult.Retry()
		}
		logger.Infof("Deletion in Progress, removed %s finalizer", finalizers.Project.String())
		return reconcileResult.Retry(3)
	}

	if controllerutil.ContainsFinalizer(project, finalizers.Foreground.String()) {
		var ns corev1.Namespace
		if err := r.Get(ctx, types.NamespacedName{Name: project.Name}, &ns); err != nil {
			if apiErrors.IsNotFound(err) {
				controllerutil.RemoveFinalizer(project, finalizers.Foreground.String())
				err := r.Update(ctx, project)
				if err != nil {
					return reconcileResult.Retry()
				}
				logger.Info("Reconcile Completed, removed foreground finalizer")
			}
		}
	}
	return reconcileResult.OK()
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crdsv1.Project{}).
		Owns(&corev1.Namespace{}).
		Complete(r)
}
