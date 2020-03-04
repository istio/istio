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

package v1alpha1

import (
	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"

	istio_rbac "istio.io/api/rbac/v1alpha1"
	istiolog "istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type v1alpha1Generator struct {
	trustDomainBundle         trustdomain.Bundle
	serviceMetadata           *authz_model.ServiceMetadata
	authzPolicies             *model.AuthorizationPolicies
	isGlobalPermissiveEnabled bool
}

func NewGenerator(
	trustDomainBundle trustdomain.Bundle,
	serviceMetadata *authz_model.ServiceMetadata,
	authzPolicies *model.AuthorizationPolicies,
	isGlobalPermissiveEnabled bool) policy.Generator {
	return &v1alpha1Generator{
		trustDomainBundle:         trustDomainBundle,
		serviceMetadata:           serviceMetadata,
		authzPolicies:             authzPolicies,
		isGlobalPermissiveEnabled: isGlobalPermissiveEnabled,
	}
}

func (g *v1alpha1Generator) Generate(forTCPFilter bool) (denyConfig *http_config.RBAC, allowConfig *http_config.RBAC) {
	rbacLog.Debugf("building v1alpha1 policy")
	enforcedConfig := &envoy_rbac.RBAC{
		Action:   envoy_rbac.RBAC_ALLOW,
		Policies: map[string]*envoy_rbac.Policy{},
	}
	permissiveConfig := &envoy_rbac.RBAC{
		Action:   envoy_rbac.RBAC_ALLOW,
		Policies: map[string]*envoy_rbac.Policy{},
	}

	serviceMetadata := g.serviceMetadata
	authzPolicies := g.authzPolicies

	namespace := serviceMetadata.GetNamespace()
	bindings := authzPolicies.ListServiceRoleBindings(namespace)
	for _, roleConfig := range authzPolicies.ListServiceRoles(namespace) {
		roleName := roleConfig.Name
		rbacLog.Debugf("checking role %v", roleName)

		var enforcedBindings []*istio_rbac.ServiceRoleBinding
		var permissiveBindings []*istio_rbac.ServiceRoleBinding
		for _, binding := range bindings[roleName] {
			if binding.Mode == istio_rbac.EnforcementMode_PERMISSIVE || g.isGlobalPermissiveEnabled {
				// If RBAC Config is set to permissive mode globally, all policies will be in
				// permissive mode regardless its own mode.
				permissiveBindings = append(permissiveBindings, binding)
			} else {
				enforcedBindings = append(enforcedBindings, binding)
			}
		}
		role := roleConfig.ServiceRole
		if p := g.generatePolicy(g.trustDomainBundle, role, enforcedBindings, forTCPFilter); p != nil {
			rbacLog.Debugf("generated policy for role: %s", roleName)
			enforcedConfig.Policies[roleName] = p
		}
		if p := g.generatePolicy(g.trustDomainBundle, role, permissiveBindings, forTCPFilter); p != nil {
			rbacLog.Debugf("generated permissive policy for role: %s", roleName)
			permissiveConfig.Policies[roleName] = p
		}
	}

	// If RBAC Config is set to permissive mode globally, RBAC is transparent to users;
	// when mapping to rbac filter config, there is only shadow rules (no normal rules).
	if g.isGlobalPermissiveEnabled {
		return nil, &http_config.RBAC{ShadowRules: permissiveConfig}
	}

	ret := &http_config.RBAC{Rules: enforcedConfig}
	// If RBAC permissive mode is only set on policy level, set ShadowRules only when there is policy in permissive mode.
	// Otherwise, non-empty shadow_rules causes permissive attributes are sent to mixer when permissive mode isn't set.
	if len(permissiveConfig.Policies) > 0 {
		ret.ShadowRules = permissiveConfig
	}
	return nil, ret
}

func (g *v1alpha1Generator) generatePolicy(trustDomainBundle trustdomain.Bundle, role *istio_rbac.ServiceRole,
	bindings []*istio_rbac.ServiceRoleBinding, forTCPFilter bool) *envoy_rbac.Policy {
	if role == nil || len(bindings) == 0 {
		return nil
	}

	m := authz_model.NewModelV1alpha1(trustDomainBundle, role, bindings)
	return m.Generate(g.serviceMetadata, forTCPFilter, false /* forDenyPolicy */)
}
