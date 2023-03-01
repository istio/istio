//go:build agent
// +build agent

// GENERATED FILE -- DO NOT EDIT
//

package collections

import (
	"reflect"

	istioioapiextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioioapimeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioioapimetav1alpha1 "istio.io/api/meta/v1alpha1"
	istioioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioioapinetworkingv1beta1 "istio.io/api/networking/v1beta1"
	istioioapisecurityv1beta1 "istio.io/api/security/v1beta1"
	istioioapitelemetryv1alpha1 "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
)

var (

	// IstioExtensionsV1Alpha1Wasmplugins describes the collection
	// istio/extensions/v1alpha1/wasmplugins
	IstioExtensionsV1Alpha1Wasmplugins = resource.Builder{
		Identifier: "WasmPlugin",
		Group:      "extensions.istio.io",
		Kind:       "WasmPlugin",
		Plural:     "wasmplugins",
		Version:    "v1alpha1",
		Proto:      "istio.extensions.v1alpha1.WasmPlugin", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapiextensionsv1alpha1.WasmPlugin{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/extensions/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateWasmPlugin,
	}.MustBuild()

	// IstioMeshV1Alpha1MeshConfig describes the collection
	// istio/mesh/v1alpha1/MeshConfig
	IstioMeshV1Alpha1MeshConfig = resource.Builder{
		Identifier:    "MeshConfig",
		Group:         "",
		Kind:          "MeshConfig",
		Plural:        "meshconfigs",
		Version:       "v1alpha1",
		Proto:         "istio.mesh.v1alpha1.MeshConfig",
		ReflectType:   reflect.TypeOf(&istioioapimeshv1alpha1.MeshConfig{}).Elem(),
		ProtoPackage:  "istio.io/api/mesh/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.EmptyValidate,
	}.MustBuild()

	// IstioMeshV1Alpha1MeshNetworks describes the collection
	// istio/mesh/v1alpha1/MeshNetworks
	IstioMeshV1Alpha1MeshNetworks = resource.Builder{
		Identifier:    "MeshNetworks",
		Group:         "",
		Kind:          "MeshNetworks",
		Plural:        "meshnetworks",
		Version:       "v1alpha1",
		Proto:         "istio.mesh.v1alpha1.MeshNetworks",
		ReflectType:   reflect.TypeOf(&istioioapimeshv1alpha1.MeshNetworks{}).Elem(),
		ProtoPackage:  "istio.io/api/mesh/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.EmptyValidate,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Destinationrules describes the collection
	// istio/networking/v1alpha3/destinationrules
	IstioNetworkingV1Alpha3Destinationrules = resource.Builder{
		Identifier: "DestinationRule",
		Group:      "networking.istio.io",
		Kind:       "DestinationRule",
		Plural:     "destinationrules",
		Version:    "v1alpha3",
		VersionAliases: []string{
			"v1beta1",
		},
		Proto: "istio.networking.v1alpha3.DestinationRule", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.DestinationRule{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateDestinationRule,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Envoyfilters describes the collection
	// istio/networking/v1alpha3/envoyfilters
	IstioNetworkingV1Alpha3Envoyfilters = resource.Builder{
		Identifier: "EnvoyFilter",
		Group:      "networking.istio.io",
		Kind:       "EnvoyFilter",
		Plural:     "envoyfilters",
		Version:    "v1alpha3",
		Proto:      "istio.networking.v1alpha3.EnvoyFilter", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.EnvoyFilter{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateEnvoyFilter,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Gateways describes the collection
	// istio/networking/v1alpha3/gateways
	IstioNetworkingV1Alpha3Gateways = resource.Builder{
		Identifier: "Gateway",
		Group:      "networking.istio.io",
		Kind:       "Gateway",
		Plural:     "gateways",
		Version:    "v1alpha3",
		VersionAliases: []string{
			"v1beta1",
		},
		Proto: "istio.networking.v1alpha3.Gateway", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.Gateway{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateGateway,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Serviceentries describes the collection
	// istio/networking/v1alpha3/serviceentries
	IstioNetworkingV1Alpha3Serviceentries = resource.Builder{
		Identifier: "ServiceEntry",
		Group:      "networking.istio.io",
		Kind:       "ServiceEntry",
		Plural:     "serviceentries",
		Version:    "v1alpha3",
		VersionAliases: []string{
			"v1beta1",
		},
		Proto: "istio.networking.v1alpha3.ServiceEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.ServiceEntry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateServiceEntry,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Sidecars describes the collection
	// istio/networking/v1alpha3/sidecars
	IstioNetworkingV1Alpha3Sidecars = resource.Builder{
		Identifier: "Sidecar",
		Group:      "networking.istio.io",
		Kind:       "Sidecar",
		Plural:     "sidecars",
		Version:    "v1alpha3",
		VersionAliases: []string{
			"v1beta1",
		},
		Proto: "istio.networking.v1alpha3.Sidecar", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.Sidecar{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateSidecar,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Virtualservices describes the collection
	// istio/networking/v1alpha3/virtualservices
	IstioNetworkingV1Alpha3Virtualservices = resource.Builder{
		Identifier: "VirtualService",
		Group:      "networking.istio.io",
		Kind:       "VirtualService",
		Plural:     "virtualservices",
		Version:    "v1alpha3",
		VersionAliases: []string{
			"v1beta1",
		},
		Proto: "istio.networking.v1alpha3.VirtualService", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.VirtualService{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateVirtualService,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Workloadentries describes the collection
	// istio/networking/v1alpha3/workloadentries
	IstioNetworkingV1Alpha3Workloadentries = resource.Builder{
		Identifier: "WorkloadEntry",
		Group:      "networking.istio.io",
		Kind:       "WorkloadEntry",
		Plural:     "workloadentries",
		Version:    "v1alpha3",
		VersionAliases: []string{
			"v1beta1",
		},
		Proto: "istio.networking.v1alpha3.WorkloadEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadEntry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateWorkloadEntry,
	}.MustBuild()

	// IstioNetworkingV1Alpha3Workloadgroups describes the collection
	// istio/networking/v1alpha3/workloadgroups
	IstioNetworkingV1Alpha3Workloadgroups = resource.Builder{
		Identifier: "WorkloadGroup",
		Group:      "networking.istio.io",
		Kind:       "WorkloadGroup",
		Plural:     "workloadgroups",
		Version:    "v1alpha3",
		VersionAliases: []string{
			"v1beta1",
		},
		Proto: "istio.networking.v1alpha3.WorkloadGroup", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadGroup{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateWorkloadGroup,
	}.MustBuild()

	// IstioNetworkingV1Beta1Proxyconfigs describes the collection
	// istio/networking/v1beta1/proxyconfigs
	IstioNetworkingV1Beta1Proxyconfigs = resource.Builder{
		Identifier: "ProxyConfig",
		Group:      "networking.istio.io",
		Kind:       "ProxyConfig",
		Plural:     "proxyconfigs",
		Version:    "v1beta1",
		Proto:      "istio.networking.v1beta1.ProxyConfig", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapinetworkingv1beta1.ProxyConfig{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/networking/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateProxyConfig,
	}.MustBuild()

	// IstioSecurityV1Beta1Authorizationpolicies describes the collection
	// istio/security/v1beta1/authorizationpolicies
	IstioSecurityV1Beta1Authorizationpolicies = resource.Builder{
		Identifier: "AuthorizationPolicy",
		Group:      "security.istio.io",
		Kind:       "AuthorizationPolicy",
		Plural:     "authorizationpolicies",
		Version:    "v1beta1",
		VersionAliases: []string{
			"v1",
		},
		Proto: "istio.security.v1beta1.AuthorizationPolicy", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapisecurityv1beta1.AuthorizationPolicy{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/security/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateAuthorizationPolicy,
	}.MustBuild()

	// IstioSecurityV1Beta1Peerauthentications describes the collection
	// istio/security/v1beta1/peerauthentications
	IstioSecurityV1Beta1Peerauthentications = resource.Builder{
		Identifier: "PeerAuthentication",
		Group:      "security.istio.io",
		Kind:       "PeerAuthentication",
		Plural:     "peerauthentications",
		Version:    "v1beta1",
		Proto:      "istio.security.v1beta1.PeerAuthentication", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapisecurityv1beta1.PeerAuthentication{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/security/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidatePeerAuthentication,
	}.MustBuild()

	// IstioSecurityV1Beta1Requestauthentications describes the collection
	// istio/security/v1beta1/requestauthentications
	IstioSecurityV1Beta1Requestauthentications = resource.Builder{
		Identifier: "RequestAuthentication",
		Group:      "security.istio.io",
		Kind:       "RequestAuthentication",
		Plural:     "requestauthentications",
		Version:    "v1beta1",
		VersionAliases: []string{
			"v1",
		},
		Proto: "istio.security.v1beta1.RequestAuthentication", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapisecurityv1beta1.RequestAuthentication{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/security/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateRequestAuthentication,
	}.MustBuild()

	// IstioTelemetryV1Alpha1Telemetries describes the collection
	// istio/telemetry/v1alpha1/telemetries
	IstioTelemetryV1Alpha1Telemetries = resource.Builder{
		Identifier: "Telemetry",
		Group:      "telemetry.istio.io",
		Kind:       "Telemetry",
		Plural:     "telemetries",
		Version:    "v1alpha1",
		Proto:      "istio.telemetry.v1alpha1.Telemetry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
		ReflectType: reflect.TypeOf(&istioioapitelemetryv1alpha1.Telemetry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
		ProtoPackage: "istio.io/api/telemetry/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
		ClusterScoped: false,
		ValidateProto: validation.ValidateTelemetry,
	}.MustBuild()

	// All contains all collections in the system.
	All = collection.NewSchemasBuilder().
		MustAdd(IstioExtensionsV1Alpha1Wasmplugins).
		MustAdd(IstioMeshV1Alpha1MeshConfig).
		MustAdd(IstioMeshV1Alpha1MeshNetworks).
		MustAdd(IstioNetworkingV1Alpha3Destinationrules).
		MustAdd(IstioNetworkingV1Alpha3Envoyfilters).
		MustAdd(IstioNetworkingV1Alpha3Gateways).
		MustAdd(IstioNetworkingV1Alpha3Serviceentries).
		MustAdd(IstioNetworkingV1Alpha3Sidecars).
		MustAdd(IstioNetworkingV1Alpha3Virtualservices).
		MustAdd(IstioNetworkingV1Alpha3Workloadentries).
		MustAdd(IstioNetworkingV1Alpha3Workloadgroups).
		MustAdd(IstioNetworkingV1Beta1Proxyconfigs).
		MustAdd(IstioSecurityV1Beta1Authorizationpolicies).
		MustAdd(IstioSecurityV1Beta1Peerauthentications).
		MustAdd(IstioSecurityV1Beta1Requestauthentications).
		MustAdd(IstioTelemetryV1Alpha1Telemetries).
		Build()

	// Istio contains only Istio collections.
	Istio = collection.NewSchemasBuilder().
		MustAdd(IstioExtensionsV1Alpha1Wasmplugins).
		MustAdd(IstioMeshV1Alpha1MeshConfig).
		MustAdd(IstioMeshV1Alpha1MeshNetworks).
		MustAdd(IstioNetworkingV1Alpha3Destinationrules).
		MustAdd(IstioNetworkingV1Alpha3Envoyfilters).
		MustAdd(IstioNetworkingV1Alpha3Gateways).
		MustAdd(IstioNetworkingV1Alpha3Serviceentries).
		MustAdd(IstioNetworkingV1Alpha3Sidecars).
		MustAdd(IstioNetworkingV1Alpha3Virtualservices).
		MustAdd(IstioNetworkingV1Alpha3Workloadentries).
		MustAdd(IstioNetworkingV1Alpha3Workloadgroups).
		MustAdd(IstioNetworkingV1Beta1Proxyconfigs).
		MustAdd(IstioSecurityV1Beta1Authorizationpolicies).
		MustAdd(IstioSecurityV1Beta1Peerauthentications).
		MustAdd(IstioSecurityV1Beta1Requestauthentications).
		MustAdd(IstioTelemetryV1Alpha1Telemetries).
		Build()

	// Kube contains only kubernetes collections.
	Kube = collection.NewSchemasBuilder().
		Build()

	// Builtin contains only native Kubernetes collections. This differs from Kube, which has
	// Kubernetes controlled CRDs
	Builtin = collection.NewSchemasBuilder().
		Build()

	// Pilot contains only collections used by Pilot.
	Pilot = collection.NewSchemasBuilder().
		MustAdd(IstioExtensionsV1Alpha1Wasmplugins).
		MustAdd(IstioNetworkingV1Alpha3Destinationrules).
		MustAdd(IstioNetworkingV1Alpha3Envoyfilters).
		MustAdd(IstioNetworkingV1Alpha3Gateways).
		MustAdd(IstioNetworkingV1Alpha3Serviceentries).
		MustAdd(IstioNetworkingV1Alpha3Sidecars).
		MustAdd(IstioNetworkingV1Alpha3Virtualservices).
		MustAdd(IstioNetworkingV1Alpha3Workloadentries).
		MustAdd(IstioNetworkingV1Alpha3Workloadgroups).
		MustAdd(IstioNetworkingV1Beta1Proxyconfigs).
		MustAdd(IstioSecurityV1Beta1Authorizationpolicies).
		MustAdd(IstioSecurityV1Beta1Peerauthentications).
		MustAdd(IstioSecurityV1Beta1Requestauthentications).
		MustAdd(IstioTelemetryV1Alpha1Telemetries).
		Build()

	// PilotGatewayAPI contains only collections used by Pilot, including experimental Service Api.
	PilotGatewayAPI = collection.NewSchemasBuilder().
			MustAdd(IstioExtensionsV1Alpha1Wasmplugins).
			MustAdd(IstioNetworkingV1Alpha3Destinationrules).
			MustAdd(IstioNetworkingV1Alpha3Envoyfilters).
			MustAdd(IstioNetworkingV1Alpha3Gateways).
			MustAdd(IstioNetworkingV1Alpha3Serviceentries).
			MustAdd(IstioNetworkingV1Alpha3Sidecars).
			MustAdd(IstioNetworkingV1Alpha3Virtualservices).
			MustAdd(IstioNetworkingV1Alpha3Workloadentries).
			MustAdd(IstioNetworkingV1Alpha3Workloadgroups).
			MustAdd(IstioNetworkingV1Beta1Proxyconfigs).
			MustAdd(IstioSecurityV1Beta1Authorizationpolicies).
			MustAdd(IstioSecurityV1Beta1Peerauthentications).
			MustAdd(IstioSecurityV1Beta1Requestauthentications).
			MustAdd(IstioTelemetryV1Alpha1Telemetries).
			Build()

	// Deprecated contains only collections used by that will soon be used by nothing.
	Deprecated = collection.NewSchemasBuilder().
			Build()
)
