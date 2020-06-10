// Copyright 2017 Istio Authors
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

	"github.com/ghodss/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/patch"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/pkg/log"
)

const (
	// addonsChartDirName is the default subdir for all addon charts.
	addonsChartDirName = "addons"
	// String to emit for any component which is disabled.
	componentDisabledStr = "component is disabled."
	yamlCommentStr       = "#"

	// devDbg generates lots of output useful in development.
	devDbg = false
)

var (
	scope = log.RegisterScope("installer", "installer", 0)
)

// Options defines options for a component.
type Options struct {
	// installSpec is the global IstioOperatorSpec.
	InstallSpec *v1alpha1.IstioOperatorSpec
	// translator is the translator for this component.
	Translator *translate.Translator
	// Namespace is the namespace for this component.
	Namespace string
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
	// addonName is the name of the addon component.
	addonName string
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
	case name.PolicyComponentName:
		component = NewPolicyComponent(opts)
	case name.TelemetryComponentName:
		component = NewTelemetryComponent(opts)
	case name.CNIComponentName:
		component = NewCNIComponent(opts)
	case name.IstiodRemoteComponentName:
		component = NewIstiodRemoteComponent(opts)
	default:
		panic("Unknown component componentName: " + string(cn))
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

// PolicyComponent is the pilot component.
type PolicyComponent struct {
	*CommonComponentFields
}

// NewPolicyComponent creates a new PilotComponent and returns a pointer to it.
func NewPolicyComponent(opts *Options) *PolicyComponent {
	cn := name.PolicyComponentName
	return &PolicyComponent{
		&CommonComponentFields{
			Options:       opts,
			ComponentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *PolicyComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *PolicyComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *PolicyComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *PolicyComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *PolicyComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *PolicyComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// TelemetryComponent is the pilot component.
type TelemetryComponent struct {
	*CommonComponentFields
}

// NewTelemetryComponent creates a new PilotComponent and returns a pointer to it.
func NewTelemetryComponent(opts *Options) *TelemetryComponent {
	cn := name.TelemetryComponentName
	return &TelemetryComponent{
		&CommonComponentFields{
			Options:       opts,
			ComponentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *TelemetryComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *TelemetryComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *TelemetryComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *TelemetryComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *TelemetryComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *TelemetryComponent) Enabled() bool {
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
	return boolValue(c.componentSpec.(*v1alpha1.GatewaySpec).Enabled)
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
	return boolValue(c.componentSpec.(*v1alpha1.GatewaySpec).Enabled)
}

// AddonComponent is an external component.
type AddonComponent struct {
	*CommonComponentFields
}

// NewAddonComponent creates a new IngressComponent and returns a pointer to it.
func NewAddonComponent(addonName, resourceName string, spec *v1alpha1.ExternalComponentSpec, opts *Options) *AddonComponent {
	return &AddonComponent{
		CommonComponentFields: &CommonComponentFields{
			Options:       opts,
			ComponentName: name.AddonComponentName,
			ResourceName:  resourceName,
			addonName:     addonName,
			componentSpec: spec,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *AddonComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *AddonComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *AddonComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *AddonComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *AddonComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *AddonComponent) Enabled() bool {
	// type assert is guaranteed to work in this context.
	return boolValue(c.componentSpec.(*v1alpha1.ExternalComponentSpec).Enabled)
}

// runComponent performs startup tasks for the component defined by the given CommonComponentFields.
func runComponent(c *CommonComponentFields) error {
	r, err := createHelmRenderer(c)
	if err != nil {
		return err
	}
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
		return "", fmt.Errorf("component %s not started in RenderManifest", cf.ComponentName)
	}

	if !c.Enabled() {
		return disabledYAMLStr(cf.ComponentName, cf.ResourceName), nil
	}

	mergedYAML, err := cf.Translator.TranslateHelmValues(cf.InstallSpec, cf.componentSpec, cf.ComponentName)
	if err != nil {
		return "", err
	}

	log.Debugf("Merged values:\n%s\n", mergedYAML)

	my, err := cf.renderer.RenderManifest(mergedYAML)
	if err != nil {
		log.Errorf("Error rendering the manifest: %s", err)
		return "", err
	}
	my += helm.YAMLSeparator + "\n"
	if devDbg {
		scope.Infof("Initial manifest with merged values:\n%s\n", my)
	}
	// Add the k8s resources from IstioOperatorSpec.
	my, err = cf.Translator.OverlayK8sSettings(my, cf.InstallSpec, cf.ComponentName, cf.ResourceName, cf.addonName, cf.index)
	if err != nil {
		return "", err
	}
	cnOutput := string(cf.ComponentName)
	if !cf.ComponentName.IsCoreComponent() && !cf.ComponentName.IsGateway() {
		cnOutput += " " + cf.addonName
	}
	my = "# Resources for " + cnOutput + " component\n\n" + my
	if devDbg {
		scope.Infof("Manifest after k8s API settings:\n%s\n", my)
	}
	// Add the k8s resource overlays from IstioOperatorSpec.
	pathToK8sOverlay := ""
	if !cf.ComponentName.IsCoreComponent() && !cf.ComponentName.IsGateway() {
		pathToK8sOverlay += fmt.Sprintf("AddonComponents.%s.", cf.addonName)
	} else {
		pathToK8sOverlay += fmt.Sprintf("Components.%s.", cf.ComponentName)
		if cf.ComponentName == name.IngressComponentName || cf.ComponentName == name.EgressComponentName {
			pathToK8sOverlay += fmt.Sprintf("%d.", cf.index)
		}
	}
	pathToK8sOverlay += "K8S.Overlays"
	var overlays []*v1alpha1.K8SObjectOverlay
	found, err := tpath.SetFromPath(cf.InstallSpec, pathToK8sOverlay, &overlays)
	if err != nil {
		return "", err
	}
	if !found {
		log.Debugf("Manifest after resources: \n%s\n", my)
		return my, nil
	}
	kyo, err := yaml.Marshal(overlays)
	if err != nil {
		return "", err
	}
	scope.Infof("Applying Kubernetes overlay: \n%s\n", kyo)
	ret, err := patch.YAMLManifestPatch(my, cf.Namespace, overlays)
	if err != nil {
		return "", err
	}

	scope.Debugf("Manifest after resources and overlay: \n%s\n", ret)
	return ret, nil
}

// createHelmRenderer creates a helm renderer for the component defined by c and returns a ptr to it.
// If a helm subdir is not found in ComponentMap translations, it is assumed to be "addon/<component name>.
func createHelmRenderer(c *CommonComponentFields) (helm.TemplateRenderer, error) {
	iop := c.InstallSpec
	cns := string(c.ComponentName)
	if c.ComponentName.IsAddon() {
		// For addons, distinguish the chart path using the addon name.
		cns = c.addonName
	}
	helmSubdir := addonsChartDirName + "/" + cns
	if cm := c.Translator.ComponentMap(cns); cm != nil {
		helmSubdir = cm.HelmSubdir
	}
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

// boolValue returns true is v is not nil and v.Value is true, or false otherwise.
func boolValue(v *v1alpha1.BoolValueForPB) bool {
	if v == nil {
		return false
	}
	return v.Value
}
