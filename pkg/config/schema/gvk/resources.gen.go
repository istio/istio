// GENERATED FILE -- DO NOT EDIT
//

package gvk

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvr"
)

var (
	AuthorizationPolicy            = config.GroupVersionKind{Group: "security.istio.io", Version: "v1beta1", Kind: "AuthorizationPolicy"}
	CertificateSigningRequest      = config.GroupVersionKind{Group: "certificates.k8s.io", Version: "v1", Kind: "CertificateSigningRequest"}
	ConfigMap                      = config.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	CustomResourceDefinition       = config.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
	Deployment                     = config.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	DestinationRule                = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "DestinationRule"}
	EndpointSlice                  = config.GroupVersionKind{Group: "", Version: "v1", Kind: "EndpointSlice"}
	Endpoints                      = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Endpoints"}
	EnvoyFilter                    = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"}
	GRPCRoute                      = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Kind: "GRPCRoute"}
	Gateway                        = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Gateway"}
	GatewayClass                   = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "GatewayClass"}
	HTTPRoute                      = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "HTTPRoute"}
	Ingress                        = config.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}
	IngressClass                   = config.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "IngressClass"}
	KubernetesGateway              = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "Gateway"}
	MeshConfig                     = config.GroupVersionKind{Group: "", Version: "v1alpha1", Kind: "MeshConfig"}
	MeshNetworks                   = config.GroupVersionKind{Group: "", Version: "v1alpha1", Kind: "MeshNetworks"}
	MutatingWebhookConfiguration   = config.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "MutatingWebhookConfiguration"}
	Namespace                      = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	Node                           = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
	PeerAuthentication             = config.GroupVersionKind{Group: "security.istio.io", Version: "v1beta1", Kind: "PeerAuthentication"}
	Pod                            = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	ProxyConfig                    = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1beta1", Kind: "ProxyConfig"}
	ReferenceGrant                 = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1beta1", Kind: "ReferenceGrant"}
	RequestAuthentication          = config.GroupVersionKind{Group: "security.istio.io", Version: "v1beta1", Kind: "RequestAuthentication"}
	Secret                         = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	Service                        = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}
	ServiceAccount                 = config.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"}
	ServiceEntry                   = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "ServiceEntry"}
	Sidecar                        = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Sidecar"}
	TCPRoute                       = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Kind: "TCPRoute"}
	TLSRoute                       = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Kind: "TLSRoute"}
	Telemetry                      = config.GroupVersionKind{Group: "telemetry.istio.io", Version: "v1alpha1", Kind: "Telemetry"}
	UDPRoute                       = config.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Kind: "UDPRoute"}
	ValidatingWebhookConfiguration = config.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "ValidatingWebhookConfiguration"}
	VirtualService                 = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "VirtualService"}
	WasmPlugin                     = config.GroupVersionKind{Group: "extensions.istio.io", Version: "v1alpha1", Kind: "WasmPlugin"}
	WorkloadEntry                  = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "WorkloadEntry"}
	WorkloadGroup                  = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "WorkloadGroup"}
)

// ToGVR converts a GVK to a GVR.
func ToGVR(g config.GroupVersionKind) (schema.GroupVersionResource, bool) {
	switch g {
	case AuthorizationPolicy:
		return gvr.AuthorizationPolicy, true
	case CertificateSigningRequest:
		return gvr.CertificateSigningRequest, true
	case ConfigMap:
		return gvr.ConfigMap, true
	case CustomResourceDefinition:
		return gvr.CustomResourceDefinition, true
	case Deployment:
		return gvr.Deployment, true
	case DestinationRule:
		return gvr.DestinationRule, true
	case EndpointSlice:
		return gvr.EndpointSlice, true
	case Endpoints:
		return gvr.Endpoints, true
	case EnvoyFilter:
		return gvr.EnvoyFilter, true
	case GRPCRoute:
		return gvr.GRPCRoute, true
	case Gateway:
		return gvr.Gateway, true
	case GatewayClass:
		return gvr.GatewayClass, true
	case HTTPRoute:
		return gvr.HTTPRoute, true
	case Ingress:
		return gvr.Ingress, true
	case IngressClass:
		return gvr.IngressClass, true
	case KubernetesGateway:
		return gvr.KubernetesGateway, true
	case MeshConfig:
		return gvr.MeshConfig, true
	case MeshNetworks:
		return gvr.MeshNetworks, true
	case MutatingWebhookConfiguration:
		return gvr.MutatingWebhookConfiguration, true
	case Namespace:
		return gvr.Namespace, true
	case Node:
		return gvr.Node, true
	case PeerAuthentication:
		return gvr.PeerAuthentication, true
	case Pod:
		return gvr.Pod, true
	case ProxyConfig:
		return gvr.ProxyConfig, true
	case ReferenceGrant:
		return gvr.ReferenceGrant, true
	case RequestAuthentication:
		return gvr.RequestAuthentication, true
	case Secret:
		return gvr.Secret, true
	case Service:
		return gvr.Service, true
	case ServiceAccount:
		return gvr.ServiceAccount, true
	case ServiceEntry:
		return gvr.ServiceEntry, true
	case Sidecar:
		return gvr.Sidecar, true
	case TCPRoute:
		return gvr.TCPRoute, true
	case TLSRoute:
		return gvr.TLSRoute, true
	case Telemetry:
		return gvr.Telemetry, true
	case UDPRoute:
		return gvr.UDPRoute, true
	case ValidatingWebhookConfiguration:
		return gvr.ValidatingWebhookConfiguration, true
	case VirtualService:
		return gvr.VirtualService, true
	case WasmPlugin:
		return gvr.WasmPlugin, true
	case WorkloadEntry:
		return gvr.WorkloadEntry, true
	case WorkloadGroup:
		return gvr.WorkloadGroup, true
	}

	return schema.GroupVersionResource{}, false
}

// MustToGVR converts a GVK to a GVR, and panics if it cannot be converted
// Warning: this is only safe for known types; do not call on arbitrary GVKs
func MustToGVR(g config.GroupVersionKind) schema.GroupVersionResource {
	r, ok := ToGVR(g)
	if !ok {
		panic("unknown kind: " + g.String())
	}
	return r
}

// FromGVR converts a GVR to a GVK.
func FromGVR(g schema.GroupVersionResource) (config.GroupVersionKind, bool) {
	switch g {
	case gvr.AuthorizationPolicy:
		return AuthorizationPolicy, true
	case gvr.CertificateSigningRequest:
		return CertificateSigningRequest, true
	case gvr.ConfigMap:
		return ConfigMap, true
	case gvr.CustomResourceDefinition:
		return CustomResourceDefinition, true
	case gvr.Deployment:
		return Deployment, true
	case gvr.DestinationRule:
		return DestinationRule, true
	case gvr.EndpointSlice:
		return EndpointSlice, true
	case gvr.Endpoints:
		return Endpoints, true
	case gvr.EnvoyFilter:
		return EnvoyFilter, true
	case gvr.GRPCRoute:
		return GRPCRoute, true
	case gvr.Gateway:
		return Gateway, true
	case gvr.GatewayClass:
		return GatewayClass, true
	case gvr.HTTPRoute:
		return HTTPRoute, true
	case gvr.Ingress:
		return Ingress, true
	case gvr.IngressClass:
		return IngressClass, true
	case gvr.KubernetesGateway:
		return KubernetesGateway, true
	case gvr.MeshConfig:
		return MeshConfig, true
	case gvr.MeshNetworks:
		return MeshNetworks, true
	case gvr.MutatingWebhookConfiguration:
		return MutatingWebhookConfiguration, true
	case gvr.Namespace:
		return Namespace, true
	case gvr.Node:
		return Node, true
	case gvr.PeerAuthentication:
		return PeerAuthentication, true
	case gvr.Pod:
		return Pod, true
	case gvr.ProxyConfig:
		return ProxyConfig, true
	case gvr.ReferenceGrant:
		return ReferenceGrant, true
	case gvr.RequestAuthentication:
		return RequestAuthentication, true
	case gvr.Secret:
		return Secret, true
	case gvr.Service:
		return Service, true
	case gvr.ServiceAccount:
		return ServiceAccount, true
	case gvr.ServiceEntry:
		return ServiceEntry, true
	case gvr.Sidecar:
		return Sidecar, true
	case gvr.TCPRoute:
		return TCPRoute, true
	case gvr.TLSRoute:
		return TLSRoute, true
	case gvr.Telemetry:
		return Telemetry, true
	case gvr.UDPRoute:
		return UDPRoute, true
	case gvr.ValidatingWebhookConfiguration:
		return ValidatingWebhookConfiguration, true
	case gvr.VirtualService:
		return VirtualService, true
	case gvr.WasmPlugin:
		return WasmPlugin, true
	case gvr.WorkloadEntry:
		return WorkloadEntry, true
	case gvr.WorkloadGroup:
		return WorkloadGroup, true
	}

	return config.GroupVersionKind{}, false
}

// FromGVR converts a GVR to a GVK, and panics if it cannot be converted
// Warning: this is only safe for known types; do not call on arbitrary GVRs
func MustFromGVR(g schema.GroupVersionResource) config.GroupVersionKind {
	r, ok := FromGVR(g)
	if !ok {
		panic("unknown kind: " + g.String())
	}
	return r
}
