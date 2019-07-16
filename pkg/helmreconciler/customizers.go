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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/helm/pkg/manifest"

	"istio.io/operator/pkg/util"
)

// SimpleRenderingCustomizer provides the basics needed for a RenderingCustomizer composed of static instances.
type SimpleRenderingCustomizer struct {
	// InputValue represents the RenderingInput for this customizer
	InputValue RenderingInput
	// PruningDetailsValue represents the PruningDetails for this customizer
	PruningDetailsValue PruningDetails
	// ListenerValue represents the RenderingListener for this customizer
	ListenerValue RenderingListener
}

var _ RenderingCustomizer = &SimpleRenderingCustomizer{}
var _ ReconcilerListener = &SimpleRenderingCustomizer{}

func (c *SimpleRenderingCustomizer) Input() RenderingInput {
	return c.InputValue
}

func (c *SimpleRenderingCustomizer) PruningDetails() PruningDetails {
	return c.PruningDetailsValue
}

func (c *SimpleRenderingCustomizer) Listener() RenderingListener {
	return c.ListenerValue
}

func (c *SimpleRenderingCustomizer) RegisterReconciler(reconciler *HelmReconciler) {
	if registrar, ok := c.InputValue.(ReconcilerListener); ok {
		registrar.RegisterReconciler(reconciler)
	}
	if registrar, ok := c.PruningDetailsValue.(ReconcilerListener); ok {
		registrar.RegisterReconciler(reconciler)
	}
	if registrar, ok := c.ListenerValue.(ReconcilerListener); ok {
		registrar.RegisterReconciler(reconciler)
	}
}

// SimplePruningDetails is a helper to implement PruningDetails from a known set of labels,
// annotations, and resource types.
type SimplePruningDetails struct {
	// OwnerLabels to be added to all rendered resources.
	OwnerLabels map[string]string
	// OwnerAnnotations to be added to all rendered resources.
	OwnerAnnotations map[string]string
	// NamespacedResources rendered by these charts
	NamespacedResources []schema.GroupVersionKind
	// NonNamespacedResources rendered by these charts
	NonNamespacedResources []schema.GroupVersionKind
}

var _ PruningDetails = &SimplePruningDetails{}

// GetOwnerLabels returns this.OwnerLabels
func (m *SimplePruningDetails) GetOwnerLabels() map[string]string {
	if m.OwnerLabels == nil {
		return map[string]string{}
	}
	return m.OwnerLabels
}

// GetOwnerAnnotations returns this.OwnerAnnotations
func (m *SimplePruningDetails) GetOwnerAnnotations() map[string]string {
	if m.OwnerAnnotations == nil {
		return map[string]string{}
	}
	return m.OwnerAnnotations
}

// GetResourceTypes returns this.NamespacedResources and this.NonNamespacedResources
func (m *SimplePruningDetails) GetResourceTypes() (namespaced []schema.GroupVersionKind, nonNamespaced []schema.GroupVersionKind) {
	return m.NamespacedResources, m.NonNamespacedResources
}

// DefaultChartCustomizerFactory is a factory for creating DefaultChartCustomizer objects
type DefaultChartCustomizerFactory struct {
	// ChartAnnotationKey is the key used to add an annotation identifying the chart that rendered the resource
	// to the rendered resource.
	ChartAnnotationKey string
}

var _ ChartCustomizerFactory = &DefaultChartCustomizerFactory{}

// NewChartCustomizer returns a new DefaultChartCustomizer for the specified chart.
func (f *DefaultChartCustomizerFactory) NewChartCustomizer(chartName string) ChartCustomizer {
	return NewDefaultChartCustomizer(chartName, f.ChartAnnotationKey)
}

// DefaultChartCustomizerListener manages ChartCustomizer objects for a rendering.
type DefaultChartCustomizerListener struct {
	*DefaultRenderingListener
	// ChartCustomizerFactory is the factory used to create ChartCustomizer objects for each chart
	// encountered during rendering.
	ChartCustomizerFactory ChartCustomizerFactory
	// ChartAnnotationKey represents the annotation key in which the chart name is stored on the rendered resource.
	ChartAnnotationKey string
	reconciler         *HelmReconciler
	customizers        map[string]ChartCustomizer
	customizer         ChartCustomizer
}

var _ RenderingListener = &DefaultChartCustomizerListener{}
var _ ReconcilerListener = &DefaultChartCustomizerListener{}

// NewDefaultChartCustomizerListener creates a new DefaultChartCustomizerListener which creates DefaultChartCustomizer
// objects for each chart (which simply adds a chart owner annotation to each rendered resource).
// The ChartCustomizerFactory may be modified by users to create custom ChartCustomizer objects.
func NewDefaultChartCustomizerListener(chartAnnotationKey string) *DefaultChartCustomizerListener {
	return &DefaultChartCustomizerListener{
		DefaultRenderingListener: &DefaultRenderingListener{},
		ChartCustomizerFactory:   &DefaultChartCustomizerFactory{ChartAnnotationKey: chartAnnotationKey},
		ChartAnnotationKey:       chartAnnotationKey,
		customizers:              map[string]ChartCustomizer{},
	}
}

// RegisterReconciler registers the HelmReconciler with the listener.
func (l *DefaultChartCustomizerListener) RegisterReconciler(reconciler *HelmReconciler) {
	l.reconciler = reconciler
}

// BeginChart creates a new ChartCustomizer for the specified chart and delegates listener calls applying to resources
// (e.g. BeginResource) to the customizer up through EndChart.
func (l *DefaultChartCustomizerListener) BeginChart(chartName string, manifests []manifest.Manifest) ([]manifest.Manifest, error) {
	l.customizer = l.GetOrCreateCustomizer(chartName)
	return l.customizer.BeginChart(chartName, manifests)
}

// BeginResource delegates to the active ChartCustomizer's BeginResource
func (l *DefaultChartCustomizerListener) BeginResource(obj runtime.Object) (runtime.Object, error) {
	if l.customizer == nil {
		// XXX: what went wrong
		// this should actually be a warning
		l.reconciler.GetLogger().Info("no active chart customizer")
		return obj, nil
	}
	return l.customizer.BeginResource(obj)
}

// ResourceCreated delegates to the active ChartCustomizer's ResourceCreated
func (l *DefaultChartCustomizerListener) ResourceCreated(created runtime.Object) error {
	if l.customizer == nil {
		// XXX: what went wrong
		// this should actually be a warning
		l.reconciler.GetLogger().Info("no active chart customizer")
		return nil
	}
	return l.customizer.ResourceCreated(created)
}

// ResourceUpdated delegates to the active ChartCustomizer's ResourceUpdated
func (l *DefaultChartCustomizerListener) ResourceUpdated(updated runtime.Object, old runtime.Object) error {
	if l.customizer == nil {
		// XXX: what went wrong
		// this should actually be a warning
		l.reconciler.GetLogger().Info("no active chart customizer")
		return nil
	}
	return l.customizer.ResourceUpdated(updated, old)
}

// ResourceError delegates to the active ChartCustomizer's ResourceError
func (l *DefaultChartCustomizerListener) ResourceError(obj runtime.Object, err error) error {
	if l.customizer == nil {
		// XXX: what went wrong
		// this should actually be a warning
		l.reconciler.GetLogger().Info("no active chart customizer")
		return nil
	}
	return l.customizer.ResourceError(obj, err)
}

// EndResource delegates to the active ChartCustomizer's EndResource
func (l *DefaultChartCustomizerListener) EndResource(obj runtime.Object) error {
	if l.customizer == nil {
		// XXX: what went wrong
		// this should actually be a warning
		l.reconciler.GetLogger().Info("no active chart customizer")
		return nil
	}
	return l.customizer.EndResource(obj)
}

// EndChart delegates to the active ChartCustomizer's EndChart and resets the active ChartCustomizer to nil.
func (l *DefaultChartCustomizerListener) EndChart(chartName string) error {
	if l.customizer == nil {
		return nil
	}
	err := l.customizer.EndChart(chartName)
	l.customizer = nil
	return err
}

// ResourceDeleted looks up the ChartCustomizer for the object that was deleted and invokes its ResourceDeleted method.
func (l *DefaultChartCustomizerListener) ResourceDeleted(deleted runtime.Object) error {
	if chartName, ok := util.GetAnnotation(deleted, l.ChartAnnotationKey); ok && len(chartName) > 0 {
		customizer := l.GetOrCreateCustomizer(chartName)
		return customizer.ResourceDeleted(deleted)
	}
	return nil
}

// GetOrCreateCustomizer does what it says.
func (l *DefaultChartCustomizerListener) GetOrCreateCustomizer(chartName string) ChartCustomizer {
	var ok bool
	var customizer ChartCustomizer
	if customizer, ok = l.customizers[chartName]; !ok {
		customizer = l.ChartCustomizerFactory.NewChartCustomizer(chartName)
		if reconcilerListener, ok := customizer.(ReconcilerListener); ok {
			reconcilerListener.RegisterReconciler(l.reconciler)
		}
		l.customizers[chartName] = customizer
	}
	return customizer
}

// DefaultChartCustomizer is a ChartCustomizer that collects resources created/deleted during rendering and adds
// a chart annotation to rendered resources.
type DefaultChartCustomizer struct {
	ChartName              string
	ChartAnnotationKey     string
	Reconciler             *HelmReconciler
	NewResourcesByKind     map[string][]runtime.Object
	DeletedResourcesByKind map[string][]runtime.Object
}

var _ ChartCustomizer = &DefaultChartCustomizer{}

// NewDefaultChartCustomizer creates a new DefaultChartCustomizer
func NewDefaultChartCustomizer(chartName, chartAnnotationKey string) *DefaultChartCustomizer {
	return &DefaultChartCustomizer{
		ChartName:              chartName,
		ChartAnnotationKey:     chartAnnotationKey,
		NewResourcesByKind:     map[string][]runtime.Object{},
		DeletedResourcesByKind: map[string][]runtime.Object{},
	}
}

// RegisterReconciler registers the HelmReconciler with this.
func (c *DefaultChartCustomizer) RegisterReconciler(reconciler *HelmReconciler) {
	c.Reconciler = reconciler
}

// BeginChart empty implementation
func (c *DefaultChartCustomizer) BeginChart(chart string, manifests []manifest.Manifest) ([]manifest.Manifest, error) {
	return manifests, nil
}

// BeginResource adds the chart annotation to the resource (ChartAnnotationKey=ChartName)
func (c *DefaultChartCustomizer) BeginResource(obj runtime.Object) (runtime.Object, error) {
	var err error
	if len(c.ChartName) > 0 && len(c.ChartAnnotationKey) > 0 {
		err = util.SetAnnotation(obj, c.ChartAnnotationKey, c.ChartName)
	}
	return obj, err
}

// ResourceCreated adds the created object to NewResourcesByKind
func (c *DefaultChartCustomizer) ResourceCreated(created runtime.Object) error {
	kind := created.GetObjectKind().GroupVersionKind().Kind
	objects, ok := c.NewResourcesByKind[kind]
	if !ok {
		objects = []runtime.Object{}
	}
	c.NewResourcesByKind[kind] = append(objects, created)
	return nil
}

// ResourceUpdated adds the updated object to NewResourcesByKind
func (c *DefaultChartCustomizer) ResourceUpdated(updated, old runtime.Object) error {
	kind := updated.GetObjectKind().GroupVersionKind().Kind
	objects, ok := c.NewResourcesByKind[kind]
	if !ok {
		objects = []runtime.Object{}
	}
	c.NewResourcesByKind[kind] = append(objects, updated)
	return nil
}

// ResourceDeleted adds the deleted object to DeletedResourcesByKind
func (c *DefaultChartCustomizer) ResourceDeleted(deleted runtime.Object) error {
	kind := deleted.GetObjectKind().GroupVersionKind().Kind
	objects, ok := c.DeletedResourcesByKind[kind]
	if !ok {
		objects = []runtime.Object{}
	}
	c.DeletedResourcesByKind[kind] = append(objects, deleted)
	return nil
}

// ResourceError empty implementation
func (c *DefaultChartCustomizer) ResourceError(obj runtime.Object, err error) error {
	return nil
}

// EndResource empty implementation
func (c *DefaultChartCustomizer) EndResource(obj runtime.Object) error {
	return nil
}

// EndChart empty implementation
func (c *DefaultChartCustomizer) EndChart(chart string) error {
	return nil
}
