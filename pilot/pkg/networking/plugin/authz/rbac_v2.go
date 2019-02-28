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
	"strconv"
	"strings"

	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	policyproto "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
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
		rbacLog.Debugf("Permissive mode is not supported for service %s.", service.name)
		// TODO(pitlv2109): Handle permissive mode in the future.
		return &http_config.RBAC{Rules: rbac}
	}

	namespace := service.attributes[attrDestNamespace]
	// Check to make sure we are not dereferencing null pointers.
	if option.authzPolicies == nil || option.authzPolicies.NamespaceToAuthorizationConfigV2 == nil {
		return &http_config.RBAC{Rules: rbac}
	}
	if _, present := option.authzPolicies.NamespaceToAuthorizationConfigV2[namespace]; !present {
		return &http_config.RBAC{Rules: rbac}
	}
	// Get all AuthorizationPolicy Istio config from this namespace.
	allAuthzPolicies := option.authzPolicies.NamespaceToAuthorizationConfigV2[namespace].AuthzPolicies
	// Get all key-value pIstioairs of AuthorizationPolicy name to list of ServiceRoleBindings in this namespace.
	for _, authzPolicy := range allAuthzPolicies {
		// If a WorkloadSelector is used in the AuthorizationPolicy config and does not match this service, skip this
		// AuthorizationPolicy.
		if authzPolicy.Policy.WorkloadSelector != nil &&
			!(model.LabelsCollection{service.labels}.IsSupersetOf(authzPolicy.Policy.WorkloadSelector.Labels)) {
			continue
		}
		for i, binding := range authzPolicy.Policy.Allow {
			// Check if we have converted this ServiceRole before.
			permissions := make([]*policyproto.Permission, 0)
			if !hasConvertedPermission(rbac, binding.RoleRef.Name) {
				// Convert role.
				role := option.authzPolicies.RoleForNameAndNamespace(namespace, binding.RoleRef.Name)
				for _, rule := range role.Rules {
					// Check to make sure the services field does not exist in the ServiceRole.
					if len(rule.Services) > 0 {
						rbacLog.Errorf("should not have services field in ServiceRole (found in %s)", binding.RoleRef.Name)
						continue
					}
					// Check to make sure that the current service (caller) is the one this rule is applying to.
					if !service.areConstraintsMatched(rule) {
						continue
					}
					if option.forTCPFilter {
						// TODO(yangminzhu): Move the validate logic to push context and add metrics.
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
					rbacLog.Debugf("role %s skipped for no rule matched", binding.RoleRef.Name)
					continue
				}
			}

			if option.forTCPFilter {
				if err := validateBindingsForTCPFilter([]*rbacproto.ServiceRoleBinding{binding}); err != nil {
					rbacLog.Debugf("role %s skipped, found HTTP only binding for a TCP service: %v", binding.RoleRef.Name, err)
					continue
				}
			}
			// Convert binding.
			enforcedPrincipals, _ := convertToPrincipals([]*rbacproto.ServiceRoleBinding{binding}, option.forTCPFilter)

			if len(enforcedPrincipals) == 0 {
				rbacLog.Debugf("role %s skipped for no principals found", binding.RoleRef.Name)
				continue
			}

			policyName := "authz-policy-" + authzPolicy.Name + "-allow-" + strconv.Itoa(i)
			rbac.Policies[policyName] = &policyproto.Policy{
				Permissions: permissions,
				Principals:  enforcedPrincipals,
			}
		}
	}
	return &http_config.RBAC{Rules: rbac}
}

// hasConvertedPermission returns True if there is already a |roleName| ServiceRole for |rbac|, False
// otherwise.
func hasConvertedPermission(rbac *policyproto.RBAC, roleName string) bool {
	if rbac.Policies[roleName] == nil {
		return false
	}
	return rbac.Policies[roleName].Permissions != nil
}

// areConstraintsMatched returns True if the calling service's attributes and/or labels match to
// the ServiceRole constraints.
func (service serviceMetadata) areConstraintsMatched(rule *rbacproto.AccessRule) bool {
	for _, constraint := range rule.Constraints {
		if !attributesEnforcedInPlugin(constraint.Key) {
			continue
		}

		var actualValue string
		var present bool
		if strings.HasPrefix(constraint.Key, attrDestLabel) {
			consLabel, err := extractNameInBrackets(strings.TrimPrefix(constraint.Key, attrDestLabel))
			if err != nil {
				rbacLog.Errorf("ignored invalid %s: %v", attrDestLabel, err)
				continue
			}
			actualValue, present = service.labels[consLabel]
		} else {
			actualValue, present = service.attributes[constraint.Key]
		}
		// The constraint is not matched if any of the follow condition is true:
		// a) the constraint is specified but not found in the serviceMetadata;
		// b) the constraint value is not matched to the actual value;
		if !present || !stringMatch(actualValue, constraint.Values) {
			return false
		}
	}
	return true
}
