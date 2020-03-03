// Copyright 2019 Istio Authors
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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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

	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

const (
	finalizer = "istio-finalizer.install.istio.io"
	// finalizerMaxRetries defines the maximum number of attempts to remove the finalizer.
	finalizerMaxRetries = 1
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new IstioOperator Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	factory := &helmreconciler.Factory{CustomizerFactory: &IstioRenderingCustomizerFactory{}}
	return &ReconcileIstioOperator{client: mgr.GetClient(), scheme: mgr.GetScheme(), factory: factory, config: mgr.GetConfig()}
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
	err = c.Watch(&source.Kind{Type: &iopv1alpha1.IstioOperator{}}, &handler.EnqueueRequestForObject{})
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

var _ reconcile.Reconciler = &ReconcileIstioOperator{}

// ReconcileIstioOperator reconciles a IstioOperator object
type ReconcileIstioOperator struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	config  *rest.Config
	scheme  *runtime.Scheme
	factory *helmreconciler.Factory
}

// Reconcile reads that state of the cluster for a IstioOperator object and makes changes based on the state read
// and what is in the IstioOperator.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileIstioOperator) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Info("Reconciling IstioOperator")

	ns := request.Namespace
	if ns == "" {
		ns = defaultNs
	} else {
		defaultNs = ns
	}
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

	deleted := iop.GetDeletionTimestamp() != nil
	finalizers := sets.NewString(iop.GetFinalizers()...)
	if deleted {
		if !finalizers.Has(finalizer) {
			log.Info("IstioOperator deleted")
			return reconcile.Result{}, nil
		}
		log.Info("Deleting IstioOperator")

		reconciler, err := r.factory.New(iop, r.client)
		if err != nil {
			log.Errorf("failed to create reconciler: %s", err)
			return reconcile.Result{}, err
		}

		err = reconciler.Delete()
		if err != nil {
			log.Errorf("failed to remove owned resources: %s", err)
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
		return reconcile.Result{}, err
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
	iopMerged.Spec, err = helmreconciler.MergeIOPSWithProfile(iopMerged)

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
	reconciler, err := r.getOrCreateReconciler(iopMerged)
	if err == nil {
		err = reconciler.Reconcile()
		if err != nil {
			log.Errorf("reconciling err: %s", err)
		}
	} else {
		log.Errorf("failed to create reconciler: %s", err)
	}

	return reconcile.Result{}, err
}

var (
	defaultNs   string
	reconcilers = map[string]*helmreconciler.HelmReconciler{}
)

func reconcilersMapKey(iop *iopv1alpha1.IstioOperator) string {
	return fmt.Sprintf("%s/%s", iop.Namespace, iop.Name)
}

var ownedResourcePredicates = predicate.Funcs{
	CreateFunc: func(_ event.CreateEvent) bool {
		// no action
		return false
	},
	GenericFunc: func(_ event.GenericEvent) bool {
		// no action
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		object, err := meta.Accessor(e.Object)
		log.Debugf("got delete event for %s.%s", object.GetName(), object.GetNamespace())
		if err != nil {
			return false
		}
		if object.GetLabels()[OwnerNameKey] != "" {
			return true
		}
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		// no action
		return false
	},
}

func (r *ReconcileIstioOperator) getOrCreateReconciler(iop *iopv1alpha1.IstioOperator) (*helmreconciler.HelmReconciler, error) {
	key := reconcilersMapKey(iop)
	var err error
	var reconciler *helmreconciler.HelmReconciler
	if reconciler, ok := reconcilers[key]; ok {
		reconciler.SetNeedUpdateAndPrune(false)
		oldInstance := reconciler.GetInstance()
		reconciler.SetInstance(iop)
		if reconciler.GetInstance() != oldInstance {
			//regenerate the reconciler
			if reconciler, err = r.factory.New(iop, r.client); err == nil {
				reconcilers[key] = reconciler
			}
		}
		return reconciler, err
	}
	//not found - generate the reconciler
	if reconciler, err = r.factory.New(iop, r.client); err == nil {
		reconcilers[key] = reconciler
	}
	return reconciler, err
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
				log.Debugf("watch a change for istio resource: %s.%s", a.Meta.GetName(), a.Meta.GetNamespace())
				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{
						Name: a.Meta.GetLabels()[OwnerNameKey],
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
