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
	componentDisabledStr = " component is disabled."
	yamlCommentStr       = "# "

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
	// Run starts the component. Must me called before the component is used.
	Run() error
	// RenderManifest returns a string with the rendered manifest for the component.
	RenderManifest() (string, error)
}

// IstioComponentImpl is a struct common to all components.
type IstioComponentImpl struct {
	*Options
	componentName name.ComponentName
	// resourceName is the name of all resources for this component.
	resourceName string
	// index is the index of the component (only used for components with multiple instances like gateways).
	index    int
	started  bool
	renderer helm.TemplateRenderer
}

// NewIstioComponentImpl creates a new IstioComponentImpl with the given componentName and options.
func NewIstioComponentImpl(cn name.ComponentName, resourceName string, opts *Options) IstioComponent {
	component := &IstioComponentImpl{
		Options:       opts,
		resourceName:  resourceName,
		componentName: cn,
	}
	return component
}

func (c *IstioComponentImpl) overlayMeshConfig(baseYAML string) (string, error) {
	if c.InstallSpec.MeshConfig == nil {
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

		meshOverride, err := gogoprotomarshal.ToYAML(c.InstallSpec.MeshConfig)
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

// NewIngressComponent creates a new IngressComponent and returns a pointer to it.
func NewIngressComponent(resourceName string, index int, opts *Options) IstioComponent {
	return &IstioComponentImpl{
		resourceName:  resourceName,
		Options:       opts,
		componentName: name.IngressComponentName,
		index:         index,
	}
}

// NewEgressComponent creates a new IngressComponent and returns a pointer to it.
func NewEgressComponent(resourceName string, index int, opts *Options) IstioComponent {
	return &IstioComponentImpl{
		resourceName:  resourceName,
		Options:       opts,
		componentName: name.EgressComponentName,
		index:         index,
	}
}

// Namespace implements the IstioComponent interface.
func (c *IstioComponentImpl) Namespace() string {
	return c.Options.Namespace
}

// ResourceName implements the IstioComponent interface.
func (c *IstioComponentImpl) ResourceName() string {
	return c.resourceName
}

// ComponentName implements the IstioComponent interface.
func (c *IstioComponentImpl) ComponentName() name.ComponentName {
	return c.componentName
}

// Run performs startup tasks for the component defined by the given CommonComponentFields.
func (c *IstioComponentImpl) Run() error {
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

// RenderManifest renders the manifest for the component defined by c and returns the resulting string.
func (c *IstioComponentImpl) RenderManifest() (string, error) {
	if !c.started {
		return "", fmt.Errorf("component %s not started in RenderManifest", c.ComponentName())
	}

	e, err := c.Translator.IsComponentEnabled(c.componentName, c.Options.InstallSpec)
	if err != nil {
		return "", err
	}

	if !e {
		return disabledYAMLStr(c.componentName), nil
	}

	mergedYAML, err := c.Translator.TranslateHelmValues(c.Options.InstallSpec, c.componentName)
	if err != nil {
		return "", err
	}

	my, err := c.renderer.RenderManifest(mergedYAML)
	if err != nil {
		log.Errorf("Error rendering the manifest: %s", err)
		return "", err
	}
	my += helm.YAMLSeparator + "\n"
	if devDbg {
		log.Infof("Initial manifest with merged values:\n%s\n", my)
	}
	// Add the k8s resources from IstioOperatorSpec.
	my, err = c.Translator.OverlayK8sSettings(my, c.Options.InstallSpec, c.componentName, c.index)
	if err != nil {
		log.Errorf("Error in OverlayK8sSettings: %s", err)
		return "", err
	}
	cnOutput := string(c.componentName)
	my += "# Resources for " + cnOutput + " component\n\n" + my
	if devDbg {
		log.Infof("Manifest after k8s API settings:\n%s\n", my)
	}
	// Add the k8s resource overlays from IstioOperatorSpec.
	pathToK8sOverlay := fmt.Sprintf("Components.%s.", c.componentName)
	if c.componentName == name.IngressComponentName || c.componentName == name.EgressComponentName {
		pathToK8sOverlay += fmt.Sprintf("%d.", c.index)
	} else if !c.ComponentName().IsAddon() {
		pathToK8sOverlay = fmt.Sprintf("AddonComponents.%s.", util.ToYAMLPathString(string(c.componentName)))
	}
	pathToK8sOverlay += fmt.Sprintf("K8S.Overlays")
	var overlays []*v1alpha1.K8SObjectOverlay
	found, err := tpath.SetFromPath(c.InstallSpec, pathToK8sOverlay, &overlays)
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
	ret, err := patch.YAMLManifestPatch(my, c.Namespace(), overlays)
	if err != nil {
		return "", err
	}

	log.Infof("Manifest after resources and overlay: \n%s\n", ret)
	if c.ComponentName() == name.PilotComponentName {
		return c.overlayMeshConfig(ret)
	}
	return ret, nil
}

// createHelmRenderer creates a helm renderer for the component defined by c and returns a ptr to it.
// If a helm subdir is not found in ComponentMap translations, it is assumed to be "addon/<component name>.
func createHelmRenderer(c *IstioComponentImpl) (helm.TemplateRenderer, error) {
	iop := c.Options.InstallSpec
	cns := string(c.componentName)
	helmSubdir := addonsChartDirName + "/" + cns
	if cm := c.Translator.ComponentMap(cns); cm != nil {
		helmSubdir = cm.HelmSubdir
	}
	return helm.NewHelmRenderer(iop.InstallPackagePath, helmSubdir, cns, c.Namespace())
}

// disabledYAMLStr returns the YAML comment string that the given component is disabled.
func disabledYAMLStr(componentName name.ComponentName) string {
	return yamlCommentStr + string(componentName) + componentDisabledStr + "\n"
}
