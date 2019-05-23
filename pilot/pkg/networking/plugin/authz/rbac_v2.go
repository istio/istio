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

// TODO: refactor this into model.AuthorizationPolicies.
func roleForBinding(binding *rbacproto.ServiceRoleBinding, namespace string, ap *model.AuthorizationPolicies) (string, *rbacproto.ServiceRole) {
	var role *rbacproto.ServiceRole
	var name string
	if binding.RoleRef != nil {
		role = ap.RoleForNameAndNamespace(binding.RoleRef.Name, namespace)
		name = binding.RoleRef.Name
	} else if binding.Role != "" {
		if strings.HasPrefix(binding.Role, rootNamespacePrefix) {
			globalRoleName := strings.TrimPrefix(binding.Role, rootNamespacePrefix)
			role = ap.RoleForNameAndNamespace(globalRoleName, model.DefaultMeshConfig().RootNamespace)
		} else {
			role = ap.RoleForNameAndNamespace(binding.Role, namespace)
		}
		name = binding.Role
	} else if len(binding.Actions) > 0 {
		role = &rbacproto.ServiceRole{Rules: binding.Actions}
		name = "%s-inline-role"
	}
	return name, role
}

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
		workloadLabels := model.LabelsCollection{service.labels}
		policySelector := authzPolicy.Policy.WorkloadSelector.GetLabels()
		if !(workloadLabels.IsSupersetOf(policySelector)) {
			// Skip if the workload labels is not a superset of the policy selector (i.e. the workload
			// is not selected by the policy).
			continue
		}

		for i, binding := range authzPolicy.Policy.Allow {
			roleName, role := roleForBinding(binding, namespace, option.authzPolicies)
			// TODO: optimize for multiple bindings referring to the same role.
			m := NewModel(role, []*rbacproto.ServiceRoleBinding{binding})
			policy := m.Generate(service, option.forTCPFilter)
			if policy != nil {
				rbacLog.Debugf("generated config for role: %s", roleName)
				policyName := fmt.Sprintf("authz-policy-%s-allow[%d]", authzPolicy.Name, i)
				rbac.Policies[policyName] = policy
			}
		}
	}
	return &http_config.RBAC{Rules: rbac}
}
