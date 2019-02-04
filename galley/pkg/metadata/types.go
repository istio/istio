// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh runtime pkg/metadata/types.go
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

	// istio/config/v1alpha2/legacy/apikeys metadata
	IstioConfigV1alpha2LegacyApikeys resource.Info

	// istio/config/v1alpha2/legacy/authorizations metadata
	IstioConfigV1alpha2LegacyAuthorizations resource.Info

	// istio/config/v1alpha2/legacy/bypasses metadata
	IstioConfigV1alpha2LegacyBypasses resource.Info

	// istio/config/v1alpha2/legacy/checknothings metadata
	IstioConfigV1alpha2LegacyChecknothings resource.Info

	// istio/config/v1alpha2/legacy/circonuses metadata
	IstioConfigV1alpha2LegacyCirconuses resource.Info

	// istio/config/v1alpha2/legacy/cloudwatches metadata
	IstioConfigV1alpha2LegacyCloudwatches resource.Info

	// istio/config/v1alpha2/legacy/deniers metadata
	IstioConfigV1alpha2LegacyDeniers resource.Info

	// istio/config/v1alpha2/legacy/dogstatsds metadata
	IstioConfigV1alpha2LegacyDogstatsds resource.Info

	// istio/config/v1alpha2/legacy/edges metadata
	IstioConfigV1alpha2LegacyEdges resource.Info

	// istio/config/v1alpha2/legacy/fluentds metadata
	IstioConfigV1alpha2LegacyFluentds resource.Info

	// istio/config/v1alpha2/legacy/kubernetesenvs metadata
	IstioConfigV1alpha2LegacyKubernetesenvs resource.Info

	// istio/config/v1alpha2/legacy/kuberneteses metadata
	IstioConfigV1alpha2LegacyKuberneteses resource.Info

	// istio/config/v1alpha2/legacy/listcheckers metadata
	IstioConfigV1alpha2LegacyListcheckers resource.Info

	// istio/config/v1alpha2/legacy/listentries metadata
	IstioConfigV1alpha2LegacyListentries resource.Info

	// istio/config/v1alpha2/legacy/logentries metadata
	IstioConfigV1alpha2LegacyLogentries resource.Info

	// istio/config/v1alpha2/legacy/memquotas metadata
	IstioConfigV1alpha2LegacyMemquotas resource.Info

	// istio/config/v1alpha2/legacy/metrics metadata
	IstioConfigV1alpha2LegacyMetrics resource.Info

	// istio/config/v1alpha2/legacy/noops metadata
	IstioConfigV1alpha2LegacyNoops resource.Info

	// istio/config/v1alpha2/legacy/opas metadata
	IstioConfigV1alpha2LegacyOpas resource.Info

	// istio/config/v1alpha2/legacy/prometheuses metadata
	IstioConfigV1alpha2LegacyPrometheuses resource.Info

	// istio/config/v1alpha2/legacy/quotas metadata
	IstioConfigV1alpha2LegacyQuotas resource.Info

	// istio/config/v1alpha2/legacy/rbacs metadata
	IstioConfigV1alpha2LegacyRbacs resource.Info

	// istio/config/v1alpha2/legacy/redisquotas metadata
	IstioConfigV1alpha2LegacyRedisquotas resource.Info

	// istio/config/v1alpha2/legacy/reportnothings metadata
	IstioConfigV1alpha2LegacyReportnothings resource.Info

	// istio/config/v1alpha2/legacy/signalfxs metadata
	IstioConfigV1alpha2LegacySignalfxs resource.Info

	// istio/config/v1alpha2/legacy/solarwindses metadata
	IstioConfigV1alpha2LegacySolarwindses resource.Info

	// istio/config/v1alpha2/legacy/stackdrivers metadata
	IstioConfigV1alpha2LegacyStackdrivers resource.Info

	// istio/config/v1alpha2/legacy/statsds metadata
	IstioConfigV1alpha2LegacyStatsds resource.Info

	// istio/config/v1alpha2/legacy/stdios metadata
	IstioConfigV1alpha2LegacyStdios resource.Info

	// istio/config/v1alpha2/legacy/tracespans metadata
	IstioConfigV1alpha2LegacyTracespans resource.Info

	// istio/config/v1alpha2/legacy/tracespans metadata
	IstioConfigV1alpha2LegacyZipkins resource.Info

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

	// k8s/core/v1/endpoints metadata
	K8sCoreV1Endpoints resource.Info

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
	IstioConfigV1alpha2LegacyApikeys = b.Register(
		"istio/config/v1alpha2/legacy/apikeys",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyAuthorizations = b.Register(
		"istio/config/v1alpha2/legacy/authorizations",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyBypasses = b.Register(
		"istio/config/v1alpha2/legacy/bypasses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyChecknothings = b.Register(
		"istio/config/v1alpha2/legacy/checknothings",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyCirconuses = b.Register(
		"istio/config/v1alpha2/legacy/circonuses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyCloudwatches = b.Register(
		"istio/config/v1alpha2/legacy/cloudwatches",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyDeniers = b.Register(
		"istio/config/v1alpha2/legacy/deniers",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyDogstatsds = b.Register(
		"istio/config/v1alpha2/legacy/dogstatsds",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyEdges = b.Register(
		"istio/config/v1alpha2/legacy/edges",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyFluentds = b.Register(
		"istio/config/v1alpha2/legacy/fluentds",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyKubernetesenvs = b.Register(
		"istio/config/v1alpha2/legacy/kubernetesenvs",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyKuberneteses = b.Register(
		"istio/config/v1alpha2/legacy/kuberneteses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyListcheckers = b.Register(
		"istio/config/v1alpha2/legacy/listcheckers",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyListentries = b.Register(
		"istio/config/v1alpha2/legacy/listentries",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyLogentries = b.Register(
		"istio/config/v1alpha2/legacy/logentries",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyMemquotas = b.Register(
		"istio/config/v1alpha2/legacy/memquotas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyMetrics = b.Register(
		"istio/config/v1alpha2/legacy/metrics",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyNoops = b.Register(
		"istio/config/v1alpha2/legacy/noops",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyOpas = b.Register(
		"istio/config/v1alpha2/legacy/opas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyPrometheuses = b.Register(
		"istio/config/v1alpha2/legacy/prometheuses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyQuotas = b.Register(
		"istio/config/v1alpha2/legacy/quotas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyRbacs = b.Register(
		"istio/config/v1alpha2/legacy/rbacs",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyRedisquotas = b.Register(
		"istio/config/v1alpha2/legacy/redisquotas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyReportnothings = b.Register(
		"istio/config/v1alpha2/legacy/reportnothings",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacySignalfxs = b.Register(
		"istio/config/v1alpha2/legacy/signalfxs",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacySolarwindses = b.Register(
		"istio/config/v1alpha2/legacy/solarwindses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyStackdrivers = b.Register(
		"istio/config/v1alpha2/legacy/stackdrivers",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyStatsds = b.Register(
		"istio/config/v1alpha2/legacy/statsds",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyStdios = b.Register(
		"istio/config/v1alpha2/legacy/stdios",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyTracespans = b.Register(
		"istio/config/v1alpha2/legacy/tracespans",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	IstioConfigV1alpha2LegacyZipkins = b.Register(
		"istio/config/v1alpha2/legacy/zipkins",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
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
	K8sCoreV1Endpoints = b.Register(
		"k8s/core/v1/endpoints",
		"type.googleapis.com/k8s.io.api.core.v1.Endpoints")
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
