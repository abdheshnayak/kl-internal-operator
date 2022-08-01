package commoncontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	apiLabels "k8s.io/apimachinery/pkg/labels"
	"operators.kloudlite.io/lib/functions"
	rApi "operators.kloudlite.io/lib/operator"
	"operators.kloudlite.io/lib/templates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	// "sigs.k8s.io/yaml"
)

// ServiceReconciler reconciles a Bridger object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=device-cluster.kloudlite.io,resources=bridgers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=device-cluster.kloudlite.io,resources=bridgers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=device-cluster.kloudlite.io,resources=bridgers/finalizers,verbs=update

func (r *ServiceReconciler) finalize(ctx context.Context, svc *corev1.Service) (ctrl.Result, error) {
	fmt.Println("finalizing ....", svc)
	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fmt.Println("--------------------new reconcilation--------------------")
	svc, e := rApi.Get(ctx, r.Client, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &corev1.Service{})

	if e != nil {
		if !apiErrors.IsNotFound(e) {
			return ctrl.Result{}, e
		}
	}

	// if svc.GetDeletionTimestamp() != nil {
	// 	r.finalize(ctx, svc)
	// }

	labels := svc.GetLabels()

	if labels["mirror.linkerd.io/mirrored-service"] != "true" {
		return ctrl.Result{}, nil
	}

	oldConfig, err := rApi.Get(ctx, r.Client, types.NamespacedName{
		Namespace: svc.GetNamespace(),
		Name:      fmt.Sprint("proxy-config-", svc.GetNamespace()),
	}, &corev1.ConfigMap{})

	type service struct {
		Id          string `json:"id"`
		Name        string `json:"name"`
		ServicePort int32  `json:"servicePort"`
		ProxyPort   int32  `json:"proxyPort"`
	}

	cServices := []service{}

	configData := []service{}

	// fmt.Println("kk:", oldConfig)

	if err == nil {
		oConfMap := map[string][]service{}
		e := json.Unmarshal([]byte(oldConfig.Data["conf.yml"]), &oConfMap)
		if oConfMap["services"] != nil {
			configData = oConfMap["services"]
		}
		if e != nil {
			fmt.Println("Failed to Unmarshal", e)
		}
	}

	// fmt.Println("c:", configData)

	isContains := func(svce []service, port int32) bool {
		for _, s := range svce {
			if s.ServicePort == port {
				return true
			}
		}
		return false
	}

	getTempPort := func(svce []service, id string) int32 {
		for _, c := range svce {
			if c.Id == id {
				return c.ProxyPort
			}
		}
		return func() int32 {
			min, max := 3000, 6000

			count := 0
			var r int

			for {
				r = rand.Intn(max-min) + min
				if !isContains(configData, int32(r)) || count > max-min {
					break
				}

				count++
			}

			return int32(r)
		}()

	}

	var services corev1.ServiceList

	err = r.List(
		ctx,
		&services,
		&client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(map[string]string{
				"mirror.linkerd.io/mirrored-service": "true",
			}),
			Namespace: svc.Namespace,
		},
	)

	if err != nil {
		return ctrl.Result{}, err
	}

	for _, s := range services.Items {

		for _, port := range s.Spec.Ports {
			tempPort := getTempPort(configData, fmt.Sprint(s.GetName(), "-", port.Port))

			cServices = append(cServices, service{
				Id:          fmt.Sprint(s.GetName(), "-", port.Port),
				Name:        s.GetName(),
				ServicePort: port.Port,
				ProxyPort:   tempPort,
			})
		}

	}

	// op, err := yaml.Marshal(map[string][]service{
	// 	"services": cServices,
	// })
	// if err != nil {

	// 	fmt.Println(err)
	// }

	// fmt.Println(string(op))

	type port struct {
		Name       string `json:"name"`
		Port       int32  `json:"port"`
		TargetPort int32  `json:"targetPort"`
		Protocol   string `json:"protocol"`
	}

	newPorts := []port{}

	for _, p := range svc.Spec.Ports {
		tp := getTempPort(cServices, fmt.Sprint(svc.GetName(), "-", p.Port))

		newPorts = append(newPorts, port{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: tp,
			Protocol:   string(p.Protocol),
		})
	}

	cm, err := json.Marshal(map[string][]service{
		"services": cServices,
	})
	if err != nil {
		fmt.Println("can't Unmarshal", err)
	}

	serviceOrignalName := strings.Split(svc.Annotations["mirror.linkerd.io/remote-svc-fq-name"], ".")[0]
	clusterName := labels["mirror.linkerd.io/cluster-name"]

	b, err := templates.Parse(templates.ProxyService, map[string]any{
		"object":        svc,
		"cluster-name":  clusterName,
		"service-name":  serviceOrignalName,
		"service-ports": newPorts,
		"configmap":     string(cm),
	})

	// fmt.Println(string(b))

	if err != nil {
		return ctrl.Result{}, err
	}

	// return ctrl.Result{}, nil

	_, err = functions.KubectlApplyExec(b)

	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		// Owns(&corev1.Service{}).
		// Owns(&appsv1.Deployment{}).
		Watches(
			&source.Kind{Type: &appsv1.Deployment{}},
			handler.EnqueueRequestsFromMapFunc(
				func(c client.Object) []reconcile.Request {
					matchedServices := []reconcile.Request{}
					labels := c.GetLabels()
					if labels == nil {
						labels = make(map[string]string)
					}
					if labels["kloudlite.io/proxyto"] != "" {

						// fmt.Println("deployment labels:", labels)
						matchedServices = append(matchedServices, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Namespace: c.GetNamespace(),
								Name:      labels["kloudlite.io/proxyto"],
							},
						})
					}

					return matchedServices
				},
			),
		).
		Complete(r)
}
