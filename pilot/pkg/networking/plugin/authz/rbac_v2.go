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

// Package authz converts Istio RBAC (role-based-access-control) policies (ServiceRole and AuthorizationPolicy)
// to corresponding filter config that is used by the envoy RBAC filter to enforce access control to
// the service co-located with envoy.
// Currently the config is only generated for sidecar node on inbound HTTP/TCP listener. The generation
// is controlled by RbacConfig (a singleton custom resource with cluster scope). User could disable
// this plugin by either deleting the ClusterRbacConfig or set the ClusterRbacConfig.mode to OFF.
// Note: ClusterRbacConfig is not created with default istio installation which means this plugin doesn't
// generate any RBAC config by default.
package authz

import (
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
		// TODO(pitlv2109): Handle permissive mode in the future.
		return &http_config.RBAC{Rules: rbac}
	}

	namespace := service.attributes[attrDestNamespace]

	// Get all AuthorizationPolicy Istio config from this namespace.
	allAuthzPolicies := option.authzPolicies.NamespaceToAuthorizationConfigV2[namespace].AuthzPolicies
	// Get all key-value pairs of AuthorizationPolicy name to list of Subjects/ServiceRoleBindings in this namespace.
	authzPolicyToSubjects := option.authzPolicies.AuthzPolicyToAllowSubjects(namespace)
	for _, authzPolicy := range allAuthzPolicies {
		// If a WorkloadSelector is used in the AuthorizationPolicy config and does not match this service, skip this
		// AuthorizationPolicy.
		if authzPolicy.Policy.WorkloadSelector != nil && !service.isServiceMatchedWorkload(authzPolicy.Policy.WorkloadSelector.Labels) {
			continue
		}
		for i, subject := range authzPolicyToSubjects[authzPolicy.Name] {
			// Convert role.
			role := option.authzPolicies.GetServiceRoleFromName(namespace, subject.RoleRef.Name)
			permission := &policyproto.Permission{}
			rule := role.Rules[0]

			// Check to make sure that the current service (caller) is the one this rule is applying to.
			if !service.match(rule) {
				continue
			}
			// Also check if we have converted this rule before.
			if !hasConvertedPermission(rbac, subject.RoleRef.Name) {
				rbacLog.Debugf("rules[%d] matched", i)
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
				permission = convertToPermission(rule)
				if permission.String() == "" {
					rbacLog.Debugf("role %s skipped for no rule matched", subject.RoleRef.Name)
					continue
				}
			}

			if option.forTCPFilter {
				if err := validateBindingsForTCPFilter([]*rbacproto.ServiceRoleBinding{subject}); err != nil {
					rbacLog.Debugf("role %s skipped, found HTTP only binding for a TCP service: %v", subject.RoleRef.Name, err)
					continue
				}
			}
			// Convert binding.
			enforcedPrincipals, _ := convertToPrincipals([]*rbacproto.ServiceRoleBinding{subject}, option.forTCPFilter)

			if len(enforcedPrincipals) == 0 {
				rbacLog.Debugf("role %s skipped for no principals found", subject.RoleRef.Name)
				continue
			}

			if len(enforcedPrincipals) != 0 {
				if rbac.Policies[subject.RoleRef.Name] == nil {
					rbac.Policies[subject.RoleRef.Name] = &policyproto.Policy{
						Permissions: []*policyproto.Permission{permission},
						Principals:  enforcedPrincipals,
					}
				} else {
					allPrincipals := rbac.Policies[subject.RoleRef.Name].Principals
					// Check if enforcedPrincipals has existed in the allPrincipals.
					if !isPrincipalInPrincipals(enforcedPrincipals, allPrincipals) {
						rbac.Policies[subject.RoleRef.Name].Principals = append(allPrincipals, enforcedPrincipals...)
					}
				}
			}
		}
	}
	return &http_config.RBAC{Rules: rbac}
}

// isServiceMatchedWorkload checks if the calling service is matched with the selector in the AuthorizationPolicy if
// present.
func (service serviceMetadata) isServiceMatchedWorkload(labels map[string]string) bool {
	workloadLabels := model.LabelsCollection{labels}
	return workloadLabels.HasSubsetOf(service.labels)
}

func isRbacV2(namespace string, option rbacOption) bool {
	// Check to make sure we are not dereferencing null pointers.
	if option.authzPolicies == nil || option.authzPolicies.NamespaceToAuthorizationConfigV2 == nil {
		return false
	}
	if _, present := option.authzPolicies.NamespaceToAuthorizationConfigV2[namespace]; !present {
		return false
	}

	// Get all AuthorizationPolicy Istio config from this namespace.
	allAuthzPolicies := option.authzPolicies.NamespaceToAuthorizationConfigV2[namespace].AuthzPolicies
	return len(allAuthzPolicies) > 0
}

// isConflictingRbacVersion is called when the RBAC filter is supposed to use RBAC v2 (with AuthorizationPolicy).
// It checks to see if ServiceRoleBinding still exists. If so, return True; False otherwise.
func isConflictingRbacVersion(namespace string, option rbacOption) bool {
	return len(option.authzPolicies.RoleToBindingsForNamespace(namespace)) > 0
}

// isPrincipalInPrincipals returns True if the |principal| has already existed in the |principals|,
// False otherwise.
// This is to make sure we don't have duplicated principals for the same ServiceRole.
// This can happen when we have a service foo that has more than one exact Subjects (in one or many
// AuthorizationPolicy) that bind to the same ServiceRole.
func isPrincipalInPrincipals(principal []*policyproto.Principal, principals []*policyproto.Principal) bool {
	for _, currPrincipal := range principals {
		if currPrincipal.String() == principal[0].String() {
			return true
		}
	}
	return false
}

// hasConvertedPermission returns True if there is already a |roleName| ServiceRole for |rbac|, False
// otherwise.
func hasConvertedPermission(rbac *policyproto.RBAC, roleName string) bool {
	if rbac.Policies[roleName] == nil {
		return false
	}
	return rbac.Policies[roleName].Permissions != nil
}
