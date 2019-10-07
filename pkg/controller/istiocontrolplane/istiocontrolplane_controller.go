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

	"istio.io/operator/pkg/apis/istio/v1alpha2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"istio.io/operator/pkg/helmreconciler"
)

var log = logf.Log.WithName("controller_istiocontrolplane")

const (
	finalizer = "istio-operator"
	v1        = "v1alpha2"
	// finalizerMaxRetries defines the maximum number of attempts to add finalizers.
	finalizerMaxRetries = 10
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new IstioControlPlane Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr, v1))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, _ string) reconcile.Reconciler {
	factory := &helmreconciler.Factory{CustomizerFactory: &IstioRenderingCustomizerFactory{}}
	return &ReconcileIstioControlPlane{client: mgr.GetClient(), scheme: mgr.GetScheme(), factory: factory}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("istiocontrolplane-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource IstioControlPlane
	err = c.Watch(&source.Kind{Type: &v1alpha2.IstioControlPlane{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner IstioControlPlane
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &v1alpha2.IstioControlPlane{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileIstioControlPlane{}

// ReconcileIstioControlPlane reconciles a IstioControlPlane object
type ReconcileIstioControlPlane struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	scheme  *runtime.Scheme
	factory *helmreconciler.Factory
}

// Reconcile reads that state of the cluster for a IstioControlPlane object and makes changes based on the state read
// and what is in the IstioControlPlane.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileIstioControlPlane) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling IstioControlPlane")

	// Fetch the IstioControlPlane instance
	instance := &v1alpha2.IstioControlPlane{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	deleted := instance.GetDeletionTimestamp() != nil
	finalizers := instance.GetFinalizers()
	finalizerIndex := indexOf(finalizers, finalizer)

	if deleted {
		if finalizerIndex < 0 {
			reqLogger.Info("IstioControlPlane deleted")
			return reconcile.Result{}, nil
		}
		reqLogger.Info("Deleting IstioControlPlane")

		reconciler, err := r.factory.New(instance, r.client, reqLogger)
		if err == nil {
			err = reconciler.Delete()
		} else {
			reqLogger.Error(err, "failed to create reconciler")
		}
		// TODO: for now, nuke the resources, regardless of errors
		finalizers = append(finalizers[:finalizerIndex], finalizers[finalizerIndex+1:]...)
		instance.SetFinalizers(finalizers)
		finalizerError := r.client.Update(context.TODO(), instance)
		for retryCount := 0; errors.IsConflict(finalizerError) && retryCount < finalizerMaxRetries; retryCount++ {
			// workaround for https://github.com/kubernetes/kubernetes/issues/73098 for k8s < 1.14
			// TODO: make this error message more meaningful.
			reqLogger.Info("conflict during finalizer removal, retrying")
			_ = r.client.Get(context.TODO(), request.NamespacedName, instance)
			finalizers = instance.GetFinalizers()
			finalizerIndex = indexOf(finalizers, finalizer)
			finalizers = append(finalizers[:finalizerIndex], finalizers[finalizerIndex+1:]...)
			instance.SetFinalizers(finalizers)
			finalizerError = r.client.Update(context.TODO(), instance)
		}
		if finalizerError != nil {
			reqLogger.Error(finalizerError, "error removing finalizer")
		}
		return reconcile.Result{}, err
	} else if finalizerIndex < 0 {
		// TODO: make this error message more meaningful.
		reqLogger.V(2).Info("Adding finalizer", "finalizer", finalizer)
		finalizers = append(finalizers, finalizer)
		instance.SetFinalizers(finalizers)
		err = r.client.Update(context.TODO(), instance)
		return reconcile.Result{}, err
	}

	reqLogger.Info("Updating IstioControlPlane")
	reconciler, err := r.factory.New(instance, r.client, reqLogger)
	if err == nil {
		err = reconciler.Reconcile()
	} else {
		reqLogger.Error(err, "failed to create reconciler")
	}

	return reconcile.Result{}, err
}

func indexOf(l []string, s string) int {
	for i, elem := range l {
		if elem == s {
			return i
		}
	}
	return -1
}
