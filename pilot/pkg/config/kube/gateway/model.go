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

package gateway

import (
	corev1 "k8s.io/api/core/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/model"
	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/sets"
)

const (
	gatewayAliasForAnnotationKey = "gateway.istio.io/alias-for"
	gatewayTLSTerminateModeKey   = "gateway.istio.io/tls-terminate-mode"
	gatewayNameOverride          = "gateway.istio.io/name-override"
	gatewaySAOverride            = "gateway.istio.io/service-account"
	serviceTypeOverride          = "networking.istio.io/service-type"
	addressTypeOverride          = "networking.istio.io/address-type"
)

// GatewayResources stores all gateway resources used for our conversion.
type GatewayResources struct {
	GatewayClass   []config.Config
	Gateway        []config.Config
	HTTPRoute      []config.Config
	GRPCRoute      []config.Config
	TCPRoute       []config.Config
	TLSRoute       []config.Config
	ReferenceGrant []config.Config
	ServiceEntry   []config.Config
	// Namespaces stores all namespace in the cluster, keyed by name
	Namespaces map[string]*corev1.Namespace
	// Credentials stores all credentials in the cluster
	Credentials credentials.Controller

	// Domain for the cluster. Typically, cluster.local
	Domain  string
	Context GatewayContext
}

type Grants struct {
	AllowAll     bool
	AllowedNames sets.String
}

type AllowedReferences map[Reference]map[Reference]*Grants

func (refs AllowedReferences) SecretAllowed(resourceName string, namespace string) bool {
	p, err := creds.ParseResourceName(resourceName, "", "", "")
	if err != nil {
		log.Warnf("failed to parse resource name %q: %v", resourceName, err)
		return false
	}
	from := Reference{Kind: gvk.KubernetesGateway, Namespace: k8s.Namespace(namespace)}
	to := Reference{Kind: gvk.Secret, Namespace: k8s.Namespace(p.Namespace)}
	allow := refs[from][to]
	if allow == nil {
		return false
	}
	return allow.AllowAll || allow.AllowedNames.Contains(p.Name)
}

func (refs AllowedReferences) BackendAllowed(
	k config.GroupVersionKind,
	backendName k8s.ObjectName,
	backendNamespace k8s.Namespace,
	routeNamespace string,
) bool {
	from := Reference{Kind: k, Namespace: k8s.Namespace(routeNamespace)}
	to := Reference{Kind: gvk.Service, Namespace: backendNamespace}
	allow := refs[from][to]
	if allow == nil {
		return false
	}
	return allow.AllowAll || allow.AllowedNames.Contains(string(backendName))
}

// IstioResources stores all outputs of our conversion
type IstioResources struct {
	Gateway        []config.Config
	VirtualService []config.Config
	// AllowedReferences stores all allowed references, from Reference -> to Reference(s)
	AllowedReferences AllowedReferences
	// ReferencedNamespaceKeys stores the label key of all namespace selections. This allows us to quickly
	// determine if a namespace update could have impacted any Gateways. See namespaceEvent.
	ReferencedNamespaceKeys sets.String

	// ResourceReferences stores all resources referenced by gateway-api resources. This allows us to quickly
	// determine if a resource update could have impacted any Gateways.
	// key: referenced resources(e.g. secrets), value: gateway-api resources(e.g. gateways)
	ResourceReferences map[model.ConfigKey][]model.ConfigKey
}

// Reference stores a reference to a namespaced GVK, as used by ReferencePolicy
type Reference struct {
	Kind      config.GroupVersionKind
	Namespace k8s.Namespace
}
