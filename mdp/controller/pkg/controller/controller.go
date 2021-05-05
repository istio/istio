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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"istio.io/istio/mdp/controller/pkg/apis/mdp/v1alpha1"
	"istio.io/istio/mdp/controller/pkg/metrics"
	"istio.io/istio/mdp/controller/pkg/reconciler"
	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("mdp", "Managed Data Plane", 0)

	controllerPredicates = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return true
		},
	}
)

// ReconcileMDPConfig reconciles a MDPConfig object
type ReconcileMDPConfig struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client        client.Client
	config        *rest.Config
	scheme        *runtime.Scheme
	mdpReconciler *reconciler.MDPReconciler
}

// Reconcile reads that state of the cluster for a MDPConfig object and makes changes based on the state read
// and what is in the MDPConfig.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileMDPConfig) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	scope.Info("Reconciling MDPConfig")

	reqNamespacedName := types.NamespacedName{
		Name:      request.Name,
		Namespace: request.Namespace,
	}
	// declare read-only mdpcfg instance to create the reconciler
	mdpcfg := &v1alpha1.MDPConfig{}
	if err := r.client.Get(ctx, reqNamespacedName, mdpcfg); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Return and don't requeue.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		metrics.CountCRFetchFail(errors.ReasonForError(err))
		return reconcile.Result{}, err
	}
	if mdpcfg.Spec == nil {
		mdpcfg.Spec = &v1alpha1.MDPConfigSpec{}
	}

	scope.Info("Updating MDPConfig")
	if err := reconciler.SetStatusBegin(mdpcfg, r.client); err != nil {
		return reconcile.Result{}, err
	}
	status, err := r.mdpReconciler.Reconcile(ctx, mdpcfg)
	if err != nil {
		scope.Errorf("Error during reconcile: %s", err)
	}
	if err := reconciler.SetStatusComplete(mdpcfg, r.client, status); err != nil {
		return reconcile.Result{}, err
	}
	if status.Status != v1alpha1.MDPStatus_READY {
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, err
}

func NewController(client client.Client, scheme *runtime.Scheme, config *rest.Config) (*ReconcileMDPConfig, error) {
	r, err := reconciler.NewMDPReconciler(client, config, nil)
	if err != nil {
		return nil, err
	}
	return &ReconcileMDPConfig{
		client:        client,
		config:        config,
		scheme:        scheme,
		mdpReconciler: r,
	}, nil
}

// Add creates a new MDPConfig Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	c, err := NewController(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig())
	if err != nil {
		return err
	}
	return add(mgr, c)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	scope.Info("Adding controller for MDPConfig.")
	// Create a new controller
	c, err := controller.New("mdp-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource MDPConfig
	err = c.Watch(&source.Kind{Type: &v1alpha1.MDPConfig{}}, &handler.EnqueueRequestForObject{}, controllerPredicates)
	if err != nil {
		return err
	}

	scope.Info("Controller added")
	return nil
}
