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

/*
Package component defines an in-memory representation of IstioOperator.<Feature>.<Component>. It provides functions
for manipulating the component and rendering a manifest from it.
See ../README.md for an architecture overview.
*/
package component

import (
	"fmt"

	"sigs.k8s.io/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/metrics"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/patch"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/pkg/log"
)

const (
	// String to emit for any component which is disabled.
	componentDisabledStr = "component is disabled."
	yamlCommentStr       = "#"
)

var scope = log.RegisterScope("installer", "installer", 0)

// Options defines options for a component.
type Options struct {
	// installSpec is the global IstioOperatorSpec.
	InstallSpec *v1alpha1.IstioOperatorSpec
	// translator is the translator for this component.
	Translator *translate.Translator
	// Namespace is the namespace for this component.
	Namespace string
	// Filter is the filenames to render
	Filter sets.Set
}

// IstioComponent defines the interface for a component.
type IstioComponent interface {
	// ComponentName returns the name of the component.
	ComponentName() name.ComponentName
	// ResourceName returns the name of the resources of the component.
	ResourceName() string
	// Namespace returns the namespace for the component.
	Namespace() string
	// Enabled reports whether the component is enabled.
	Enabled() bool
	// Run starts the component. Must me called before the component is used.
	Run() error
	// RenderManifest returns a string with the rendered manifest for the component.
	RenderManifest() (string, error)
}

// CommonComponentFields is a struct common to all components.
type CommonComponentFields struct {
	*Options
	ComponentName name.ComponentName
	// resourceName is the name of all resources for this component.
	ResourceName string
	// index is the index of the component (only used for components with multiple instances like gateways).
	index int
	// componentSpec for the actual component e.g. GatewaySpec, ComponentSpec.
	componentSpec interface{}
	// started reports whether the component is in initialized and running.
	started  bool
	renderer helm.TemplateRenderer
}

// NewCoreComponent creates a new IstioComponent with the given componentName and options.
func NewCoreComponent(cn name.ComponentName, opts *Options) IstioComponent {
	var component IstioComponent
	switch cn {
	case name.IstioBaseComponentName:
		component = NewCRDComponent(opts)
	case name.PilotComponentName:
		component = NewPilotComponent(opts)
	case name.CNIComponentName:
		component = NewCNIComponent(opts)
	case name.IstiodRemoteComponentName:
		component = NewIstiodRemoteComponent(opts)
	default:
		scope.Errorf("Unknown component componentName: " + string(cn))
	}
	return component
}

// BaseComponent is the base component.
type BaseComponent struct {
	*CommonComponentFields
}

// NewCRDComponent creates a new BaseComponent and returns a pointer to it.
func NewCRDComponent(opts *Options) *BaseComponent {
	return &BaseComponent{
		&CommonComponentFields{
			Options:       opts,
			ComponentName: name.IstioBaseComponentName,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *BaseComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *BaseComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *BaseComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *BaseComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *BaseComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *BaseComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// PilotComponent is the pilot component.
type PilotComponent struct {
	*CommonComponentFields
}

// NewPilotComponent creates a new PilotComponent and returns a pointer to it.
func NewPilotComponent(opts *Options) *PilotComponent {
	cn := name.PilotComponentName
	return &PilotComponent{
		&CommonComponentFields{
			Options:       opts,
			ComponentName: cn,
			ResourceName:  opts.Translator.ComponentMaps[cn].ResourceName,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *PilotComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *PilotComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *PilotComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *PilotComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *PilotComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *PilotComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// CNIComponent is the istio cni component.
type CNIComponent struct {
	*CommonComponentFields
}

// NewCNIComponent creates a new NewCNIComponent and returns a pointer to it.
func NewCNIComponent(opts *Options) *CNIComponent {
	cn := name.CNIComponentName
	return &CNIComponent{
		&CommonComponentFields{
			Options:       opts,
			ComponentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *CNIComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *CNIComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *CNIComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *CNIComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *CNIComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *CNIComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// IstiodRemoteComponent is the istiod remote component.
type IstiodRemoteComponent struct {
	*CommonComponentFields
}

// NewIstiodRemoteComponent creates a new NewIstiodRemoteComponent and returns a pointer to it.
func NewIstiodRemoteComponent(opts *Options) *IstiodRemoteComponent {
	cn := name.IstiodRemoteComponentName
	return &IstiodRemoteComponent{
		&CommonComponentFields{
			Options:       opts,
			ComponentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *IstiodRemoteComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *IstiodRemoteComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *IstiodRemoteComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *IstiodRemoteComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *IstiodRemoteComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *IstiodRemoteComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// IngressComponent is the ingress gateway component.
type IngressComponent struct {
	*CommonComponentFields
}

// NewIngressComponent creates a new IngressComponent and returns a pointer to it.
func NewIngressComponent(resourceName string, index int, spec *v1alpha1.GatewaySpec, opts *Options) *IngressComponent {
	cn := name.IngressComponentName
	return &IngressComponent{
		CommonComponentFields: &CommonComponentFields{
			Options:       opts,
			ComponentName: cn,
			ResourceName:  resourceName,
			index:         index,
			componentSpec: spec,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *IngressComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *IngressComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *IngressComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *IngressComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *IngressComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *IngressComponent) Enabled() bool {
	// type assert is guaranteed to work in this context.
	return c.componentSpec.(*v1alpha1.GatewaySpec).Enabled.GetValue()
}

// EgressComponent is the egress gateway component.
type EgressComponent struct {
	*CommonComponentFields
}

// NewEgressComponent creates a new IngressComponent and returns a pointer to it.
func NewEgressComponent(resourceName string, index int, spec *v1alpha1.GatewaySpec, opts *Options) *EgressComponent {
	cn := name.EgressComponentName
	return &EgressComponent{
		CommonComponentFields: &CommonComponentFields{
			Options:       opts,
			ComponentName: cn,
			index:         index,
			componentSpec: spec,
			ResourceName:  resourceName,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *EgressComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *EgressComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *EgressComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *EgressComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *EgressComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *EgressComponent) Enabled() bool {
	// type assert is guaranteed to work in this context.
	return c.componentSpec.(*v1alpha1.GatewaySpec).Enabled.GetValue()
}

// runComponent performs startup tasks for the component defined by the given CommonComponentFields.
func runComponent(c *CommonComponentFields) error {
	r := createHelmRenderer(c)
	if err := r.Run(); err != nil {
		return err
	}
	c.renderer = r
	c.started = true
	return nil
}

// renderManifest renders the manifest for the component defined by c and returns the resulting string.
func renderManifest(c IstioComponent, cf *CommonComponentFields) (string, error) {
	if !cf.started {
		metrics.CountManifestRenderError(c.ComponentName(), metrics.RenderNotStartedError)
		return "", fmt.Errorf("component %s not started in RenderManifest", cf.ComponentName)
	}

	if !c.Enabled() {
		return disabledYAMLStr(cf.ComponentName, cf.ResourceName), nil
	}

	mergedYAML, err := cf.Translator.TranslateHelmValues(cf.InstallSpec, cf.componentSpec, cf.ComponentName)
	if err != nil {
		metrics.CountManifestRenderError(c.ComponentName(), metrics.HelmTranslateIOPToValuesError)
		return "", err
	}

	scope.Debugf("Merged values:\n%s\n", mergedYAML)

	my, err := cf.renderer.RenderManifestFiltered(mergedYAML, func(s string) bool {
		return len(cf.Filter) == 0 || cf.Filter.Contains(s)
	})
	if err != nil {
		log.Errorf("Error rendering the manifest: %s", err)
		metrics.CountManifestRenderError(c.ComponentName(), metrics.HelmChartRenderError)
		return "", err
	}
	my += helm.YAMLSeparator + "\n"
	scope.Debugf("Initial manifest with merged values:\n%s\n", my)

	// Add the k8s resources from IstioOperatorSpec.
	my, err = cf.Translator.OverlayK8sSettings(my, cf.InstallSpec, cf.ComponentName, cf.ResourceName, cf.index)
	if err != nil {
		metrics.CountManifestRenderError(c.ComponentName(), metrics.K8SSettingsOverlayError)
		return "", err
	}
	cnOutput := string(cf.ComponentName)
	my = "# Resources for " + cnOutput + " component\n\n" + my
	scope.Debugf("Manifest after k8s API settings:\n%s\n", my)

	// Add the k8s resource overlays from IstioOperatorSpec.
	pathToK8sOverlay := fmt.Sprintf("Components.%s.", cf.ComponentName)
	if cf.ComponentName == name.IngressComponentName || cf.ComponentName == name.EgressComponentName {
		pathToK8sOverlay += fmt.Sprintf("%d.", cf.index)
	}

	pathToK8sOverlay += "K8S.Overlays"
	var overlays []*v1alpha1.K8SObjectOverlay
	found, err := tpath.SetFromPath(cf.InstallSpec, pathToK8sOverlay, &overlays)
	if err != nil {
		return "", err
	}
	if !found {
		scope.Debugf("Manifest after resources: \n%s\n", my)
		metrics.CountManifestRender(cf.ComponentName)
		return my, nil
	}
	kyo, err := yaml.Marshal(overlays)
	if err != nil {
		return "", err
	}
	scope.Infof("Applying Kubernetes overlay: \n%s\n", kyo)
	ret, err := patch.YAMLManifestPatch(my, cf.Namespace, overlays)
	if err != nil {
		metrics.CountManifestRenderError(c.ComponentName(), metrics.K8SManifestPatchError)
		return "", err
	}

	scope.Debugf("Manifest after resources and overlay: \n%s\n", ret)
	metrics.CountManifestRender(cf.ComponentName)
	return ret, nil
}

// createHelmRenderer creates a helm renderer for the component defined by c and returns a ptr to it.
// If a helm subdir is not found in ComponentMap translations, it is assumed to be "addon/<component name>.
func createHelmRenderer(c *CommonComponentFields) helm.TemplateRenderer {
	iop := c.InstallSpec
	cns := string(c.ComponentName)
	helmSubdir := c.Translator.ComponentMap(cns).HelmSubdir
	return helm.NewHelmRenderer(iop.InstallPackagePath, helmSubdir, cns, c.Namespace)
}

func isCoreComponentEnabled(c *CommonComponentFields) bool {
	enabled, err := c.Translator.IsComponentEnabled(c.ComponentName, c.InstallSpec)
	if err != nil {
		return false
	}
	return enabled
}

// disabledYAMLStr returns the YAML comment string that the given component is disabled.
func disabledYAMLStr(componentName name.ComponentName, resourceName string) string {
	fullName := string(componentName)
	if resourceName != "" {
		fullName += " " + resourceName
	}
	return fmt.Sprintf("%s %s %s\n", yamlCommentStr, fullName, componentDisabledStr)
}
