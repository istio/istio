
// GENERATED FILE -- DO NOT EDIT
//

package gvk

import (
	"istio.io/istio/pkg/config"
)

var (
	AuthorizationPolicy = config.GroupVersionKind{Group: "security.istio.io", Version: "v1beta1", Kind: "AuthorizationPolicy"}
	BackendPolicy = config.GroupVersionKind{Group: "networking.x-k8s.io", Version: "v1alpha1", Kind: "BackendPolicy"}
	ConfigMap = config.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	CustomResourceDefinition = config.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
	Deployment = config.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	DestinationRule = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "DestinationRule"}
	Endpoints = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Endpoints"}
	EnvoyFilter = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"}
	Gateway = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Gateway"}
	GatewayClass = config.GroupVersionKind{Group: "networking.x-k8s.io", Version: "v1alpha1", Kind: "GatewayClass"}
	HTTPRoute = config.GroupVersionKind{Group: "networking.x-k8s.io", Version: "v1alpha1", Kind: "HTTPRoute"}
	Ingress = config.GroupVersionKind{Group: "extensions", Version: "v1beta1", Kind: "Ingress"}
	MeshConfig = config.GroupVersionKind{Group: "", Version: "v1alpha1", Kind: "MeshConfig"}
	MeshNetworks = config.GroupVersionKind{Group: "", Version: "v1alpha1", Kind: "MeshNetworks"}
	MutatingWebhookConfiguration = config.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1", Kind: "MutatingWebhookConfiguration"}
	Namespace = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	Node = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
	PeerAuthentication = config.GroupVersionKind{Group: "security.istio.io", Version: "v1beta1", Kind: "PeerAuthentication"}
	Pod = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	RequestAuthentication = config.GroupVersionKind{Group: "security.istio.io", Version: "v1beta1", Kind: "RequestAuthentication"}
	Secret = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	Service = config.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}
	ServiceApisGateway = config.GroupVersionKind{Group: "networking.x-k8s.io", Version: "v1alpha1", Kind: "Gateway"}
	ServiceEntry = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "ServiceEntry"}
	Sidecar = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Sidecar"}
	TCPRoute = config.GroupVersionKind{Group: "networking.x-k8s.io", Version: "v1alpha1", Kind: "TCPRoute"}
	TLSRoute = config.GroupVersionKind{Group: "networking.x-k8s.io", Version: "v1alpha1", Kind: "TLSRoute"}
	Telemetry = config.GroupVersionKind{Group: "telemetry.istio.io", Version: "v1alpha1", Kind: "Telemetry"}
	VirtualService = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "VirtualService"}
	WasmPlugin = config.GroupVersionKind{Group: "extensions.istio.io", Version: "v1alpha1", Kind: "WasmPlugin"}
	WorkloadEntry = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "WorkloadEntry"}
	WorkloadGroup = config.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "WorkloadGroup"}
)
