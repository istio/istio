// GENERATED FILE -- DO NOT EDIT
//

package kubetypes

import (
	"fmt"
	"reflect"

	k8sioapiadmissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8sioapiappsv1 "k8s.io/api/apps/v1"
	k8sioapicorev1 "k8s.io/api/core/v1"
	k8sioapidiscoveryv1 "k8s.io/api/discovery/v1"
	k8sioapinetworkingv1 "k8s.io/api/networking/v1"
	k8sioapiextensionsapiserverpkgapisapiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	sigsk8siogatewayapiapisv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	sigsk8siogatewayapiapisv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	istioioapiextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioioapimeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioioapinetworkingv1beta1 "istio.io/api/networking/v1beta1"
	istioioapisecurityv1beta1 "istio.io/api/security/v1beta1"
	istioioapitelemetryv1alpha1 "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

func GetGVK[T runtime.Object]() config.GroupVersionKind {
	i := *new(T)
	t := reflect.TypeOf(i)
	switch t {
	case reflect.TypeOf(&istioioapisecurityv1beta1.AuthorizationPolicy{}):
		return gvk.AuthorizationPolicy
	case reflect.TypeOf(&k8sioapicorev1.ConfigMap{}):
		return gvk.ConfigMap
	case reflect.TypeOf(&k8sioapiextensionsapiserverpkgapisapiextensionsv1.CustomResourceDefinition{}):
		return gvk.CustomResourceDefinition
	case reflect.TypeOf(&k8sioapiappsv1.Deployment{}):
		return gvk.Deployment
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.DestinationRule{}):
		return gvk.DestinationRule
	case reflect.TypeOf(&k8sioapidiscoveryv1.EndpointSlice{}):
		return gvk.EndpointSlice
	case reflect.TypeOf(&k8sioapicorev1.Endpoints{}):
		return gvk.Endpoints
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.EnvoyFilter{}):
		return gvk.EnvoyFilter
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.GRPCRoute{}):
		return gvk.GRPCRoute
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.Gateway{}):
		return gvk.Gateway
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1beta1.GatewayClass{}):
		return gvk.GatewayClass
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1beta1.HTTPRoute{}):
		return gvk.HTTPRoute
	case reflect.TypeOf(&k8sioapinetworkingv1.Ingress{}):
		return gvk.Ingress
	case reflect.TypeOf(&k8sioapinetworkingv1.IngressClass{}):
		return gvk.IngressClass
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1beta1.Gateway{}):
		return gvk.KubernetesGateway
	case reflect.TypeOf(&istioioapimeshv1alpha1.MeshConfig{}):
		return gvk.MeshConfig
	case reflect.TypeOf(&istioioapimeshv1alpha1.MeshNetworks{}):
		return gvk.MeshNetworks
	case reflect.TypeOf(&k8sioapiadmissionregistrationv1.MutatingWebhookConfiguration{}):
		return gvk.MutatingWebhookConfiguration
	case reflect.TypeOf(&k8sioapicorev1.Namespace{}):
		return gvk.Namespace
	case reflect.TypeOf(&k8sioapicorev1.Node{}):
		return gvk.Node
	case reflect.TypeOf(&istioioapisecurityv1beta1.PeerAuthentication{}):
		return gvk.PeerAuthentication
	case reflect.TypeOf(&k8sioapicorev1.Pod{}):
		return gvk.Pod
	case reflect.TypeOf(&istioioapinetworkingv1beta1.ProxyConfig{}):
		return gvk.ProxyConfig
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.ReferenceGrant{}):
		return gvk.ReferenceGrant
	case reflect.TypeOf(&istioioapisecurityv1beta1.RequestAuthentication{}):
		return gvk.RequestAuthentication
	case reflect.TypeOf(&k8sioapicorev1.Secret{}):
		return gvk.Secret
	case reflect.TypeOf(&k8sioapicorev1.Service{}):
		return gvk.Service
	case reflect.TypeOf(&k8sioapicorev1.ServiceAccount{}):
		return gvk.ServiceAccount
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.ServiceEntry{}):
		return gvk.ServiceEntry
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.Sidecar{}):
		return gvk.Sidecar
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.TCPRoute{}):
		return gvk.TCPRoute
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.TLSRoute{}):
		return gvk.TLSRoute
	case reflect.TypeOf(&istioioapitelemetryv1alpha1.Telemetry{}):
		return gvk.Telemetry
	case reflect.TypeOf(&sigsk8siogatewayapiapisv1alpha2.UDPRoute{}):
		return gvk.UDPRoute
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.VirtualService{}):
		return gvk.VirtualService
	case reflect.TypeOf(&istioioapiextensionsv1alpha1.WasmPlugin{}):
		return gvk.WasmPlugin
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadEntry{}):
		return gvk.WorkloadEntry
	case reflect.TypeOf(&istioioapinetworkingv1alpha3.WorkloadGroup{}):
		return gvk.WorkloadGroup
	default:
		panic(fmt.Sprintf("Unknown type %T", i))
	}
}
