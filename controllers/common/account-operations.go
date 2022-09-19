package commoncontroller

import (
	"bytes"
	"fmt"
	"net/http"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"operators.kloudlite.io/lib/errors"
	"operators.kloudlite.io/lib/functions"
	rApi "operators.kloudlite.io/lib/operator"
	"operators.kloudlite.io/lib/templates"
	"operators.kloudlite.io/lib/wireguard"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	managementv1 "operators.kloudlite.io/apis/management/v1"
)

func (r *AccountReconciler) reconcileOperations(req *rApi.Request[*managementv1.Account]) rApi.StepResult {

	// create namespace
	if err := func() error {
		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "NamespaceFound") {
			return nil
		}

		if b, err := templates.Parse(templates.AccountNamespace, map[string]any{
			"obj":        req.Object,
			"owner-refs": functions.AsOwner(req.Object, true),
		}); err != nil {
			return err
		} else if _, err = functions.KubectlApplyExec(b); err != nil {
			return err
		}

		// fmt.Println(o.String())

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// create coredns deployment
	if err := func() error {
		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "DNSServerCreated") {
			return nil
		}

		if b, err := templates.Parse(templates.WireGuard, map[string]any{
			"obj":        req.Object,
			"owner-refs": functions.AsOwner(req.Object, true),
		}); err != nil {
			return err
		} else if _, err = functions.KubectlApplyExec(b); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// generating wireguard server config
	if err := func() error {
		if !meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGSecretNotFound") {
			return nil
		}

		if pub, priv, err := wireguard.GenerateWgKeys(); err != nil {
			return err
		} else if err = functions.KubectlApply(req.Context(), r.Client,
			functions.ParseSecret(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-keys",
					Namespace: "wg-" + req.Object.Name,
					OwnerReferences: []metav1.OwnerReference{
						functions.AsOwner(req.Object, true),
					},
				},
				Data: map[string][]byte{
					"private-key": []byte(priv),
					"public-key":  []byte(pub),
				},
			})); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// upsert domains with it's latest ips
	if err := func() error {

		var regions managementv1.RegionList
		err := r.List(req.Context(), &regions, &client.ListOptions{
			LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
				"kloudlite.io/account-ref": req.Object.Name,
			}),
		})
		if err != nil {
			return err
		}

		for _, region := range regions.Items {

			wgDomain, ok := rApi.GetLocal[string](req, "wg-domain")
			if !ok {
				return fmt.Errorf("CAN'T WG DOMAINS")
			}

			ipsAny, ok := region.Status.DisplayVars.Get("kloudlite.io/node-ips")
			if !ok {
				continue
			}

			wgBaseDomain := os.Getenv("WG_DOMAIN")
			if wgDomain == "" {
				return fmt.Errorf(("can't find wg_domain in environment"))
			}

			err = functions.KubectlApply(req.Context(), r.Client, &managementv1.Domain{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "management.kloudlite.io/v1",
					Kind:       "Domain",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("wg-%s-%s", region.Name, req.Object.Name),
					Labels: map[string]string{
						"kloudlite.io/wg-domain":         wgDomain,
						"kloudlite.io/wg-region":         region.Name,
						"kloudlite.io/wg-domain-account": req.Object.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						functions.AsOwner(req.Object),
						functions.AsOwner(&region),
					},
				},
				Spec: managementv1.DomainSpec{
					Name: fmt.Sprintf("%s.%s.wg.%s", region.Name, wgDomain, wgBaseDomain),
					Ips: func() []string {

						// fmt.Println(ipsAny)
						ips := []string{}
						for _, ip := range ipsAny.([]interface{}) {
							ips = append(ips, ip.(string))
						}
						return ips

					}(),
				},
			})

			if err != nil {
				fmt.Println(err)
				return err
			}
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// creating wireguard deployment service and namespace
	if err := func() error {

		wgNotReady := func() bool {
			return meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGNamespaceFound") ||
				meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGNodePortNotReady") ||
				meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGServiceFound") ||
				meta.IsStatusConditionFalse(req.Object.Status.Conditions, "DNSServerReady") ||
				meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGDeploymentExists")
		}()

		// creating wireguard deployment service and namespace
		if wgNotReady {
			var regions managementv1.RegionList
			var klregions managementv1.RegionList

			if err := r.Client.List(req.Context(), &regions, &client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
					"kloudlite.io/account-ref": req.Object.Name,
				}),
			}); err != nil {
				return err
			}

			if err := r.Client.List(req.Context(), &klregions, &client.ListOptions{
				LabelSelector: apiLabels.SelectorFromValidatedSet(apiLabels.Set{
					"kloudlite.io/account-ref": "",
				}),
			}); err != nil {
				return err
			}

			regions.Items = append(regions.Items, klregions.Items...)

			activeRegions, ok := rApi.GetLocal[map[string]bool](req, "active-regions")
			// fmt.Println(activeRegions)
			if !ok {
				activeRegions = map[string]bool{}
			}

			for _, region := range regions.Items {

				if region.Spec.Account != "" && region.Spec.Account != req.Object.Name {
					continue
				}

				// fmt.Println(".................................")
				deviceProxyConfigExists := true

				if _, err := rApi.Get(req.Context(), r.Client, functions.NN("wg-"+req.Object.Name, "device-proxy-config"), &corev1.ConfigMap{}); err != nil {
					deviceProxyConfigExists = false
				}

				if b, err := templates.Parse(templates.WireGuard, map[string]any{
					"wgdeploy": activeRegions[region.Name],
					// "wgdeploy":                   true,
					"obj":                        req.Object,
					"owner-refs":                 functions.AsOwner(req.Object, true),
					"region-owner-refs":          functions.AsOwner(&region),
					"region":                     region.Name,
					"device-proxy-config-exists": deviceProxyConfigExists,
				}); err != nil {
					return err
				} else if _, err = functions.KubectlApplyExec(b); err != nil {
					return err
				}

				// fmt.Println(string(b))
			}

			req.Done(&ctrl.Result{Requeue: true})
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// storing wg server config into secret
	if err := func() error {
		// storing wg server config into secret
		if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGServerConfigExists") {
			serverConfig, ok := rApi.GetLocal[string](req, "serverWgConfig")
			if !ok {
				return errors.New("serverWgConfig not found")
			}

			err := r.Client.Create(req.Context(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-config",
					Namespace: "wg-" + req.Object.Name,
					OwnerReferences: []metav1.OwnerReference{
						functions.AsOwner(req.Object),
					},
				},
				Data: map[string][]byte{
					"data": []byte(serverConfig),
				},
			})
			if err != nil {
				return err
			}
		}
		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	// updating wireguard server config
	if err := func() error {

		if meta.IsStatusConditionFalse(req.Object.Status.Conditions, "WGServerConfigMatching") {
			serverConfig, ok := rApi.GetLocal[string](req, "serverWgConfig")
			if !ok {
				return errors.New("serverWgConfig not found")
			}
			err := r.Client.Update(req.Context(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wg-server-config",
					Namespace: "wg-" + req.Object.Name,
				},
				Data: map[string][]byte{
					"data": []byte(serverConfig),
				},
			})
			if err != nil {
				return err
			}

			regions, ok := rApi.GetLocal[managementv1.RegionList](req, "regions")
			if !ok {
				return errors.New("Regions not found")
			}

			var updateError error
			for _, region := range regions.Items {

				_, err = http.Post(fmt.Sprintf("http://wg-api-service-%s.wg-%s.svc.cluster.local:2998/post", region.Name, req.Object.Name), "application/json", bytes.NewBuffer([]byte(serverConfig)))

				if err != nil {
					fmt.Println(region.Name, ":", err)
					updateError = err
				}
			}

			if updateError != nil {
				return updateError
			}
		}

		return nil
	}(); err != nil {
		return req.FailWithOpError(err)
	}

	return req.Done()
}
