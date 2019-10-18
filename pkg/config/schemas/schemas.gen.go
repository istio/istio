// GENERATED FILE -- DO NOT EDIT

package schemas

import (
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/validation"
)

var (

	// VirtualService describes v1alpha3 route rules
	VirtualService = schema.Instance{
		Type:          "virtual-service",
		Plural:        "virtual-services",
		Group:         "networking",
		Version:       "v1alpha3",
		MessageName:   "istio.networking.v1alpha3.VirtualService",
		Validate:      validation.ValidateVirtualService,
		Collection:    "istio/networking/v1alpha3/virtualservices",
		ClusterScoped: false,
		VariableName:  "VirtualService",
	}

	// Gateway describes a gateway (how a proxy is exposed on the network)
	Gateway = schema.Instance{
		Type:          "gateway",
		Plural:        "gateways",
		Group:         "networking",
		Version:       "v1alpha3",
		MessageName:   "istio.networking.v1alpha3.Gateway",
		Validate:      validation.ValidateGateway,
		Collection:    "istio/networking/v1alpha3/gateways",
		ClusterScoped: false,
		VariableName:  "Gateway",
	}

	// ServiceEntry describes service entries
	ServiceEntry = schema.Instance{
		Type:          "service-entry",
		Plural:        "service-entries",
		Group:         "networking",
		Version:       "v1alpha3",
		MessageName:   "istio.networking.v1alpha3.ServiceEntry",
		Validate:      validation.ValidateServiceEntry,
		Collection:    "istio/networking/v1alpha3/serviceentries",
		ClusterScoped: false,
		VariableName:  "ServiceEntry",
	}

	// SyntheticServiceEntry describes synthetic service entries
	SyntheticServiceEntry = schema.Instance{
		Type:          "synthetic-service-entry",
		Plural:        "synthetic-service-entries",
		Group:         "networking",
		Version:       "v1alpha3",
		MessageName:   "istio.networking.v1alpha3.ServiceEntry",
		Validate:      validation.ValidateSyntheticServiceEntry,
		Collection:    "istio/networking/v1alpha3/synthetic/serviceentries",
		ClusterScoped: true,
		VariableName:  "SyntheticServiceEntry",
	}

	// DestinationRule describes destination rules
	DestinationRule = schema.Instance{
		Type:          "destination-rule",
		Plural:        "destination-rules",
		Group:         "networking",
		Version:       "v1alpha3",
		MessageName:   "istio.networking.v1alpha3.DestinationRule",
		Validate:      validation.ValidateDestinationRule,
		Collection:    "istio/networking/v1alpha3/destinationrules",
		ClusterScoped: false,
		VariableName:  "DestinationRule",
	}

	// EnvoyFilter describes additional envoy filters to be inserted by Pilot
	EnvoyFilter = schema.Instance{
		Type:          "envoy-filter",
		Plural:        "envoy-filters",
		Group:         "networking",
		Version:       "v1alpha3",
		MessageName:   "istio.networking.v1alpha3.EnvoyFilter",
		Validate:      validation.ValidateEnvoyFilter,
		Collection:    "istio/networking/v1alpha3/envoyfilters",
		ClusterScoped: false,
		VariableName:  "EnvoyFilter",
	}

	// Sidecar describes the listeners associated with sidecars in a namespace
	Sidecar = schema.Instance{
		Type:          "sidecar",
		Plural:        "sidecars",
		Group:         "networking",
		Version:       "v1alpha3",
		MessageName:   "istio.networking.v1alpha3.Sidecar",
		Validate:      validation.ValidateSidecar,
		Collection:    "istio/networking/v1alpha3/sidecars",
		ClusterScoped: false,
		VariableName:  "Sidecar",
	}

	// HTTPAPISpec describes an HTTP API specification.
	HTTPAPISpec = schema.Instance{
		Type:          "http-api-spec",
		Plural:        "http-api-specs",
		Group:         "config",
		Version:       "v1alpha2",
		MessageName:   "istio.mixer.v1.config.client.HTTPAPISpec",
		Validate:      validation.ValidateHTTPAPISpec,
		Collection:    "istio/config/v1alpha2/httpapispecs",
		ClusterScoped: false,
		VariableName:  "HTTPAPISpec",
	}

	// HTTPAPISpecBinding describes an HTTP API specification binding.
	HTTPAPISpecBinding = schema.Instance{
		Type:          "http-api-spec-binding",
		Plural:        "http-api-spec-bindings",
		Group:         "config",
		Version:       "v1alpha2",
		MessageName:   "istio.mixer.v1.config.client.HTTPAPISpecBinding",
		Validate:      validation.ValidateHTTPAPISpecBinding,
		Collection:    "istio/config/v1alpha2/httpapispecbindings",
		ClusterScoped: false,
		VariableName:  "HTTPAPISpecBinding",
	}

	// QuotaSpec describes an Quota specification.
	QuotaSpec = schema.Instance{
		Type:          "quota-spec",
		Plural:        "quota-specs",
		Group:         "config",
		Version:       "v1alpha2",
		MessageName:   "istio.mixer.v1.config.client.QuotaSpec",
		Validate:      validation.ValidateQuotaSpec,
		Collection:    "istio/mixer/v1/config/client/quotaspecs",
		ClusterScoped: false,
		VariableName:  "QuotaSpec",
	}

	// QuotaSpecBinding describes an Quota specification binding.
	QuotaSpecBinding = schema.Instance{
		Type:          "quota-spec-binding",
		Plural:        "quota-spec-bindings",
		Group:         "config",
		Version:       "v1alpha2",
		MessageName:   "istio.mixer.v1.config.client.QuotaSpecBinding",
		Validate:      validation.ValidateQuotaSpecBinding,
		Collection:    "istio/mixer/v1/config/client/quotaspecbindings",
		ClusterScoped: false,
		VariableName:  "QuotaSpecBinding",
	}

	// AuthenticationPolicy describes an authentication policy.
	AuthenticationPolicy = schema.Instance{
		Type:          "policy",
		Plural:        "policies",
		Group:         "authentication",
		Version:       "v1alpha1",
		MessageName:   "istio.authentication.v1alpha1.Policy",
		Validate:      validation.ValidateAuthenticationPolicy,
		Collection:    "istio/authentication/v1alpha1/policies",
		ClusterScoped: false,
		VariableName:  "AuthenticationPolicy",
	}

	// AuthenticationMeshPolicy describes an authentication policy at mesh
	// level.
	AuthenticationMeshPolicy = schema.Instance{
		Type:          "mesh-policy",
		Plural:        "mesh-policies",
		Group:         "authentication",
		Version:       "v1alpha1",
		MessageName:   "istio.authentication.v1alpha1.Policy",
		Validate:      validation.ValidateAuthenticationPolicy,
		Collection:    "istio/authentication/v1alpha1/meshpolicies",
		ClusterScoped: true,
		VariableName:  "AuthenticationMeshPolicy",
	}

	// ServiceRole describes an RBAC service role.
	ServiceRole = schema.Instance{
		Type:          "service-role",
		Plural:        "service-roles",
		Group:         "rbac",
		Version:       "v1alpha1",
		MessageName:   "istio.rbac.v1alpha1.ServiceRole",
		Validate:      validation.ValidateServiceRole,
		Collection:    "istio/rbac/v1alpha1/serviceroles",
		ClusterScoped: false,
		VariableName:  "ServiceRole",
	}

	// ServiceRoleBinding describes an RBAC service role.
	ServiceRoleBinding = schema.Instance{
		Type:          "service-role-binding",
		Plural:        "service-role-bindings",
		Group:         "rbac",
		Version:       "v1alpha1",
		MessageName:   "istio.rbac.v1alpha1.ServiceRoleBinding",
		Validate:      validation.ValidateServiceRoleBinding,
		Collection:    "istio/rbac/v1alpha1/servicerolebindings",
		ClusterScoped: false,
		VariableName:  "ServiceRoleBinding",
	}

	// RbacConfig describes the mesh level RBAC config.
	// Deprecated: use ClusterRbacConfig instead.
	// See https://github.com/istio/istio/issues/8825 for more details.
	RbacConfig = schema.Instance{
		Type:          "rbac-config",
		Plural:        "rbac-configs",
		Group:         "rbac",
		Version:       "v1alpha1",
		MessageName:   "istio.rbac.v1alpha1.RbacConfig",
		Validate:      validation.ValidateRbacConfig,
		Collection:    "istio/rbac/v1alpha1/rbacconfigs",
		ClusterScoped: false,
		VariableName:  "RbacConfig",
	}

	// ClusterRbacConfig describes the cluster level RBAC config.
	ClusterRbacConfig = schema.Instance{
		Type:          "cluster-rbac-config",
		Plural:        "clusterrbacconfigs",
		Group:         "rbac",
		Version:       "v1alpha1",
		MessageName:   "istio.rbac.v1alpha1.RbacConfig",
		Validate:      validation.ValidateClusterRbacConfig,
		Collection:    "istio/rbac/v1alpha1/clusterrbacconfigs",
		ClusterScoped: true,
		VariableName:  "ClusterRbacConfig",
	}

	// AuthorizationPolicy describes the authorization policy.
	AuthorizationPolicy = schema.Instance{
		Type:          "authorization-policy",
		Plural:        "authorizationpolicies",
		Group:         "security",
		Version:       "v1beta1",
		MessageName:   "istio.security.v1beta1.AuthorizationPolicy",
		Validate:      validation.ValidateAuthorizationPolicy,
		Collection:    "istio/security/v1beta1/authorizationpolicies",
		ClusterScoped: false,
		VariableName:  "AuthorizationPolicy",
	}

	// Istio lists all Istio schemas.
	Istio = schema.Set{
		VirtualService,
		Gateway,
		ServiceEntry,
		SyntheticServiceEntry,
		DestinationRule,
		EnvoyFilter,
		Sidecar,
		HTTPAPISpec,
		HTTPAPISpecBinding,
		QuotaSpec,
		QuotaSpecBinding,
		AuthenticationPolicy,
		AuthenticationMeshPolicy,
		ServiceRole,
		ServiceRoleBinding,
		RbacConfig,
		ClusterRbacConfig,
		AuthorizationPolicy,
	}
)
