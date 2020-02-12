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
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/patch"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/util/gogoprotomarshal"
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
	componentName name.ComponentName
	// addonName is the name of the addon component.
	addonName string
	// resourceName is the name of all resources for this component.
	resourceName string
	// index is the index of the component (only used for components with multiple instances like gateways).
	index int
	// spec for the actual component.
	spec interface{}
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
	case name.GalleyComponentName:
		component = NewGalleyComponent(opts)
	case name.SidecarInjectorComponentName:
		component = NewSidecarInjectorComponent(opts)
	case name.PolicyComponentName:
		component = NewPolicyComponent(opts)
	case name.TelemetryComponentName:
		component = NewTelemetryComponent(opts)
	case name.CitadelComponentName:
		component = NewCitadelComponent(opts)
	case name.NodeAgentComponentName:
		component = NewNodeAgentComponent(opts)
	case name.CNIComponentName:
		component = NewCNIComponent(opts)
	default:
		panic("Unknown component componentName: " + string(cn))
	}
	return component
}

// CRDComponent is the pilot component.
type CRDComponent struct {
	*CommonComponentFields
}

// NewCRDComponent creates a new CRDComponent and returns a pointer to it.
func NewCRDComponent(opts *Options) *CRDComponent {
	return &CRDComponent{
		&CommonComponentFields{
			Options:       opts,
			componentName: name.IstioBaseComponentName,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *CRDComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *CRDComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *CRDComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *CRDComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *CRDComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *CRDComponent) Enabled() bool {
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
			componentName: cn,
			resourceName:  opts.Translator.ComponentMaps[cn].ResourceName,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *PilotComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *PilotComponent) RenderManifest() (string, error) {
	baseYAML, err := renderManifest(c, c.CommonComponentFields)
	if err != nil {
		return "", err
	}

	return c.overlayMeshConfig(baseYAML)
}

func (c *PilotComponent) overlayMeshConfig(baseYAML string) (string, error) {
	if c.CommonComponentFields.InstallSpec.MeshConfig == nil {
		return baseYAML, nil
	}

	// Overlay MeshConfig onto the istio configmap
	baseObjs, err := object.ParseK8sObjectsFromYAMLManifest(baseYAML)
	if err != nil {
		return "", err
	}

	for _, obj := range baseObjs {
		if !isMeshConfigMap(obj) {
			continue
		}

		u := obj.UnstructuredObject()

		// Ignore any configMap that isn't of the format we're expecting
		meshStr, ok, err := unstructured.NestedString(u.Object, "data", "mesh")
		if !ok || err != nil {
			continue
		}

		meshOverride, err := gogoprotomarshal.ToYAML(c.CommonComponentFields.InstallSpec.MeshConfig)
		if err != nil {
			return "", err
		}

		// Merge the MeshConfig yaml on top of whatever is in the configMap already
		meshStr, err = util.OverlayYAML(meshStr, meshOverride)
		if err != nil {
			return "", err
		}

		meshStr = strings.TrimSpace(meshStr)

		log.Debugf("Merged MeshConfig:\n%s\n", meshStr)

		// Set the new yaml string back into the configMap
		if err := unstructured.SetNestedField(u.Object, meshStr, "data", "mesh"); err != nil {
			return "", err
		}

		newObj := object.NewK8sObject(u, nil, nil)

		// Replace the unstructured object in the slice
		*obj = *newObj

		return baseObjs.YAMLManifest()
	}

	return baseYAML, nil
}

func isMeshConfigMap(obj *object.K8sObject) bool {
	switch {
	case obj.Kind != "ConfigMap", !strings.HasPrefix(obj.Name, "istio"):
		return false
	default:
		return true
	}
}

// ComponentName implements the IstioComponent interface.
func (c *PilotComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *PilotComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *PilotComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *PilotComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// CitadelComponent is the pilot component.
type CitadelComponent struct {
	*CommonComponentFields
}

// NewCitadelComponent creates a new PilotComponent and returns a pointer to it.
func NewCitadelComponent(opts *Options) *CitadelComponent {
	cn := name.CitadelComponentName
	return &CitadelComponent{
		&CommonComponentFields{
			Options:       opts,
			componentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *CitadelComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *CitadelComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *CitadelComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *CitadelComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *CitadelComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *CitadelComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// NodeAgentComponent is the pilot component.
type NodeAgentComponent struct {
	*CommonComponentFields
}

// NewNodeAgentComponent creates a new PilotComponent and returns a pointer to it.
func NewNodeAgentComponent(opts *Options) *NodeAgentComponent {
	cn := name.NodeAgentComponentName
	return &NodeAgentComponent{
		&CommonComponentFields{
			Options:       opts,
			componentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *NodeAgentComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *NodeAgentComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *NodeAgentComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *NodeAgentComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *NodeAgentComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *NodeAgentComponent) Enabled() bool {
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
			componentName: cn,
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
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *PolicyComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
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
			componentName: cn,
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
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *TelemetryComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *TelemetryComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *TelemetryComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// GalleyComponent is the pilot component.
type GalleyComponent struct {
	*CommonComponentFields
}

// NewGalleyComponent creates a new PilotComponent and returns a pointer to it.
func NewGalleyComponent(opts *Options) *GalleyComponent {
	cn := name.GalleyComponentName
	return &GalleyComponent{
		&CommonComponentFields{
			Options:       opts,
			componentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *GalleyComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *GalleyComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *GalleyComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *GalleyComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *GalleyComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *GalleyComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// SidecarInjectorComponent is the pilot component.
type SidecarInjectorComponent struct {
	*CommonComponentFields
}

// NewSidecarInjectorComponent creates a new PilotComponent and returns a pointer to it.
func NewSidecarInjectorComponent(opts *Options) *SidecarInjectorComponent {
	cn := name.SidecarInjectorComponentName
	return &SidecarInjectorComponent{
		&CommonComponentFields{
			Options:       opts,
			componentName: cn,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *SidecarInjectorComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *SidecarInjectorComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *SidecarInjectorComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *SidecarInjectorComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *SidecarInjectorComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *SidecarInjectorComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}

// CNIComponent is the egress gateway component.
type CNIComponent struct {
	*CommonComponentFields
}

// NewCNIComponent creates a new IngressComponent and returns a pointer to it.
func NewCNIComponent(opts *Options) *CNIComponent {
	cn := name.CNIComponentName
	return &CNIComponent{
		&CommonComponentFields{
			Options:       opts,
			componentName: cn,
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
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *CNIComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *CNIComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *CNIComponent) Enabled() bool {
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
			componentName: cn,
			resourceName:  resourceName,
			index:         index,
			spec:          spec,
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
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *IngressComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *IngressComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *IngressComponent) Enabled() bool {
	// type assert is guaranteed to work in this context.
	return boolValue(c.spec.(*v1alpha1.GatewaySpec).Enabled)
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
			componentName: cn,
			index:         index,
			spec:          spec,
			resourceName:  resourceName,
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
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *EgressComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *EgressComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *EgressComponent) Enabled() bool {
	// type assert is guaranteed to work in this context.
	return boolValue(c.spec.(*v1alpha1.GatewaySpec).Enabled)
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
			componentName: name.AddonComponentName,
			resourceName:  resourceName,
			addonName:     addonName,
			spec:          spec,
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
	return c.CommonComponentFields.componentName
}

// ResourceName implements the IstioComponent interface.
func (c *AddonComponent) ResourceName() string {
	return c.CommonComponentFields.resourceName
}

// Namespace implements the IstioComponent interface.
func (c *AddonComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *AddonComponent) Enabled() bool {
	// type assert is guaranteed to work in this context.
	return boolValue(c.spec.(*v1alpha1.ExternalComponentSpec).Enabled)
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
		return "", fmt.Errorf("component %s not started in RenderManifest", cf.componentName)
	}

	if !c.Enabled() {
		return disabledYAMLStr(cf.componentName, cf.resourceName), nil
	}

	mergedYAML, err := cf.Translator.TranslateHelmValues(cf.InstallSpec, cf.componentName)
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
		log.Infof("Initial manifest with merged values:\n%s\n", my)
	}
	// Add the k8s resources from IstioOperatorSpec.
	my, err = cf.Translator.OverlayK8sSettings(my, cf.InstallSpec, cf.componentName, cf.index)
	if err != nil {
		log.Errorf("Error in OverlayK8sSettings: %s", err)
		return "", err
	}
	cnOutput := string(cf.componentName)
	if !cf.componentName.IsCoreComponent() && !cf.componentName.IsGateway() {
		cnOutput += " " + cf.addonName
	}
	my = "# Resources for " + cnOutput + " component\n\n" + my
	if devDbg {
		log.Infof("Manifest after k8s API settings:\n%s\n", my)
	}
	// Add the k8s resource overlays from IstioOperatorSpec.
	pathToK8sOverlay := fmt.Sprintf("Components.%s.", cf.componentName)
	if cf.componentName == name.IngressComponentName || cf.componentName == name.EgressComponentName {
		pathToK8sOverlay += fmt.Sprintf("%d.", cf.index)
	}
	pathToK8sOverlay += fmt.Sprintf("K8S.Overlays")
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
	log.Infof("Applying kubernetes overlay: \n%s\n", kyo)
	ret, err := patch.YAMLManifestPatch(my, cf.Namespace, overlays)
	if err != nil {
		return "", err
	}

	log.Infof("Manifest after resources and overlay: \n%s\n", ret)
	return ret, nil
}

// createHelmRenderer creates a helm renderer for the component defined by c and returns a ptr to it.
// If a helm subdir is not found in ComponentMap translations, it is assumed to be "addon/<component name>.
func createHelmRenderer(c *CommonComponentFields) (helm.TemplateRenderer, error) {
	iop := c.InstallSpec
	cns := string(c.componentName)
	if c.componentName.IsAddon() {
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
	enabled, err := c.Translator.IsComponentEnabled(c.componentName, c.InstallSpec)
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
