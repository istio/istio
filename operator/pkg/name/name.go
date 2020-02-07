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
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"

	"istio.io/api/operator/v1alpha1"
	iop "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/vfs"
	binversion "istio.io/istio/operator/version"
	"istio.io/pkg/log"
)

const (
	// OperatorAPINamespace is the API namespace for operator config.
	// TODO: move this to a base definitions file when one is created.
	OperatorAPINamespace = "operator.istio.io"
	// ConfigFolder is the folder where we store translation configurations
	ConfigFolder = "translateConfig"
	// ConfigPrefix is the prefix of IstioOperator's translation configuration file
	ConfigPrefix = "names-"
)

const (
	DefaultProfileName = "default"
)

// ComponentName is a component name string, typed to constrain allowed values.
type ComponentName string

const (
	// IstioComponent names corresponding to the IstioOperator proto component names. Must be the same, since these
	// are used for struct traversal.
	IstioBaseComponentName ComponentName = "Base"
	PilotComponentName     ComponentName = "Pilot"
	GalleyComponentName    ComponentName = "Galley"
	PolicyComponentName    ComponentName = "Policy"
	TelemetryComponentName ComponentName = "Telemetry"
	CitadelComponentName   ComponentName = "Citadel"

	CNIComponentName ComponentName = "Cni"

	// Gateway components
	IngressComponentName ComponentName = "IngressGateways"
	EgressComponentName  ComponentName = "EgressGateways"

	// Addon root component
	AddonComponentName ComponentName = "AddonComponents"

	// Operator components
	IstioOperatorComponentName      ComponentName = "IstioOperator"
	IstioOperatorCustomResourceName ComponentName = "IstioOperatorCustomResource"
)

var (
	scope                 = log.RegisterScope("name", "operator naming", 0)
	AllCoreComponentNames = []ComponentName{
		IstioBaseComponentName,
		PilotComponentName,
		GalleyComponentName,
		PolicyComponentName,
		TelemetryComponentName,
		CitadelComponentName,
		CNIComponentName,
	}
	allComponentNamesMap = make(map[ComponentName]bool)
	// DeprecatedComponentNamesMap defines the names of deprecated istio core components used in old versions,
	// which would not appear as standalone components in current version.
	DeprecatedComponentNamesMap = make(map[ComponentName]bool)
	// AddonComponentNamesMap defines the component name for addon components used in old versions.
	AddonComponentNamesMap = make(map[ComponentName]bool)
)

func init() {
	for _, n := range AllCoreComponentNames {
		allComponentNamesMap[n] = true
	}
	minorVersion := binversion.OperatorBinaryVersion.MinorVersion
	f := filepath.Join(ConfigFolder, ConfigPrefix+minorVersion.String()+".yaml")
	b, err := vfs.ReadFile(f)
	if err != nil {
		log.Errorf("fail to read naming file: %v", err)
		return
	}
	names := make(map[string][]string)
	err = yaml.Unmarshal(b, &names)
	if err != nil {
		scope.Errorf("fail to unmarshal naming config file: %v", err)
		return
	}
	legacyAddonComponentNames, ok := names["legacyAddonComponentNames"]
	if !ok {
		scope.Errorf("fail to find legacyAddonComponentNames")
		return
	}
	for _, an := range legacyAddonComponentNames {
		AddonComponentNamesMap[ComponentName(an)] = true
	}
	deprecatedComponentNames, ok := names["deprecatedComponentNames"]
	if !ok {
		scope.Errorf("fail to find legacyAddonComponentNames")
		return
	}
	for _, n := range deprecatedComponentNames {
		DeprecatedComponentNamesMap[ComponentName(n)] = true
	}
}

// ManifestMap is a map of ComponentName to its manifest string.
type ManifestMap map[ComponentName][]string

// IsCoreComponent reports whether cn is a core component.
func (cn ComponentName) IsCoreComponent() bool {
	return allComponentNamesMap[cn]
}

// IsDeprecatedName reports whether cn is a deprecated component.
func (cn ComponentName) IsDeprecatedName() bool {
	return DeprecatedComponentNamesMap[cn]
}

// IsGateway reports whether cn is a gateway component.
func (cn ComponentName) IsGateway() bool {
	return cn == IngressComponentName || cn == EgressComponentName
}

// IsAddon reports whether cn is an addon component.
func (cn ComponentName) IsAddon() bool {
	return cn == AddonComponentName
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

	defaultNamespace := iop.Namespace(controlPlaneSpec)

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
