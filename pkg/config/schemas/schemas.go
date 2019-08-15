// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package schemas

import (
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/validation"
)

const (
	// Default API version of an Istio config proto message.
	istioAPIVersion = "v1alpha2"
)

var (
	// VirtualService describes v1alpha3 route rules
	VirtualService = schema.Instance{
		Type:        "virtual-service",
		Plural:      "virtual-services",
		Group:       "networking",
		Version:     "v1alpha3",
		MessageName: "istio.networking.v1alpha3.VirtualService",
		Validate:    validation.ValidateVirtualService,
		Collection:  metadata.IstioNetworkingV1alpha3Virtualservices.Collection.String(),
	}

	// Gateway describes a gateway (how a proxy is exposed on the network)
	Gateway = schema.Instance{
		Type:        "gateway",
		Plural:      "gateways",
		Group:       "networking",
		Version:     "v1alpha3",
		MessageName: "istio.networking.v1alpha3.Gateway",
		Validate:    validation.ValidateGateway,
		Collection:  metadata.IstioNetworkingV1alpha3Gateways.Collection.String(),
	}

	// ServiceEntry describes service entries
	ServiceEntry = schema.Instance{
		Type:        "service-entry",
		Plural:      "service-entries",
		Group:       "networking",
		Version:     "v1alpha3",
		MessageName: "istio.networking.v1alpha3.ServiceEntry",
		Validate:    validation.ValidateServiceEntry,
		Collection:  metadata.IstioNetworkingV1alpha3Serviceentries.Collection.String(),
	}

	// DestinationRule describes destination rules
	DestinationRule = schema.Instance{
		Type:        "destination-rule",
		Plural:      "destination-rules",
		Group:       "networking",
		Version:     "v1alpha3",
		MessageName: "istio.networking.v1alpha3.DestinationRule",
		Validate:    validation.ValidateDestinationRule,
		Collection:  metadata.IstioNetworkingV1alpha3Destinationrules.Collection.String(),
	}

	// EnvoyFilter describes additional envoy filters to be inserted by Pilot
	EnvoyFilter = schema.Instance{
		Type:        "envoy-filter",
		Plural:      "envoy-filters",
		Group:       "networking",
		Version:     "v1alpha3",
		MessageName: "istio.networking.v1alpha3.EnvoyFilter",
		Validate:    validation.ValidateEnvoyFilter,
		Collection:  metadata.IstioNetworkingV1alpha3Envoyfilters.Collection.String(),
	}

	// Sidecar describes the listeners associated with sidecars in a namespace
	Sidecar = schema.Instance{
		Type:        "sidecar",
		Plural:      "sidecars",
		Group:       "networking",
		Version:     "v1alpha3",
		MessageName: "istio.networking.v1alpha3.Sidecar",
		Validate:    validation.ValidateSidecar,
		Collection:  metadata.IstioNetworkingV1alpha3Sidecars.Collection.String(),
	}

	// HTTPAPISpec describes an HTTP API specification.
	HTTPAPISpec = schema.Instance{
		Type:        "http-api-spec",
		Plural:      "http-api-specs",
		Group:       "config",
		Version:     istioAPIVersion,
		MessageName: "istio.mixer.v1.config.client.HTTPAPISpec",
		Validate:    validation.ValidateHTTPAPISpec,
		Collection:  metadata.IstioConfigV1alpha2Httpapispecs.Collection.String(),
	}

	// HTTPAPISpecBinding describes an HTTP API specification binding.
	HTTPAPISpecBinding = schema.Instance{
		Type:        "http-api-spec-binding",
		Plural:      "http-api-spec-bindings",
		Group:       "config",
		Version:     istioAPIVersion,
		MessageName: "istio.mixer.v1.config.client.HTTPAPISpecBinding",
		Validate:    validation.ValidateHTTPAPISpecBinding,
		Collection:  metadata.IstioConfigV1alpha2Httpapispecbindings.Collection.String(),
	}

	// QuotaSpec describes an Quota specification.
	QuotaSpec = schema.Instance{
		Type:        "quota-spec",
		Plural:      "quota-specs",
		Group:       "config",
		Version:     istioAPIVersion,
		MessageName: "istio.mixer.v1.config.client.QuotaSpec",
		Validate:    validation.ValidateQuotaSpec,
		Collection:  metadata.IstioMixerV1ConfigClientQuotaspecs.Collection.String(),
	}

	// QuotaSpecBinding describes an Quota specification binding.
	QuotaSpecBinding = schema.Instance{
		Type:        "quota-spec-binding",
		Plural:      "quota-spec-bindings",
		Group:       "config",
		Version:     istioAPIVersion,
		MessageName: "istio.mixer.v1.config.client.QuotaSpecBinding",
		Validate:    validation.ValidateQuotaSpecBinding,
		Collection:  metadata.IstioMixerV1ConfigClientQuotaspecbindings.Collection.String(),
	}

	// AuthenticationPolicy describes an authentication policy.
	AuthenticationPolicy = schema.Instance{
		VariableName: "AuthenticationPolicy",
		Type:         "policy",
		Plural:       "policies",
		Group:        "authentication",
		Version:      "v1alpha1",
		MessageName:  "istio.authentication.v1alpha1.Policy",
		Validate:     validation.ValidateAuthenticationPolicy,
		Collection:   metadata.IstioAuthenticationV1alpha1Policies.Collection.String(),
	}

	// AuthenticationMeshPolicy describes an authentication policy at mesh level.
	AuthenticationMeshPolicy = schema.Instance{
		ClusterScoped: true,
		VariableName:  "AuthenticationMeshPolicy",
		Type:          "mesh-policy",
		Plural:        "mesh-policies",
		Group:         "authentication",
		Version:       "v1alpha1",
		MessageName:   "istio.authentication.v1alpha1.Policy",
		Validate:      validation.ValidateAuthenticationPolicy,
		Collection:    metadata.IstioAuthenticationV1alpha1Meshpolicies.Collection.String(),
	}

	// ServiceRole describes an RBAC service role.
	ServiceRole = schema.Instance{
		Type:        "service-role",
		Plural:      "service-roles",
		Group:       "rbac",
		Version:     "v1alpha1",
		MessageName: "istio.rbac.v1alpha1.ServiceRole",
		Validate:    validation.ValidateServiceRole,
		Collection:  metadata.IstioRbacV1alpha1Serviceroles.Collection.String(),
	}

	// ServiceRoleBinding describes an RBAC service role.
	ServiceRoleBinding = schema.Instance{
		ClusterScoped: false,
		Type:          "service-role-binding",
		Plural:        "service-role-bindings",
		Group:         "rbac",
		Version:       "v1alpha1",
		MessageName:   "istio.rbac.v1alpha1.ServiceRoleBinding",
		Validate:      validation.ValidateServiceRoleBinding,
		Collection:    metadata.IstioRbacV1alpha1Servicerolebindings.Collection.String(),
	}

	// RbacConfig describes the mesh level RBAC config.
	// Deprecated: use ClusterRbacConfig instead.
	// See https://github.com/istio/istio/issues/8825 for more details.
	RbacConfig = schema.Instance{
		Type:        "rbac-config",
		Plural:      "rbac-configs",
		Group:       "rbac",
		Version:     "v1alpha1",
		MessageName: "istio.rbac.v1alpha1.RbacConfig",
		Validate:    validation.ValidateRbacConfig,
		Collection:  metadata.IstioRbacV1alpha1Rbacconfigs.Collection.String(),
	}

	// ClusterRbacConfig describes the cluster level RBAC config.
	ClusterRbacConfig = schema.Instance{
		ClusterScoped: true,
		Type:          "cluster-rbac-config",
		Plural:        "cluster-rbac-configs",
		Group:         "rbac",
		Version:       "v1alpha1",
		MessageName:   "istio.rbac.v1alpha1.RbacConfig",
		Validate:      validation.ValidateClusterRbacConfig,
		Collection:    metadata.IstioRbacV1alpha1Clusterrbacconfigs.Collection.String(),
	}

	// Istio lists all Istio schemas.
	Istio = schema.Set{
		VirtualService,
		Gateway,
		ServiceEntry,
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
	}
)
