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

	// MeshPolicy metadata
	MeshPolicy resource.Info

	// Policy metadata
	Policy resource.Info

	// Adapter metadata
	Adapter resource.Info

	// HTTPAPISpecBinding metadata
	HTTPAPISpecBinding resource.Info

	// HTTPAPISpec metadata
	HTTPAPISpec resource.Info

	// Apikey metadata
	Apikey resource.Info

	// Authorization metadata
	Authorization resource.Info

	// Bypass metadata
	Bypass resource.Info

	// Checknothing metadata
	Checknothing resource.Info

	// Circonus metadata
	Circonus resource.Info

	// Cloudwatch metadata
	Cloudwatch resource.Info

	// Denier metadata
	Denier resource.Info

	// Dogstatsd metadata
	Dogstatsd resource.Info

	// Edge metadata
	Edge resource.Info

	// Fluentd metadata
	Fluentd resource.Info

	// Kubernetesenv metadata
	Kubernetesenv resource.Info

	// Kubernetes metadata
	Kubernetes resource.Info

	// Listchecker metadata
	Listchecker resource.Info

	// Listentry metadata
	Listentry resource.Info

	// Logentry metadata
	Logentry resource.Info

	// Memquota metadata
	Memquota resource.Info

	// Metric metadata
	Metric resource.Info

	// Noop metadata
	Noop resource.Info

	// Opa metadata
	Opa resource.Info

	// Prometheus metadata
	Prometheus resource.Info

	// Quota metadata
	Quota resource.Info

	// Rbac metadata
	Rbac resource.Info

	// Redisquota metadata
	Redisquota resource.Info

	// Reportnothing metadata
	Reportnothing resource.Info

	// Servicecontrolreport metadata
	Servicecontrolreport resource.Info

	// Servicecontrol metadata
	Servicecontrol resource.Info

	// Signalfx metadata
	Signalfx resource.Info

	// Solarwinds metadata
	Solarwinds resource.Info

	// Stackdriver metadata
	Stackdriver resource.Info

	// Statsd metadata
	Statsd resource.Info

	// Stdio metadata
	Stdio resource.Info

	// Tracespan metadata
	Tracespan resource.Info

	// Template metadata
	Template resource.Info

	// QuotaSpecBinding metadata
	QuotaSpecBinding resource.Info

	// QuotaSpec metadata
	QuotaSpec resource.Info

	// DestinationRule metadata
	DestinationRule resource.Info

	// EnvoyFilter metadata
	EnvoyFilter resource.Info

	// Gateway metadata
	Gateway resource.Info

	// ServiceEntry metadata
	ServiceEntry resource.Info

	// Sidecar metadata
	Sidecar resource.Info

	// VirtualService metadata
	VirtualService resource.Info

	// Attributemanifest metadata
	Attributemanifest resource.Info

	// Handler metadata
	Handler resource.Info

	// Instance metadata
	Instance resource.Info

	// Rule metadata
	Rule resource.Info

	// ClusterRbacConfig metadata
	ClusterRbacConfig resource.Info

	// RbacConfig metadata
	RbacConfig resource.Info

	// ServiceRoleBinding metadata
	ServiceRoleBinding resource.Info

	// ServiceRole metadata
	ServiceRole resource.Info

	// Endpoints metadata
	Endpoints resource.Info

	// Node metadata
	Node resource.Info

	// Pod metadata
	Pod resource.Info

	// Service metadata
	Service resource.Info

	// Ingress metadata
	Ingress resource.Info
)

func init() {
	b := resource.NewSchemaBuilder()

	MeshPolicy = b.Register(
		"istio/authentication/v1alpha1/meshpolicies",
		"type.googleapis.com/istio.authentication.v1alpha1.Policy")
	Policy = b.Register(
		"istio/authentication/v1alpha1/policies",
		"type.googleapis.com/istio.authentication.v1alpha1.Policy")
	Adapter = b.Register(
		"istio/config/v1alpha2/adapters",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	HTTPAPISpecBinding = b.Register(
		"istio/config/v1alpha2/httpapispecbindings",
		"type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpecBinding")
	HTTPAPISpec = b.Register(
		"istio/config/v1alpha2/httpapispecs",
		"type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpec")
	Apikey = b.Register(
		"istio/config/v1alpha2/legacy/apikeys",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Authorization = b.Register(
		"istio/config/v1alpha2/legacy/authorizations",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Bypass = b.Register(
		"istio/config/v1alpha2/legacy/bypasses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Checknothing = b.Register(
		"istio/config/v1alpha2/legacy/checknothings",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Circonus = b.Register(
		"istio/config/v1alpha2/legacy/circonuses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Cloudwatch = b.Register(
		"istio/config/v1alpha2/legacy/cloudwatches",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Denier = b.Register(
		"istio/config/v1alpha2/legacy/deniers",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Dogstatsd = b.Register(
		"istio/config/v1alpha2/legacy/dogstatsds",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Edge = b.Register(
		"istio/config/v1alpha2/legacy/edges",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Fluentd = b.Register(
		"istio/config/v1alpha2/legacy/fluentds",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Kubernetesenv = b.Register(
		"istio/config/v1alpha2/legacy/kubernetesenvs",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Kubernetes = b.Register(
		"istio/config/v1alpha2/legacy/kuberneteses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Listchecker = b.Register(
		"istio/config/v1alpha2/legacy/listcheckers",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Listentry = b.Register(
		"istio/config/v1alpha2/legacy/listentries",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Logentry = b.Register(
		"istio/config/v1alpha2/legacy/logentries",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Memquota = b.Register(
		"istio/config/v1alpha2/legacy/memquotas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Metric = b.Register(
		"istio/config/v1alpha2/legacy/metrics",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Noop = b.Register(
		"istio/config/v1alpha2/legacy/noops",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Opa = b.Register(
		"istio/config/v1alpha2/legacy/opas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Prometheus = b.Register(
		"istio/config/v1alpha2/legacy/prometheuses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Quota = b.Register(
		"istio/config/v1alpha2/legacy/quotas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Rbac = b.Register(
		"istio/config/v1alpha2/legacy/rbacs",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Redisquota = b.Register(
		"istio/config/v1alpha2/legacy/redisquotas",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Reportnothing = b.Register(
		"istio/config/v1alpha2/legacy/reportnothings",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Servicecontrolreport = b.Register(
		"istio/config/v1alpha2/legacy/servicecontrolreports",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Servicecontrol = b.Register(
		"istio/config/v1alpha2/legacy/servicecontrols",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Signalfx = b.Register(
		"istio/config/v1alpha2/legacy/signalfxs",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Solarwinds = b.Register(
		"istio/config/v1alpha2/legacy/solarwindses",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Stackdriver = b.Register(
		"istio/config/v1alpha2/legacy/stackdrivers",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Statsd = b.Register(
		"istio/config/v1alpha2/legacy/statsds",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Stdio = b.Register(
		"istio/config/v1alpha2/legacy/stdios",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Tracespan = b.Register(
		"istio/config/v1alpha2/legacy/tracespans",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	Template = b.Register(
		"istio/config/v1alpha2/templates",
		"type.googleapis.com/type.googleapis.com/google.protobuf.Struct")
	QuotaSpecBinding = b.Register(
		"istio/mixer/v1/config/client/quotaspecbindings",
		"type.googleapis.com/istio.mixer.v1.config.client.QuotaSpecBinding")
	QuotaSpec = b.Register(
		"istio/mixer/v1/config/client/quotaspecs",
		"type.googleapis.com/istio.mixer.v1.config.client.QuotaSpec")
	DestinationRule = b.Register(
		"istio/networking/v1alpha3/destinationrules",
		"type.googleapis.com/istio.networking.v1alpha3.DestinationRule")
	EnvoyFilter = b.Register(
		"istio/networking/v1alpha3/envoyfilters",
		"type.googleapis.com/istio.networking.v1alpha3.EnvoyFilter")
	Gateway = b.Register(
		"istio/networking/v1alpha3/gateways",
		"type.googleapis.com/istio.networking.v1alpha3.Gateway")
	ServiceEntry = b.Register(
		"istio/networking/v1alpha3/serviceentries",
		"type.googleapis.com/istio.networking.v1alpha3.ServiceEntry")
	Sidecar = b.Register(
		"istio/networking/v1alpha3/sidecars",
		"type.googleapis.com/istio.networking.v1alpha3.Sidecar")
	VirtualService = b.Register(
		"istio/networking/v1alpha3/virtualservices",
		"type.googleapis.com/istio.networking.v1alpha3.VirtualService")
	Attributemanifest = b.Register(
		"istio/policy/v1beta1/attributemanifests",
		"type.googleapis.com/istio.policy.v1beta1.AttributeManifest")
	Handler = b.Register(
		"istio/policy/v1beta1/handlers",
		"type.googleapis.com/istio.policy.v1beta1.Handler")
	Instance = b.Register(
		"istio/policy/v1beta1/instances",
		"type.googleapis.com/istio.policy.v1beta1.Instance")
	Rule = b.Register(
		"istio/policy/v1beta1/rules",
		"type.googleapis.com/istio.policy.v1beta1.Rule")
	ClusterRbacConfig = b.Register(
		"istio/rbac/v1alpha1/clusterrbacconfigs",
		"type.googleapis.com/istio.rbac.v1alpha1.RbacConfig")
	RbacConfig = b.Register(
		"istio/rbac/v1alpha1/rbacconfigs",
		"type.googleapis.com/istio.rbac.v1alpha1.RbacConfig")
	ServiceRoleBinding = b.Register(
		"istio/rbac/v1alpha1/servicerolebindings",
		"type.googleapis.com/istio.rbac.v1alpha1.ServiceRoleBinding")
	ServiceRole = b.Register(
		"istio/rbac/v1alpha1/serviceroles",
		"type.googleapis.com/istio.rbac.v1alpha1.ServiceRole")
	Endpoints = b.Register(
		"k8s/core/v1/endpoints",
		"type.googleapis.com/k8s.io.api.core.v1.Endpoints")
	Node = b.Register(
		"k8s/core/v1/nodes",
		"type.googleapis.com/k8s.io.api.core.v1.NodeSpec")
	Pod = b.Register(
		"k8s/core/v1/pods",
		"type.googleapis.com/k8s.io.api.core.v1.Pod")
	Service = b.Register(
		"k8s/core/v1/services",
		"type.googleapis.com/k8s.io.api.core.v1.ServiceSpec")
	Ingress = b.Register(
		"k8s/extensions/v1beta1/ingresses",
		"type.googleapis.com/k8s.io.api.extensions.v1beta1.IngressSpec")

	Types = b.Build()
}
