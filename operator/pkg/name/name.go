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

	"istio.io/istio/operator/pkg/vfs"
	"istio.io/istio/operator/version"

	"istio.io/api/operator/v1alpha1"
	iop "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/tpath"
)

const (
	// OperatorAPINamespace is the API namespace for operator config.
	// TODO: move this to a base definitions file when one is created.
	OperatorAPINamespace = "operator.istio.io"
	// ConfigFolder is the folder where we store translation configurations
	ConfigFolder = "translateConfig"
	// ConfigPrefix is the prefix of IstioOperator's translation configuration file
	ConfigPrefix = "names-"
	// DefaultProfileName is the name of the default profile.
	DefaultProfileName = "default"
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
	NodeAgentComponentName       ComponentName = "NodeAgent"
	CNIComponentName             ComponentName = "Cni"

	// Gateway components
	IngressComponentName ComponentName = "IngressGateways"
	EgressComponentName  ComponentName = "EgressGateways"

	// Addon root component
	AddonComponentName ComponentName = "AddonComponents"

	// Operator components
	IstioOperatorComponentName      ComponentName = "IstioOperator"
	IstioOperatorCustomResourceName ComponentName = "IstioOperatorCustomResource"
)

// ComponentNamesConfig is used for unmarshaling legacy and addon naming data.
type ComponentNamesConfig struct {
	BundledAddonComponentNames []string
	DeprecatedComponentNames   []string
}

var (
	AllCoreComponentNames = []ComponentName{
		IstioBaseComponentName,
		PilotComponentName,
		GalleyComponentName,
		SidecarInjectorComponentName,
		PolicyComponentName,
		TelemetryComponentName,
		CitadelComponentName,
		NodeAgentComponentName,
		CNIComponentName,
	}

	allComponentNamesMap = make(map[ComponentName]bool)
	// DeprecatedComponentNamesMap defines the names of deprecated istio core components used in old versions,
	// which would not appear as standalone components in current version. This is used for pruning, and alerting
	// users to the fact that the components are deprecated.
	DeprecatedComponentNamesMap = make(map[ComponentName]bool)

	// BundledAddonComponentNamesMap is a map of component names of addons which have helm charts bundled with Istio
	// and have built in path definitions beyond standard addons coming from external charts.
	BundledAddonComponentNamesMap = make(map[ComponentName]bool)

	// ValuesEnablementPathMap defines a mapping between legacy values enablement paths and the corresponding enablement
	// paths in IstioOperator.
	ValuesEnablementPathMap = make(map[string]string)
)

func init() {
	for _, n := range AllCoreComponentNames {
		allComponentNamesMap[n] = true
	}
	if err := loadComponentNamesConfig(); err != nil {
		panic(err)
	}
	generateValuesEnablementMap()
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

// loadComponentNamesConfig loads a config that defines version specific components names, such as legacy components
// names that may not otherwise exist in the code.
func loadComponentNamesConfig() error {
	minorVersion := version.OperatorBinaryVersion.MinorVersion
	f := filepath.Join(ConfigFolder, ConfigPrefix+minorVersion.String()+".yaml")
	b, err := vfs.ReadFile(f)
	if err != nil {
		return fmt.Errorf("failed to read naming file: %v", err)
	}
	namesConfig := &ComponentNamesConfig{}
	err = yaml.Unmarshal(b, &namesConfig)
	if err != nil {
		return fmt.Errorf("failed to unmarshal naming config file: %v", err)
	}
	for _, an := range namesConfig.BundledAddonComponentNames {
		BundledAddonComponentNamesMap[ComponentName(an)] = true
	}
	for _, n := range namesConfig.DeprecatedComponentNames {
		DeprecatedComponentNamesMap[ComponentName(n)] = true
	}
	return nil
}

func generateValuesEnablementMap() {
	for n := range BundledAddonComponentNamesMap {
		cn := strings.ToLower(string(n))
		valuePath := fmt.Sprintf("spec.values.%s.enabled", cn)
		iopPath := fmt.Sprintf("spec.addonComponents.%s.enabled", cn)
		ValuesEnablementPathMap[valuePath] = iopPath
	}
	ValuesEnablementPathMap["spec.values.gateways.istio-ingressgateway.enabled"] = "spec.components.ingressGateways.[name:istio-ingressgateway].enabled"
	ValuesEnablementPathMap["spec.values.gateways.istio-egressgateway.enabled"] = "spec.components.egressGateways.[name:istio-egressgateway].enabled"
}
