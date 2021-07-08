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
	sigsk8siogatewayapiapisv1alpha1 "sigs.k8s.io/gateway-api/apis/v1alpha1"

	istioioapiextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioioapimeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioioapimetav1alpha1 "istio.io/api/meta/v1alpha1"
	istioioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "extensions.istio.io",
			Kind:    "WasmPlugin",
			Plural:  "wasmplugins",
			Version: "v1alpha1",
			Proto:   "istio.extensions.v1alpha1.WasmPlugin", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapiextensionsv1alpha1.WasmPlugin{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/extensions/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// IstioMeshV1Alpha1MeshConfig describes the collection
	// istio/mesh/v1alpha1/MeshConfig
	IstioMeshV1Alpha1MeshConfig = collection.Builder{
		Name:         "istio/mesh/v1alpha1/MeshConfig",
		VariableName: "IstioMeshV1Alpha1MeshConfig",
		Disabled:     false,
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
		Disabled:     false,
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "DestinationRule",
			Plural:  "destinationrules",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.DestinationRule", StatusProto: "istio.meta.v1alpha1.IstioStatus",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "EnvoyFilter",
			Plural:  "envoyfilters",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.EnvoyFilter", StatusProto: "istio.meta.v1alpha1.IstioStatus",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "Gateway",
			Plural:  "gateways",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.Gateway", StatusProto: "istio.meta.v1alpha1.IstioStatus",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "ServiceEntry",
			Plural:  "serviceentries",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.ServiceEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "Sidecar",
			Plural:  "sidecars",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.Sidecar", StatusProto: "istio.meta.v1alpha1.IstioStatus",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "VirtualService",
			Plural:  "virtualservices",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.VirtualService", StatusProto: "istio.meta.v1alpha1.IstioStatus",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "WorkloadEntry",
			Plural:  "workloadentries",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.WorkloadEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "WorkloadGroup",
			Plural:  "workloadgroups",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.WorkloadGroup", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadGroup{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateWorkloadGroup,
		}.MustBuild(),
	}.MustBuild()

	// IstioSecurityV1Beta1Authorizationpolicies describes the collection
	// istio/security/v1beta1/authorizationpolicies
	IstioSecurityV1Beta1Authorizationpolicies = collection.Builder{
		Name:         "istio/security/v1beta1/authorizationpolicies",
		VariableName: "IstioSecurityV1Beta1Authorizationpolicies",
		Disabled:     false,
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
		Disabled:     false,
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
		Disabled:     false,
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "telemetry.istio.io",
			Kind:    "Telemetry",
			Plural:  "telemetries",
			Version: "v1alpha1",
			Proto:   "istio.telemetry.v1alpha1.Telemetry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapitelemetryv1alpha1.Telemetry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/telemetry/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations describes
	// the collection
	// k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations
	K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations = collection.Builder{
		Name:         "k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations",
		VariableName: "K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations",
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "admissionregistration.k8s.io",
			Kind:          "MutatingWebhookConfiguration",
			Plural:        "MutatingWebhookConfigurations",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "apiextensions.k8s.io",
			Kind:          "CustomResourceDefinition",
			Plural:        "CustomResourceDefinitions",
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "apps",
			Kind:          "Deployment",
			Plural:        "Deployments",
			Version:       "v1",
			Proto:         "k8s.io.api.apps.v1.Deployment",
			ReflectType:   reflect.TypeOf(&k8sioapiappsv1.Deployment{}).Elem(),
			ProtoPackage:  "k8s.io/api/apps/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Configmaps describes the collection k8s/core/v1/configmaps
	K8SCoreV1Configmaps = collection.Builder{
		Name:         "k8s/core/v1/configmaps",
		VariableName: "K8SCoreV1Configmaps",
		Disabled:     false,
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
		Disabled:     false,
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
		Disabled:     false,
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
		Disabled:     false,
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
		Disabled:     false,
		Resource: resource.Builder{
			Group:         "",
			Kind:          "Pod",
			Plural:        "pods",
			Version:       "v1",
			Proto:         "k8s.io.api.core.v1.Pod",
			ReflectType:   reflect.TypeOf(&k8sioapicorev1.Pod{}).Elem(),
			ProtoPackage:  "k8s.io/api/core/v1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SCoreV1Secrets describes the collection k8s/core/v1/secrets
	K8SCoreV1Secrets = collection.Builder{
		Name:         "k8s/core/v1/secrets",
		VariableName: "K8SCoreV1Secrets",
		Disabled:     false,
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
		Disabled:     false,
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

	// K8SExtensionsIstioIoV1Alpha1Wasmplugins describes the collection
	// k8s/extensions.istio.io/v1alpha1/wasmplugins
	K8SExtensionsIstioIoV1Alpha1Wasmplugins = collection.Builder{
		Name:         "k8s/extensions.istio.io/v1alpha1/wasmplugins",
		VariableName: "K8SExtensionsIstioIoV1Alpha1Wasmplugins",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "extensions.istio.io",
			Kind:    "WasmPlugin",
			Plural:  "wasmplugins",
			Version: "v1alpha1",
			Proto:   "istio.extensions.v1alpha1.WasmPlugin", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapiextensionsv1alpha1.WasmPlugin{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/extensions/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SExtensionsV1Beta1Ingresses describes the collection
	// k8s/extensions/v1beta1/ingresses
	K8SExtensionsV1Beta1Ingresses = collection.Builder{
		Name:         "k8s/extensions/v1beta1/ingresses",
		VariableName: "K8SExtensionsV1Beta1Ingresses",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "extensions",
			Kind:    "Ingress",
			Plural:  "ingresses",
			Version: "v1beta1",
			Proto:   "k8s.io.api.extensions.v1beta1.IngressSpec", StatusProto: "k8s.io.service_apis.api.v1alpha1.IngressStatus",
			ReflectType: reflect.TypeOf(&k8sioapiextensionsv1beta1.IngressSpec{}).Elem(), StatusType: reflect.TypeOf(&k8sioapiextensionsv1beta1.IngressStatus{}).Elem(),
			ProtoPackage: "k8s.io/api/extensions/v1beta1", StatusPackage: "k8s.io/api/extensions/v1beta1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Destinationrules describes the collection
	// k8s/networking.istio.io/v1alpha3/destinationrules
	K8SNetworkingIstioIoV1Alpha3Destinationrules = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/destinationrules",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Destinationrules",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "DestinationRule",
			Plural:  "destinationrules",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.DestinationRule", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.DestinationRule{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateDestinationRule,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Envoyfilters describes the collection
	// k8s/networking.istio.io/v1alpha3/envoyfilters
	K8SNetworkingIstioIoV1Alpha3Envoyfilters = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/envoyfilters",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Envoyfilters",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "EnvoyFilter",
			Plural:  "envoyfilters",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.EnvoyFilter", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.EnvoyFilter{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateEnvoyFilter,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Gateways describes the collection
	// k8s/networking.istio.io/v1alpha3/gateways
	K8SNetworkingIstioIoV1Alpha3Gateways = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/gateways",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Gateways",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "Gateway",
			Plural:  "gateways",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.Gateway", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.Gateway{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateGateway,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Serviceentries describes the collection
	// k8s/networking.istio.io/v1alpha3/serviceentries
	K8SNetworkingIstioIoV1Alpha3Serviceentries = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/serviceentries",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Serviceentries",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "ServiceEntry",
			Plural:  "serviceentries",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.ServiceEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.ServiceEntry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateServiceEntry,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Sidecars describes the collection
	// k8s/networking.istio.io/v1alpha3/sidecars
	K8SNetworkingIstioIoV1Alpha3Sidecars = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/sidecars",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Sidecars",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "Sidecar",
			Plural:  "sidecars",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.Sidecar", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.Sidecar{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateSidecar,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Virtualservices describes the collection
	// k8s/networking.istio.io/v1alpha3/virtualservices
	K8SNetworkingIstioIoV1Alpha3Virtualservices = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/virtualservices",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Virtualservices",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "VirtualService",
			Plural:  "virtualservices",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.VirtualService", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.VirtualService{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateVirtualService,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Workloadentries describes the collection
	// k8s/networking.istio.io/v1alpha3/workloadentries
	K8SNetworkingIstioIoV1Alpha3Workloadentries = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/workloadentries",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Workloadentries",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "WorkloadEntry",
			Plural:  "workloadentries",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.WorkloadEntry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadEntry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateWorkloadEntry,
		}.MustBuild(),
	}.MustBuild()

	// K8SNetworkingIstioIoV1Alpha3Workloadgroups describes the collection
	// k8s/networking.istio.io/v1alpha3/workloadgroups
	K8SNetworkingIstioIoV1Alpha3Workloadgroups = collection.Builder{
		Name:         "k8s/networking.istio.io/v1alpha3/workloadgroups",
		VariableName: "K8SNetworkingIstioIoV1Alpha3Workloadgroups",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.istio.io",
			Kind:    "WorkloadGroup",
			Plural:  "workloadgroups",
			Version: "v1alpha3",
			Proto:   "istio.networking.v1alpha3.WorkloadGroup", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadGroup{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/networking/v1alpha3", StatusPackage: "istio.io/api/meta/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.ValidateWorkloadGroup,
		}.MustBuild(),
	}.MustBuild()

	// K8SSecurityIstioIoV1Beta1Authorizationpolicies describes the collection
	// k8s/security.istio.io/v1beta1/authorizationpolicies
	K8SSecurityIstioIoV1Beta1Authorizationpolicies = collection.Builder{
		Name:         "k8s/security.istio.io/v1beta1/authorizationpolicies",
		VariableName: "K8SSecurityIstioIoV1Beta1Authorizationpolicies",
		Disabled:     false,
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

	// K8SSecurityIstioIoV1Beta1Peerauthentications describes the collection
	// k8s/security.istio.io/v1beta1/peerauthentications
	K8SSecurityIstioIoV1Beta1Peerauthentications = collection.Builder{
		Name:         "k8s/security.istio.io/v1beta1/peerauthentications",
		VariableName: "K8SSecurityIstioIoV1Beta1Peerauthentications",
		Disabled:     false,
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

	// K8SSecurityIstioIoV1Beta1Requestauthentications describes the
	// collection k8s/security.istio.io/v1beta1/requestauthentications
	K8SSecurityIstioIoV1Beta1Requestauthentications = collection.Builder{
		Name:         "k8s/security.istio.io/v1beta1/requestauthentications",
		VariableName: "K8SSecurityIstioIoV1Beta1Requestauthentications",
		Disabled:     false,
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

	// K8SServiceApisV1Alpha1Backendpolicies describes the collection
	// k8s/service_apis/v1alpha1/backendpolicies
	K8SServiceApisV1Alpha1Backendpolicies = collection.Builder{
		Name:         "k8s/service_apis/v1alpha1/backendpolicies",
		VariableName: "K8SServiceApisV1Alpha1Backendpolicies",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.x-k8s.io",
			Kind:    "BackendPolicy",
			Plural:  "backendpolicies",
			Version: "v1alpha1",
			Proto:   "k8s.io.service_apis.api.v1alpha1.BackendPolicySpec", StatusProto: "k8s.io.service_apis.api.v1alpha1.BackendPolicyStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.BackendPolicySpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.BackendPolicyStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SServiceApisV1Alpha1Gatewayclasses describes the collection
	// k8s/service_apis/v1alpha1/gatewayclasses
	K8SServiceApisV1Alpha1Gatewayclasses = collection.Builder{
		Name:         "k8s/service_apis/v1alpha1/gatewayclasses",
		VariableName: "K8SServiceApisV1Alpha1Gatewayclasses",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.x-k8s.io",
			Kind:    "GatewayClass",
			Plural:  "gatewayclasses",
			Version: "v1alpha1",
			Proto:   "k8s.io.service_apis.api.v1alpha1.GatewayClassSpec", StatusProto: "k8s.io.service_apis.api.v1alpha1.GatewayClassStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.GatewayClassSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.GatewayClassStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1",
			ClusterScoped: true,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SServiceApisV1Alpha1Gateways describes the collection
	// k8s/service_apis/v1alpha1/gateways
	K8SServiceApisV1Alpha1Gateways = collection.Builder{
		Name:         "k8s/service_apis/v1alpha1/gateways",
		VariableName: "K8SServiceApisV1Alpha1Gateways",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.x-k8s.io",
			Kind:    "Gateway",
			Plural:  "gateways",
			Version: "v1alpha1",
			Proto:   "k8s.io.service_apis.api.v1alpha1.GatewaySpec", StatusProto: "k8s.io.service_apis.api.v1alpha1.GatewayStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.GatewaySpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.GatewayStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SServiceApisV1Alpha1Httproutes describes the collection
	// k8s/service_apis/v1alpha1/httproutes
	K8SServiceApisV1Alpha1Httproutes = collection.Builder{
		Name:         "k8s/service_apis/v1alpha1/httproutes",
		VariableName: "K8SServiceApisV1Alpha1Httproutes",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.x-k8s.io",
			Kind:    "HTTPRoute",
			Plural:  "httproutes",
			Version: "v1alpha1",
			Proto:   "k8s.io.service_apis.api.v1alpha1.HTTPRouteSpec", StatusProto: "k8s.io.service_apis.api.v1alpha1.HTTPRouteStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.HTTPRouteSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.HTTPRouteStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SServiceApisV1Alpha1Tcproutes describes the collection
	// k8s/service_apis/v1alpha1/tcproutes
	K8SServiceApisV1Alpha1Tcproutes = collection.Builder{
		Name:         "k8s/service_apis/v1alpha1/tcproutes",
		VariableName: "K8SServiceApisV1Alpha1Tcproutes",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.x-k8s.io",
			Kind:    "TCPRoute",
			Plural:  "tcproutes",
			Version: "v1alpha1",
			Proto:   "k8s.io.service_apis.api.v1alpha1.TCPRouteSpec", StatusProto: "k8s.io.service_apis.api.v1alpha1.TCPRouteStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.TCPRouteSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.TCPRouteStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8SServiceApisV1Alpha1Tlsroutes describes the collection
	// k8s/service_apis/v1alpha1/tlsroutes
	K8SServiceApisV1Alpha1Tlsroutes = collection.Builder{
		Name:         "k8s/service_apis/v1alpha1/tlsroutes",
		VariableName: "K8SServiceApisV1Alpha1Tlsroutes",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "networking.x-k8s.io",
			Kind:    "TLSRoute",
			Plural:  "tlsroutes",
			Version: "v1alpha1",
			Proto:   "k8s.io.service_apis.api.v1alpha1.TLSRouteSpec", StatusProto: "k8s.io.service_apis.api.v1alpha1.TLSRouteStatus",
			ReflectType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.TLSRouteSpec{}).Elem(), StatusType: reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha1.TLSRouteStatus{}).Elem(),
			ProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1", StatusPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1",
			ClusterScoped: false,
			ValidateProto: validation.EmptyValidate,
		}.MustBuild(),
	}.MustBuild()

	// K8STelemetryIstioIoV1Alpha1Telemetries describes the collection
	// k8s/telemetry.istio.io/v1alpha1/telemetries
	K8STelemetryIstioIoV1Alpha1Telemetries = collection.Builder{
		Name:         "k8s/telemetry.istio.io/v1alpha1/telemetries",
		VariableName: "K8STelemetryIstioIoV1Alpha1Telemetries",
		Disabled:     false,
		Resource: resource.Builder{
			Group:   "telemetry.istio.io",
			Kind:    "Telemetry",
			Plural:  "telemetries",
			Version: "v1alpha1",
			Proto:   "istio.telemetry.v1alpha1.Telemetry", StatusProto: "istio.meta.v1alpha1.IstioStatus",
			ReflectType: reflect.TypeOf(&istioioapitelemetryv1alpha1.Telemetry{}).Elem(), StatusType: reflect.TypeOf(&istioioapimetav1alpha1.IstioStatus{}).Elem(),
			ProtoPackage: "istio.io/api/telemetry/v1alpha1", StatusPackage: "istio.io/api/meta/v1alpha1",
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
		MustAdd(K8SExtensionsIstioIoV1Alpha1Wasmplugins).
		MustAdd(K8SExtensionsV1Beta1Ingresses).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Destinationrules).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Envoyfilters).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Gateways).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Serviceentries).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Sidecars).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Virtualservices).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Workloadentries).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Workloadgroups).
		MustAdd(K8SSecurityIstioIoV1Beta1Authorizationpolicies).
		MustAdd(K8SSecurityIstioIoV1Beta1Peerauthentications).
		MustAdd(K8SSecurityIstioIoV1Beta1Requestauthentications).
		MustAdd(K8SServiceApisV1Alpha1Backendpolicies).
		MustAdd(K8SServiceApisV1Alpha1Gatewayclasses).
		MustAdd(K8SServiceApisV1Alpha1Gateways).
		MustAdd(K8SServiceApisV1Alpha1Httproutes).
		MustAdd(K8SServiceApisV1Alpha1Tcproutes).
		MustAdd(K8SServiceApisV1Alpha1Tlsroutes).
		MustAdd(K8STelemetryIstioIoV1Alpha1Telemetries).
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
		MustAdd(K8SExtensionsIstioIoV1Alpha1Wasmplugins).
		MustAdd(K8SExtensionsV1Beta1Ingresses).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Destinationrules).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Envoyfilters).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Gateways).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Serviceentries).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Sidecars).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Virtualservices).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Workloadentries).
		MustAdd(K8SNetworkingIstioIoV1Alpha3Workloadgroups).
		MustAdd(K8SSecurityIstioIoV1Beta1Authorizationpolicies).
		MustAdd(K8SSecurityIstioIoV1Beta1Peerauthentications).
		MustAdd(K8SSecurityIstioIoV1Beta1Requestauthentications).
		MustAdd(K8SServiceApisV1Alpha1Backendpolicies).
		MustAdd(K8SServiceApisV1Alpha1Gatewayclasses).
		MustAdd(K8SServiceApisV1Alpha1Gateways).
		MustAdd(K8SServiceApisV1Alpha1Httproutes).
		MustAdd(K8SServiceApisV1Alpha1Tcproutes).
		MustAdd(K8SServiceApisV1Alpha1Tlsroutes).
		MustAdd(K8STelemetryIstioIoV1Alpha1Telemetries).
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
		MustAdd(IstioSecurityV1Beta1Authorizationpolicies).
		MustAdd(IstioSecurityV1Beta1Peerauthentications).
		MustAdd(IstioSecurityV1Beta1Requestauthentications).
		MustAdd(IstioTelemetryV1Alpha1Telemetries).
		Build()

	// PilotServiceApi contains only collections used by Pilot, including experimental Service Api.
	PilotServiceApi = collection.NewSchemasBuilder().
			MustAdd(IstioExtensionsV1Alpha1Wasmplugins).
			MustAdd(IstioNetworkingV1Alpha3Destinationrules).
			MustAdd(IstioNetworkingV1Alpha3Envoyfilters).
			MustAdd(IstioNetworkingV1Alpha3Gateways).
			MustAdd(IstioNetworkingV1Alpha3Serviceentries).
			MustAdd(IstioNetworkingV1Alpha3Sidecars).
			MustAdd(IstioNetworkingV1Alpha3Virtualservices).
			MustAdd(IstioNetworkingV1Alpha3Workloadentries).
			MustAdd(IstioNetworkingV1Alpha3Workloadgroups).
			MustAdd(IstioSecurityV1Beta1Authorizationpolicies).
			MustAdd(IstioSecurityV1Beta1Peerauthentications).
			MustAdd(IstioSecurityV1Beta1Requestauthentications).
			MustAdd(IstioTelemetryV1Alpha1Telemetries).
			MustAdd(K8SServiceApisV1Alpha1Backendpolicies).
			MustAdd(K8SServiceApisV1Alpha1Gatewayclasses).
			MustAdd(K8SServiceApisV1Alpha1Gateways).
			MustAdd(K8SServiceApisV1Alpha1Httproutes).
			MustAdd(K8SServiceApisV1Alpha1Tcproutes).
			MustAdd(K8SServiceApisV1Alpha1Tlsroutes).
			Build()

	// Deprecated contains only collections used by that will soon be used by nothing.
	Deprecated = collection.NewSchemasBuilder().
			Build()
)
