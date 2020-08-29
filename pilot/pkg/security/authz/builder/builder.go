// Copyright Istio Authors
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
	"fmt"

	tcppb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	rbactcppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
)

var (
	authzLog = log.RegisterScope("authorization", "Istio Authorization Policy", 0)
)

// Builder builds Istio authorization policy to Envoy RBAC filter.
type Builder struct {
	trustDomainBundle  trustdomain.Bundle
	denyPolicies       []model.AuthorizationPolicy
	allowPolicies      []model.AuthorizationPolicy
	auditPolicies      []model.AuthorizationPolicy
	isIstioVersionGE15 bool
}

// New returns a new builder for the given workload with the authorization policy.
// Returns nil if none of the authorization policies are enabled for the workload.
func New(trustDomainBundle trustdomain.Bundle, workload labels.Collection, namespace string,
	policies *model.AuthorizationPolicies, isIstioVersionGE15 bool) *Builder {
	denyPolicies, allowPolicies, auditPolicies := policies.ListAuthorizationPolicies(namespace, workload)
	if len(denyPolicies) == 0 && len(allowPolicies) == 0 && len(auditPolicies) == 0 {
		return nil
	}
	return &Builder{
		trustDomainBundle:  trustDomainBundle,
		denyPolicies:       denyPolicies,
		allowPolicies:      allowPolicies,
		auditPolicies:      auditPolicies,
		isIstioVersionGE15: isIstioVersionGE15,
	}
}

// BuildHTTP returns the RBAC HTTP filters built from the authorization policy.
func (b Builder) BuildHTTP() []*httppb.HttpFilter {
	var filters []*httppb.HttpFilter

	if auditConfig := build(b.auditPolicies, b.trustDomainBundle,
		rbacpb.RBAC_LOG, false /* forTCP */, b.isIstioVersionGE15); auditConfig != nil {
		filters = append(filters, createHTTPFilter(auditConfig))
	}
	if denyConfig := build(b.denyPolicies, b.trustDomainBundle,
		rbacpb.RBAC_DENY, false /* forTCP */, b.isIstioVersionGE15); denyConfig != nil {
		filters = append(filters, createHTTPFilter(denyConfig))
	}
	if allowConfig := build(b.allowPolicies, b.trustDomainBundle,
		rbacpb.RBAC_ALLOW, false /* forTCP */, b.isIstioVersionGE15); allowConfig != nil {
		filters = append(filters, createHTTPFilter(allowConfig))
	}

	return filters
}

// BuildTCP returns the RBAC TCP filters built from the authorization policy.
func (b Builder) BuildTCP() []*tcppb.Filter {
	var filters []*tcppb.Filter

	if auditConfig := build(b.auditPolicies, b.trustDomainBundle,
		rbacpb.RBAC_LOG, true /* forTCP */, b.isIstioVersionGE15); auditConfig != nil {
		filters = append(filters, createTCPFilter(auditConfig))
	}
	if denyConfig := build(b.denyPolicies, b.trustDomainBundle,
		rbacpb.RBAC_DENY, true /* forTCP */, b.isIstioVersionGE15); denyConfig != nil {
		filters = append(filters, createTCPFilter(denyConfig))
	}
	if allowConfig := build(b.allowPolicies, b.trustDomainBundle,
		rbacpb.RBAC_ALLOW, true /* forTCP */, b.isIstioVersionGE15); allowConfig != nil {
		filters = append(filters, createTCPFilter(allowConfig))
	}

	return filters
}

func build(policies []model.AuthorizationPolicy, tdBundle trustdomain.Bundle, action rbacpb.RBAC_Action, forTCP, isIstioVersionGE15 bool) *rbachttppb.RBAC {
	if len(policies) == 0 {
		return nil
	}

	rules := &rbacpb.RBAC{
		Action:   action,
		Policies: map[string]*rbacpb.Policy{},
	}

	for _, policy := range policies {
		for i, rule := range policy.Spec.Rules {
			name := fmt.Sprintf("ns[%s]-policy[%s]-rule[%d]", policy.Namespace, policy.Name, i)
			if rule == nil {
				authzLog.Errorf("skipped nil rule %s", name)
				continue
			}
			m, err := authzmodel.New(rule, isIstioVersionGE15)
			if err != nil {
				authzLog.Errorf("skipped rule %s: %v", name, err)
				continue
			}
			m.MigrateTrustDomain(tdBundle)
			generated, err := m.Generate(forTCP, action)
			if err != nil {
				authzLog.Errorf("skipped rule %s: %v", name, err)
				continue
			}
			if generated != nil {
				rules.Policies[name] = generated
				authzLog.Debugf("rule %s generated policy: %+v", name, generated)
			}
		}
	}

	return &rbachttppb.RBAC{Rules: rules}
}

// nolint: interfacer
func createHTTPFilter(config *rbachttppb.RBAC) *httppb.HttpFilter {
	if config == nil {
		return nil
	}
	return &httppb.HttpFilter{
		Name:       authzmodel.RBACHTTPFilterName,
		ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(config)},
	}
}

func createTCPFilter(config *rbachttppb.RBAC) *tcppb.Filter {
	if config == nil {
		return nil
	}
	rbacConfig := &rbactcppb.RBAC{
		Rules:      config.Rules,
		StatPrefix: authzmodel.RBACTCPFilterStatPrefix,
	}
	return &tcppb.Filter{
		Name:       authzmodel.RBACTCPFilterName,
		ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbacConfig)},
	}
}
