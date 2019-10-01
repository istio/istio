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

	istio_rbac "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	istiolog "istio.io/pkg/log"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type v1beta1Generator struct {
	policies []model.Config
}

func NewGenerator(policies []model.Config) policy.Generator {
	return &v1beta1Generator{
		policies: policies,
	}
}

func (g *v1beta1Generator) Generate(forTCPFilter bool) *http_config.RBAC {
	rbacLog.Debugf("building v1beta1 policy")

	rbac := &envoy_rbac.RBAC{
		Action:   envoy_rbac.RBAC_ALLOW,
		Policies: map[string]*envoy_rbac.Policy{},
	}

	for _, config := range g.policies {
		spec := config.Spec.(*istio_rbac.AuthorizationPolicy)
		for i, rule := range spec.Rules {
			if p := g.generatePolicy(rule, forTCPFilter); p != nil {
				name := fmt.Sprintf("ns[%s]-policy[%s]-rule[%d]", config.Namespace, config.Name, i)
				rbac.Policies[name] = p
				rbacLog.Debugf("generated policy %s: %+v", name, p)
			}
		}
	}
	return &http_config.RBAC{Rules: rbac}
}

func (g *v1beta1Generator) generatePolicy(rule *istio_rbac.Rule, forTCPFilter bool) *envoy_rbac.Policy {
	if rule == nil {
		return nil
	}

	m := authz_model.NewModelFromV1beta1(rule)
	rbacLog.Debugf("constructed internal model: %+v", m)
	return m.Generate(nil, forTCPFilter)
}
