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

package name

import (
	"fmt"
	"strings"

	"istio.io/api/operator/v1alpha1"
	iop "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/tpath"
)

// Kubernetes Kind strings.
const (
	CRDStr                            = "CustomResourceDefinition"
	ClusterRoleStr                    = "ClusterRole"
	ClusterRoleBindingStr             = "ClusterRoleBinding"
	CMStr                             = "ConfigMap"
	DaemonSetStr                      = "DaemonSet"
	DeploymentStr                     = "Deployment"
	EndpointStr                       = "Endpoints"
	HPAStr                            = "HorizontalPodAutoscaler"
	IngressStr                        = "Ingress"
	IstioOperator                     = "IstioOperator"
	MutatingWebhookConfigurationStr   = "MutatingWebhookConfiguration"
	NamespaceStr                      = "Namespace"
	PVCStr                            = "PersistentVolumeClaim"
	PodStr                            = "Pod"
	PDBStr                            = "PodDisruptionBudget"
	ReplicationControllerStr          = "ReplicationController"
	ReplicaSetStr                     = "ReplicaSet"
	RoleStr                           = "Role"
	RoleBindingStr                    = "RoleBinding"
	SAStr                             = "ServiceAccount"
	ServiceStr                        = "Service"
	SecretStr                         = "Secret"
	StatefulSetStr                    = "StatefulSet"
	ValidatingWebhookConfigurationStr = "ValidatingWebhookConfiguration"
)

// Istio Kind strings
const (
	EnvoyFilterStr        = "EnvoyFilter"
	GatewayStr            = "Gateway"
	DestinationRuleStr    = "DestinationRule"
	MeshPolicyStr         = "MeshPolicy"
	PeerAuthenticationStr = "PeerAuthentication"
	VirtualServiceStr     = "VirtualService"
	IstioOperatorStr      = "IstioOperator"
)

// Istio API Group Names
const (
	AuthenticationAPIGroupName = "authentication.istio.io"
	ConfigAPIGroupName         = "config.istio.io"
	NetworkingAPIGroupName     = "networking.istio.io"
	OperatorAPIGroupName       = "operator.istio.io"
	SecurityAPIGroupName       = "security.istio.io"
)

const (
	// OperatorAPINamespace is the API namespace for operator config.
	// TODO: move this to a base definitions file when one is created.
	OperatorAPINamespace = OperatorAPIGroupName

	// DefaultProfileName is the name of the default profile.
	DefaultProfileName = "default"

	// installedSpecCRPrefix is the prefix of any IstioOperator CR stored in the cluster that is a copy of the CR used
	// in the last install operation.
	InstalledSpecCRPrefix = "installed-state"
)

// ComponentName is a component name string, typed to constrain allowed values.
type ComponentName string

const (
	// IstioComponent names corresponding to the IstioOperator proto component names. Must be the same, since these
	// are used for struct traversal.
	IstioBaseComponentName ComponentName = "Base"
	PilotComponentName     ComponentName = "Pilot"

	CNIComponentName ComponentName = "Cni"

	// istiod remote component
	IstiodRemoteComponentName ComponentName = "IstiodRemote"

	// Gateway components
	IngressComponentName ComponentName = "IngressGateways"
	EgressComponentName  ComponentName = "EgressGateways"

	// Operator components
	IstioOperatorComponentName      ComponentName = "IstioOperator"
	IstioOperatorCustomResourceName ComponentName = "IstioOperatorCustomResource"
)

// ComponentNamesConfig is used for unmarshaling legacy and addon naming data.
type ComponentNamesConfig struct {
	DeprecatedComponentNames []string
}

var (
	AllCoreComponentNames = []ComponentName{
		IstioBaseComponentName,
		PilotComponentName,
		CNIComponentName,
		IstiodRemoteComponentName,
	}

	// AllComponentNames is a list of all Istio components.
	AllComponentNames = append(AllCoreComponentNames, IngressComponentName, EgressComponentName,
		IstioOperatorComponentName, IstioOperatorCustomResourceName)

	allCoreComponentNamesMap = map[ComponentName]bool{}

	// ValuesEnablementPathMap defines a mapping between legacy values enablement paths and the corresponding enablement
	// paths in IstioOperator.
	ValuesEnablementPathMap = map[string]string{
		"spec.values.gateways.istio-ingressgateway.enabled": "spec.components.ingressGateways.[name:istio-ingressgateway].enabled",
		"spec.values.gateways.istio-egressgateway.enabled":  "spec.components.egressGateways.[name:istio-egressgateway].enabled",
	}

	// userFacingComponentNames are the names of components that are displayed to the user in high level CLIs
	// (like progress log).
	userFacingComponentNames = map[ComponentName]string{
		IstioBaseComponentName:          "Istio core",
		PilotComponentName:              "Istiod",
		CNIComponentName:                "CNI",
		IngressComponentName:            "Ingress gateways",
		EgressComponentName:             "Egress gateways",
		IstioOperatorComponentName:      "Istio operator",
		IstioOperatorCustomResourceName: "Istio operator CRDs",
		IstiodRemoteComponentName:       "Istiod remote",
	}
)

// Manifest defines a manifest for a component.
type Manifest struct {
	Name    ComponentName
	Content string
}

// ManifestMap is a map of ComponentName to its manifest string.
type ManifestMap map[ComponentName][]string

func init() {
	for _, c := range AllCoreComponentNames {
		allCoreComponentNamesMap[c] = true
	}
}

// Consolidated returns a representation of mm where all manifests in the slice under a key are combined into a single
// manifest.
func (mm ManifestMap) Consolidated() map[string]string {
	out := make(map[string]string)
	for cname, ms := range mm {
		allM := ""
		for _, m := range ms {
			allM += m + helm.YAMLSeparator
		}
		out[string(cname)] = allM
	}
	return out
}

// MergeManifestSlices merges a slice of manifests into a single manifest string.
func MergeManifestSlices(manifests []string) string {
	return strings.Join(manifests, helm.YAMLSeparator)
}

// String implements the Stringer interface.
func (mm ManifestMap) String() string {
	out := ""
	for _, ms := range mm {
		for _, m := range ms {
			out += m + helm.YAMLSeparator
		}
	}
	return out
}

// IsGateway reports whether cn is a gateway component.
func (cn ComponentName) IsGateway() bool {
	return cn == IngressComponentName || cn == EgressComponentName
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

// UserFacingComponentName returns the name of the given component that should be displayed to the user in high
// level CLIs (like progress log).
func UserFacingComponentName(name ComponentName) string {
	ret, ok := userFacingComponentNames[name]
	if !ok {
		return "Unknown"
	}
	return ret
}
