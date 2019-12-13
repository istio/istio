// GENERATED FILE -- DO NOT EDIT
//
//go:generate $REPO_ROOT/galley/tools/gen-meta/gen-meta.sh runtime pkg/metadata/types.gen.go
//

package metadata

import (
	// Pull in all the known proto types to ensure we get their types registered.

	// Register protos in "github.com/gogo/protobuf/types"
	_ "github.com/gogo/protobuf/types"

	// Register protos in "istio.io/api/authentication/v1alpha1"
	_ "istio.io/api/authentication/v1alpha1"

	// Register protos in "istio.io/api/mixer/v1/config/client"
	_ "istio.io/api/mixer/v1/config/client"

	// Register protos in "istio.io/api/networking/v1alpha3"
	_ "istio.io/api/networking/v1alpha3"

	// Register protos in "istio.io/api/policy/v1beta1"
	_ "istio.io/api/policy/v1beta1"

	// Register protos in "istio.io/api/rbac/v1alpha1"
	_ "istio.io/api/rbac/v1alpha1"

	// Register protos in "istio.io/api/security/v1beta1"
	_ "istio.io/api/security/v1beta1"

	// Register protos in "k8s.io/api/core/v1"
	_ "k8s.io/api/core/v1"

	// Register protos in "k8s.io/api/extensions/v1beta1"
	_ "k8s.io/api/extensions/v1beta1"

	"istio.io/istio/galley/pkg/runtime/resource"
)

// Types of known resources.
var Types *resource.Schema

var (

	// istio/authentication/v1alpha1/meshpolicies metadata
	IstioAuthenticationV1alpha1Meshpolicies resource.Info

	// istio/authentication/v1alpha1/policies metadata
	IstioAuthenticationV1alpha1Policies resource.Info

	// istio/config/v1alpha2/adapters metadata
	IstioConfigV1alpha2Adapters resource.Info

	// istio/config/v1alpha2/httpapispecbindings metadata
	IstioConfigV1alpha2Httpapispecbindings resource.Info

	// istio/config/v1alpha2/httpapispecs metadata
	IstioConfigV1alpha2Httpapispecs resource.Info

	// istio/config/v1alpha2/templates metadata
	IstioConfigV1alpha2Templates resource.Info

	// istio/mixer/v1/config/client/quotaspecbindings metadata
	IstioMixerV1ConfigClientQuotaspecbindings resource.Info

	// istio/mixer/v1/config/client/quotaspecs metadata
	IstioMixerV1ConfigClientQuotaspecs resource.Info

	// istio/networking/v1alpha3/destinationrules metadata
	IstioNetworkingV1alpha3Destinationrules resource.Info

	// istio/networking/v1alpha3/envoyfilters metadata
	IstioNetworkingV1alpha3Envoyfilters resource.Info

	// istio/networking/v1alpha3/gateways metadata
	IstioNetworkingV1alpha3Gateways resource.Info

	// istio/networking/v1alpha3/serviceentries metadata
	IstioNetworkingV1alpha3Serviceentries resource.Info

	// istio/networking/v1alpha3/sidecars metadata
	IstioNetworkingV1alpha3Sidecars resource.Info

	// istio/networking/v1alpha3/synthetic/serviceentries metadata
	IstioNetworkingV1alpha3SyntheticServiceentries resource.Info

	// istio/networking/v1alpha3/virtualservices metadata
	IstioNetworkingV1alpha3Virtualservices resource.Info

	// istio/policy/v1beta1/attributemanifests metadata
	IstioPolicyV1beta1Attributemanifests resource.Info

	// istio/policy/v1beta1/handlers metadata
	IstioPolicyV1beta1Handlers resource.Info

	// istio/policy/v1beta1/instances metadata
	IstioPolicyV1beta1Instances resource.Info

	// istio/policy/v1beta1/rules metadata
	IstioPolicyV1beta1Rules resource.Info

	// istio/rbac/v1alpha1/clusterrbacconfigs metadata
	IstioRbacV1alpha1Clusterrbacconfigs resource.Info

	// istio/rbac/v1alpha1/rbacconfigs metadata
	IstioRbacV1alpha1Rbacconfigs resource.Info

	// istio/rbac/v1alpha1/servicerolebindings metadata
	IstioRbacV1alpha1Servicerolebindings resource.Info

	// istio/rbac/v1alpha1/serviceroles metadata
	IstioRbacV1alpha1Serviceroles resource.Info

	// istio/security/v1beta1/authorizationpolicies metadata
	IstioSecurityV1beta1Authorizationpolicies resource.Info

	// istio/security/v1beta1/requestauthentications metadata
	IstioSecurityV1beta1Requestauthentications resource.Info

	// k8s/core/v1/endpoints metadata
	K8sCoreV1Endpoints resource.Info

	// k8s/core/v1/namespaces metadata
	K8sCoreV1Namespaces resource.Info

	// k8s/core/v1/nodes metadata
	K8sCoreV1Nodes resource.Info

	// k8s/core/v1/pods metadata
	K8sCoreV1Pods resource.Info

	// k8s/core/v1/services metadata
	K8sCoreV1Services resource.Info

	// k8s/extensions/v1beta1/ingresses metadata
	K8sExtensionsV1beta1Ingresses resource.Info
)

func init() {
	b := resource.NewSchemaBuilder()

	IstioAuthenticationV1alpha1Meshpolicies = b.Register(
		"istio/authentication/v1alpha1/meshpolicies",
		"type.googleapis.com/istio.authentication.v1alpha1.Policy")
	IstioAuthenticationV1alpha1Policies = b.Register(
		"istio/authentication/v1alpha1/policies",
		"type.googleapis.com/istio.authentication.v1alpha1.Policy")
	IstioConfigV1alpha2Adapters = b.Register(
		"istio/config/v1alpha2/adapters",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2Httpapispecbindings = b.Register(
		"istio/config/v1alpha2/httpapispecbindings",
		"type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpecBinding")
	IstioConfigV1alpha2Httpapispecs = b.Register(
		"istio/config/v1alpha2/httpapispecs",
		"type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpec")
	IstioConfigV1alpha2Templates = b.Register(
		"istio/config/v1alpha2/templates",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioMixerV1ConfigClientQuotaspecbindings = b.Register(
		"istio/mixer/v1/config/client/quotaspecbindings",
		"type.googleapis.com/istio.mixer.v1.config.client.QuotaSpecBinding")
	IstioMixerV1ConfigClientQuotaspecs = b.Register(
		"istio/mixer/v1/config/client/quotaspecs",
		"type.googleapis.com/istio.mixer.v1.config.client.QuotaSpec")
	IstioNetworkingV1alpha3Destinationrules = b.Register(
		"istio/networking/v1alpha3/destinationrules",
		"type.googleapis.com/istio.networking.v1alpha3.DestinationRule")
	IstioNetworkingV1alpha3Envoyfilters = b.Register(
		"istio/networking/v1alpha3/envoyfilters",
		"type.googleapis.com/istio.networking.v1alpha3.EnvoyFilter")
	IstioNetworkingV1alpha3Gateways = b.Register(
		"istio/networking/v1alpha3/gateways",
		"type.googleapis.com/istio.networking.v1alpha3.Gateway")
	IstioNetworkingV1alpha3Serviceentries = b.Register(
		"istio/networking/v1alpha3/serviceentries",
		"type.googleapis.com/istio.networking.v1alpha3.ServiceEntry")
	IstioNetworkingV1alpha3Sidecars = b.Register(
		"istio/networking/v1alpha3/sidecars",
		"type.googleapis.com/istio.networking.v1alpha3.Sidecar")
	IstioNetworkingV1alpha3SyntheticServiceentries = b.Register(
		"istio/networking/v1alpha3/synthetic/serviceentries",
		"type.googleapis.com/istio.networking.v1alpha3.ServiceEntry")
	IstioNetworkingV1alpha3Virtualservices = b.Register(
		"istio/networking/v1alpha3/virtualservices",
		"type.googleapis.com/istio.networking.v1alpha3.VirtualService")
	IstioPolicyV1beta1Attributemanifests = b.Register(
		"istio/policy/v1beta1/attributemanifests",
		"type.googleapis.com/istio.policy.v1beta1.AttributeManifest")
	IstioPolicyV1beta1Handlers = b.Register(
		"istio/policy/v1beta1/handlers",
		"type.googleapis.com/istio.policy.v1beta1.Handler")
	IstioPolicyV1beta1Instances = b.Register(
		"istio/policy/v1beta1/instances",
		"type.googleapis.com/istio.policy.v1beta1.Instance")
	IstioPolicyV1beta1Rules = b.Register(
		"istio/policy/v1beta1/rules",
		"type.googleapis.com/istio.policy.v1beta1.Rule")
	IstioRbacV1alpha1Clusterrbacconfigs = b.Register(
		"istio/rbac/v1alpha1/clusterrbacconfigs",
		"type.googleapis.com/istio.rbac.v1alpha1.RbacConfig")
	IstioRbacV1alpha1Rbacconfigs = b.Register(
		"istio/rbac/v1alpha1/rbacconfigs",
		"type.googleapis.com/istio.rbac.v1alpha1.RbacConfig")
	IstioRbacV1alpha1Servicerolebindings = b.Register(
		"istio/rbac/v1alpha1/servicerolebindings",
		"type.googleapis.com/istio.rbac.v1alpha1.ServiceRoleBinding")
	IstioRbacV1alpha1Serviceroles = b.Register(
		"istio/rbac/v1alpha1/serviceroles",
		"type.googleapis.com/istio.rbac.v1alpha1.ServiceRole")
	IstioSecurityV1beta1Authorizationpolicies = b.Register(
		"istio/security/v1beta1/authorizationpolicies",
		"type.googleapis.com/istio.security.v1beta1.AuthorizationPolicy")
	IstioSecurityV1beta1Requestauthentications = b.Register(
		"istio/security/v1beta1/requestauthentications",
		"type.googleapis.com/istio.security.v1beta1.RequestAuthentication")
	K8sCoreV1Endpoints = b.Register(
		"k8s/core/v1/endpoints",
		"type.googleapis.com/k8s.io.api.core.v1.Endpoints")
	K8sCoreV1Namespaces = b.Register(
		"k8s/core/v1/namespaces",
		"type.googleapis.com/k8s.io.api.core.v1.NamespaceSpec")
	K8sCoreV1Nodes = b.Register(
		"k8s/core/v1/nodes",
		"type.googleapis.com/k8s.io.api.core.v1.NodeSpec")
	K8sCoreV1Pods = b.Register(
		"k8s/core/v1/pods",
		"type.googleapis.com/k8s.io.api.core.v1.Pod")
	K8sCoreV1Services = b.Register(
		"k8s/core/v1/services",
		"type.googleapis.com/k8s.io.api.core.v1.ServiceSpec")
	K8sExtensionsV1beta1Ingresses = b.Register(
		"k8s/extensions/v1beta1/ingresses",
		"type.googleapis.com/k8s.io.api.extensions.v1beta1.IngressSpec")

	Types = b.Build()
}
