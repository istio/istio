// Copyright 2019 Istio Authors
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

// Package authz converts Istio authorization policies (ServiceRole and AuthorizationPolicy)
// to corresponding filter config that is used by the envoy RBAC filter to enforce access control to
// the service co-located with envoy.
// Currently the config is only generated for sidecar node on inbound HTTP/TCP listener. The generation
// is controlled by ClusterRbacConfig (a singleton custom resource with cluster scope). User could disable
// this plugin by either deleting the ClusterRbacConfig or set the ClusterRbacConfig.mode to OFF.
// Note: ClusterRbacConfig is not created with default istio installation which means this plugin doesn't
// generate any RBAC config by default.
//
// Changes from rbac_v2.go compared to rbac.go:
// * Deprecate ServiceRoleBinding. Only support two CRDs: ServiceRole and AuthorizationPolicy.
// * Allow multiple bindings and roles in one CRD, i.e. Authorization.
// * Support workload selector.
package authz

import (
	"fmt"
	"strings"

	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	policyproto "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

const (
	// rootNamespacePrefix is the prefix of the root namespace. This is used to refer to ServiceRoles
	// that are defined in the root namespace.
	// For more details, check out root_namespace field at https://github.com/istio/api/blob/master/mesh/v1alpha1/config.proto
	rootNamespacePrefix = "/"
)

// convertRbacRulesToFilterConfigV2 is the successor of convertRbacRulesToFilterConfig, which supports
// converting AuthorizationPolicy.
func convertRbacRulesToFilterConfigV2(service *serviceMetadata, option rbacOption) *http_config.RBAC {
	rbac := &policyproto.RBAC{
		Action:   policyproto.RBAC_ALLOW,
		Policies: map[string]*policyproto.Policy{},
	}

	if option.globalPermissiveMode {
		// Permissive Mode will be ignored.
		rbacLog.Warnf("Permissive mode is not supported for service %s.", service.name)
		// TODO(pitlv2109): Handle permissive mode in the future.
		return &http_config.RBAC{Rules: rbac}
	}

	namespace := service.attributes[attrDestNamespace]
	if option.authzPolicies == nil || option.authzPolicies.NamespaceToAuthorizationConfigV2 == nil {
		return &http_config.RBAC{Rules: rbac}
	}
	var authorizationConfigV2FromNamespace, present = option.authzPolicies.NamespaceToAuthorizationConfigV2[namespace]
	if !present {
		return &http_config.RBAC{Rules: rbac}
	}
	// Get all AuthorizationPolicy Istio config from this namespace.
	allAuthzPolicies := authorizationConfigV2FromNamespace.AuthzPolicies
	for _, authzPolicy := range allAuthzPolicies {
		// If a WorkloadSelector is used in the AuthorizationPolicy config and does not match this service, skip this
		// AuthorizationPolicy.
		serviceLabelsCollection := model.LabelsCollection{service.labels}
		if authzPolicy.Policy.WorkloadSelector != nil &&
			!(serviceLabelsCollection.IsSupersetOf(authzPolicy.Policy.WorkloadSelector.Labels)) {
			continue
		}
		for i, binding := range authzPolicy.Policy.Allow {
			permissions := make([]*policyproto.Permission, 0)
			var role *rbacproto.ServiceRole
			var serviceRoleName string
			if binding.RoleRef != nil {
				role = option.authzPolicies.RoleForNameAndNamespace(binding.RoleRef.Name, namespace)
				serviceRoleName = binding.RoleRef.Name
			} else if binding.Role != "" {
				if strings.HasPrefix(binding.Role, rootNamespacePrefix) {
					globalRoleName := strings.TrimPrefix(binding.Role, rootNamespacePrefix)
					role = option.authzPolicies.RoleForNameAndNamespace(globalRoleName, model.DefaultMeshConfig().RootNamespace)
				} else {
					role = option.authzPolicies.RoleForNameAndNamespace(binding.Role, namespace)
				}
				serviceRoleName = binding.Role
			} else if len(binding.Actions) > 0 {
				role = &rbacproto.ServiceRole{Rules: binding.Actions}
				serviceRoleName = fmt.Sprintf("%s-inline-role", authzPolicy.Name)
			}
			for _, rule := range role.Rules {
				// Check to make sure that the current service (caller) is the one this rule is applying to.
				if !service.areConstraintsMatched(rule) {
					rbacLog.Debugf("rule has constraints but doesn't match with the service (found in %s)", serviceRoleName)
					continue
				}
				if option.forTCPFilter {
					// TODO(yangminzhu): Add metrics.
					if err := validateRuleForTCPFilter(rule); err != nil {
						// It's a user misconfiguration if a HTTP rule is specified to a TCP service.
						// For safety consideration, we ignore the whole rule which means no access is opened to
						// the TCP service in this case.
						rbacLog.Debugf("rules[%d] ignored, found HTTP only rule for a TCP service: %v", i, err)
						continue
					}
				}
				// Generate the policy if the service is matched and validated to the services specified in
				// ServiceRole.
				permissions = append(permissions, convertToPermission(rule))
			}
			if len(permissions) == 0 {
				rbacLog.Debugf("role %s skipped for no rule matched", serviceRoleName)
				continue
			}

			if option.forTCPFilter {
				if err := validateBindingsForTCPFilter([]*rbacproto.ServiceRoleBinding{binding}); err != nil {
					rbacLog.Debugf("role %s skipped, found HTTP only binding for a TCP service: %v", serviceRoleName, err)
					continue
				}
			}

			enforcedPrincipals, _ := convertToPrincipals([]*rbacproto.ServiceRoleBinding{binding}, option.forTCPFilter)

			if len(enforcedPrincipals) == 0 {
				rbacLog.Debugf("role %s skipped for no principals found", serviceRoleName)
				continue
			}

			policyName := fmt.Sprintf("authz-policy-%s-allow[%d]", authzPolicy.Name, i)
			rbac.Policies[policyName] = &policyproto.Policy{
				Permissions: permissions,
				Principals:  enforcedPrincipals,
			}
		}
	}
	return &http_config.RBAC{Rules: rbac}
}
