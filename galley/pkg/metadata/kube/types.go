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

	b.Add(schema.ResourceSpec{
		Kind:      "MeshPolicy",
		ListKind:  "MeshPolicyList",
		Singular:  "meshpolicy",
		Plural:    "meshpolicies",
		Version:   "v1alpha1",
		Group:     "authentication.istio.io",
		Target:    metadata.Types.Get("istio/authentication/v1alpha1/meshpolicies"),
		Converter: converter.Get("auth-policy-resource"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Policy",
		ListKind:  "PolicyList",
		Singular:  "policy",
		Plural:    "policies",
		Version:   "v1alpha1",
		Group:     "authentication.istio.io",
		Target:    metadata.Types.Get("istio/authentication/v1alpha1/policies"),
		Converter: converter.Get("auth-policy-resource"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "adapter",
		ListKind:  "adapterList",
		Singular:  "adapter",
		Plural:    "adapters",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/adapters"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "HTTPAPISpecBinding",
		ListKind:  "HTTPAPISpecBindingList",
		Singular:  "httpapispecbinding",
		Plural:    "httpapispecbindings",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/httpapispecbindings"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "HTTPAPISpec",
		ListKind:  "HTTPAPISpecList",
		Singular:  "httpapispec",
		Plural:    "httpapispecs",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/httpapispecs"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "template",
		ListKind:  "templateList",
		Singular:  "template",
		Plural:    "templates",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/config/v1alpha2/templates"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "QuotaSpecBinding",
		ListKind:  "QuotaSpecBindingList",
		Singular:  "quotaspecbinding",
		Plural:    "quotaspecbindings",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/mixer/v1/config/client/quotaspecbindings"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "QuotaSpec",
		ListKind:  "QuotaSpecList",
		Singular:  "quotaspec",
		Plural:    "quotaspecs",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/mixer/v1/config/client/quotaspecs"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "DestinationRule",
		ListKind:  "DestinationRuleList",
		Singular:  "destinationrule",
		Plural:    "destinationrules",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/destinationrules"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "EnvoyFilter",
		ListKind:  "EnvoyFilterList",
		Singular:  "envoyfilter",
		Plural:    "envoyfilters",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/envoyfilters"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Gateway",
		ListKind:  "GatewayList",
		Singular:  "gateway",
		Plural:    "gateways",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/gateways"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "ServiceEntry",
		ListKind:  "ServiceEntryList",
		Singular:  "serviceentry",
		Plural:    "serviceentries",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/serviceentries"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Sidecar",
		ListKind:  "SidecarList",
		Singular:  "sidecar",
		Plural:    "sidecars",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/sidecars"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "VirtualService",
		ListKind:  "VirtualServiceList",
		Singular:  "virtualservice",
		Plural:    "virtualservices",
		Version:   "v1alpha3",
		Group:     "networking.istio.io",
		Target:    metadata.Types.Get("istio/networking/v1alpha3/virtualservices"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "attributemanifest",
		ListKind:  "attributemanifestList",
		Singular:  "attributemanifest",
		Plural:    "attributemanifests",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/attributemanifests"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "handler",
		ListKind:  "handlerList",
		Singular:  "handler",
		Plural:    "handlers",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/handlers"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "instance",
		ListKind:  "instanceList",
		Singular:  "instance",
		Plural:    "instances",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/instances"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "rule",
		ListKind:  "ruleList",
		Singular:  "rule",
		Plural:    "rules",
		Version:   "v1alpha2",
		Group:     "config.istio.io",
		Target:    metadata.Types.Get("istio/policy/v1beta1/rules"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "AuthorizationPolicy",
		ListKind:  "AuthorizationPolicyList",
		Singular:  "authorizationpolicy",
		Plural:    "authorizationpolicies",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/authorizationpolicies"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "ClusterRbacConfig",
		ListKind:  "ClusterRbacConfigList",
		Singular:  "clusterrbacconfig",
		Plural:    "clusterrbacconfigs",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/clusterrbacconfigs"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "RbacConfig",
		ListKind:  "RbacConfigList",
		Singular:  "rbacconfig",
		Plural:    "rbacconfigs",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/rbacconfigs"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "ServiceRoleBinding",
		ListKind:  "ServiceRoleBindingList",
		Singular:  "servicerolebinding",
		Plural:    "servicerolebindings",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/servicerolebindings"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "ServiceRole",
		ListKind:  "ServiceRoleList",
		Singular:  "servicerole",
		Plural:    "serviceroles",
		Version:   "v1alpha1",
		Group:     "rbac.istio.io",
		Target:    metadata.Types.Get("istio/rbac/v1alpha1/serviceroles"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Endpoints",
		ListKind:  "EndpointsList",
		Singular:  "endpoints",
		Plural:    "endpoints",
		Version:   "v1",
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/endpoints"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Node",
		ListKind:  "NodeList",
		Singular:  "node",
		Plural:    "nodes",
		Version:   "v1",
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/nodes"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Pod",
		ListKind:  "PodList",
		Singular:  "pod",
		Plural:    "pods",
		Version:   "v1",
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/pods"),
		Converter: converter.Get("identity"),
	})

	b.Add(schema.ResourceSpec{
		Kind:      "Service",
		ListKind:  "ServiceList",
		Singular:  "service",
		Plural:    "services",
		Version:   "v1",
		Group:     "",
		Target:    metadata.Types.Get("k8s/core/v1/services"),
		Converter: converter.Get("kube-service-resource"),
	})

	b.Add(schema.ResourceSpec{
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
