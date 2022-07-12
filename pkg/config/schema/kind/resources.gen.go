
// GENERATED FILE -- DO NOT EDIT
//

package kind

import (
	"istio.io/istio/pkg/config"
)

const (
	AuthorizationPolicy Kind = iota
	ConfigMap
	CustomResourceDefinition
	Deployment
	DestinationRule
	Endpoints
	EnvoyFilter
	Gateway
	GatewayClass
	HTTPRoute
	Ingress
	KubernetesGateway
	MeshConfig
	MeshNetworks
	MutatingWebhookConfiguration
	Namespace
	Node
	PeerAuthentication
	Pod
	ProxyConfig
	ReferenceGrant
	ReferencePolicy
	RequestAuthentication
	Secret
	Service
	ServiceEntry
	Sidecar
	TCPRoute
	TLSRoute
	Telemetry
	VirtualService
	WasmPlugin
	WorkloadEntry
	WorkloadGroup
)

func (k Kind) String() string {
	switch k {
	case AuthorizationPolicy:
		return "AuthorizationPolicy"
	case ConfigMap:
		return "ConfigMap"
	case CustomResourceDefinition:
		return "CustomResourceDefinition"
	case Deployment:
		return "Deployment"
	case DestinationRule:
		return "DestinationRule"
	case Endpoints:
		return "Endpoints"
	case EnvoyFilter:
		return "EnvoyFilter"
	case Gateway:
		return "Gateway"
	case GatewayClass:
		return "GatewayClass"
	case HTTPRoute:
		return "HTTPRoute"
	case Ingress:
		return "Ingress"
	case KubernetesGateway:
		return "Gateway"
	case MeshConfig:
		return "MeshConfig"
	case MeshNetworks:
		return "MeshNetworks"
	case MutatingWebhookConfiguration:
		return "MutatingWebhookConfiguration"
	case Namespace:
		return "Namespace"
	case Node:
		return "Node"
	case PeerAuthentication:
		return "PeerAuthentication"
	case Pod:
		return "Pod"
	case ProxyConfig:
		return "ProxyConfig"
	case ReferenceGrant:
		return "ReferenceGrant"
	case ReferencePolicy:
		return "ReferencePolicy"
	case RequestAuthentication:
		return "RequestAuthentication"
	case Secret:
		return "Secret"
	case Service:
		return "Service"
	case ServiceEntry:
		return "ServiceEntry"
	case Sidecar:
		return "Sidecar"
	case TCPRoute:
		return "TCPRoute"
	case TLSRoute:
		return "TLSRoute"
	case Telemetry:
		return "Telemetry"
	case VirtualService:
		return "VirtualService"
	case WasmPlugin:
		return "WasmPlugin"
	case WorkloadEntry:
		return "WorkloadEntry"
	case WorkloadGroup:
		return "WorkloadGroup"
	default:
		return "Unknown"
	}
}

func FromGvk(gvk config.GroupVersionKind) Kind {
	if gvk.Kind == "AuthorizationPolicy" && gvk.Group == "security.istio.io" && gvk.Version == "v1beta1" {
		return AuthorizationPolicy
	}
	if gvk.Kind == "ConfigMap" && gvk.Group == "" && gvk.Version == "v1" {
		return ConfigMap
	}
	if gvk.Kind == "CustomResourceDefinition" && gvk.Group == "apiextensions.k8s.io" && gvk.Version == "v1" {
		return CustomResourceDefinition
	}
	if gvk.Kind == "Deployment" && gvk.Group == "apps" && gvk.Version == "v1" {
		return Deployment
	}
	if gvk.Kind == "DestinationRule" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return DestinationRule
	}
	if gvk.Kind == "Endpoints" && gvk.Group == "" && gvk.Version == "v1" {
		return Endpoints
	}
	if gvk.Kind == "EnvoyFilter" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return EnvoyFilter
	}
	if gvk.Kind == "Gateway" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return Gateway
	}
	if gvk.Kind == "GatewayClass" && gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1alpha2" {
		return GatewayClass
	}
	if gvk.Kind == "HTTPRoute" && gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1alpha2" {
		return HTTPRoute
	}
	if gvk.Kind == "Ingress" && gvk.Group == "extensions" && gvk.Version == "v1beta1" {
		return Ingress
	}
	if gvk.Kind == "Gateway" && gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1alpha2" {
		return KubernetesGateway
	}
	if gvk.Kind == "MeshConfig" && gvk.Group == "" && gvk.Version == "v1alpha1" {
		return MeshConfig
	}
	if gvk.Kind == "MeshNetworks" && gvk.Group == "" && gvk.Version == "v1alpha1" {
		return MeshNetworks
	}
	if gvk.Kind == "MutatingWebhookConfiguration" && gvk.Group == "admissionregistration.k8s.io" && gvk.Version == "v1" {
		return MutatingWebhookConfiguration
	}
	if gvk.Kind == "Namespace" && gvk.Group == "" && gvk.Version == "v1" {
		return Namespace
	}
	if gvk.Kind == "Node" && gvk.Group == "" && gvk.Version == "v1" {
		return Node
	}
	if gvk.Kind == "PeerAuthentication" && gvk.Group == "security.istio.io" && gvk.Version == "v1beta1" {
		return PeerAuthentication
	}
	if gvk.Kind == "Pod" && gvk.Group == "" && gvk.Version == "v1" {
		return Pod
	}
	if gvk.Kind == "ProxyConfig" && gvk.Group == "networking.istio.io" && gvk.Version == "v1beta1" {
		return ProxyConfig
	}
	if gvk.Kind == "ReferenceGrant" && gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1alpha2" {
		return ReferenceGrant
	}
	if gvk.Kind == "ReferencePolicy" && gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1alpha2" {
		return ReferencePolicy
	}
	if gvk.Kind == "RequestAuthentication" && gvk.Group == "security.istio.io" && gvk.Version == "v1beta1" {
		return RequestAuthentication
	}
	if gvk.Kind == "Secret" && gvk.Group == "" && gvk.Version == "v1" {
		return Secret
	}
	if gvk.Kind == "Service" && gvk.Group == "" && gvk.Version == "v1" {
		return Service
	}
	if gvk.Kind == "ServiceEntry" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return ServiceEntry
	}
	if gvk.Kind == "Sidecar" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return Sidecar
	}
	if gvk.Kind == "TCPRoute" && gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1alpha2" {
		return TCPRoute
	}
	if gvk.Kind == "TLSRoute" && gvk.Group == "gateway.networking.k8s.io" && gvk.Version == "v1alpha2" {
		return TLSRoute
	}
	if gvk.Kind == "Telemetry" && gvk.Group == "telemetry.istio.io" && gvk.Version == "v1alpha1" {
		return Telemetry
	}
	if gvk.Kind == "VirtualService" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return VirtualService
	}
	if gvk.Kind == "WasmPlugin" && gvk.Group == "extensions.istio.io" && gvk.Version == "v1alpha1" {
		return WasmPlugin
	}
	if gvk.Kind == "WorkloadEntry" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return WorkloadEntry
	}
	if gvk.Kind == "WorkloadGroup" && gvk.Group == "networking.istio.io" && gvk.Version == "v1alpha3" {
		return WorkloadGroup
	}

	panic("unknown kind: " + gvk.String())
}
