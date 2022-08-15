//go:build !agent
// +build !agent

// GENERATED FILE -- DO NOT EDIT
//

package collections

import (
	"reflect"

	k8sioapiadmissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8sioapiappsv1 "k8s.io/api/apps/v1"
	k8sioapicorev1 "k8s.io/api/core/v1"
	k8sioapiextensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	k8sioapiextensionsapiserverpkgapisapiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	sigsk8siogatewayapiapisv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

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
	IstioExtensionsV1Alpha1Wasmplugins = collection.Builder{
		Name:         "istio/extensions/v1alpha1/wasmplugins",
		VariableName: "IstioExtensionsV1Alpha1Wasmplugins",
		Resource: resource.Builder{
			Group:   "extensions.istio.io",
			Kind:    "WasmPlugin",
			Plural:  "wasmplugins",
			Version: "v1alpha1",
			Proto:   "istio.extensions.v1alpha1.WasmPlugin", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapiextensionsv1alpha1.WasmPlugin{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/extensions/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateWasmPlugin,
		}.MustBuild(),
	}.MustBuild()

	// IstioMeshV1Alpha1MeshConfig describes the collection
	// istio/mesh/v1alpha1/MeshConfig
	IstioMeshV1Alpha1MeshConfig = collection.Builder{
		Name:         "istio/mesh/v1alpha1/MeshConfig",
		VariableName: "IstioMeshV1Alpha1MeshConfig",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "MeshConfig",
			Plural:        "meshconfigs",
			Version:       "v1alpha1",
			Proto:         "istio.mesh.v1alpha1.MeshConfig",
			ReflectType:   reflect.TypeOf(&istioioapimeshv1alpha1.MeshConfig{}).Elem(),
			ProtoPackage:  "istio.io/api/mesh/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// IstioMeshV1Alpha1MeshNetworks describes the collection
	// istio/mesh/v1alpha1/MeshNetworks
	IstioMeshV1Alpha1MeshNetworks = collection.Builder{
		Name:         "istio/mesh/v1alpha1/MeshNetworks",
		VariableName: "IstioMeshV1Alpha1MeshNetworks",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "MeshNetworks",
			Plural:        "meshnetworks",
			Version:       "v1alpha1",
			Proto:         "istio.mesh.v1alpha1.MeshNetworks",
			ReflectType:   reflect.TypeOf(&istioioapimeshv1alpha1.MeshNetworks{}).Elem(),
			ProtoPackage:  "istio.io/api/mesh/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Destinationrules describes the collection
	// istio/networking/v1alpha3/destinationrules
	IstioNetworkingV1Alpha3Destinationrules = collection.Builder{
		Name:         "istio/networking/v1alpha3/destinationrules",
		VariableName: "IstioNetworkingV1Alpha3Destinationrules",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "DestinationRule",
			Plural:  "destinationrules",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.DestinationRule", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.DestinationRule{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateDestinationRule,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Envoyfilters describes the collection
	// istio/networking/v1alpha3/envoyfilters
	IstioNetworkingV1Alpha3Envoyfilters = collection.Builder{
		Name:         "istio/networking/v1alpha3/envoyfilters",
		VariableName: "IstioNetworkingV1Alpha3Envoyfilters",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "EnvoyFilter",
			Plural:  "envoyfilters",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.EnvoyFilter", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.EnvoyFilter{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateEnvoyFilter,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Gateways describes the collection
	// istio/networking/v1alpha3/gateways
	IstioNetworkingV1Alpha3Gateways = collection.Builder{
		Name:         "istio/networking/v1alpha3/gateways",
		VariableName: "IstioNetworkingV1Alpha3Gateways",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "Gateway",
			Plural:  "gateways",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.Gateway", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.Gateway{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateGateway,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Serviceentries describes the collection
	// istio/networking/v1alpha3/serviceentries
	IstioNetworkingV1Alpha3Serviceentries = collection.Builder{
		Name:         "istio/networking/v1alpha3/serviceentries",
		VariableName: "IstioNetworkingV1Alpha3Serviceentries",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "ServiceEntry",
			Plural:  "serviceentries",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.ServiceEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.ServiceEntry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateServiceEntry,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Sidecars describes the collection
	// istio/networking/v1alpha3/sidecars
	IstioNetworkingV1Alpha3Sidecars = collection.Builder{
		Name:         "istio/networking/v1alpha3/sidecars",
		VariableName: "IstioNetworkingV1Alpha3Sidecars",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "Sidecar",
			Plural:  "sidecars",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.Sidecar", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.Sidecar{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateSidecar,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Virtualservices describes the collection
	// istio/networking/v1alpha3/virtualservices
	IstioNetworkingV1Alpha3Virtualservices = collection.Builder{
		Name:         "istio/networking/v1alpha3/virtualservices",
		VariableName: "IstioNetworkingV1Alpha3Virtualservices",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "VirtualService",
			Plural:  "virtualservices",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.VirtualService", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.VirtualService{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateVirtualService,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Workloadentries describes the collection
	// istio/networking/v1alpha3/workloadentries
	IstioNetworkingV1Alpha3Workloadentries = collection.Builder{
		Name:         "istio/networking/v1alpha3/workloadentries",
		VariableName: "IstioNetworkingV1Alpha3Workloadentries",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "WorkloadEntry",
			Plural:  "workloadentries",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.WorkloadEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadEntry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateWorkloadEntry,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Alpha3Workloadgroups describes the collection
	// istio/networking/v1alpha3/workloadgroups
	IstioNetworkingV1Alpha3Workloadgroups = collection.Builder{
		Name:         "istio/networking/v1alpha3/workloadgroups",
		VariableName: "IstioNetworkingV1Alpha3Workloadgroups",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "WorkloadGroup",
			Plural:  "workloadgroups",
			Version: "v1alpha3",
			VersionAliases: []string{
				"v1beta1",
			},
			Proto: "istio.networking.v1alpha3.WorkloadGroup", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadGroup{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateWorkloadGroup,
		}.MustBuild(),
	}.MustBuild()

	// IstioNetworkingV1Beta1Proxyconfigs describes the collection
	// istio/networking/v1beta1/proxyconfigs
	IstioNetworkingV1Beta1Proxyconfigs = collection.Builder{
		Name:         "istio/networking/v1beta1/proxyconfigs",
		VariableName: "IstioNetworkingV1Beta1Proxyconfigs",
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "ProxyConfig",
			Plural:  "proxyconfigs",
			Version: "v1beta1",
			Proto:   "istio.networking.v1beta1.ProxyConfig", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1beta1.ProxyConfig{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateProxyConfig,
		}.MustBuild(),
	}.MustBuild()

	// IstioSecurityV1Beta1Authorizationpolicies describes the collection
	// istio/security/v1beta1/authorizationpolicies
	IstioSecurityV1Beta1Authorizationpolicies = collection.Builder{
		Name:         "istio/security/v1beta1/authorizationpolicies",
		VariableName: "IstioSecurityV1Beta1Authorizationpolicies",
		Resource: resource.Builder{
			Group:   "security.istio.io",
			Kind:    "AuthorizationPolicy",
			Plural:  "authorizationpolicies",
			Version: "v1beta1",
			Proto:   "istio.security.v1beta1.AuthorizationPolicy", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapisecurityv1beta1.AuthorizationPolicy{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/security/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateAuthorizationPolicy,
		}.MustBuild(),
	}.MustBuild()

	// IstioSecurityV1Beta1Peerauthentications describes the collection
	// istio/security/v1beta1/peerauthentications
	IstioSecurityV1Beta1Peerauthentications = collection.Builder{
		Name:         "istio/security/v1beta1/peerauthentications",
		VariableName: "IstioSecurityV1Beta1Peerauthentications",
		Resource: resource.Builder{
			Group:   "security.istio.io",
			Kind:    "PeerAuthentication",
			Plural:  "peerauthentications",
			Version: "v1beta1",
			Proto:   "istio.security.v1beta1.PeerAuthentication", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapisecurityv1beta1.PeerAuthentication{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/security/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidatePeerAuthentication,
		}.MustBuild(),
	}.MustBuild()

	// IstioSecurityV1Beta1Requestauthentications describes the collection
	// istio/security/v1beta1/requestauthentications
	IstioSecurityV1Beta1Requestauthentications = collection.Builder{
		Name:         "istio/security/v1beta1/requestauthentications",
		VariableName: "IstioSecurityV1Beta1Requestauthentications",
		Resource: resource.Builder{
			Group:   "security.istio.io",
			Kind:    "RequestAuthentication",
			Plural:  "requestauthentications",
			Version: "v1beta1",
			Proto:   "istio.security.v1beta1.RequestAuthentication", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapisecurityv1beta1.RequestAuthentication{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/security/v1beta1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateRequestAuthentication,
		}.MustBuild(),
	}.MustBuild()

	// IstioTelemetryV1Alpha1Telemetries describes the collection
	// istio/telemetry/v1alpha1/telemetries
	IstioTelemetryV1Alpha1Telemetries = collection.Builder{
		Name:         "istio/telemetry/v1alpha1/telemetries",
		VariableName: "IstioTelemetryV1Alpha1Telemetries",
		Resource: resource.Builder{
			Group:   "telemetry.istio.io",
			Kind:    "Telemetry",
			Plural:  "telemetries",
			Version: "v1alpha1",
			Proto:   "istio.telemetry.v1alpha1.Telemetry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapitelemetryv1alpha1.Telemetry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/telemetry/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateTelemetry,
		}.MustBuild(),
	}.MustBuild()

	// K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations describes
	// the collection
	// k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations
	K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations = collection.Builder{
		Name:         "k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations",
		VariableName: "K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations",
		Resource: resource.Builder{
			Group:         "admissionregistration.k8s.io",
			Kind:          "MutatingWebhookConfiguration",
			Plural:        "mutatingwebhookconfigurations",
			Version:       "v1",
			Proto:         "k8s.io.api.admissionregistration.v1.MutatingWebhookConfiguration",
			ReflectType:   reflect.TypeOf(&k8sioapiadmissionregistrationv1.MutatingWebhookConfiguration{}).Elem(),
			ProtoPackage:  "k8s.io/api/admissionregistration/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SApiextensionsK8SIoV1Customresourcedefinitions describes the
	// collection k8s/apiextensions.k8s.io/v1/customresourcedefinitions
	K8SApiextensionsK8SIoV1Customresourcedefinitions = collection.Builder{
		Name:         "k8s/apiextensions.k8s.io/v1/customresourcedefinitions",
		VariableName: "K8SApiextensionsK8SIoV1Customresourcedefinitions",
		Resource: resource.Builder{
			Group:         "apiextensions.k8s.io",
			Kind:          "CustomResourceDefinition",
			Plural:        "customresourcedefinitions",
			Version:       "v1",
			Proto:         "k8s.io.apiextensions_apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinition",
			ReflectType:   reflect.TypeOf(&k8sioapiextensionsapiserverpkgapisapiextensionsv1.CustomResourceDefinition{}).Elem(),
			ProtoPackage:  "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SAppsV1Deployments describes the collection k8s/apps/v1/deployments
	K8SAppsV1Deployments = collection.Builder{
		Name:         "k8s/apps/v1/deployments",
		VariableName: "K8SAppsV1Deployments",
		Resource: resource.Builder{
			Group:         "apps",
			Kind:          "Deployment",
			Plural:        "deployments",
			Version:       "v1",
			Proto:         "k8s.io.api.apps.v1.DeploymentSpec",
			ReflectType:   reflect.TypeOf(&k8sioapiappsv1.DeploymentSpec{}).Elem(),
			ProtoPackage:  "k8s.io/api/apps/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Configmaps describes the collection k8s/core/v1/configmaps
	K8SCoreV1Configmaps = collection.Builder{
		Name:         "k8s/core/v1/configmaps",
		VariableName: "K8SCoreV1Configmaps",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "ConfigMap",
			Plural:        "configmaps",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.ConfigMap",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.ConfigMap{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Endpoints describes the collection k8s/core/v1/endpoints
	K8SCoreV1Endpoints = collection.Builder{
		Name:         "k8s/core/v1/endpoints",
		VariableName: "K8SCoreV1Endpoints",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Endpoints",
			Plural:        "endpoints",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.Endpoints",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.Endpoints{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Namespaces describes the collection k8s/core/v1/namespaces
	K8SCoreV1Namespaces = collection.Builder{
		Name:         "k8s/core/v1/namespaces",
		VariableName: "K8SCoreV1Namespaces",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Namespace",
			Plural:        "namespaces",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.NamespaceSpec",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.NamespaceSpec{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: true,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Nodes describes the collection k8s/core/v1/nodes
	K8SCoreV1Nodes = collection.Builder{
		Name:         "k8s/core/v1/nodes",
		VariableName: "K8SCoreV1Nodes",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Node",
			Plural:        "nodes",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.NodeSpec",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.NodeSpec{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: true,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Pods describes the collection k8s/core/v1/pods
	K8SCoreV1Pods = collection.Builder{
		Name:         "k8s/core/v1/pods",
		VariableName: "K8SCoreV1Pods",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Pod",
			Plural:        "pods",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.PodSpec",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.PodSpec{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Secrets describes the collection k8s/core/v1/secrets
	K8SCoreV1Secrets = collection.Builder{
		Name:         "k8s/core/v1/secrets",
		VariableName: "K8SCoreV1Secrets",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Secret",
			Plural:        "secrets",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.Secret",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.Secret{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Services describes the collection k8s/core/v1/services
	K8SCoreV1Services = collection.Builder{
		Name:         "k8s/core/v1/services",
		VariableName: "K8SCoreV1Services",
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Service",
			Plural:        "services",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.ServiceSpec",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.ServiceSpec{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SExtensionsV1Beta1Ingresses describes the collection
	// k8s/extensions/v1beta1/ingresses
	K8SExtensionsV1Beta1Ingresses = collection.Builder{
		Name:         "k8s/extensions/v1beta1/ingresses",
		VariableName: "K8SExtensionsV1Beta1Ingresses",
		Resource: resource.Builder{
			Group:   "extensions",
			Kind:    "Ingress",
			Plural:  "ingresses",
			Version: "v1beta1",
			Proto:   "k8s.io.api.extensions.v1beta1.IngressSpec", StatusProto: "k8s.io.gateway_api.api.v1alpha1.IngressStatus",
			ReflectType: reflect.TypeOf(&k8sioapiextensionsv1beta1.IngressSpec{}).Elem(), StatusType: reflect.TypeOf(&k8sioapiextensionsv1beta1.IngressStatus{}).Elem(),
			ProtoPackage: "k8s.io/api/extensions/v1beta1", StatusPackage: "k8s.io/api/extensions/v1beta1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SGatewayApiV1Alpha2Gatewayclasses describes the collection
	// k8s/gateway_api/v1alpha2/gatewayclasses
	K8SGatewayApiV1Alpha2Gatewayclasses = collection.Builder{
		Name:         "k8s/gateway_api/v1alpha2/gatewayclasses",
		VariableName: "K8SGatewayApiV1Alpha2Gatewayclasses",
		Resource: resource.Builder{
			Group:   "gateway.networking.k8s.io",
			Kind:    "GatewayClass",
			Plural:  "gatewayclasses",
			Version: "v1alpha2",
			Proto:   "k8s.io.gateway_api.api.v1alpha1.GatewayClassSpec", StatusProto: "k8s.io.gateway_api.api.v1alpha1.GatewayClassStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.GatewayClassSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.GatewayClassStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2",
			ClusterScoped: true,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SGatewayApiV1Alpha2Gateways describes the collection
	// k8s/gateway_api/v1alpha2/gateways
	K8SGatewayApiV1Alpha2Gateways = collection.Builder{
		Name:         "k8s/gateway_api/v1alpha2/gateways",
		VariableName: "K8SGatewayApiV1Alpha2Gateways",
		Resource: resource.Builder{
			Group:   "gateway.networking.k8s.io",
			Kind:    "Gateway",
			Plural:  "gateways",
			Version: "v1alpha2",
			Proto:   "k8s.io.gateway_api.api.v1alpha1.GatewaySpec", StatusProto: "k8s.io.gateway_api.api.v1alpha1.GatewayStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.GatewaySpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.GatewayStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SGatewayApiV1Alpha2Httproutes describes the collection
	// k8s/gateway_api/v1alpha2/httproutes
	K8SGatewayApiV1Alpha2Httproutes = collection.Builder{
		Name:         "k8s/gateway_api/v1alpha2/httproutes",
		VariableName: "K8SGatewayApiV1Alpha2Httproutes",
		Resource: resource.Builder{
			Group:   "gateway.networking.k8s.io",
			Kind:    "HTTPRoute",
			Plural:  "httproutes",
			Version: "v1alpha2",
			Proto:   "k8s.io.gateway_api.api.v1alpha1.HTTPRouteSpec", StatusProto: "k8s.io.gateway_api.api.v1alpha1.HTTPRouteStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.HTTPRouteSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.HTTPRouteStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SGatewayApiV1Alpha2Referencegrants describes the collection
	// k8s/gateway_api/v1alpha2/referencegrants
	K8SGatewayApiV1Alpha2Referencegrants = collection.Builder{
		Name:         "k8s/gateway_api/v1alpha2/referencegrants",
		VariableName: "K8SGatewayApiV1Alpha2Referencegrants",
		Resource: resource.Builder{
			Group:         "gateway.networking.k8s.io",
			Kind:          "ReferenceGrant",
			Plural:        "referencegrants",
			Version:       "v1alpha2",
			Proto:         "k8s.io.gateway_api.api.v1alpha1.ReferenceGrantSpec",
			ReflectType:   reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.ReferenceGrantSpec{}).Elem(),
			ProtoPackage:  "sigs.k8s.io/gateway-api/apis/v1alpha2",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SGatewayApiV1Alpha2Referencepolicies describes the collection
	// k8s/gateway_api/v1alpha2/referencepolicies
	K8SGatewayApiV1Alpha2Referencepolicies = collection.Builder{
		Name:         "k8s/gateway_api/v1alpha2/referencepolicies",
		VariableName: "K8SGatewayApiV1Alpha2Referencepolicies",
		Resource: resource.Builder{
			Group:         "gateway.networking.k8s.io",
			Kind:          "ReferencePolicy",
			Plural:        "referencepolicies",
			Version:       "v1alpha2",
			Proto:         "k8s.io.gateway_api.api.v1alpha1.ReferenceGrantSpec",
			ReflectType:   reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.ReferenceGrantSpec{}).Elem(),
			ProtoPackage:  "sigs.k8s.io/gateway-api/apis/v1alpha2",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SGatewayApiV1Alpha2Tcproutes describes the collection
	// k8s/gateway_api/v1alpha2/tcproutes
	K8SGatewayApiV1Alpha2Tcproutes = collection.Builder{
		Name:         "k8s/gateway_api/v1alpha2/tcproutes",
		VariableName: "K8SGatewayApiV1Alpha2Tcproutes",
		Resource: resource.Builder{
			Group:   "gateway.networking.k8s.io",
			Kind:    "TCPRoute",
			Plural:  "tcproutes",
			Version: "v1alpha2",
			Proto:   "k8s.io.gateway_api.api.v1alpha1.TCPRouteSpec", StatusProto: "k8s.io.gateway_api.api.v1alpha1.TCPRouteStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.TCPRouteSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.TCPRouteStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SGatewayApiV1Alpha2Tlsroutes describes the collection
	// k8s/gateway_api/v1alpha2/tlsroutes
	K8SGatewayApiV1Alpha2Tlsroutes = collection.Builder{
		Name:         "k8s/gateway_api/v1alpha2/tlsroutes",
		VariableName: "K8SGatewayApiV1Alpha2Tlsroutes",
		Resource: resource.Builder{
			Group:   "gateway.networking.k8s.io",
			Kind:    "TLSRoute",
			Plural:  "tlsroutes",
			Version: "v1alpha2",
			Proto:   "k8s.io.gateway_api.api.v1alpha1.TLSRouteSpec", StatusProto: "k8s.io.gateway_api.api.v1alpha1.TLSRouteStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.TLSRouteSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.TLSRouteStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha2",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
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
		MustAdd(K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations).
		MustAdd(K8SApiextensionsK8SIoV1Customresourcedefinitions).
		MustAdd(K8SAppsV1Deployments).
		MustAdd(K8SCoreV1Configmaps).
		MustAdd(K8SCoreV1Endpoints).
		MustAdd(K8SCoreV1Namespaces).
		MustAdd(K8SCoreV1Nodes).
		MustAdd(K8SCoreV1Pods).
		MustAdd(K8SCoreV1Secrets).
		MustAdd(K8SCoreV1Services).
		MustAdd(K8SExtensionsV1Beta1Ingresses).
		MustAdd(K8SGatewayApiV1Alpha2Gatewayclasses).
		MustAdd(K8SGatewayApiV1Alpha2Gateways).
		MustAdd(K8SGatewayApiV1Alpha2Httproutes).
		MustAdd(K8SGatewayApiV1Alpha2Referencegrants).
		MustAdd(K8SGatewayApiV1Alpha2Referencepolicies).
		MustAdd(K8SGatewayApiV1Alpha2Tcproutes).
		MustAdd(K8SGatewayApiV1Alpha2Tlsroutes).
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
		MustAdd(K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations).
		MustAdd(K8SApiextensionsK8SIoV1Customresourcedefinitions).
		MustAdd(K8SAppsV1Deployments).
		MustAdd(K8SCoreV1Configmaps).
		MustAdd(K8SCoreV1Endpoints).
		MustAdd(K8SCoreV1Namespaces).
		MustAdd(K8SCoreV1Nodes).
		MustAdd(K8SCoreV1Pods).
		MustAdd(K8SCoreV1Secrets).
		MustAdd(K8SCoreV1Services).
		MustAdd(K8SExtensionsV1Beta1Ingresses).
		MustAdd(K8SGatewayApiV1Alpha2Gatewayclasses).
		MustAdd(K8SGatewayApiV1Alpha2Gateways).
		MustAdd(K8SGatewayApiV1Alpha2Httproutes).
		MustAdd(K8SGatewayApiV1Alpha2Referencegrants).
		MustAdd(K8SGatewayApiV1Alpha2Referencepolicies).
		MustAdd(K8SGatewayApiV1Alpha2Tcproutes).
		MustAdd(K8SGatewayApiV1Alpha2Tlsroutes).
		Build()

	// Builtin contains only native Kubernetes collections. This differs from Kube, which has
	// Kubernetes controlled CRDs
	Builtin = collection.NewSchemasBuilder().
		MustAdd(K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations).
		MustAdd(K8SApiextensionsK8SIoV1Customresourcedefinitions).
		MustAdd(K8SAppsV1Deployments).
		MustAdd(K8SCoreV1Configmaps).
		MustAdd(K8SCoreV1Endpoints).
		MustAdd(K8SCoreV1Namespaces).
		MustAdd(K8SCoreV1Nodes).
		MustAdd(K8SCoreV1Pods).
		MustAdd(K8SCoreV1Secrets).
		MustAdd(K8SCoreV1Services).
		MustAdd(K8SExtensionsV1Beta1Ingresses).
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
			MustAdd(K8SGatewayApiV1Alpha2Gatewayclasses).
			MustAdd(K8SGatewayApiV1Alpha2Gateways).
			MustAdd(K8SGatewayApiV1Alpha2Httproutes).
			MustAdd(K8SGatewayApiV1Alpha2Referencegrants).
			MustAdd(K8SGatewayApiV1Alpha2Referencepolicies).
			MustAdd(K8SGatewayApiV1Alpha2Tcproutes).
			MustAdd(K8SGatewayApiV1Alpha2Tlsroutes).
			Build()

	// Deprecated contains only collections used by that will soon be used by nothing.
	Deprecated = collection.NewSchemasBuilder().
			Build()
)
