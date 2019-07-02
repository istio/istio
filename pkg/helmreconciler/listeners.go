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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/helm/pkg/manifest"

	"istio.io/operator/pkg/controller/common"
)

// CompositeRenderingListener is an implementation of RenderingListener which is composed of an array of listeners.
// All methods are delegated to each element in the Listeners array.  For completion events (e.g. EndResource()), the
// delegates are invoked last to first.
type CompositeRenderingListener struct {
	// Listeners represents a list of Listeners to which this object will delegate calls.
	Listeners []RenderingListener
}

var _ RenderingListener = &CompositeRenderingListener{}
var _ ReconcilerListener = &CompositeRenderingListener{}

// RegisterReconciler will register the HelmReconciler with any Listeners also implementing ReconcilerListener.
func (l *CompositeRenderingListener) RegisterReconciler(reconciler *HelmReconciler) {
	for _, listener := range l.Listeners {
		if reconcilerListener, ok := listener.(ReconcilerListener); ok {
			reconcilerListener.RegisterReconciler(reconciler)
		}
	}
}

// BeginReconcile delegates BeginReconcile to the Listeners in first to last order.
func (l *CompositeRenderingListener) BeginReconcile(instance runtime.Object) error {
	var allErrors []error
	for _, listener := range l.Listeners {
		if err := listener.BeginReconcile(instance); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// BeginDelete delegates BeginDelete to the Listeners in first to last order.
func (l *CompositeRenderingListener) BeginDelete(instance runtime.Object) error {
	var allErrors []error
	for _, listener := range l.Listeners {
		if err := listener.BeginDelete(instance); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// BeginChart delegates BeginChart to the Listeners in first to last order.
func (l *CompositeRenderingListener) BeginChart(chart string, manifests []manifest.Manifest) ([]manifest.Manifest, error) {
	var allErrors []error
	var err error
	for _, listener := range l.Listeners {
		if manifests, err = listener.BeginChart(chart, manifests); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return manifests, utilerrors.NewAggregate(allErrors)
}

// BeginResource delegates BeginResource to the Listeners in first to last order.
func (l *CompositeRenderingListener) BeginResource(obj runtime.Object) (runtime.Object, error) {
	var allErrors []error
	var err error
	for _, listener := range l.Listeners {
		if obj, err = listener.BeginResource(obj); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return obj, utilerrors.NewAggregate(allErrors)
}

// ResourceCreated delegates ResourceCreated to the Listeners in first to last order.
func (l *CompositeRenderingListener) ResourceCreated(created runtime.Object) error {
	var allErrors []error
	for _, listener := range l.Listeners {
		if err := listener.ResourceCreated(created); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// ResourceUpdated delegates ResourceUpdated to the Listeners in first to last order.
func (l *CompositeRenderingListener) ResourceUpdated(updated runtime.Object, old runtime.Object) error {
	var allErrors []error
	for _, listener := range l.Listeners {
		if err := listener.ResourceUpdated(updated, old); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// ResourceDeleted delegates ResourceDeleted to the Listeners in first to last order.
func (l *CompositeRenderingListener) ResourceDeleted(deleted runtime.Object) error {
	var allErrors []error
	for _, listener := range l.Listeners {
		if err := listener.ResourceDeleted(deleted); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// ResourceError delegates ResourceError to the Listeners in first to last order.
func (l *CompositeRenderingListener) ResourceError(obj runtime.Object, err error) error {
	// reverse order for completions
	var allErrors []error
	for index := len(l.Listeners) - 1; index > -1; index-- {
		if listenerErr := l.Listeners[index].ResourceError(obj, err); listenerErr != nil {
			allErrors = append(allErrors, listenerErr)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// EndResource delegates EndResource to the Listeners in last to first order.
func (l *CompositeRenderingListener) EndResource(obj runtime.Object) error {
	// reverse order for completions
	var allErrors []error
	for index := len(l.Listeners) - 1; index > -1; index-- {
		if listenerErr := l.Listeners[index].EndResource(obj); listenerErr != nil {
			allErrors = append(allErrors, listenerErr)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// EndChart delegates EndChart to the Listeners in last to first order.
func (l *CompositeRenderingListener) EndChart(chart string) error {
	// reverse order for completions
	var allErrors []error
	for index := len(l.Listeners) - 1; index > -1; index-- {
		if listenerErr := l.Listeners[index].EndChart(chart); listenerErr != nil {
			allErrors = append(allErrors, listenerErr)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// BeginPrune delegates BeginPrune to the Listeners in first to last order.
func (l *CompositeRenderingListener) BeginPrune(all bool) error {
	var allErrors []error
	for _, listener := range l.Listeners {
		if err := listener.BeginPrune(all); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// EndPrune delegates EndPrune to the Listeners in last to first order.
func (l *CompositeRenderingListener) EndPrune() error {
	// reverse order for completions
	var allErrors []error
	for index := len(l.Listeners) - 1; index > -1; index-- {
		if listenerErr := l.Listeners[index].EndPrune(); listenerErr != nil {
			allErrors = append(allErrors, listenerErr)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// EndDelete delegates EndDelete to the Listeners in last to first order.
func (l *CompositeRenderingListener) EndDelete(instance runtime.Object, err error) error {
	// reverse order for completions
	var allErrors []error
	for index := len(l.Listeners) - 1; index > -1; index-- {
		if listenerErr := l.Listeners[index].EndDelete(instance, err); listenerErr != nil {
			allErrors = append(allErrors, listenerErr)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// EndReconcile delegates EndReconcile to the Listeners in last to first order.
func (l *CompositeRenderingListener) EndReconcile(instance runtime.Object, err error) error {
	// reverse order for completions
	var allErrors []error
	for index := len(l.Listeners) - 1; index > -1; index-- {
		if listenerErr := l.Listeners[index].EndReconcile(instance, err); listenerErr != nil {
			allErrors = append(allErrors, listenerErr)
		}
	}
	return utilerrors.NewAggregate(allErrors)
}

// LoggingRenderingListener is a RenderingListener which logs events.  It also updates the HelmReconciler logger with
// values applying to various stages of processing (e.g. chart=chart-name, kind=resource-kind, etc.).  This can be used
// as the first listener in a CompositeRenderingListener.
type LoggingRenderingListener struct {
	reconciler  *HelmReconciler
	Level       int
	loggerStack []logr.Logger
}

var _ RenderingListener = &LoggingRenderingListener{}
var _ ReconcilerListener = &LoggingRenderingListener{}

// RegisterReconciler associates the HelmReconciler with the LoggingRenderingListener
func (l *LoggingRenderingListener) RegisterReconciler(reconciler *HelmReconciler) {
	l.reconciler = reconciler
}

// BeginReconcile logs the event
func (l *LoggingRenderingListener) BeginReconcile(instance runtime.Object) error {
	l.reconciler.logger.V(l.Level).Info("begin reconciling resources")
	return nil
}

// BeginDelete logs the event
func (l *LoggingRenderingListener) BeginDelete(instance runtime.Object) error {
	l.reconciler.logger.V(l.Level).Info("begin deleting resources")
	return nil
}

// BeginChart logs the event and updates the logger to log with values chart=chart-name
func (l *LoggingRenderingListener) BeginChart(chart string, manifests []manifest.Manifest) ([]manifest.Manifest, error) {
	l.loggerStack = append(l.loggerStack, l.reconciler.logger)
	l.reconciler.logger = l.reconciler.logger.WithValues("chart", chart)
	l.reconciler.logger.V(l.Level).Info("begin updating resources for chart")
	return manifests, nil
}

// BeginResource logs the event and updates the logger to log with values resource=name, kind=kind, apiVersion=api-version
func (l *LoggingRenderingListener) BeginResource(obj runtime.Object) (runtime.Object, error) {
	l.loggerStack = append(l.loggerStack, l.reconciler.logger)
	accessor := meta.NewAccessor()
	kind, _ := accessor.Kind(obj)
	name, _ := accessor.Name(obj)
	version, _ := accessor.APIVersion(obj)
	l.reconciler.logger = l.reconciler.logger.WithValues("resource", name, "kind", kind, "apiVersion", version)
	l.reconciler.logger.V(l.Level).Info("begin resource update")
	return obj, nil
}

// ResourceCreated logs the event
func (l *LoggingRenderingListener) ResourceCreated(created runtime.Object) error {
	l.reconciler.logger.V(l.Level).Info("new resource created")
	return nil
}

// ResourceUpdated logs the event
func (l *LoggingRenderingListener) ResourceUpdated(updated runtime.Object, old runtime.Object) error {
	l.reconciler.logger.V(l.Level).Info("existing resource updated")
	return nil
}

// ResourceDeleted logs the event
func (l *LoggingRenderingListener) ResourceDeleted(deleted runtime.Object) error {
	l.reconciler.logger.V(l.Level).Info("resource deleted")
	return nil
}

// ResourceError logs the event and the error
func (l *LoggingRenderingListener) ResourceError(obj runtime.Object, err error) error {
	l.reconciler.logger.Error(err, "error processing resource")
	return nil
}

// EndResource logs the event and resets the logger to its previous state (i.e. removes the resource specific labels).
func (l *LoggingRenderingListener) EndResource(obj runtime.Object) error {
	l.reconciler.logger.V(l.Level).Info("end resource update")
	lastIndex := len(l.loggerStack) - 1
	if lastIndex >= 0 {
		l.reconciler.logger = l.loggerStack[lastIndex]
		l.loggerStack = l.loggerStack[:lastIndex]
	}
	return nil
}

// EndChart logs the event and resets the logger to its previous state (i.e. removes the chart specific labels).
func (l *LoggingRenderingListener) EndChart(chart string) error {
	l.reconciler.logger.V(l.Level).Info("end chart update")
	lastIndex := len(l.loggerStack) - 1
	if lastIndex >= 0 {
		l.reconciler.logger = l.loggerStack[lastIndex]
		l.loggerStack = l.loggerStack[:lastIndex]
	}
	return nil
}

// BeginPrune logs the event and updates the logger to log with values all=true/false
func (l *LoggingRenderingListener) BeginPrune(all bool) error {
	l.loggerStack = append(l.loggerStack, l.reconciler.logger)
	l.reconciler.logger = l.reconciler.logger.WithValues("all", all)
	l.reconciler.logger.V(l.Level).Info("begin pruning")
	return nil
}

// EndPrune logs the event and resets the logger to its previous state (i.e. removes the all label).
func (l *LoggingRenderingListener) EndPrune() error {
	l.reconciler.logger.V(l.Level).Info("end pruning")
	lastIndex := len(l.loggerStack) - 1
	if lastIndex >= 0 {
		l.reconciler.logger = l.loggerStack[lastIndex]
		l.loggerStack = l.loggerStack[:lastIndex]
	}
	return nil
}

// EndDelete logs the event and any error that occurred
func (l *LoggingRenderingListener) EndDelete(instance runtime.Object, err error) error {
	if err != nil {
		l.reconciler.logger.Error(err, "errors occurred during deletion")
	}
	l.reconciler.logger.V(l.Level).Info("end deleting resources")
	return nil
}

// EndReconcile logs the event and any error that occurred
func (l *LoggingRenderingListener) EndReconcile(instance runtime.Object, err error) error {
	if err != nil {
		l.reconciler.logger.Error(err, "errors occurred during reconcilation")
	}
	l.reconciler.logger.V(l.Level).Info("end reconciling resources")
	return nil
}

// DefaultRenderingListener is a base type with empty implementations for each callback.
type DefaultRenderingListener struct {
}

var _ RenderingListener = &DefaultRenderingListener{}

// BeginReconcile default implementation
func (l *DefaultRenderingListener) BeginReconcile(instance runtime.Object) error {
	return nil
}

// BeginDelete default implementation
func (l *DefaultRenderingListener) BeginDelete(instance runtime.Object) error {
	return nil
}

// BeginChart default implementation
func (l *DefaultRenderingListener) BeginChart(chart string, manifests []manifest.Manifest) ([]manifest.Manifest, error) {
	return manifests, nil
}

// BeginResource default implementation
func (l *DefaultRenderingListener) BeginResource(obj runtime.Object) (runtime.Object, error) {
	return obj, nil
}

// ResourceCreated default implementation
func (l *DefaultRenderingListener) ResourceCreated(created runtime.Object) error {
	return nil
}

// ResourceUpdated default implementation
func (l *DefaultRenderingListener) ResourceUpdated(updated runtime.Object, old runtime.Object) error {
	return nil
}

// ResourceDeleted default implementation
func (l *DefaultRenderingListener) ResourceDeleted(deleted runtime.Object) error {
	return nil
}

// ResourceError default implementation
func (l *DefaultRenderingListener) ResourceError(obj runtime.Object, err error) error {
	return nil
}

// EndResource default implementation
func (l *DefaultRenderingListener) EndResource(obj runtime.Object) error {
	return nil
}

// EndChart default implementation
func (l *DefaultRenderingListener) EndChart(chart string) error {
	return nil
}

// BeginPrune default implementation
func (l *DefaultRenderingListener) BeginPrune(all bool) error {
	return nil
}

// EndPrune default implementation
func (l *DefaultRenderingListener) EndPrune() error {
	return nil
}

// EndDelete default implementation
func (l *DefaultRenderingListener) EndDelete(instance runtime.Object, err error) error {
	return nil
}

// EndReconcile default implementation
func (l *DefaultRenderingListener) EndReconcile(instance runtime.Object, err error) error {
	return nil
}

// NewPruningMarkingsDecorator creates a new RenderingListener that applies PruningDetails to
// rendered resources.
// pruneDetails are the PruningDetails (owner labels and owner annotations) to be applied to the resources
func NewPruningMarkingsDecorator(pruneDetails PruningDetails) RenderingListener {
	return &pruningMarkingsDecorator{DefaultRenderingListener: &DefaultRenderingListener{}, pruningDetails: pruneDetails}
}

type pruningMarkingsDecorator struct {
	*DefaultRenderingListener
	pruningDetails PruningDetails
}

// BeginResource applies owner labels and annotations to the object
func (d *pruningMarkingsDecorator) BeginResource(obj runtime.Object) (runtime.Object, error) {
	for key, value := range d.pruningDetails.GetOwnerLabels() {
		err := common.SetLabel(obj, key, value)
		if err != nil {
			return obj, err
		}
	}
	for key, value := range d.pruningDetails.GetOwnerAnnotations() {
		err := common.SetAnnotation(obj, key, value)
		if err != nil {
			return obj, err
		}
	}
	return obj, nil
}

// NewOwnerReferenceDecorator creates a new OwnerReferenceDecorator that adds an OwnerReference,
// where applicable to rendered resources.
// instance is the owner (custom resource) of the rendered resources.
func NewOwnerReferenceDecorator(instance runtime.Object) (RenderingListener, error) {
	instanceAccessor, err := meta.Accessor(instance)
	if err != nil {
		return nil, err
	}
	return &ownerReferenceDecorator{
		DefaultRenderingListener: &DefaultRenderingListener{},
		ownerReference:           metav1.NewControllerRef(instanceAccessor, instance.GetObjectKind().GroupVersionKind()),
		namespace:                instanceAccessor.GetNamespace(),
	}, nil
}

type ownerReferenceDecorator struct {
	*DefaultRenderingListener
	ownerReference *metav1.OwnerReference
	namespace      string
}

// BeginResource adds an OwnerReference, where applicable (i.e. resource is namespaced and in same namespace
// as custom resource, or custom resource is non-namespaced), to the resource.
func (d *ownerReferenceDecorator) BeginResource(obj runtime.Object) (runtime.Object, error) {
	objAccessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	if len(d.namespace) > 0 && objAccessor.GetNamespace() != d.namespace {
		return obj, nil
	}
	existingOwnerReferences := objAccessor.GetOwnerReferences()
	for _, or := range existingOwnerReferences {
		if or.UID == d.ownerReference.UID {
			return obj, nil
		}
	}
	objAccessor.SetOwnerReferences(append(existingOwnerReferences, *d.ownerReference))
	return obj, nil
}
