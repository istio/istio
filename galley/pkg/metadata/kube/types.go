// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh kube pkg/metadata/kube/types.go
//

package kube

import (
	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/converter"
	"istio.io/istio/galley/pkg/metadata"
)

// Types in the schema.
var Types *kube.Schema

func init() {
	b := kube.NewSchemaBuilder()

	b.Add(kube.ResourceSpec{
		Kind:      "MeshPolicy",
		ListKind:  "MeshPolicyList",
		Singular:  "meshpolicy",
		Plural:    "meshpolicies",
		Version:   "v1alpha1",
		Group:     "authentication.istio.io",
		Target:    metadata.Types.Get("istio/authentication/v1alpha1/meshpolicies"),
		Converter: converter.Get("auth-policy-resource"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "Policy",
		ListKind:  "PolicyList",
		Singular:  "policy",
		Plural:    "policies",
		Version:   "v1alpha1",
		Group:     "authentication.istio.io",
		Target:    metadata.Types.Get("istio/authentication/v1alpha1/policies"),
		Converter: converter.Get("auth-policy-resource"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "adapter",
		ListKind:  "adapterList",
		Singular:  "adapter",
		Plural:    "adapters",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/adapters"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "HTTPAPISpecBinding",
		ListKind:  "HTTPAPISpecBindingList",
		Singular:  "httpapispecbinding",
		Plural:    "httpapispecbindings",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/httpapispecbindings"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "HTTPAPISpec",
		ListKind:  "HTTPAPISpecList",
		Singular:  "httpapispec",
		Plural:    "httpapispecs",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/httpapispecs"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "apikey",
		ListKind:  "apikeyList",
		Singular:  "apikey",
		Plural:    "apikeys",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/apikeys"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "authorization",
		ListKind:  "authorizationList",
		Singular:  "authorization",
		Plural:    "authorizations",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/authorizations"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "bypass",
		ListKind:  "bypassList",
		Singular:  "bypass",
		Plural:    "bypasses",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/bypasses"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "checknothing",
		ListKind:  "checknothingList",
		Singular:  "checknothing",
		Plural:    "checknothings",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/checknothings"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "circonus",
		ListKind:  "circonusList",
		Singular:  "circonus",
		Plural:    "circonuses",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/circonuses"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "cloudwatch",
		ListKind:  "cloudwatchList",
		Singular:  "cloudwatch",
		Plural:    "cloudwatches",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/cloudwatches"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "denier",
		ListKind:  "denierList",
		Singular:  "denier",
		Plural:    "deniers",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/deniers"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "dogstatsd",
		ListKind:  "dogstatsdList",
		Singular:  "dogstatsd",
		Plural:    "dogstatsds",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/dogstatsds"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "edge",
		ListKind:  "edgeList",
		Singular:  "edge",
		Plural:    "edges",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/edges"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "fluentd",
		ListKind:  "fluentdList",
		Singular:  "fluentd",
		Plural:    "fluentds",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/fluentds"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "kubernetesenv",
		ListKind:  "kubernetesenvList",
		Singular:  "kubernetesenv",
		Plural:    "kubernetesenvs",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/kubernetesenvs"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "kubernetes",
		ListKind:  "kubernetesList",
		Singular:  "kubernetes",
		Plural:    "kuberneteses",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/kuberneteses"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "listchecker",
		ListKind:  "listcheckerList",
		Singular:  "listchecker",
		Plural:    "listcheckers",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/listcheckers"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "listentry",
		ListKind:  "listentryList",
		Singular:  "listentry",
		Plural:    "listentries",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/listentries"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "logentry",
		ListKind:  "logentryList",
		Singular:  "logentry",
		Plural:    "logentries",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/logentries"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "memquota",
		ListKind:  "memquotaList",
		Singular:  "memquota",
		Plural:    "memquotas",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/memquotas"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "metric",
		ListKind:  "metricList",
		Singular:  "metric",
		Plural:    "metrics",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/metrics"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "noop",
		ListKind:  "noopList",
		Singular:  "noop",
		Plural:    "noops",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/noops"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "opa",
		ListKind:  "opaList",
		Singular:  "opa",
		Plural:    "opas",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/opas"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "prometheus",
		ListKind:  "prometheusList",
		Singular:  "prometheus",
		Plural:    "prometheuses",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/prometheuses"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "quota",
		ListKind:  "quotaList",
		Singular:  "quota",
		Plural:    "quotas",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/quotas"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "rbac",
		ListKind:  "rbacList",
		Singular:  "rbac",
		Plural:    "rbacs",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/rbacs"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "redisquota",
		ListKind:  "redisquotaList",
		Singular:  "redisquota",
		Plural:    "redisquotas",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/redisquotas"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "reportnothing",
		ListKind:  "reportnothingList",
		Singular:  "reportnothing",
		Plural:    "reportnothings",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/reportnothings"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "servicecontrolreport",
		ListKind:  "servicecontrolreportList",
		Singular:  "servicecontrolreport",
		Plural:    "servicecontrolreports",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/servicecontrolreports"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "servicecontrol",
		ListKind:  "servicecontrolList",
		Singular:  "servicecontrol",
		Plural:    "servicecontrols",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/servicecontrols"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "signalfx",
		ListKind:  "signalfxList",
		Singular:  "signalfx",
		Plural:    "signalfxs",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/signalfxs"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "solarwinds",
		ListKind:  "solarwindsList",
		Singular:  "solarwinds",
		Plural:    "solarwindses",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/solarwindses"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "stackdriver",
		ListKind:  "stackdriverList",
		Singular:  "stackdriver",
		Plural:    "stackdrivers",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/stackdrivers"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "statsd",
		ListKind:  "statsdList",
		Singular:  "statsd",
		Plural:    "statsds",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/statsds"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "stdio",
		ListKind:  "stdioList",
		Singular:  "stdio",
		Plural:    "stdios",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/stdios"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "tracespan",
		ListKind:  "tracespanList",
		Singular:  "tracespan",
		Plural:    "tracespans",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/legacy/tracespans"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "template",
		ListKind:  "templateList",
		Singular:  "template",
		Plural:    "templates",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/templates"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "QuotaSpecBinding",
		ListKind:  "QuotaSpecBindingList",
		Singular:  "quotaspecbinding",
		Plural:    "quotaspecbindings",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/mixer/v1/config/client/quotaspecbindings"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "QuotaSpec",
		ListKind:  "QuotaSpecList",
		Singular:  "quotaspec",
		Plural:    "quotaspecs",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/mixer/v1/config/client/quotaspecs"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "DestinationRule",
		ListKind:  "DestinationRuleList",
		Singular:  "destinationrule",
		Plural:    "destinationrules",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/destinationrules"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "EnvoyFilter",
		ListKind:  "EnvoyFilterList",
		Singular:  "envoyfilter",
		Plural:    "envoyfilters",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/envoyfilters"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "Gateway",
		ListKind:  "GatewayList",
		Singular:  "gateway",
		Plural:    "gateways",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/gateways"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "ServiceEntry",
		ListKind:  "ServiceEntryList",
		Singular:  "serviceentry",
		Plural:    "serviceentries",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/serviceentries"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "Sidecar",
		ListKind:  "SidecarList",
		Singular:  "sidecar",
		Plural:    "sidecars",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/sidecars"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "VirtualService",
		ListKind:  "VirtualServiceList",
		Singular:  "virtualservice",
		Plural:    "virtualservices",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/virtualservices"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "attributemanifest",
		ListKind:  "attributemanifestList",
		Singular:  "attributemanifest",
		Plural:    "attributemanifests",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/attributemanifests"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "handler",
		ListKind:  "handlerList",
		Singular:  "handler",
		Plural:    "handlers",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/handlers"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "instance",
		ListKind:  "instanceList",
		Singular:  "instance",
		Plural:    "instances",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/instances"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "rule",
		ListKind:  "ruleList",
		Singular:  "rule",
		Plural:    "rules",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/rules"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "ClusterRbacConfig",
		ListKind:  "ClusterRbacConfigList",
		Singular:  "clusterrbacconfig",
		Plural:    "clusterrbacconfigs",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/clusterrbacconfigs"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "RbacConfig",
		ListKind:  "RbacConfigList",
		Singular:  "rbacconfig",
		Plural:    "rbacconfigs",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/rbacconfigs"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "ServiceRoleBinding",
		ListKind:  "ServiceRoleBindingList",
		Singular:  "servicerolebinding",
		Plural:    "servicerolebindings",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/servicerolebindings"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "ServiceRole",
		ListKind:  "ServiceRoleList",
		Singular:  "servicerole",
		Plural:    "serviceroles",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/serviceroles"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "Node",
		ListKind:  "NodeList",
		Singular:  "node",
		Plural:    "nodes",
		Version:   "v1",
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/nodes"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "Pod",
		ListKind:  "PodList",
		Singular:  "pod",
		Plural:    "pods",
		Version:   "v1",
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/pods"),
		Converter: converter.Get("identity"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "Service",
		ListKind:  "ServiceList",
		Singular:  "service",
		Plural:    "services",
		Version:   "v1",
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/services"),
		Converter: converter.Get("kube-service-resource"),
	})

	b.Add(kube.ResourceSpec{
		Kind:      "Ingress",
		ListKind:  "IngressList",
		Singular:  "ingress",
		Plural:    "ingresses",
		Version:   "v1beta1",
		Group:     "extensions",
		Target:    metadata.Types.Get("k8s/extensions/v1beta1/ingresses"),
		Converter: converter.Get("kube-ingress-resource"),
	})

	Types = b.Build()
}
