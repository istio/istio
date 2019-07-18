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

package builder

import (
	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"

	istio_rbac "istio.io/api/rbac/v1alpha1"
)

// buildV1 builds the v1 policy to filter config.
func (b *Builder) buildV1(forTCPFilter bool) *http_config.RBAC {
	rbacLog.Debugf("building v1 policy")

	enforcedConfig := &envoy_rbac.RBAC{
		Action:   envoy_rbac.RBAC_ALLOW,
		Policies: map[string]*envoy_rbac.Policy{},
	}
	permissiveConfig := &envoy_rbac.RBAC{
		Action:   envoy_rbac.RBAC_ALLOW,
		Policies: map[string]*envoy_rbac.Policy{},
	}

	serviceMetadata := b.serviceMetadata
	authzPolicies := b.authzPolicies

	namespace := serviceMetadata.GetNamespace()
	roleToBindings := authzPolicies.RoleToBindingsForNamespace(namespace)
	for _, roleConfig := range authzPolicies.RolesForNamespace(namespace) {
		roleName := roleConfig.Name
		rbacLog.Debugf("checking role %v", roleName)

		var enforcedBindings []*istio_rbac.ServiceRoleBinding
		var permissiveBindings []*istio_rbac.ServiceRoleBinding
		for _, binding := range roleToBindings[roleName] {
			if binding.Mode == istio_rbac.EnforcementMode_PERMISSIVE || b.isGlobalPermissiveEnabled {
				// If RBAC Config is set to permissive mode globally, all policies will be in
				// permissive mode regardless its own mode.
				permissiveBindings = append(permissiveBindings, binding)
			} else {
				enforcedBindings = append(enforcedBindings, binding)
			}
		}

		role := roleConfig.Spec.(*istio_rbac.ServiceRole)
		if policy := b.generatePolicy(role, enforcedBindings, forTCPFilter); policy != nil {
			rbacLog.Debugf("generated policy for role: %s", roleName)
			enforcedConfig.Policies[roleName] = policy
		}
		if policy := b.generatePolicy(role, permissiveBindings, forTCPFilter); policy != nil {
			rbacLog.Debugf("generated permissive policy for role: %s", roleName)
			permissiveConfig.Policies[roleName] = policy
		}
	}

	// If RBAC Config is set to permissive mode globally, RBAC is transparent to users;
	// when mapping to rbac filter config, there is only shadow rules (no normal rules).
	if b.isGlobalPermissiveEnabled {
		return &http_config.RBAC{ShadowRules: permissiveConfig}
	}

	ret := &http_config.RBAC{Rules: enforcedConfig}
	// If RBAC permissive mode is only set on policy level, set ShadowRules only when there is policy in permissive mode.
	// Otherwise, non-empty shadow_rules causes permissive attributes are sent to mixer when permissive mode isn't set.
	if len(permissiveConfig.Policies) > 0 {
		ret.ShadowRules = permissiveConfig
	}
	return ret
}
