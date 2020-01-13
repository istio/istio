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

package v1beta1

import (
	"fmt"

	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"

	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"

	istiolog "istio.io/pkg/log"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type v1beta1Generator struct {
	trustDomainBundle trustdomain.Bundle
	denyPolicies      []model.AuthorizationPolicyConfig
	allowPolicies     []model.AuthorizationPolicyConfig
}

func NewGenerator(trustDomainBundle trustdomain.Bundle, allowPolicies []model.AuthorizationPolicyConfig,
	denyPolicies []model.AuthorizationPolicyConfig) policy.Generator {
	return &v1beta1Generator{
		trustDomainBundle: trustDomainBundle,
		denyPolicies:      denyPolicies,
		allowPolicies:     allowPolicies,
	}
}

func (g *v1beta1Generator) Generate(forTCPFilter bool) (denyConfig *http_config.RBAC, allowConfig *http_config.RBAC) {
	rbacLog.Debugf("building v1beta1 policy")
	denyConfig = g.generatePolicy(g.denyPolicies, envoy_rbac.RBAC_DENY, forTCPFilter)
	allowConfig = g.generatePolicy(g.allowPolicies, envoy_rbac.RBAC_ALLOW, forTCPFilter)
	return
}

func (g *v1beta1Generator) generatePolicy(policies []model.AuthorizationPolicyConfig, action envoy_rbac.RBAC_Action,
	forTCPFilter bool) *http_config.RBAC {
	if len(policies) == 0 {
		return nil
	}

	rbac := &envoy_rbac.RBAC{
		Action:   action,
		Policies: map[string]*envoy_rbac.Policy{},
	}

	for _, config := range policies {
		for i, rule := range config.AuthorizationPolicy.Rules {
			if rule == nil {
				continue
			}
			m := authz_model.NewModelV1beta1(g.trustDomainBundle, rule)
			rbacLog.Debugf("constructed internal model: %+v", m)
			if p := m.Generate(nil, forTCPFilter); p != nil {
				name := fmt.Sprintf("ns[%s]-policy[%s]-rule[%d]", config.Namespace, config.Name, i)
				rbac.Policies[name] = p
				rbacLog.Debugf("generated policy %s: %+v", name, p)
			}
		}
	}
	return &http_config.RBAC{Rules: rbac}
}
