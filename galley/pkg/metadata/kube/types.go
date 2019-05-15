// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh kube pkg/metadata/kube/types.go
//

package kube

import (
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
)

// Types in the schema.
var Types *schema.Instance

func init() {
	b := schema.NewBuilder()
	var versions []string

	versions = make([]string, 0)

	versions = append(versions, "v1alpha1")

	b.Add(schema.ResourceSpec{
		Kind:      "MeshPolicy",
		ListKind:  "MeshPolicyList",
		Singular:  "meshpolicy",
		Plural:    "meshpolicies",
		Versions:  versions,
		Group:     "authentication.istio.io",
		Target:    metadata.Types.Get("istio/authentication/v1alpha1/meshpolicies"),
		Converter: converter.Get("auth-policy-resource"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha1")

	b.Add(schema.ResourceSpec{
		Kind:      "Policy",
		ListKind:  "PolicyList",
		Singular:  "policy",
		Plural:    "policies",
		Versions:  versions,
		Group:     "authentication.istio.io",
		Target:    metadata.Types.Get("istio/authentication/v1alpha1/policies"),
		Converter: converter.Get("auth-policy-resource"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "adapter",
		ListKind:  "adapterList",
		Singular:  "adapter",
		Plural:    "adapters",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/adapters"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "HTTPAPISpecBinding",
		ListKind:  "HTTPAPISpecBindingList",
		Singular:  "httpapispecbinding",
		Plural:    "httpapispecbindings",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/httpapispecbindings"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "HTTPAPISpec",
		ListKind:  "HTTPAPISpecList",
		Singular:  "httpapispec",
		Plural:    "httpapispecs",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/httpapispecs"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "apikey",
		ListKind:  "apikeyList",
		Singular:  "apikey",
		Plural:    "apikeys",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/apikeys"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "authorization",
		ListKind:  "authorizationList",
		Singular:  "authorization",
		Plural:    "authorizations",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/authorizations"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "bypass",
		ListKind:  "bypassList",
		Singular:  "bypass",
		Plural:    "bypasses",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/bypasses"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "checknothing",
		ListKind:  "checknothingList",
		Singular:  "checknothing",
		Plural:    "checknothings",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/checknothings"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "circonus",
		ListKind:  "circonusList",
		Singular:  "circonus",
		Plural:    "circonuses",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/circonuses"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "cloudwatch",
		ListKind:  "cloudwatchList",
		Singular:  "cloudwatch",
		Plural:    "cloudwatches",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/cloudwatches"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "denier",
		ListKind:  "denierList",
		Singular:  "denier",
		Plural:    "deniers",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/deniers"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "dogstatsd",
		ListKind:  "dogstatsdList",
		Singular:  "dogstatsd",
		Plural:    "dogstatsds",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/dogstatsds"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "edge",
		ListKind:  "edgeList",
		Singular:  "edge",
		Plural:    "edges",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/edges"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "fluentd",
		ListKind:  "fluentdList",
		Singular:  "fluentd",
		Plural:    "fluentds",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/fluentds"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "kubernetesenv",
		ListKind:  "kubernetesenvList",
		Singular:  "kubernetesenv",
		Plural:    "kubernetesenvs",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/kubernetesenvs"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "kubernetes",
		ListKind:  "kubernetesList",
		Singular:  "kubernetes",
		Plural:    "kuberneteses",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/kuberneteses"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "listchecker",
		ListKind:  "listcheckerList",
		Singular:  "listchecker",
		Plural:    "listcheckers",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/listcheckers"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "listentry",
		ListKind:  "listentryList",
		Singular:  "listentry",
		Plural:    "listentries",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/listentries"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "logentry",
		ListKind:  "logentryList",
		Singular:  "logentry",
		Plural:    "logentries",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/logentries"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "memquota",
		ListKind:  "memquotaList",
		Singular:  "memquota",
		Plural:    "memquotas",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/memquotas"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "metric",
		ListKind:  "metricList",
		Singular:  "metric",
		Plural:    "metrics",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/metrics"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "noop",
		ListKind:  "noopList",
		Singular:  "noop",
		Plural:    "noops",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/noops"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "opa",
		ListKind:  "opaList",
		Singular:  "opa",
		Plural:    "opas",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/opas"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "prometheus",
		ListKind:  "prometheusList",
		Singular:  "prometheus",
		Plural:    "prometheuses",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/prometheuses"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "quota",
		ListKind:  "quotaList",
		Singular:  "quota",
		Plural:    "quotas",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/quotas"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "rbac",
		ListKind:  "rbacList",
		Singular:  "rbac",
		Plural:    "rbacs",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/rbacs"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "redisquota",
		ListKind:  "redisquotaList",
		Singular:  "redisquota",
		Plural:    "redisquotas",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/redisquotas"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "reportnothing",
		ListKind:  "reportnothingList",
		Singular:  "reportnothing",
		Plural:    "reportnothings",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/reportnothings"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "signalfx",
		ListKind:  "signalfxList",
		Singular:  "signalfx",
		Plural:    "signalfxs",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/signalfxs"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "solarwinds",
		ListKind:  "solarwindsList",
		Singular:  "solarwinds",
		Plural:    "solarwindses",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/solarwindses"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "stackdriver",
		ListKind:  "stackdriverList",
		Singular:  "stackdriver",
		Plural:    "stackdrivers",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/stackdrivers"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "statsd",
		ListKind:  "statsdList",
		Singular:  "statsd",
		Plural:    "statsds",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/statsds"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "stdio",
		ListKind:  "stdioList",
		Singular:  "stdio",
		Plural:    "stdios",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/stdios"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "tracespan",
		ListKind:  "tracespanList",
		Singular:  "tracespan",
		Plural:    "tracespans",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/tracespans"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "zipkin",
		ListKind:  "zipkinList",
		Singular:  "zipkin",
		Plural:    "zipkins",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/zipkins"),
		Converter: converter.Get("identity"),

		Optional: true,
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "template",
		ListKind:  "templateList",
		Singular:  "template",
		Plural:    "templates",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/templates"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "QuotaSpecBinding",
		ListKind:  "QuotaSpecBindingList",
		Singular:  "quotaspecbinding",
		Plural:    "quotaspecbindings",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/mixer/v1/config/client/quotaspecbindings"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "QuotaSpec",
		ListKind:  "QuotaSpecList",
		Singular:  "quotaspec",
		Plural:    "quotaspecs",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/mixer/v1/config/client/quotaspecs"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha3")

	b.Add(schema.ResourceSpec{
		Kind:      "DestinationRule",
		ListKind:  "DestinationRuleList",
		Singular:  "destinationrule",
		Plural:    "destinationrules",
		Versions:  versions,
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/destinationrules"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha3")

	b.Add(schema.ResourceSpec{
		Kind:      "EnvoyFilter",
		ListKind:  "EnvoyFilterList",
		Singular:  "envoyfilter",
		Plural:    "envoyfilters",
		Versions:  versions,
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/envoyfilters"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha3")

	b.Add(schema.ResourceSpec{
		Kind:      "Gateway",
		ListKind:  "GatewayList",
		Singular:  "gateway",
		Plural:    "gateways",
		Versions:  versions,
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/gateways"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha3")

	b.Add(schema.ResourceSpec{
		Kind:      "ServiceEntry",
		ListKind:  "ServiceEntryList",
		Singular:  "serviceentry",
		Plural:    "serviceentries",
		Versions:  versions,
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/serviceentries"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha3")

	b.Add(schema.ResourceSpec{
		Kind:      "Sidecar",
		ListKind:  "SidecarList",
		Singular:  "sidecar",
		Plural:    "sidecars",
		Versions:  versions,
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/sidecars"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha3")

	b.Add(schema.ResourceSpec{
		Kind:      "VirtualService",
		ListKind:  "VirtualServiceList",
		Singular:  "virtualservice",
		Plural:    "virtualservices",
		Versions:  versions,
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/virtualservices"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "attributemanifest",
		ListKind:  "attributemanifestList",
		Singular:  "attributemanifest",
		Plural:    "attributemanifests",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/attributemanifests"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "handler",
		ListKind:  "handlerList",
		Singular:  "handler",
		Plural:    "handlers",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/handlers"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "instance",
		ListKind:  "instanceList",
		Singular:  "instance",
		Plural:    "instances",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/instances"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha2")

	b.Add(schema.ResourceSpec{
		Kind:      "rule",
		ListKind:  "ruleList",
		Singular:  "rule",
		Plural:    "rules",
		Versions:  versions,
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/rules"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha1")

	b.Add(schema.ResourceSpec{
		Kind:      "AuthorizationPolicy",
		ListKind:  "AuthorizationPolicyList",
		Singular:  "authorizationpolicy",
		Plural:    "authorizationpolicies",
		Versions:  versions,
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/authorizationpolicies"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha1")

	b.Add(schema.ResourceSpec{
		Kind:      "ClusterRbacConfig",
		ListKind:  "ClusterRbacConfigList",
		Singular:  "clusterrbacconfig",
		Plural:    "clusterrbacconfigs",
		Versions:  versions,
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/clusterrbacconfigs"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha1")

	b.Add(schema.ResourceSpec{
		Kind:      "RbacConfig",
		ListKind:  "RbacConfigList",
		Singular:  "rbacconfig",
		Plural:    "rbacconfigs",
		Versions:  versions,
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/rbacconfigs"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha1")

	b.Add(schema.ResourceSpec{
		Kind:      "ServiceRoleBinding",
		ListKind:  "ServiceRoleBindingList",
		Singular:  "servicerolebinding",
		Plural:    "servicerolebindings",
		Versions:  versions,
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/servicerolebindings"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1alpha1")

	b.Add(schema.ResourceSpec{
		Kind:      "ServiceRole",
		ListKind:  "ServiceRoleList",
		Singular:  "servicerole",
		Plural:    "serviceroles",
		Versions:  versions,
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/serviceroles"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1")

	b.Add(schema.ResourceSpec{
		Kind:      "Endpoints",
		ListKind:  "EndpointsList",
		Singular:  "endpoints",
		Plural:    "endpoints",
		Versions:  versions,
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/endpoints"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Namespace",
		ListKind:  "NamespaceList",
		Singular:  "namespace",
		Plural:    "namespaces",
		Versions:  versions,
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/namespaces"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1")

	b.Add(schema.ResourceSpec{
		Kind:      "Node",
		ListKind:  "NodeList",
		Singular:  "node",
		Plural:    "nodes",
		Versions:  versions,
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/nodes"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1")

	b.Add(schema.ResourceSpec{
		Kind:      "Pod",
		ListKind:  "PodList",
		Singular:  "pod",
		Plural:    "pods",
		Versions:  versions,
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/pods"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1")

	b.Add(schema.ResourceSpec{
		Kind:      "Service",
		ListKind:  "ServiceList",
		Singular:  "service",
		Plural:    "services",
		Versions:  versions,
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/services"),
		Converter: converter.Get("identity"),
	})

	versions = make([]string, 0)

	versions = append(versions, "v1beta1")

	b.Add(schema.ResourceSpec{
		Kind:      "Ingress",
		ListKind:  "IngressList",
		Singular:  "ingress",
		Plural:    "ingresses",
		Versions:  versions,
		Group:     "extensions",
		Target:    metadata.Types.Get("k8s/extensions/v1beta1/ingresses"),
		Converter: converter.Get("kube-ingress-resource"),
	})

	Types = b.Build()
}
