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

package component

import (
	"fmt"

	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/values"
)

type Component struct {
	// UserFacingName is the component name in user facing cases
	UserFacingName Name
	// SpecName is the yaml key in the IstioOperator spec
	SpecName string
	Default  bool
	Multi    bool
	// ResourceType maps a Name to the type of the rendered k8s resource.
	ResourceType string
	// ResourceName maps a Name to the name of the rendered k8s resource.
	ResourceName string
	// ContainerName maps a Name to the name of the container in a Deployment.
	ContainerName string
	// HelmSubdir is a mapping between a component name and the subdirectory of the component Chart.
	HelmSubdir string
	// ToHelmValuesTreeRoot is the tree root in values YAML files for the component.
	ToHelmValuesTreeRoot string
	// FlattenValues, if true, means the component expects values not prefixed with ToHelmValuesTreeRoot
	// For example `.name=foo` instead of `.component.name=foo`.
	FlattenValues bool
	// AltEnablementPath is the alternative path, from values, to enable the component
	AltEnablementPath string
	// ReleaseName is the name of the chart in the official istio helm release. Not present for all components
	ReleaseName string
}

func (c Component) Get(merged values.Map) ([]apis.GatewayComponentSpec, error) {
	defaultNamespace := merged.GetPathString("metadata.namespace")
	var defaultResponse []apis.GatewayComponentSpec
	def := c.Default
	altEnabled := false
	if c.AltEnablementPath != "" {
		if merged.GetPathBool(c.AltEnablementPath) {
			def = true
			altEnabled = true
		}
	}
	if def {
		defaultResponse = []apis.GatewayComponentSpec{{ComponentSpec: apis.ComponentSpec{Namespace: defaultNamespace}}}
	}

	buildSpec := func(m values.Map) (apis.GatewayComponentSpec, error) {
		spec, err := values.ConvertMap[apis.GatewayComponentSpec](m)
		if err != nil {
			return apis.GatewayComponentSpec{}, fmt.Errorf("fail to convert %v: %v", c.SpecName, err)
		}
		if spec.Namespace == "" {
			spec.Namespace = defaultNamespace
		}
		if spec.Namespace == "" {
			spec.Namespace = "istio-system"
		}

		// We might copy this later by serializing and then deserializing from JSON.
		// However, `nil` Go maps get serialized to JSON `null` and desrialized to untyped `nil`.
		// When Helm tries to index from an untyped nil, it throws an error instead of returning
		// an empty value. We avoid this issue by explicitly initializing the Go map, so it gets serialized
		// into an empty JSON object `{}`.
		if spec.Label == nil {
			spec.Label = make(map[string]string)
		}
		spec.Raw = m
		return spec, nil
	}
	// List of components
	if c.Multi {
		s, ok := merged.GetPath("spec.components." + c.SpecName)
		if !ok {
			return defaultResponse, nil
		}
		var specs []apis.GatewayComponentSpec
		for _, cur := range s.([]any) {
			m, _ := values.CastAsMap(cur)
			spec, err := buildSpec(m)
			if err != nil {
				return nil, err
			}
			if spec.Enabled.GetValueOrTrue() || altEnabled {
				specs = append(specs, spec)
			}
		}
		return specs, nil
	}
	// Single component
	s, ok := merged.GetPathMap("spec.components." + c.SpecName)
	if !ok {
		return defaultResponse, nil
	}
	spec, err := buildSpec(s)
	if err != nil {
		return nil, err
	}
	if !(spec.Enabled.GetValueOrTrue() || altEnabled) {
		return nil, nil
	}
	return []apis.GatewayComponentSpec{spec}, nil
}

func (c Component) IsGateway() bool {
	return c.UserFacingName == IngressComponentName || c.UserFacingName == EgressComponentName
}

// Name is a component name string, typed to constrain allowed values.
type Name string

const (
	// IstioComponent names corresponding to the IstioOperator proto component names. Must be the same, since these
	// are used for struct traversal.
	BaseComponentName  Name = "Base"
	PilotComponentName Name = "Pilot"

	CNIComponentName     Name = "Cni"
	ZtunnelComponentName Name = "Ztunnel"

	ZtunnelWindowsComponentName Name = "ZtunnelWindows"
	CNIWindowsComponentName     Name = "CniWindows"

	IngressComponentName Name = "IngressGateways"
	EgressComponentName  Name = "EgressGateways"
)

var AllComponents = []Component{
	{
		UserFacingName:       BaseComponentName,
		SpecName:             "base",
		Default:              true,
		HelmSubdir:           "base",
		ToHelmValuesTreeRoot: "global",
		ReleaseName:          "base",
	},
	{
		UserFacingName:       PilotComponentName,
		SpecName:             "pilot",
		Default:              true,
		ResourceType:         "Deployment",
		ResourceName:         "istiod",
		ContainerName:        "discovery",
		HelmSubdir:           "istio-control/istio-discovery",
		ToHelmValuesTreeRoot: "pilot",
		ReleaseName:          "istiod",
	},
	{
		UserFacingName:       IngressComponentName,
		SpecName:             "ingressGateways",
		Multi:                true,
		Default:              true,
		ResourceType:         "Deployment",
		ResourceName:         "istio-ingressgateway",
		ContainerName:        "istio-proxy",
		HelmSubdir:           "gateways/istio-ingress",
		ToHelmValuesTreeRoot: "gateways.istio-ingressgateway",
		AltEnablementPath:    "spec.values.gateways.istio-ingressgateway.enabled",
	},
	{
		UserFacingName:       EgressComponentName,
		SpecName:             "egressGateways",
		Multi:                true,
		ResourceType:         "Deployment",
		ResourceName:         "istio-egressgateway",
		ContainerName:        "istio-proxy",
		HelmSubdir:           "gateways/istio-egress",
		ToHelmValuesTreeRoot: "gateways.istio-egressgateway",
		AltEnablementPath:    "spec.values.gateways.istio-egressgateway.enabled",
	},
	{
		UserFacingName:       CNIComponentName,
		SpecName:             "cni",
		ResourceType:         "DaemonSet",
		ResourceName:         "istio-cni-node",
		ContainerName:        "install-cni",
		HelmSubdir:           "istio-cni",
		ToHelmValuesTreeRoot: "cni",
		ReleaseName:          "cni",
	},
	{
		UserFacingName:       CNIWindowsComponentName,
		SpecName:             "cni-windows",
		ResourceType:         "DaemonSet",
		ResourceName:         "istio-cni-node",
		ContainerName:        "install-cni",
		HelmSubdir:           "istio-cni-windows",
		ToHelmValuesTreeRoot: "cni-windows",
		ReleaseName:          "cni",
	},
	{
		UserFacingName:       ZtunnelComponentName,
		SpecName:             "ztunnel",
		ResourceType:         "DaemonSet",
		ResourceName:         "ztunnel",
		HelmSubdir:           "ztunnel",
		ToHelmValuesTreeRoot: "ztunnel",
		ContainerName:        "istio-proxy",
		FlattenValues:        true,
		ReleaseName:          "ztunnel",
	},
	{
		UserFacingName:       ZtunnelWindowsComponentName,
		SpecName:             "ztunnel-windows",
		ResourceType:         "DaemonSet",
		ResourceName:         "ztunnel",
		HelmSubdir:           "ztunnel-windows",
		ToHelmValuesTreeRoot: "ztunnel_windows",
		ContainerName:        "istio-proxy",
		FlattenValues:        true,
		ReleaseName:          "ztunnel",
	},
}

var (
	userFacingComponentNames = map[Name]string{
		BaseComponentName:           "Istio core",
		PilotComponentName:          "Istiod",
		CNIComponentName:            "CNI",
		CNIWindowsComponentName:     "CNI (Windows)",
		ZtunnelComponentName:        "Ztunnel",
		ZtunnelWindowsComponentName: "Ztunnel (Windows)",
		IngressComponentName:        "Ingress gateways",
		EgressComponentName:         "Egress gateways",
	}

	Icons = map[Name]string{
		BaseComponentName:    "⛵️",
		PilotComponentName:   "🧠",
		CNIComponentName:     "🪢",
		ZtunnelComponentName: "🔒",
		IngressComponentName: "🛬",
		EgressComponentName:  "🛫",
	}
)

// UserFacingComponentName returns the name of the given component that should be displayed to the user in high
// level CLIs (like progress log).
func UserFacingComponentName(name Name) string {
	ret, ok := userFacingComponentNames[name]
	if !ok {
		return "Unknown"
	}
	return ret
}
