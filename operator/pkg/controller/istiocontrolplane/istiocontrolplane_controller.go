// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package istiocontrolplane

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	finalizer = "istio-finalizer.install.istio.io"
	// finalizerMaxRetries defines the maximum number of attempts to remove the finalizer.
	finalizerMaxRetries = 1
)

var (
	restConfig *rest.Config
	// watchedResources contains all resources we will watch and reconcile when changed
	// Ideally this would also contain Istio CRDs, but there is a race condition here - we cannot watch
	// a type that does not yet exist.
	watchedResources = []schema.GroupVersionKind{
		{Group: "autoscaling", Version: "v2beta1", Kind: name.HPAStr},
		{Group: "policy", Version: "v1beta1", Kind: name.PDBStr},
		{Group: "apps", Version: "v1", Kind: name.StatefulSetStr},
		{Group: "apps", Version: "v1", Kind: name.DeploymentStr},
		{Group: "apps", Version: "v1", Kind: name.DaemonSetStr},
		{Group: "extensions", Version: "v1beta1", Kind: name.IngressStr},
		{Group: "", Version: "v1", Kind: name.ServiceStr},
		// Endpoints should not be pruned because these are generated and not in the manifest.
		// {Group: "", Version: "v1", Kind: name.EndpointStr},
		{Group: "", Version: "v1", Kind: name.CMStr},
		{Group: "", Version: "v1", Kind: name.PVCStr},
		{Group: "", Version: "v1", Kind: name.PodStr},
		{Group: "", Version: "v1", Kind: name.SecretStr},
		{Group: "", Version: "v1", Kind: name.SAStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1beta1", Kind: name.RoleBindingStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.RoleBindingStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1beta1", Kind: name.RoleStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.RoleStr},
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: name.MutatingWebhookConfigurationStr},
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: name.ValidatingWebhookConfigurationStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.ClusterRoleStr},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: name.ClusterRoleBindingStr},
		{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: name.CRDStr},
	}

	ownedResourcePredicates = predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			// no action
			return false
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			// no action
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			obj, err := meta.Accessor(e.Object)
			log.Debugf("got delete event for %s.%s", obj.GetName(), obj.GetNamespace())
			if err != nil {
				return false
			}
			unsObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(e.Object)
			if err != nil {
				return false
			}
			if isOperatorCreatedResource(obj) {
				crName := obj.GetLabels()[helmreconciler.OwningResourceName]
				crNamespace := obj.GetLabels()[helmreconciler.OwningResourceNamespace]
				componentName := obj.GetLabels()[helmreconciler.IstioComponentLabelStr]
				var host string
				if restConfig != nil {
					host = restConfig.Host
				}
				crHash := strings.Join([]string{crName, crNamespace, componentName, host}, "-")
				oh := object.NewK8sObject(&unstructured.Unstructured{Object: unsObj}, nil, nil).Hash()
				cache.RemoveObject(crHash, oh)
				return true
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// no action
			return false
		},
	}

	operatorPredicates = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldIOP, ok := e.ObjectOld.(*iopv1alpha1.IstioOperator)
			if !ok {
				log.Error("failed to get old IstioOperator")
				return false
			}
			newIOP := e.ObjectNew.(*iopv1alpha1.IstioOperator)
			if !ok {
				log.Error("failed to get new IstioOperator")
				return false
			}
			if !reflect.DeepEqual(oldIOP.Spec, newIOP.Spec) ||
				oldIOP.GetDeletionTimestamp() != newIOP.GetDeletionTimestamp() ||
				oldIOP.GetGeneration() != newIOP.GetGeneration() {
				return true
			}
			return false
		},
	}
)

// NewReconcileIstioOperator creates a new ReconcileIstioOperator and returns a ptr to it.
func NewReconcileIstioOperator(client client.Client, config *rest.Config, scheme *runtime.Scheme) *ReconcileIstioOperator {
	return &ReconcileIstioOperator{
		client: client,
		config: config,
		scheme: scheme,
	}
}

// ReconcileIstioOperator reconciles a IstioOperator object
type ReconcileIstioOperator struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	config *rest.Config
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a IstioOperator object and makes changes based on the state read
// and what is in the IstioOperator.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileIstioOperator) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconciling IstioOperator")

	ns := request.Namespace
	reqNamespacedName := types.NamespacedName{
		Name:      request.Name,
		Namespace: ns,
	}
	// declare read-only iop instance to create the reconciler
	iop := &iopv1alpha1.IstioOperator{}
	if err := r.client.Get(context.TODO(), reqNamespacedName, iop); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Errorf("error getting IstioOperator iop: %s", err)
		return reconcile.Result{}, err
	}
	if iop.Spec == nil {
		iop.Spec = &v1alpha1.IstioOperatorSpec{Profile: name.DefaultProfileName}
	}
	deleted := iop.GetDeletionTimestamp() != nil
	finalizers := sets.NewString(iop.GetFinalizers()...)
	if deleted {
		if !finalizers.Has(finalizer) {
			log.Info("IstioOperator deleted")
			return reconcile.Result{}, nil
		}
		log.Info("Deleting IstioOperator")

		reconciler, err := helmreconciler.NewHelmReconciler(r.client, r.config, iop, nil)
		if err != nil {
			return reconcile.Result{}, err
		}
		if err := reconciler.Delete(); err != nil {
			return reconcile.Result{}, err
		}
		finalizers.Delete(finalizer)
		iop.SetFinalizers(finalizers.List())
		finalizerError := r.client.Update(context.TODO(), iop)
		for retryCount := 0; errors.IsConflict(finalizerError) && retryCount < finalizerMaxRetries; retryCount++ {
			// workaround for https://github.com/kubernetes/kubernetes/issues/73098 for k8s < 1.14
			// TODO: make this error message more meaningful.
			log.Info("conflict during finalizer removal, retrying")
			_ = r.client.Get(context.TODO(), request.NamespacedName, iop)
			finalizers = sets.NewString(iop.GetFinalizers()...)
			finalizers.Delete(finalizer)
			iop.SetFinalizers(finalizers.List())
			finalizerError = r.client.Update(context.TODO(), iop)
		}
		if finalizerError != nil {
			if errors.IsNotFound(finalizerError) {
				log.Infof("Could not remove finalizer from %v: the object was deleted", request)
				return reconcile.Result{}, nil
			} else if errors.IsConflict(finalizerError) {
				log.Infof("Could not remove finalizer from %v due to conflict. Operation will be retried in next reconcile attempt", request)
				return reconcile.Result{}, nil
			}
			log.Errorf("error removing finalizer: %s", finalizerError)
			return reconcile.Result{}, finalizerError
		}
		return reconcile.Result{}, nil
	} else if !finalizers.Has(finalizer) {
		log.Infof("Adding finalizer %v to %v", finalizer, request)
		finalizers.Insert(finalizer)
		iop.SetFinalizers(finalizers.List())
		err := r.client.Update(context.TODO(), iop)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof("Could not add finalizer to %v: the object was deleted", request)
				return reconcile.Result{}, nil
			} else if errors.IsConflict(err) {
				log.Infof("Could not add finalizer to %v due to conflict. Operation will be retried in next reconcile attempt", request)
				return reconcile.Result{}, nil
			}
			log.Errorf("Failed to update IstioOperator with finalizer, %v", err)
			return reconcile.Result{}, err
		}
	}

	log.Info("Updating IstioOperator")
	var err error
	iopMerged := &iopv1alpha1.IstioOperator{}
	*iopMerged = *iop
	iopMerged.Spec, err = mergeIOPSWithProfile(iopMerged)

	if err != nil {
		log.Errorf("failed to generate IstioOperator spec, %v", err)
		return reconcile.Result{}, err
	}

	if _, ok := iopMerged.Spec.Values["global"]; !ok {
		iopMerged.Spec.Values["global"] = make(map[string]interface{})
	}
	globalValues := iopMerged.Spec.Values["global"].(map[string]interface{})
	log.Info("Detecting third-party JWT support")
	var jwtPolicy util.JWTPolicy
	if jwtPolicy, err = util.DetectSupportedJWTPolicy(r.config); err != nil {
		log.Warnf("Failed to detect third-party JWT support: %v", err)
	} else {
		if jwtPolicy == util.FirstPartyJWT {
			// nolint: lll
			log.Info("Detected that your cluster does not support third party JWT authentication. " +
				"Falling back to less secure first party JWT. See https://istio.io/docs/ops/best-practices/security/#configure-third-party-service-account-tokens for details.")
		}
		globalValues["jwtPolicy"] = string(jwtPolicy)
	}
	reconciler, err := helmreconciler.NewHelmReconciler(r.client, r.config, iopMerged, nil)
	if err != nil {
		return reconcile.Result{}, err
	}
	if err := reconciler.SetStatusBegin(); err != nil {
		return reconcile.Result{}, err
	}
	status, err := reconciler.Reconcile()
	if err != nil {
		log.Errorf("reconciling err: %s", err)
	}
	if err := reconciler.SetStatusComplete(status); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, err
}

// mergeIOPSWithProfile overlays the values in iop on top of the defaults for the profile given by iop.profile and
// returns the merged result.
func mergeIOPSWithProfile(iop *iopv1alpha1.IstioOperator) (*v1alpha1.IstioOperatorSpec, error) {
	profileYAML, err := helm.GetProfileYAML(iop.Spec.InstallPackagePath, iop.Spec.Profile)
	if err != nil {
		return nil, err
	}

	// Due to the fact that base profile is compiled in before a tag can be created, we must allow an additional
	// override from variables that are set during release build time.
	hub := version.DockerInfo.Hub
	tag := version.DockerInfo.Tag
	if hub != "" && hub != "unknown" && tag != "" && tag != "unknown" {
		buildHubTagOverlayYAML, err := helm.GenerateHubTagOverlay(hub, tag)
		if err != nil {
			return nil, err
		}
		profileYAML, err = util.OverlayYAML(profileYAML, buildHubTagOverlayYAML)
		if err != nil {
			return nil, err
		}
	}

	overlayYAML, err := util.MarshalWithJSONPB(iop)
	if err != nil {
		return nil, err
	}
	t := translate.NewReverseTranslator()
	overlayYAML, err = t.TranslateK8SfromValueToIOP(overlayYAML)
	if err != nil {
		return nil, fmt.Errorf("could not overlay k8s settings from values to IOP: %s", err)
	}

	mergedYAML, err := util.OverlayIOP(profileYAML, overlayYAML)
	if err != nil {
		return nil, err
	}

	mergedYAML, err = translate.OverlayValuesEnablement(mergedYAML, overlayYAML, "")
	if err != nil {
		return nil, err
	}

	mergedYAMLSpec, err := tpath.GetSpecSubtree(mergedYAML)
	if err != nil {
		return nil, err
	}

	return istio.UnmarshalAndValidateIOPS(mergedYAMLSpec)
}

// Add creates a new IstioOperator Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	restConfig = mgr.GetConfig()
	return add(mgr, &ReconcileIstioOperator{client: mgr.GetClient(), scheme: mgr.GetScheme(), config: mgr.GetConfig()})
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	log.Info("Adding controller for IstioOperator")
	// Create a new controller
	c, err := controller.New("istiocontrolplane-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource IstioOperator
	err = c.Watch(&source.Kind{Type: &iopv1alpha1.IstioOperator{}}, &handler.EnqueueRequestForObject{}, operatorPredicates)
	if err != nil {
		return err
	}
	//watch for changes to Istio resources
	err = watchIstioResources(c)
	if err != nil {
		return err
	}
	log.Info("Controller added")
	return nil
}

// Watch changes for Istio resources managed by the operator
func watchIstioResources(c controller.Controller) error {
	for _, t := range watchedResources {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Kind:    t.Kind,
			Group:   t.Group,
			Version: t.Version,
		})
		err := c.Watch(&source.Kind{Type: u}, &handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
				log.Infof("watch a change for istio resource: %s.%s", a.Meta.GetName(), a.Meta.GetNamespace())
				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{
						Name:      a.Meta.GetLabels()[helmreconciler.OwningResourceName],
						Namespace: a.Meta.GetLabels()[helmreconciler.OwningResourceNamespace],
					}},
				}
			}),
		}, ownedResourcePredicates)
		if err != nil {
			log.Warnf("can not create watch for resources %s.%s.%s due to %q", t.Kind, t.Group, t.Version, err)
		}
	}
	return nil
}

// Check if the specified object is created by operator
func isOperatorCreatedResource(obj metav1.Object) bool {
	return obj.GetLabels()[helmreconciler.OwningResourceName] != "" &&
		obj.GetLabels()[helmreconciler.OwningResourceNamespace] != "" &&
		obj.GetLabels()[helmreconciler.IstioComponentLabelStr] != ""
}
