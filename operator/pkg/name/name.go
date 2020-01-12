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

package name

import (
	"fmt"
	"strings"

	"istio.io/api/operator/v1alpha1"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/util"
)

const (
	// OperatorAPINamespace is the API namespace for operator config.
	// TODO: move this to a base definitions file when one is created.
	OperatorAPINamespace = "operator.istio.io"
)

// ComponentName is a component name string, typed to constrain allowed values.
type ComponentName string

const (
	// IstioComponent names corresponding to the IstioOperator proto component names. Must be the same, since these
	// are used for struct traversal.
	IstioBaseComponentName       ComponentName = "Base"
	PilotComponentName           ComponentName = "Pilot"
	GalleyComponentName          ComponentName = "Galley"
	SidecarInjectorComponentName ComponentName = "SidecarInjector"
	PolicyComponentName          ComponentName = "Policy"
	TelemetryComponentName       ComponentName = "Telemetry"
	CitadelComponentName         ComponentName = "Citadel"
	CertManagerComponentName     ComponentName = "CertManager"
	NodeAgentComponentName       ComponentName = "NodeAgent"
	CNIComponentName             ComponentName = "Cni"

	// Gateway components
	IngressComponentName ComponentName = "IngressGateways"
	EgressComponentName  ComponentName = "EgressGateways"

	// Addon components
	AddonComponentName ComponentName = "Addon"

	// Operator components
	IstioOperatorComponentName      ComponentName = "IstioOperator"
	IstioOperatorCustomResourceName ComponentName = "IstioOperatorCustomResource"
)

var (
	AllCoreComponentNames = []ComponentName{
		IstioBaseComponentName,
		PilotComponentName,
		GalleyComponentName,
		SidecarInjectorComponentName,
		PolicyComponentName,
		TelemetryComponentName,
		CitadelComponentName,
		CertManagerComponentName,
		NodeAgentComponentName,
		CNIComponentName,
	}
	allComponentNamesMap = make(map[ComponentName]bool)
)

func init() {
	for _, n := range AllCoreComponentNames {
		allComponentNamesMap[n] = true
	}
}

// ManifestMap is a map of ComponentName to its manifest string.
type ManifestMap map[ComponentName][]string

// IsCoreComponent reports whether cn is a core component.
func (cn ComponentName) IsCoreComponent() bool {
	return allComponentNamesMap[cn]
}

// IsGateway reports whether cn is a gateway component.
func (cn ComponentName) IsGateway() bool {
	return cn == IngressComponentName || cn == EgressComponentName
}

// IsAddon reports whether cn is an addon component.
func (cn ComponentName) IsAddon() bool {
	return cn == AddonComponentName
}

// IsComponentEnabledInSpec reports whether the given component is enabled in the given spec.
// IsComponentEnabledInSpec assumes that controlPlaneSpec has been validated.
func IsComponentEnabledInSpec(componentName ComponentName, controlPlaneSpec *v1alpha1.IstioOperatorSpec) (bool, error) {
	if componentName == IngressComponentName {
		return len(controlPlaneSpec.Components.IngressGateways) != 0, nil
	}
	if componentName == EgressComponentName {
		return len(controlPlaneSpec.Components.EgressGateways) != 0, nil
	}

	componentNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, "Components."+string(componentName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsComponentEnabledInSpec GetFromStructPath componentEnabled for component=%s: %s",
			componentName, err)
	}
	if !found || componentNodeI == nil {
		return false, nil
	}
	componentNode, ok := componentNodeI.(*v1alpha1.BoolValueForPB)
	if !ok {
		return false, fmt.Errorf("component %s enabled has bad type %T, expect *v1alpha2.BoolValueForPB", componentName, componentNodeI)
	}
	if componentNode == nil {
		return false, nil
	}
	return componentNode.Value, nil
}

// IsComponentEnabledFromValue get whether component is enabled in helm value.yaml tree.
// valuePath points to component path in the values tree.
func IsComponentEnabledFromValue(valuePath string, valueSpec map[string]interface{}) (bool, error) {
	enabledPath := valuePath + ".enabled"
	enableNodeI, found, err := tpath.GetFromTreePath(valueSpec, util.ToYAMLPath(enabledPath))
	if err != nil {
		return false, fmt.Errorf("error finding component enablement path: %s in helm value.yaml tree", enabledPath)
	}
	if !found {
		// Some components do not specify enablement should be treated as enabled if the root node in the component subtree exists.
		_, found, err := tpath.GetFromTreePath(valueSpec, util.ToYAMLPath(valuePath))
		if found && err == nil {
			return true, nil
		}
		return false, nil
	}
	enableNode, ok := enableNodeI.(bool)
	if !ok {
		return false, fmt.Errorf("node at valuePath %s has bad type %T, expect bool", enabledPath, enableNodeI)
	}
	return enableNode, nil
}

// NamespaceFromValue gets the namespace value in helm value.yaml tree.
func NamespaceFromValue(valuePath string, valueSpec map[string]interface{}) (string, error) {
	nsNodeI, found, err := tpath.GetFromTreePath(valueSpec, util.ToYAMLPath(valuePath))
	if err != nil {
		return "", fmt.Errorf("namespace path not found: %s from helm value.yaml tree", valuePath)
	}
	if !found || nsNodeI == nil {
		return "", nil
	}
	nsNode, ok := nsNodeI.(string)
	if !ok {
		return "", fmt.Errorf("node at helm value.yaml tree path %s has bad type %T, expect string", valuePath, nsNodeI)
	}
	return nsNode, nil
}

// Namespace returns the namespace for the component. It follows these rules:
// 1. If DefaultNamespace is unset, log and error and return the empty string.
// 2. If the feature and component namespaces are unset, return DefaultNamespace.
// 3. If the feature namespace is set but component name is unset, return the feature namespace.
// 4. Otherwise return the component namespace.
// Namespace assumes that controlPlaneSpec has been validated.
// TODO: remove extra validations when comfort level is high enough.
func Namespace(componentName ComponentName, controlPlaneSpec *v1alpha1.IstioOperatorSpec) (string, error) {
	defaultNamespaceI, found, err := tpath.GetFromStructPath(controlPlaneSpec, "MeshConfig.RootNamespace")
	if !found {
		return "", fmt.Errorf("can't find any setting for defaultNamespace for component=%s", componentName)
	}
	if err != nil {
		return "", fmt.Errorf("error in Namepsace for component=%s: %s", componentName, err)

	}
	defaultNamespace, ok := defaultNamespaceI.(string)
	if !ok {
		return "", fmt.Errorf("defaultNamespace has bad type %T, expect string", defaultNamespaceI)
	}
	if defaultNamespace == "" {
		return "", fmt.Errorf("defaultNamespace must be set")
	}

	componentNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, "Components."+string(componentName)+".Namespace")
	if err != nil {
		return "", fmt.Errorf("error in Namepsace GetFromStructPath componentNamespace for component=%s: %s", componentName, err)
	}
	if !found {
		return defaultNamespace, nil
	}
	if componentNodeI == nil {
		return defaultNamespace, nil
	}
	componentNamespace, ok := componentNodeI.(string)
	if !ok {
		return "", fmt.Errorf("component %s enabled has bad type %T, expect string", componentName, componentNodeI)
	}
	if componentNamespace == "" {
		return defaultNamespace, nil
	}
	return componentNamespace, nil
}

// TitleCase returns a capitalized version of n.
func TitleCase(n ComponentName) ComponentName {
	s := string(n)
	return ComponentName(strings.ToUpper(s[0:1]) + s[1:])
}
