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
	"strconv"

	tcppb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	rbactcppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/hashicorp/go-multierror"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
)

var rbacPolicyMatchNever = &rbacpb.Policy{
	Permissions: []*rbacpb.Permission{{Rule: &rbacpb.Permission_NotRule{
		NotRule: &rbacpb.Permission{Rule: &rbacpb.Permission_Any{Any: true}},
	}}},
	Principals: []*rbacpb.Principal{{Identifier: &rbacpb.Principal_NotId{
		NotId: &rbacpb.Principal{Identifier: &rbacpb.Principal_Any{Any: true}},
	}}},
}

// General setting to control behavior
type Option struct {
	IsCustomBuilder bool
	Logger          *AuthzLogger
}

// Builder builds Istio authorization policy to Envoy filters.
type Builder struct {
	trustDomainBundle trustdomain.Bundle
	option            Option

	// populated when building for CUSTOM action.
	customPolicies []model.AuthorizationPolicy
	extensions     map[string]*builtExtAuthz

	// populated when building for ALLOW/DENY/AUDIT action.
	denyPolicies  []model.AuthorizationPolicy
	allowPolicies []model.AuthorizationPolicy
	auditPolicies []model.AuthorizationPolicy
}

// New returns a new builder for the given workload with the authorization policy.
// Returns nil if none of the authorization policies are enabled for the workload.
func New(trustDomainBundle trustdomain.Bundle, push *model.PushContext, policies model.AuthorizationPoliciesResult, option Option) *Builder {
	if option.IsCustomBuilder {
		option.Logger.AppendDebugf("found %d CUSTOM actions", len(policies.Custom))
		if len(policies.Custom) == 0 {
			return nil
		}
		return &Builder{
			customPolicies:    policies.Custom,
			extensions:        processExtensionProvider(push),
			trustDomainBundle: trustDomainBundle,
			option:            option,
		}
	}

	option.Logger.AppendDebugf("found %d DENY actions, %d ALLOW actions, %d AUDIT actions", len(policies.Deny), len(policies.Allow), len(policies.Audit))
	if len(policies.Deny) == 0 && len(policies.Allow) == 0 && len(policies.Audit) == 0 {
		return nil
	}
	return &Builder{
		denyPolicies:      policies.Deny,
		allowPolicies:     policies.Allow,
		auditPolicies:     policies.Audit,
		trustDomainBundle: trustDomainBundle,
		option:            option,
	}
}

// BuildHTTP returns the HTTP filters built from the authorization policy.
func (b Builder) BuildHTTP() []*httppb.HttpFilter {
	if b.option.IsCustomBuilder {
		// Use the DENY action so that a HTTP rule is properly handled when generating for TCP filter chain.
		if configs := b.build(b.customPolicies, rbacpb.RBAC_DENY, false); configs != nil {
			b.option.Logger.AppendDebugf("built %d HTTP filters for CUSTOM action", len(configs.http))
			return configs.http
		}
		return nil
	}

	var filters []*httppb.HttpFilter
	if configs := b.build(b.auditPolicies, rbacpb.RBAC_LOG, false); configs != nil {
		b.option.Logger.AppendDebugf("built %d HTTP filters for AUDIT action", len(configs.http))
		filters = append(filters, configs.http...)
	}
	if configs := b.build(b.denyPolicies, rbacpb.RBAC_DENY, false); configs != nil {
		b.option.Logger.AppendDebugf("built %d HTTP filters for DENY action", len(configs.http))
		filters = append(filters, configs.http...)
	}
	if configs := b.build(b.allowPolicies, rbacpb.RBAC_ALLOW, false); configs != nil {
		b.option.Logger.AppendDebugf("built %d HTTP filters for ALLOW action", len(configs.http))
		filters = append(filters, configs.http...)
	}
	return filters
}

// BuildTCP returns the TCP filters built from the authorization policy.
func (b Builder) BuildTCP() []*tcppb.Filter {
	if b.option.IsCustomBuilder {
		if configs := b.build(b.customPolicies, rbacpb.RBAC_DENY, true); configs != nil {
			b.option.Logger.AppendDebugf("built %d TCP filters for CUSTOM action", len(configs.tcp))
			return configs.tcp
		}
		return nil
	}

	var filters []*tcppb.Filter
	if configs := b.build(b.auditPolicies, rbacpb.RBAC_LOG, true); configs != nil {
		b.option.Logger.AppendDebugf("built %d TCP filters for AUDIT action", len(configs.tcp))
		filters = append(filters, configs.tcp...)
	}
	if configs := b.build(b.denyPolicies, rbacpb.RBAC_DENY, true); configs != nil {
		b.option.Logger.AppendDebugf("built %d TCP filters for DENY action", len(configs.tcp))
		filters = append(filters, configs.tcp...)
	}
	if configs := b.build(b.allowPolicies, rbacpb.RBAC_ALLOW, true); configs != nil {
		b.option.Logger.AppendDebugf("built %d TCP filters for ALLOW action", len(configs.tcp))
		filters = append(filters, configs.tcp...)
	}
	return filters
}

type builtConfigs struct {
	http []*httppb.HttpFilter
	tcp  []*tcppb.Filter
}

func (b Builder) isDryRun(policy model.AuthorizationPolicy) bool {
	dryRun := false
	if val, ok := policy.Annotations[annotation.IoIstioDryRun.Name]; ok {
		var err error
		dryRun, err = strconv.ParseBool(val)
		if err != nil {
			b.option.Logger.AppendError(fmt.Errorf("failed to parse the value of %s: %v", annotation.IoIstioDryRun.Name, err))
		}
	}
	return dryRun
}

func shadowRuleStatPrefix(rule *rbacpb.RBAC) string {
	switch rule.GetAction() {
	case rbacpb.RBAC_ALLOW:
		return authzmodel.RBACShadowRulesAllowStatPrefix
	case rbacpb.RBAC_DENY:
		return authzmodel.RBACShadowRulesDenyStatPrefix
	default:
		return ""
	}
}

func (b Builder) build(policies []model.AuthorizationPolicy, action rbacpb.RBAC_Action, forTCP bool) *builtConfigs {
	if len(policies) == 0 {
		return nil
	}

	enforceRules := &rbacpb.RBAC{
		Action:   action,
		Policies: map[string]*rbacpb.Policy{},
	}
	shadowRules := &rbacpb.RBAC{
		Action:   action,
		Policies: map[string]*rbacpb.Policy{},
	}

	var providers []string
	filterType := "HTTP"
	if forTCP {
		filterType = "TCP"
	}
	hasEnforcePolicy, hasDryRunPolicy := false, false
	for _, policy := range policies {
		var currentRule *rbacpb.RBAC
		if b.isDryRun(policy) {
			currentRule = shadowRules
			hasDryRunPolicy = true
		} else {
			currentRule = enforceRules
			hasEnforcePolicy = true
		}
		if b.option.IsCustomBuilder {
			providers = append(providers, policy.Spec.GetProvider().GetName())
		}
		for i, rule := range policy.Spec.Rules {
			// The name will later be used by ext_authz filter to get the evaluation result from dynamic metadata.
			name := policyName(policy.Namespace, policy.Name, i, b.option)
			if rule == nil {
				b.option.Logger.AppendError(fmt.Errorf("skipped nil rule %s", name))
				continue
			}
			m, err := authzmodel.New(rule)
			if err != nil {
				b.option.Logger.AppendError(multierror.Prefix(err, fmt.Sprintf("skipped invalid rule %s:", name)))
				continue
			}
			m.MigrateTrustDomain(b.trustDomainBundle)
			if len(b.trustDomainBundle.TrustDomains) > 1 {
				b.option.Logger.AppendDebugf("patched source principal with trust domain aliases %v", b.trustDomainBundle.TrustDomains)
			}
			generated, err := m.Generate(forTCP, action)
			if err != nil {
				b.option.Logger.AppendDebugf("skipped rule %s on TCP filter chain: %v", name, err)
				continue
			}
			if generated != nil {
				currentRule.Policies[name] = generated
				b.option.Logger.AppendDebugf("generated config from rule %s on %s filter chain successfully", name, filterType)
			}
		}
		if len(policy.Spec.Rules) == 0 {
			// Generate an explicit policy that never matches.
			name := policyName(policy.Namespace, policy.Name, 0, b.option)
			b.option.Logger.AppendDebugf("generated config from policy %s on %s filter chain successfully", name, filterType)
			currentRule.Policies[name] = rbacPolicyMatchNever
		}
	}

	if !hasEnforcePolicy {
		enforceRules = nil
	}
	if !hasDryRunPolicy {
		shadowRules = nil
	}
	if forTCP {
		return &builtConfigs{tcp: b.buildTCP(enforceRules, shadowRules, providers)}
	}
	return &builtConfigs{http: b.buildHTTP(enforceRules, shadowRules, providers)}
}

func (b Builder) buildHTTP(rules *rbacpb.RBAC, shadowRules *rbacpb.RBAC, providers []string) []*httppb.HttpFilter {
	if !b.option.IsCustomBuilder {
		rbac := &rbachttppb.RBAC{
			Rules:                 rules,
			ShadowRules:           shadowRules,
			ShadowRulesStatPrefix: shadowRuleStatPrefix(shadowRules),
		}
		return []*httppb.HttpFilter{
			{
				Name:       wellknown.HTTPRoleBasedAccessControl,
				ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	}

	extauthz, err := getExtAuthz(b.extensions, providers)
	if err != nil {
		b.option.Logger.AppendError(multierror.Prefix(err, "failed to process CUSTOM action:"))
		rbac := &rbachttppb.RBAC{Rules: rbacDefaultDenyAll}
		return []*httppb.HttpFilter{
			{
				Name:       wellknown.HTTPRoleBasedAccessControl,
				ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	}
	// Add the RBAC filter in shadow mode so that it only evaluates the matching rules for CUSTOM action but not enforce it.
	// The evaluation result is stored in the dynamic metadata keyed by the policy name. And then the ext_authz filter
	// can utilize these metadata to trigger the enforcement conditionally.
	// See https://docs.google.com/document/d/1V4mCQCw7mlGp0zSQQXYoBdbKMDnkPOjeyUb85U07iSI/edit#bookmark=kix.jdq8u0an2r6s
	// for more details.
	rbac := &rbachttppb.RBAC{
		ShadowRules:           rules,
		ShadowRulesStatPrefix: authzmodel.RBACExtAuthzShadowRulesStatPrefix,
	}
	return []*httppb.HttpFilter{
		{
			Name:       wellknown.HTTPRoleBasedAccessControl,
			ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
		},
		{
			Name:       wellknown.HTTPExternalAuthorization,
			ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(extauthz.http)},
		},
	}
}

func (b Builder) buildTCP(rules *rbacpb.RBAC, shadowRules *rbacpb.RBAC, providers []string) []*tcppb.Filter {
	if !b.option.IsCustomBuilder {
		rbac := &rbactcppb.RBAC{
			Rules:                 rules,
			StatPrefix:            authzmodel.RBACTCPFilterStatPrefix,
			ShadowRules:           shadowRules,
			ShadowRulesStatPrefix: shadowRuleStatPrefix(shadowRules),
		}
		return []*tcppb.Filter{
			{
				Name:       wellknown.RoleBasedAccessControl,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	}

	if extauthz, err := getExtAuthz(b.extensions, providers); err != nil {
		b.option.Logger.AppendError(multierror.Prefix(err, "failed to parse CUSTOM action, will generate a deny all config:"))
		rbac := &rbactcppb.RBAC{
			Rules:      rbacDefaultDenyAll,
			StatPrefix: authzmodel.RBACTCPFilterStatPrefix,
		}
		return []*tcppb.Filter{
			{
				Name:       wellknown.RoleBasedAccessControl,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	} else if extauthz.tcp == nil {
		b.option.Logger.AppendDebugf("ignored CUSTOM action with HTTP provider on TCP filter chain")
		return nil
	} else {
		rbac := &rbactcppb.RBAC{
			ShadowRules:           rules,
			StatPrefix:            authzmodel.RBACTCPFilterStatPrefix,
			ShadowRulesStatPrefix: authzmodel.RBACExtAuthzShadowRulesStatPrefix,
		}
		return []*tcppb.Filter{
			{
				Name:       wellknown.RoleBasedAccessControl,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
			{
				Name:       wellknown.ExternalAuthorization,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(extauthz.tcp)},
			},
		}
	}
}

func policyName(namespace, name string, rule int, option Option) string {
	prefix := ""
	if option.IsCustomBuilder {
		prefix = extAuthzMatchPrefix + "-"
	}
	return fmt.Sprintf("%sns[%s]-policy[%s]-rule[%d]", prefix, namespace, name, rule)
}
