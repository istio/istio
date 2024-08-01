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
package controlplane

import (
	"fmt"

	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/patch"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/pkg/log"
)

const (
	// String to emit for any component which is disabled.
	componentDisabledStr = "component is disabled."
	yamlCommentStr       = "#"
)

var scope = log.RegisterScope("installer", "installer")

// Options defines options for a component.
type options struct {
	// installSpec is the global IstioOperatorSpec.
	InstallSpec *v1alpha1.IstioOperatorSpec
	// translator is the translator for this component.
	Translator *translate.Translator
	// Namespace is the namespace for this component.
	Namespace string
	// Version is the Kubernetes version information.
	Version *version.Info
}

type istioComponent struct {
	*options
	ComponentName name.ComponentName
	// resourceName is the name of all resources for this component.
	ResourceName string
	// index is the index of the component (only used for components with multiple instances like gateways).
	index int
	// componentSpec for the actual component e.g. GatewaySpec, ComponentSpec.
	componentSpec any
}

func (c *istioComponent) Enabled() bool {
	if c.ComponentName.IsGateway() {
		// type assert is guaranteed to work in this context.
		return c.componentSpec.(*v1alpha1.GatewaySpec).Enabled.GetValue()
	}

	return c.isCoreComponentEnabled()
}

func (c *istioComponent) RenderManifest() (string, error) {
	return renderManifest(c)
}

// newCoreComponent creates a new istioComponent with the given componentName and options.
func newCoreComponent(cn name.ComponentName, opts *options) *istioComponent {
	var component *istioComponent
	switch cn {
	case name.IstioBaseComponentName:
		component = newCRDComponent(opts)
	case name.PilotComponentName:
		component = newPilotComponent(opts)
	case name.CNIComponentName:
		component = newCNIComponent(opts)
	case name.IstiodRemoteComponentName:
		component = newIstiodRemoteComponent(opts)
	case name.ZtunnelComponentName:
		component = newZtunnelComponent(opts)
	default:
		scope.Errorf("Unknown component componentName: " + string(cn))
	}
	return component
}

// newCRDComponent creates a new istioComponent and returns a pointer to it.
func newCRDComponent(opts *options) *istioComponent {
	return &istioComponent{
		options:       opts,
		ComponentName: name.IstioBaseComponentName,
	}
}

// newPilotComponent creates a new istioComponent and returns a pointer to it.
func newPilotComponent(opts *options) *istioComponent {
	cn := name.PilotComponentName
	return &istioComponent{
		options:       opts,
		ComponentName: cn,
		ResourceName:  opts.Translator.ComponentMaps[cn].ResourceName,
	}
}

// newCNIComponent creates a new istioComponent and returns a pointer to it.
func newCNIComponent(opts *options) *istioComponent {
	cn := name.CNIComponentName
	return &istioComponent{
		options:       opts,
		ComponentName: cn,
	}
}

// newIstiodRemoteComponent creates a new istioComponent and returns a pointer to it.
func newIstiodRemoteComponent(opts *options) *istioComponent {
	cn := name.IstiodRemoteComponentName
	return &istioComponent{
		options:       opts,
		ComponentName: cn,
	}
}

// newIngressComponent creates a new istioComponent and returns a pointer to it.
func newIngressComponent(resourceName string, index int, spec *v1alpha1.GatewaySpec, opts *options) *istioComponent {
	return &istioComponent{
		options:       opts,
		ComponentName: name.IngressComponentName,
		ResourceName:  resourceName,
		index:         index,
		componentSpec: spec,
	}
}

// newEgressComponent creates a new istioComponent and returns a pointer to it.
func newEgressComponent(resourceName string, index int, spec *v1alpha1.GatewaySpec, opts *options) *istioComponent {
	return &istioComponent{
		options:       opts,
		ComponentName: name.EgressComponentName,
		index:         index,
		componentSpec: spec,
		ResourceName:  resourceName,
	}
}

// newZtunnelComponent creates a new istioComponent and returns a pointer to it.
func newZtunnelComponent(opts *options) *istioComponent {
	return &istioComponent{
		options:       opts,
		ComponentName: name.ZtunnelComponentName,
	}
}

// renderManifest renders the manifest for the component defined by c and returns the resulting string.
func renderManifest(cf *istioComponent) (string, error) {
	if !cf.Enabled() {
		return disabledYAMLStr(cf.ComponentName, cf.ResourceName), nil
	}

	mergedYAML, err := cf.Translator.TranslateHelmValues(cf.InstallSpec, cf.componentSpec, cf.ComponentName)
	if err != nil {
		return "", err
	}

	scope.Debugf("Merged values:\n%s\n", mergedYAML)

	cns := string(cf.ComponentName)
	helmSubdir := cf.Translator.ComponentMap(cns).HelmSubdir
	renderer, err := helm.NewHelmRenderer(cf.InstallSpec.InstallPackagePath, helmSubdir, cns, cf.Namespace, cf.Version)
	if err != nil {
		log.Errorf("Error rendering the manifest: %s", err)
		return "", err
	}
	my, err := renderer.RenderManifest(mergedYAML)
	if err != nil {
		log.Errorf("Error rendering the manifest: %s", err)
		return "", err
	}
	my += helm.YAMLSeparator + "\n"
	scope.Debugf("Initial manifest with merged values:\n%s\n", my)

	// Add the k8s resources from IstioOperatorSpec.
	my, err = cf.Translator.OverlayK8sSettings(my, cf.InstallSpec, cf.ComponentName,
		cf.ResourceName, cf.index)
	if err != nil {
		return "", err
	}
	cnOutput := string(cf.ComponentName)
	my = "# Resources for " + cnOutput + " component\n\n" + my
	scope.Debugf("Manifest after k8s API settings:\n%s\n", my)

	// Add the k8s resource overlays from IstioOperatorSpec.
	pathToK8sOverlay := fmt.Sprintf("Components.%s.", cf.ComponentName)
	if cf.ComponentName.IsGateway() {
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

func (c *istioComponent) isCoreComponentEnabled() bool {
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
