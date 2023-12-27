// GENERATED FILE -- DO NOT EDIT
//

package kubeclient

import (
	"context"
	"fmt"
	"time"

	k8sioapiadmissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8sioapiappsv1 "k8s.io/api/apps/v1"
	k8sioapicertificatesv1 "k8s.io/api/certificates/v1"
	k8sioapicorev1 "k8s.io/api/core/v1"
	k8sioapidiscoveryv1 "k8s.io/api/discovery/v1"
	k8sioapinetworkingv1 "k8s.io/api/networking/v1"
	k8sioapiextensionsapiserverpkgapisapiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kubeext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kubeextinformer "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/tools/cache"
	sigsk8siogatewayapiapisv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	sigsk8siogatewayapiapisv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayapiclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	gatewayapiinformer "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"

	apiistioioapiextensionsv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apiistioioapinetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apiistioioapinetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	apiistioioapisecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	apiistioioapitelemetryv1alpha1 "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformer "istio.io/client-go/pkg/informers/externalversions"
	"istio.io/istio/pilot/pkg/features"
	ktypes "istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/ptr"
)

type ClientGetter interface {
	// Ext returns the API extensions client.
	Ext() kubeext.Interface

	// Kube returns the core kube client
	Kube() kubernetes.Interface

	// Dynamic client.
	Dynamic() dynamic.Interface

	// Metadata returns the Metadata kube client.
	Metadata() metadata.Interface

	// Istio returns the Istio kube client.
	Istio() istioclient.Interface

	// GatewayAPI returns the gateway-api kube client.
	GatewayAPI() gatewayapiclient.Interface

	// KubeInformer returns an informer for core kube client
	KubeInformer() informers.SharedInformerFactory

	// IstioInformer returns an informer for the istio client
	IstioInformer() istioinformer.SharedInformerFactory

	// GatewayAPIInformer returns an informer for the gateway-api client
	GatewayAPIInformer() gatewayapiinformer.SharedInformerFactory

	// ExtInformer returns an informer for the extension client
	ExtInformer() kubeextinformer.SharedInformerFactory
}

func GetWriteClient[T runtime.Object](c ClientGetter, namespace string) ktypes.WriteAPI[T] {
	switch any(ptr.Empty[T]()).(type) {
	case *apiistioioapisecurityv1beta1.AuthorizationPolicy:
		return c.Istio().SecurityV1beta1().AuthorizationPolicies(namespace).(ktypes.WriteAPI[T])
	case *k8sioapicertificatesv1.CertificateSigningRequest:
		return c.Kube().CertificatesV1().CertificateSigningRequests().(ktypes.WriteAPI[T])
	case *k8sioapicorev1.ConfigMap:
		return c.Kube().CoreV1().ConfigMaps(namespace).(ktypes.WriteAPI[T])
	case *k8sioapiextensionsapiserverpkgapisapiextensionsv1.CustomResourceDefinition:
		return c.Ext().ApiextensionsV1().CustomResourceDefinitions().(ktypes.WriteAPI[T])
	case *k8sioapiappsv1.Deployment:
		return c.Kube().AppsV1().Deployments(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.DestinationRule:
		return c.Istio().NetworkingV1alpha3().DestinationRules(namespace).(ktypes.WriteAPI[T])
	case *k8sioapidiscoveryv1.EndpointSlice:
		return c.Kube().DiscoveryV1().EndpointSlices(namespace).(ktypes.WriteAPI[T])
	case *k8sioapicorev1.Endpoints:
		return c.Kube().CoreV1().Endpoints(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.EnvoyFilter:
		return c.Istio().NetworkingV1alpha3().EnvoyFilters(namespace).(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1alpha2.GRPCRoute:
		return c.GatewayAPI().GatewayV1alpha2().GRPCRoutes(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.Gateway:
		return c.Istio().NetworkingV1alpha3().Gateways(namespace).(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1beta1.GatewayClass:
		return c.GatewayAPI().GatewayV1beta1().GatewayClasses().(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1beta1.HTTPRoute:
		return c.GatewayAPI().GatewayV1beta1().HTTPRoutes(namespace).(ktypes.WriteAPI[T])
	case *k8sioapinetworkingv1.Ingress:
		return c.Kube().NetworkingV1().Ingresses(namespace).(ktypes.WriteAPI[T])
	case *k8sioapinetworkingv1.IngressClass:
		return c.Kube().NetworkingV1().IngressClasses().(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1beta1.Gateway:
		return c.GatewayAPI().GatewayV1beta1().Gateways(namespace).(ktypes.WriteAPI[T])
	case *k8sioapiadmissionregistrationv1.MutatingWebhookConfiguration:
		return c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().(ktypes.WriteAPI[T])
	case *k8sioapicorev1.Namespace:
		return c.Kube().CoreV1().Namespaces().(ktypes.WriteAPI[T])
	case *k8sioapicorev1.Node:
		return c.Kube().CoreV1().Nodes().(ktypes.WriteAPI[T])
	case *apiistioioapisecurityv1beta1.PeerAuthentication:
		return c.Istio().SecurityV1beta1().PeerAuthentications(namespace).(ktypes.WriteAPI[T])
	case *k8sioapicorev1.Pod:
		return c.Kube().CoreV1().Pods(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1beta1.ProxyConfig:
		return c.Istio().NetworkingV1beta1().ProxyConfigs(namespace).(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1beta1.ReferenceGrant:
		return c.GatewayAPI().GatewayV1beta1().ReferenceGrants(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapisecurityv1beta1.RequestAuthentication:
		return c.Istio().SecurityV1beta1().RequestAuthentications(namespace).(ktypes.WriteAPI[T])
	case *k8sioapicorev1.Secret:
		return c.Kube().CoreV1().Secrets(namespace).(ktypes.WriteAPI[T])
	case *k8sioapicorev1.Service:
		return c.Kube().CoreV1().Services(namespace).(ktypes.WriteAPI[T])
	case *k8sioapicorev1.ServiceAccount:
		return c.Kube().CoreV1().ServiceAccounts(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.ServiceEntry:
		return c.Istio().NetworkingV1alpha3().ServiceEntries(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.Sidecar:
		return c.Istio().NetworkingV1alpha3().Sidecars(namespace).(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1alpha2.TCPRoute:
		return c.GatewayAPI().GatewayV1alpha2().TCPRoutes(namespace).(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1alpha2.TLSRoute:
		return c.GatewayAPI().GatewayV1alpha2().TLSRoutes(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapitelemetryv1alpha1.Telemetry:
		return c.Istio().TelemetryV1alpha1().Telemetries(namespace).(ktypes.WriteAPI[T])
	case *sigsk8siogatewayapiapisv1alpha2.UDPRoute:
		return c.GatewayAPI().GatewayV1alpha2().UDPRoutes(namespace).(ktypes.WriteAPI[T])
	case *k8sioapiadmissionregistrationv1.ValidatingWebhookConfiguration:
		return c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.VirtualService:
		return c.Istio().NetworkingV1alpha3().VirtualServices(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapiextensionsv1alpha1.WasmPlugin:
		return c.Istio().ExtensionsV1alpha1().WasmPlugins(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.WorkloadEntry:
		return c.Istio().NetworkingV1alpha3().WorkloadEntries(namespace).(ktypes.WriteAPI[T])
	case *apiistioioapinetworkingv1alpha3.WorkloadGroup:
		return c.Istio().NetworkingV1alpha3().WorkloadGroups(namespace).(ktypes.WriteAPI[T])
	default:
		panic(fmt.Sprintf("Unknown type %T", ptr.Empty[T]()))
	}
}

func GetClient[T, TL runtime.Object](c ClientGetter, namespace string) ktypes.ReadWriteAPI[T, TL] {
	switch any(ptr.Empty[T]()).(type) {
	case *apiistioioapisecurityv1beta1.AuthorizationPolicy:
		return c.Istio().SecurityV1beta1().AuthorizationPolicies(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicertificatesv1.CertificateSigningRequest:
		return c.Kube().CertificatesV1().CertificateSigningRequests().(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.ConfigMap:
		return c.Kube().CoreV1().ConfigMaps(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapiextensionsapiserverpkgapisapiextensionsv1.CustomResourceDefinition:
		return c.Ext().ApiextensionsV1().CustomResourceDefinitions().(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapiappsv1.Deployment:
		return c.Kube().AppsV1().Deployments(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.DestinationRule:
		return c.Istio().NetworkingV1alpha3().DestinationRules(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapidiscoveryv1.EndpointSlice:
		return c.Kube().DiscoveryV1().EndpointSlices(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.Endpoints:
		return c.Kube().CoreV1().Endpoints(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.EnvoyFilter:
		return c.Istio().NetworkingV1alpha3().EnvoyFilters(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1alpha2.GRPCRoute:
		return c.GatewayAPI().GatewayV1alpha2().GRPCRoutes(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.Gateway:
		return c.Istio().NetworkingV1alpha3().Gateways(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1beta1.GatewayClass:
		return c.GatewayAPI().GatewayV1beta1().GatewayClasses().(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1beta1.HTTPRoute:
		return c.GatewayAPI().GatewayV1beta1().HTTPRoutes(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapinetworkingv1.Ingress:
		return c.Kube().NetworkingV1().Ingresses(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapinetworkingv1.IngressClass:
		return c.Kube().NetworkingV1().IngressClasses().(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1beta1.Gateway:
		return c.GatewayAPI().GatewayV1beta1().Gateways(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapiadmissionregistrationv1.MutatingWebhookConfiguration:
		return c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.Namespace:
		return c.Kube().CoreV1().Namespaces().(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.Node:
		return c.Kube().CoreV1().Nodes().(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapisecurityv1beta1.PeerAuthentication:
		return c.Istio().SecurityV1beta1().PeerAuthentications(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.Pod:
		return c.Kube().CoreV1().Pods(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1beta1.ProxyConfig:
		return c.Istio().NetworkingV1beta1().ProxyConfigs(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1beta1.ReferenceGrant:
		return c.GatewayAPI().GatewayV1beta1().ReferenceGrants(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapisecurityv1beta1.RequestAuthentication:
		return c.Istio().SecurityV1beta1().RequestAuthentications(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.Secret:
		return c.Kube().CoreV1().Secrets(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.Service:
		return c.Kube().CoreV1().Services(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapicorev1.ServiceAccount:
		return c.Kube().CoreV1().ServiceAccounts(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.ServiceEntry:
		return c.Istio().NetworkingV1alpha3().ServiceEntries(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.Sidecar:
		return c.Istio().NetworkingV1alpha3().Sidecars(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1alpha2.TCPRoute:
		return c.GatewayAPI().GatewayV1alpha2().TCPRoutes(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1alpha2.TLSRoute:
		return c.GatewayAPI().GatewayV1alpha2().TLSRoutes(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapitelemetryv1alpha1.Telemetry:
		return c.Istio().TelemetryV1alpha1().Telemetries(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *sigsk8siogatewayapiapisv1alpha2.UDPRoute:
		return c.GatewayAPI().GatewayV1alpha2().UDPRoutes(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *k8sioapiadmissionregistrationv1.ValidatingWebhookConfiguration:
		return c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.VirtualService:
		return c.Istio().NetworkingV1alpha3().VirtualServices(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapiextensionsv1alpha1.WasmPlugin:
		return c.Istio().ExtensionsV1alpha1().WasmPlugins(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.WorkloadEntry:
		return c.Istio().NetworkingV1alpha3().WorkloadEntries(namespace).(ktypes.ReadWriteAPI[T, TL])
	case *apiistioioapinetworkingv1alpha3.WorkloadGroup:
		return c.Istio().NetworkingV1alpha3().WorkloadGroups(namespace).(ktypes.ReadWriteAPI[T, TL])
	default:
		panic(fmt.Sprintf("Unknown type %T", ptr.Empty[T]()))
	}
}

func GetInformerFiltered[T runtime.Object](c ClientGetter, opts ktypes.InformerOptions) cache.SharedIndexInformer {
	var l func(options metav1.ListOptions) (runtime.Object, error)
	var w func(options metav1.ListOptions) (watch.Interface, error)

	switch any(ptr.Empty[T]()).(type) {
	case *apiistioioapisecurityv1beta1.AuthorizationPolicy:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().SecurityV1beta1().AuthorizationPolicies(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().SecurityV1beta1().AuthorizationPolicies(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapicertificatesv1.CertificateSigningRequest:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CertificatesV1().CertificateSigningRequests().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CertificatesV1().CertificateSigningRequests().Watch(context.Background(), options)
		}
	case *k8sioapicorev1.ConfigMap:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().ConfigMaps(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().ConfigMaps(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapiextensionsapiserverpkgapisapiextensionsv1.CustomResourceDefinition:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Ext().ApiextensionsV1().CustomResourceDefinitions().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Ext().ApiextensionsV1().CustomResourceDefinitions().Watch(context.Background(), options)
		}
	case *k8sioapiappsv1.Deployment:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().AppsV1().Deployments(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().AppsV1().Deployments(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.DestinationRule:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().DestinationRules(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().DestinationRules(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapidiscoveryv1.EndpointSlice:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().DiscoveryV1().EndpointSlices(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().DiscoveryV1().EndpointSlices(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapicorev1.Endpoints:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().Endpoints(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().Endpoints(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.EnvoyFilter:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().EnvoyFilters(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().EnvoyFilters(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1alpha2.GRPCRoute:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1alpha2().GRPCRoutes(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1alpha2().GRPCRoutes(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.Gateway:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().Gateways(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().Gateways(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1beta1.GatewayClass:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1beta1().GatewayClasses().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1beta1().GatewayClasses().Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1beta1.HTTPRoute:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1beta1().HTTPRoutes(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1beta1().HTTPRoutes(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapinetworkingv1.Ingress:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().NetworkingV1().Ingresses(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().NetworkingV1().Ingresses(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapinetworkingv1.IngressClass:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().NetworkingV1().IngressClasses().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().NetworkingV1().IngressClasses().Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1beta1.Gateway:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1beta1().Gateways(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1beta1().Gateways(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapiadmissionregistrationv1.MutatingWebhookConfiguration:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().Watch(context.Background(), options)
		}
	case *k8sioapicorev1.Namespace:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().Namespaces().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().Namespaces().Watch(context.Background(), options)
		}
	case *k8sioapicorev1.Node:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().Nodes().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().Nodes().Watch(context.Background(), options)
		}
	case *apiistioioapisecurityv1beta1.PeerAuthentication:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().SecurityV1beta1().PeerAuthentications(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().SecurityV1beta1().PeerAuthentications(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapicorev1.Pod:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().Pods(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().Pods(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1beta1.ProxyConfig:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1beta1().ProxyConfigs(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1beta1().ProxyConfigs(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1beta1.ReferenceGrant:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1beta1().ReferenceGrants(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1beta1().ReferenceGrants(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapisecurityv1beta1.RequestAuthentication:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().SecurityV1beta1().RequestAuthentications(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().SecurityV1beta1().RequestAuthentications(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapicorev1.Secret:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().Secrets(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().Secrets(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapicorev1.Service:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().Services(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().Services(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapicorev1.ServiceAccount:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().CoreV1().ServiceAccounts(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().CoreV1().ServiceAccounts(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.ServiceEntry:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().ServiceEntries(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().ServiceEntries(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.Sidecar:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().Sidecars(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().Sidecars(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1alpha2.TCPRoute:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1alpha2().TCPRoutes(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1alpha2().TCPRoutes(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1alpha2.TLSRoute:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1alpha2().TLSRoutes(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1alpha2().TLSRoutes(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapitelemetryv1alpha1.Telemetry:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().TelemetryV1alpha1().Telemetries(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().TelemetryV1alpha1().Telemetries(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *sigsk8siogatewayapiapisv1alpha2.UDPRoute:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.GatewayAPI().GatewayV1alpha2().UDPRoutes(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.GatewayAPI().GatewayV1alpha2().UDPRoutes(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *k8sioapiadmissionregistrationv1.ValidatingWebhookConfiguration:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().AdmissionregistrationV1().ValidatingWebhookConfigurations().Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.VirtualService:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().VirtualServices(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().VirtualServices(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapiextensionsv1alpha1.WasmPlugin:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().ExtensionsV1alpha1().WasmPlugins(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().ExtensionsV1alpha1().WasmPlugins(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.WorkloadEntry:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().WorkloadEntries(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().WorkloadEntries(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	case *apiistioioapinetworkingv1alpha3.WorkloadGroup:
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.Istio().NetworkingV1alpha3().WorkloadGroups(features.InformerWatchNamespace).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.Istio().NetworkingV1alpha3().WorkloadGroups(features.InformerWatchNamespace).Watch(context.Background(), options)
		}
	default:
		panic(fmt.Sprintf("Unknown type %T", ptr.Empty[T]()))
	}
	return c.KubeInformer().InformerFor(*new(T), func(k kubernetes.Interface, resync time.Duration) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return l(options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return w(options)
				},
			},
			*new(T),
			resync,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
	})
}

func GetInformer[T runtime.Object](c ClientGetter) cache.SharedIndexInformer {
	switch any(ptr.Empty[T]()).(type) {
	case *apiistioioapisecurityv1beta1.AuthorizationPolicy:
		return c.IstioInformer().Security().V1beta1().AuthorizationPolicies().Informer()
	case *k8sioapicertificatesv1.CertificateSigningRequest:
		return c.KubeInformer().Certificates().V1().CertificateSigningRequests().Informer()
	case *k8sioapicorev1.ConfigMap:
		return c.KubeInformer().Core().V1().ConfigMaps().Informer()
	case *k8sioapiextensionsapiserverpkgapisapiextensionsv1.CustomResourceDefinition:
		return c.ExtInformer().Apiextensions().V1().CustomResourceDefinitions().Informer()
	case *k8sioapiappsv1.Deployment:
		return c.KubeInformer().Apps().V1().Deployments().Informer()
	case *apiistioioapinetworkingv1alpha3.DestinationRule:
		return c.IstioInformer().Networking().V1alpha3().DestinationRules().Informer()
	case *k8sioapidiscoveryv1.EndpointSlice:
		return c.KubeInformer().Discovery().V1().EndpointSlices().Informer()
	case *k8sioapicorev1.Endpoints:
		return c.KubeInformer().Core().V1().Endpoints().Informer()
	case *apiistioioapinetworkingv1alpha3.EnvoyFilter:
		return c.IstioInformer().Networking().V1alpha3().EnvoyFilters().Informer()
	case *sigsk8siogatewayapiapisv1alpha2.GRPCRoute:
		return c.GatewayAPIInformer().Gateway().V1alpha2().GRPCRoutes().Informer()
	case *apiistioioapinetworkingv1alpha3.Gateway:
		return c.IstioInformer().Networking().V1alpha3().Gateways().Informer()
	case *sigsk8siogatewayapiapisv1beta1.GatewayClass:
		return c.GatewayAPIInformer().Gateway().V1beta1().GatewayClasses().Informer()
	case *sigsk8siogatewayapiapisv1beta1.HTTPRoute:
		return c.GatewayAPIInformer().Gateway().V1beta1().HTTPRoutes().Informer()
	case *k8sioapinetworkingv1.Ingress:
		return c.KubeInformer().Networking().V1().Ingresses().Informer()
	case *k8sioapinetworkingv1.IngressClass:
		return c.KubeInformer().Networking().V1().IngressClasses().Informer()
	case *sigsk8siogatewayapiapisv1beta1.Gateway:
		return c.GatewayAPIInformer().Gateway().V1beta1().Gateways().Informer()
	case *k8sioapiadmissionregistrationv1.MutatingWebhookConfiguration:
		return c.KubeInformer().Admissionregistration().V1().MutatingWebhookConfigurations().Informer()
	case *k8sioapicorev1.Namespace:
		return c.KubeInformer().Core().V1().Namespaces().Informer()
	case *k8sioapicorev1.Node:
		return c.KubeInformer().Core().V1().Nodes().Informer()
	case *apiistioioapisecurityv1beta1.PeerAuthentication:
		return c.IstioInformer().Security().V1beta1().PeerAuthentications().Informer()
	case *k8sioapicorev1.Pod:
		return c.KubeInformer().Core().V1().Pods().Informer()
	case *apiistioioapinetworkingv1beta1.ProxyConfig:
		return c.IstioInformer().Networking().V1beta1().ProxyConfigs().Informer()
	case *sigsk8siogatewayapiapisv1beta1.ReferenceGrant:
		return c.GatewayAPIInformer().Gateway().V1beta1().ReferenceGrants().Informer()
	case *apiistioioapisecurityv1beta1.RequestAuthentication:
		return c.IstioInformer().Security().V1beta1().RequestAuthentications().Informer()
	case *k8sioapicorev1.Secret:
		return c.KubeInformer().Core().V1().Secrets().Informer()
	case *k8sioapicorev1.Service:
		return c.KubeInformer().Core().V1().Services().Informer()
	case *k8sioapicorev1.ServiceAccount:
		return c.KubeInformer().Core().V1().ServiceAccounts().Informer()
	case *apiistioioapinetworkingv1alpha3.ServiceEntry:
		return c.IstioInformer().Networking().V1alpha3().ServiceEntries().Informer()
	case *apiistioioapinetworkingv1alpha3.Sidecar:
		return c.IstioInformer().Networking().V1alpha3().Sidecars().Informer()
	case *sigsk8siogatewayapiapisv1alpha2.TCPRoute:
		return c.GatewayAPIInformer().Gateway().V1alpha2().TCPRoutes().Informer()
	case *sigsk8siogatewayapiapisv1alpha2.TLSRoute:
		return c.GatewayAPIInformer().Gateway().V1alpha2().TLSRoutes().Informer()
	case *apiistioioapitelemetryv1alpha1.Telemetry:
		return c.IstioInformer().Telemetry().V1alpha1().Telemetries().Informer()
	case *sigsk8siogatewayapiapisv1alpha2.UDPRoute:
		return c.GatewayAPIInformer().Gateway().V1alpha2().UDPRoutes().Informer()
	case *k8sioapiadmissionregistrationv1.ValidatingWebhookConfiguration:
		return c.KubeInformer().Admissionregistration().V1().ValidatingWebhookConfigurations().Informer()
	case *apiistioioapinetworkingv1alpha3.VirtualService:
		return c.IstioInformer().Networking().V1alpha3().VirtualServices().Informer()
	case *apiistioioapiextensionsv1alpha1.WasmPlugin:
		return c.IstioInformer().Extensions().V1alpha1().WasmPlugins().Informer()
	case *apiistioioapinetworkingv1alpha3.WorkloadEntry:
		return c.IstioInformer().Networking().V1alpha3().WorkloadEntries().Informer()
	case *apiistioioapinetworkingv1alpha3.WorkloadGroup:
		return c.IstioInformer().Networking().V1alpha3().WorkloadGroups().Informer()
	default:
		panic(fmt.Sprintf("Unknown type %T", ptr.Empty[T]()))
	}
}
