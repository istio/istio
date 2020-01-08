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

package helmreconciler

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/helm/pkg/manifest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	"istio.io/operator/pkg/name"
)

// RenderingCustomizer encompasses all the customization details for a specific rendering invocation.
type RenderingCustomizer interface {
	// Input returns the RenderingInput that should be used to rendering the charts.
	Input() RenderingInput
	// PruningDetails returns the details required to support pruning of obsolete resources.
	PruningDetails() PruningDetails
	// Listener returns the RenderingListener that should handle the rendering callbacks.
	Listener() RenderingListener
}

type RenderingCustomizerFactory interface {
	// NewCustomizer returns a new RenderingCustomizer for the specified object.
	NewCustomizer(obj runtime.Object) (RenderingCustomizer, error)
}

// PruningDetails define the labels and annotations used to mark resources managed by the operator, as well as the
// resource types managed by the operator.
type PruningDetails interface {
	// GetOwnerLabels returns the labels applied to all resources managed by the operator.
	// These are used as label selectors when selecting resources managed by the operator (e.g. as part of pruning
	// operations).  A typical example might be:
	//
	// myoperator.example.com/owner-name=my-custom-resource
	// myoperator.example.com/owner-namespace=containing-namespace
	//
	GetOwnerLabels() map[string]string
	// GetOwnerAnnotations returns the annotations applied to all resources managed by the operator.  These annotations
	// are used to determine whether or not an object should be pruned, i.e. all objects selected using the owner labels
	// that don't have annotations with values matching these will be pruned.  A typical example might be:
	//
	// myoperator.example.com/owner-generation=5
	//
	// which would cause resources with a different generation value to be
	// pruned.  To avoid pruning derived resources (which typically inherit the parent's labels), the prune logic
	// verifies that the annotation keys exist.
	GetOwnerAnnotations() map[string]string
	// GetResourceTypes returns the types of resources managed by the operator.  These types are used when selecting
	// resources to be pruned.
	GetResourceTypes() (namespaced []schema.GroupVersionKind, nonNamespaced []schema.GroupVersionKind)
}

// ChartManifestsMap is a typedef representing a map of chart-name: []manifest, i.e. the manifests
// associated with a specific chart
type ChartManifestsMap map[string][]manifest.Manifest

// RenderingInput specifies the details used for rendering charts.
type RenderingInput interface {
	// GetCRPath returns the absolute path locating the cr.
	GetCRPath() string
	// GetInputConfig returns the values object used during rendering.
	GetInputConfig() interface{}
	// GetTargetNamespace returns the target namespace which should be applied to namespaced resources
	// (i.e. used to set Release.Namespace)
	GetTargetNamespace() string
	// GetProcessingOrder returns a dependency tree for the given manifests. ComponentNameToListMap is a map of
	// each component to its dependencies. DependencyWaitCh is a map of channels, indexed by name. The component with
	// the given name must wait on the channel before starting its processing.
	GetProcessingOrder(manifests ChartManifestsMap) (ComponentNameToListMap, DependencyWaitCh)
}

// RenderingListener is the main hook into the rendering process.  The methods represent each stage in the
// rendering process.
type RenderingListener interface {
	// BeginReconcile occurs when a reconciliation is started.  instance represents the object (custom resource)
	// being reconciled.  Reconciliation occurs when a custom resource is created or modified.
	BeginReconcile(instance runtime.Object) error
	// BeginDelete is similar to BeginReconcile, but applies to deletion of a custom resource.
	BeginDelete(instance runtime.Object) error
	// BeginChart occurs before processing manifests associated with a specific chart.
	// chart is the name of the chart being processed.
	// manifests is the list of manifests to be applied.
	// The returned list of manifest.Manifest objects are the manifests that will be applied.
	BeginChart(chart string, manifests []manifest.Manifest) ([]manifest.Manifest, error)
	// BeginResource occurs when a new resource is being processed.  This method allows users to programmatically
	// customize resources created by the charts.  Examples of modifications:  applying owner labels/annotations;
	// applying settings that are specific to the environment, e.g. URLs from Ingress/Service resources created from
	// other charts; etc.
	// obj represents a resource created from a manifest.
	// chart represents the helm chart obj belongs to.
	// The returned runtime.Object is the object that will be reconciled (created/updated).
	BeginResource(chart string, obj runtime.Object) (runtime.Object, error)
	// ResourceCreated occurs after a resource has been created (i.e. client.Create(obj)).  This method allows users
	// to programmatically apply other details which are necessary as part of the object creation, e.g. updating
	// SecurityContextConstraints for a new ServiceAccount.
	// created is the object returned from the client.Create() call.
	ResourceCreated(created runtime.Object) error
	// ResourceUpdated occurs after a resource has been updated.  This method is similar to ResourceCreated, but applies
	// to client.Update().
	// updated represents the new state of the object
	// old represents the existing state of the object
	ResourceUpdated(updated, old runtime.Object) error
	// ResourceError occurs after a create/update/delete operation fails.
	// obj is the object on which the error occurred.
	// err is the error returned from the api server.
	ResourceError(obj runtime.Object, err error) error
	// EndResource represents the end of resource processing.  This is the counterpart to BeginResource.
	// obj is the resource whose processing has completed.
	EndResource(obj runtime.Object) error
	// EndChart represents the end of chart processing.  This is the counterpart to BeginChart.
	// chart is the name of the chart whose processing has completed.
	EndChart(chart string) error
	// BeginPrune represents the beginning of the pruning process.  Pruning occurs after all chart processing.
	// all indicates whether or not all resources are being pruned (i.e. a delete operation) or just out of sync
	// resources.
	BeginPrune(all bool) error
	// ResourceDeleted occurs after a resource has been deleted.  This method is similar to ResourceCreated, but applies
	// to client.Delete().  Like ResourceCreated, this method should be used to cleanup any programmatically applied
	// changes made when the object was created, e.g. removing a ServiceAccount from a SecurityContextConstraints.
	// deleted represents the object that was deleted.
	ResourceDeleted(deleted runtime.Object) error
	// EndPrune represents the end of the pruning process.
	EndPrune() error
	// EndDelete occurs after the deletion process has completed.
	// instance is the custom resource being deleted
	// err is any error that might have occurred during the deletion proecess
	EndDelete(instance runtime.Object, err error) error
	// EndReconcile occurs after reconciliation has completed.  It is similar to EndDelete, but applies to reconciliation.
	// instance is the custom resource being reconciled
	// status is the status and errors of components at the end of reconciliation.
	EndReconcile(instance runtime.Object, status map[string]*v1alpha1.IstioOperatorSpec_VersionStatus) error
}

// ChartCustomizer defines callbacks used by a listener that manages customizations for a specific chart.
type ChartCustomizer interface {
	// BeginChart is the same as RenderingListener.BeginChart
	BeginChart(chart string, manifests []manifest.Manifest) ([]manifest.Manifest, error)
	// BeginResource is the same as RenderingListener.BeginResource
	BeginResource(chart string, obj runtime.Object) (runtime.Object, error)
	// ResourceCreated is the same as RenderingListener.ResourceCreated
	ResourceCreated(created runtime.Object) error
	// ResourceUpdated is the same as RenderingListener.ResourceUpdated
	ResourceUpdated(new, existing runtime.Object) error
	// ResourceError is the same as RenderingListener.ResourceError
	ResourceError(obj runtime.Object, err error) error
	// EndResource is the same as RenderingListener.EndResource
	EndResource(obj runtime.Object) error
	// EndChart is the same as RenderingListener.EndChart
	EndChart(chart string) error
	// ResourceDeleted is the same as RenderingListener.ResourceDeleted
	ResourceDeleted(deleted runtime.Object) error
}

// ChartCustomizerFactory is a factory for creating ChartCustomizer objects.
type ChartCustomizerFactory interface {
	// NewChartCustomizer returns a new ChartCustomizer for the specified chartName.
	NewChartCustomizer(chartName string) ChartCustomizer
}

// ReconcilerListener is an interface that may be implemented by objects which require access to the HelmReconciler.
// These objects would typically require access to the kubernetes client or logger.
type ReconcilerListener interface {
	// RegisterReconciler is the callback function that allows the HelmReconciler to be registered.
	RegisterReconciler(reconciler *HelmReconciler)
}

// Patch represents a "patch" for an object
// XXX: currently, this is internal to HelmReconciler
type Patch interface {
	// Apply applies the patch to object through the api server
	// the returned object is the updated resource
	Apply() (*unstructured.Unstructured, error)
}

// LoggerProvider is a helper interface which allows HelmReconciler to expose a logger to clients.
type LoggerProvider interface {
	// GetLogger returns a logger
	GetLogger() logr.Logger
}

// ClientProvider is a helper interface which allows HelmReconciler to expose a client to clients.
type ClientProvider interface {
	// GetClient returns a kubernetes client.
	GetClient() client.Client
}

// ComponentNameToListMap is a map of ComponentName to a list of ComponentNames.
type ComponentNameToListMap map[name.ComponentName][]name.ComponentName

// ComponentTree defines a dependency tree.
type ComponentTree map[name.ComponentName]interface{}

// DependencyWaitCh defines a map of component name to a channel which signals when its parent dependency has
// completed.
type DependencyWaitCh map[name.ComponentName]chan struct{}
