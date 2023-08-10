// GENERATED FILE -- DO NOT EDIT
//

package gvr

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	ServiceExport                  = schema.GroupVersionResource{Group: "multicluster.x-k8s.io", Version: "v1alpha1", Resource: "serviceexports"}
	ServiceImport                  = schema.GroupVersionResource{Group: "multicluster.x-k8s.io", Version: "v1alpha1", Resource: "serviceimports"}
	AuthorizationPolicy            = schema.GroupVersionResource{Group: "security.istio.io", Version: "v1beta1", Resource: "authorizationpolicies"}
	CertificateSigningRequest      = schema.GroupVersionResource{Group: "certificates.k8s.io", Version: "v1", Resource: "certificatesigningrequests"}
	ConfigMap                      = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	CustomResourceDefinition       = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	Deployment                     = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	DestinationRule                = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "destinationrules"}
	EndpointSlice                  = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "endpointslices"}
	Endpoints                      = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "endpoints"}
	EnvoyFilter                    = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "envoyfilters"}
	GRPCRoute                      = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "grpcroutes"}
	Gateway                        = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "gateways"}
	GatewayClass                   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "gatewayclasses"}
	HTTPRoute                      = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "httproutes"}
	Ingress                        = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
	IngressClass                   = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}
	KubernetesGateway              = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "gateways"}
	MeshConfig                     = schema.GroupVersionResource{Group: "", Version: "v1alpha1", Resource: "meshconfigs"}
	MeshNetworks                   = schema.GroupVersionResource{Group: "", Version: "v1alpha1", Resource: "meshnetworks"}
	MutatingWebhookConfiguration   = schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"}
	Namespace                      = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	Node                           = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}
	PeerAuthentication             = schema.GroupVersionResource{Group: "security.istio.io", Version: "v1beta1", Resource: "peerauthentications"}
	Pod                            = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	ProxyConfig                    = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1beta1", Resource: "proxyconfigs"}
	ReferenceGrant                 = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}
	RequestAuthentication          = schema.GroupVersionResource{Group: "security.istio.io", Version: "v1beta1", Resource: "requestauthentications"}
	Secret                         = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}
	Service                        = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	ServiceAccount                 = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}
	ServiceEntry                   = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "serviceentries"}
	Sidecar                        = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "sidecars"}
	TCPRoute                       = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tcproutes"}
	TLSRoute                       = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tlsroutes"}
	Telemetry                      = schema.GroupVersionResource{Group: "telemetry.istio.io", Version: "v1alpha1", Resource: "telemetries"}
	UDPRoute                       = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "udproutes"}
	ValidatingWebhookConfiguration = schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"}
	VirtualService                 = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "virtualservices"}
	WasmPlugin                     = schema.GroupVersionResource{Group: "extensions.istio.io", Version: "v1alpha1", Resource: "wasmplugins"}
	WorkloadEntry                  = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "workloadentries"}
	WorkloadGroup                  = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "workloadgroups"}
)

func IsClusterScoped(g schema.GroupVersionResource) bool {
	switch g {
	case ServiceExport:
		return false
	case ServiceImport:
		return false
	case AuthorizationPolicy:
		return false
	case CertificateSigningRequest:
		return true
	case ConfigMap:
		return false
	case CustomResourceDefinition:
		return true
	case Deployment:
		return false
	case DestinationRule:
		return false
	case EndpointSlice:
		return false
	case Endpoints:
		return false
	case EnvoyFilter:
		return false
	case GRPCRoute:
		return false
	case Gateway:
		return false
	case GatewayClass:
		return true
	case HTTPRoute:
		return false
	case Ingress:
		return false
	case IngressClass:
		return true
	case KubernetesGateway:
		return false
	case MutatingWebhookConfiguration:
		return true
	case Namespace:
		return true
	case Node:
		return true
	case PeerAuthentication:
		return false
	case Pod:
		return false
	case ProxyConfig:
		return false
	case ReferenceGrant:
		return false
	case RequestAuthentication:
		return false
	case Secret:
		return false
	case Service:
		return false
	case ServiceAccount:
		return false
	case ServiceEntry:
		return false
	case Sidecar:
		return false
	case TCPRoute:
		return false
	case TLSRoute:
		return false
	case Telemetry:
		return false
	case UDPRoute:
		return false
	case ValidatingWebhookConfiguration:
		return true
	case VirtualService:
		return false
	case WasmPlugin:
		return false
	case WorkloadEntry:
		return false
	case WorkloadGroup:
		return false
	}
	// shouldn't happen
	return false
}
