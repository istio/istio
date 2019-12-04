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

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/util"
)

// FeatureName is a feature name string, typed to constrain allowed values.
type FeatureName string

const (
	// OperatorAPINamespace is the API namespace for operator config.
	// TODO: move this to a base definitions file when one is created.
	OperatorAPINamespace = "operator.istio.io"
)

const (
	// IstioFeature names, must be the same as feature names defined in the IstioControlPlane proto, since these are
	// used to reference structure paths.
	IstioBaseFeatureName         FeatureName = "Base"
	TrafficManagementFeatureName FeatureName = "TrafficManagement"
	PolicyFeatureName            FeatureName = "Policy"
	TelemetryFeatureName         FeatureName = "Telemetry"
	SecurityFeatureName          FeatureName = "Security"
	ConfigManagementFeatureName  FeatureName = "ConfigManagement"
	AutoInjectionFeatureName     FeatureName = "AutoInjection"
	GatewayFeatureName           FeatureName = "Gateways"
	CNIFeatureName               FeatureName = "Cni"
	CoreDNSFeatureName           FeatureName = "CoreDNS"
	ThirdPartyFeatureName        FeatureName = "ThirdParty"
)

// ComponentName is a component name string, typed to constrain allowed values.
type ComponentName string

const (
	// IstioComponent names corresponding to the IstioControlPlane proto component names. Must be the same, since these
	// are used for struct traversal.
	IstioBaseComponentName       ComponentName = "Base"
	PilotComponentName           ComponentName = "Pilot"
	GalleyComponentName          ComponentName = "Galley"
	SidecarInjectorComponentName ComponentName = "Injector"
	PolicyComponentName          ComponentName = "Policy"
	TelemetryComponentName       ComponentName = "Telemetry"
	CitadelComponentName         ComponentName = "Citadel"
	CertManagerComponentName     ComponentName = "CertManager"
	NodeAgentComponentName       ComponentName = "NodeAgent"
	IngressComponentName         ComponentName = "IngressGateway"
	EgressComponentName          ComponentName = "EgressGateway"
	CNIComponentName             ComponentName = "Cni"
	CoreDNSComponentName         ComponentName = "CoreDNS"

	// The following are third party components, not a part of the IstioControlPlaneAPI but still installed in some
	// profiles through the Helm API.
	PrometheusComponentName         ComponentName = "Prometheus"
	PrometheusOperatorComponentName ComponentName = "PrometheusOperator"
	GrafanaComponentName            ComponentName = "Grafana"
	KialiComponentName              ComponentName = "Kiali"
	TracingComponentName            ComponentName = "Tracing"

	// Operator components
	IstioOperatorComponentName      ComponentName = "IstioOperator"
	IstioOperatorCustomResourceName ComponentName = "IstioOperatorCustomResource"
)

var (
	ComponentNameToFeatureName = map[ComponentName]FeatureName{
		IstioBaseComponentName:       IstioBaseFeatureName,
		PilotComponentName:           TrafficManagementFeatureName,
		GalleyComponentName:          ConfigManagementFeatureName,
		SidecarInjectorComponentName: AutoInjectionFeatureName,
		PolicyComponentName:          PolicyFeatureName,
		TelemetryComponentName:       TelemetryFeatureName,
		CitadelComponentName:         SecurityFeatureName,
		CertManagerComponentName:     SecurityFeatureName,
		NodeAgentComponentName:       SecurityFeatureName,
		IngressComponentName:         GatewayFeatureName,
		EgressComponentName:          GatewayFeatureName,
		CNIComponentName:             CNIFeatureName,
		CoreDNSComponentName:         CoreDNSFeatureName,

		// External
		PrometheusComponentName:         ThirdPartyFeatureName,
		PrometheusOperatorComponentName: ThirdPartyFeatureName,
		GrafanaComponentName:            ThirdPartyFeatureName,
		KialiComponentName:              ThirdPartyFeatureName,
		TracingComponentName:            ThirdPartyFeatureName,
	}
)

// ManifestMap is a map of ComponentName to its manifest string.
type ManifestMap map[ComponentName]string

// IsFeatureEnabledInSpec reports whether the given feature is enabled in the given spec.
// This follows the logic description in IstioControlPlane proto.
// IsFeatureEnabledInSpec assumes that controlPlaneSpec has been validated.
func IsFeatureEnabledInSpec(featureName FeatureName, controlPlaneSpec *v1alpha2.IstioControlPlaneSpec) (bool, error) {
	featureNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, string(featureName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsFeatureEnabledInSpec GetFromStructPath featureEnabled for feature=%s: %s", featureName, err)
	}
	if !found || featureNodeI == nil {
		return false, nil
	}
	featureNode, ok := featureNodeI.(*v1alpha2.BoolValueForPB)
	if !ok {
		return false, fmt.Errorf("feature %s enabled has bad type %T, expect *v1alpha2.BoolValueForPB", featureName, featureNodeI)
	}
	if featureNode == nil || !featureNode.Value {
		return false, nil
	}
	return featureNode.Value, nil
}

// IsComponentEnabledInSpec reports whether the given feature and component are enabled in the given spec. The logic is, in
// order of evaluation:
// 1. if the feature is not defined, the component is disabled, else
// 2. if the feature is disabled, the component is disabled, else
// 3. if the component is not defined, it is reported disabled, else
// 4. if the component disabled, it is reported disabled, else
// 5. the component is enabled.
// This follows the logic description in IstioControlPlane proto.
// IsComponentEnabledInSpec assumes that controlPlaneSpec has been validated.
// TODO: remove extra validations when comfort level is high enough.
func IsComponentEnabledInSpec(featureName FeatureName, componentName ComponentName, controlPlaneSpec *v1alpha2.IstioControlPlaneSpec) (bool, error) {
	//check in Values part as well for third Party components
	if featureName == ThirdPartyFeatureName {
		return IsComponentEnabledFromValue(string(componentName), controlPlaneSpec.Values)
	}
	featureNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, string(featureName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsComponentEnabledInSpec GetFromStructPath featureEnabled for feature=%s, component=%s: %s",
			featureName, componentName, err)
	}
	if !found || featureNodeI == nil {
		return false, nil
	}
	featureNode, ok := featureNodeI.(*v1alpha2.BoolValueForPB)
	if !ok {
		return false, fmt.Errorf("feature %s enabled has bad type %T, expect *v1alpha2.BoolValueForPB", featureName, featureNodeI)
	}
	if featureNode == nil || !featureNode.Value {
		return false, nil
	}

	componentNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, string(featureName)+".Components."+string(componentName)+".Enabled")
	if err != nil {
		return false, fmt.Errorf("error in IsComponentEnabledInSpec GetFromStructPath componentEnabled for feature=%s, component=%s: %s",
			featureName, componentName, err)
	}
	if !found || componentNodeI == nil {
		return featureNode.Value, nil
	}
	componentNode, ok := componentNodeI.(*v1alpha2.BoolValueForPB)
	if !ok {
		return false, fmt.Errorf("component %s enabled has bad type %T, expect *v1alpha2.BoolValueForPB", componentName, componentNodeI)
	}
	if componentNode == nil {
		return featureNode.Value, nil
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
func Namespace(featureName FeatureName, componentName ComponentName, controlPlaneSpec *v1alpha2.IstioControlPlaneSpec) (string, error) {
	defaultNamespaceI, found, err := tpath.GetFromStructPath(controlPlaneSpec, "DefaultNamespace")
	if !found {
		return "", fmt.Errorf("can't find any setting for defaultNamespace for feature=%s, component=%s", featureName, componentName)
	}
	if err != nil {
		return "", fmt.Errorf("error in Namepsace for feature=%s, component=%s: %s", featureName, componentName, err)

	}
	defaultNamespace, ok := defaultNamespaceI.(string)
	if !ok {
		return "", fmt.Errorf("defaultNamespace has bad type %T, expect string", defaultNamespaceI)
	}
	if defaultNamespace == "" {
		return "", fmt.Errorf("defaultNamespace must be set")
	}

	featureNamespace := defaultNamespace
	featureNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, string(featureName)+".Components.Namespace")
	if err != nil {
		return "", fmt.Errorf("error in Namepsace GetFromStructPath featureNamespace for feature=%s, component=%s: %s", featureName, componentName, err)
	}
	if found && featureNodeI != nil {
		featureNamespace, ok = featureNodeI.(string)
		if !ok {
			return "", fmt.Errorf("feature %s namespace has bad type %T, expect string", featureName, featureNodeI)
		}
		if featureNamespace == "" {
			featureNamespace = defaultNamespace
		}
	}

	componentNodeI, found, err := tpath.GetFromStructPath(controlPlaneSpec, string(featureName)+".Components."+string(componentName)+".Namespace")
	if err != nil {
		return "", fmt.Errorf("error in Namepsace GetFromStructPath componentNamespace for feature=%s, component=%s: %s", featureName, componentName, err)
	}
	if !found {
		return featureNamespace, nil
	}
	if componentNodeI == nil {
		return featureNamespace, nil
	}
	componentNamespace, ok := componentNodeI.(string)
	if !ok {
		return "", fmt.Errorf("component %s enabled has bad type %T, expect string", componentName, componentNodeI)
	}
	if componentNamespace == "" {
		return featureNamespace, nil
	}
	return componentNamespace, nil
}
