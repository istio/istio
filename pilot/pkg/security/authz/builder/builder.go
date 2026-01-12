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

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	rbactcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	"github.com/hashicorp/go-multierror"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/wellknown"
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
	UseFilterState  bool
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
func (b Builder) BuildHTTP() []*hcm.HttpFilter {
	return build(b, b.buildHTTP, "HTTP", false)
}

// BuildTCP returns the TCP filters built from the authorization policy.
func (b Builder) BuildTCP() []*listener.Filter {
	return build(b, b.buildTCP, "TCP", true)
}

// BuildTCPRulesAsHTTPFilter returns the HTTP filters built from TCP authorization policy.
func (b Builder) BuildTCPRulesAsHTTPFilter() []*hcm.HttpFilter {
	return build(b, b.buildHTTP, "HTTP for TCP", true)
}

// build computes the rules and transforms them to the appropriate type (via translateFn)
func build[T any](b Builder, translateFn func(rule *builtRule, logger *AuthzLogger) []T, logType string, forTCP bool) []T {
	logger := &AuthzLogger{}
	defer logger.Report()
	if b.option.IsCustomBuilder {
		if configs := b.build(b.customPolicies, rbacpb.RBAC_DENY, forTCP, logger); configs != nil {
			t := translateFn(configs, logger)
			logger.AppendDebugf("built %d %s filters for CUSTOM action", len(t), logType)
			return t
		}
		return nil
	}

	var filters []T
	if configs := b.build(b.auditPolicies, rbacpb.RBAC_LOG, forTCP, logger); configs != nil {
		t := translateFn(configs, logger)
		logger.AppendDebugf("built %d %s filters for AUDIT action", len(t), logType)
		filters = append(filters, t...)
	}
	if configs := b.build(b.denyPolicies, rbacpb.RBAC_DENY, forTCP, logger); configs != nil {
		t := translateFn(configs, logger)
		logger.AppendDebugf("built %d %s filters for DENY action", len(t), logType)
		filters = append(filters, t...)
	}
	if configs := b.build(b.allowPolicies, rbacpb.RBAC_ALLOW, forTCP, logger); configs != nil {
		t := translateFn(configs, logger)
		logger.AppendDebugf("built %d %s filters for ALLOW action", len(t), logType)
		filters = append(filters, t...)
	}
	return filters
}

type builtRule struct {
	rules       *rbacpb.RBAC
	shadowRules *rbacpb.RBAC
	providers   []string
}

func isDryRun(policy model.AuthorizationPolicy, logger *AuthzLogger) bool {
	dryRun := false
	if val, ok := policy.Annotations[annotation.IoIstioDryRun.Name]; ok {
		var err error
		dryRun, err = strconv.ParseBool(val)
		if err != nil {
			logger.AppendError(fmt.Errorf("failed to parse the value of %s: %v", annotation.IoIstioDryRun.Name, err))
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

func (b Builder) build(policies []model.AuthorizationPolicy, action rbacpb.RBAC_Action, forTCP bool, logger *AuthzLogger) *builtRule {
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
		if isDryRun(policy, logger) {
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
				logger.AppendError(fmt.Errorf("skipped nil rule %s", name))
				continue
			}
			m, err := authzmodel.New(policy.NamespacedName(), rule)
			if err != nil {
				logger.AppendError(multierror.Prefix(err, fmt.Sprintf("skipped invalid rule %s:", name)))
				continue
			}
			m.MigrateTrustDomain(b.trustDomainBundle)
			if len(b.trustDomainBundle.TrustDomains) > 1 {
				logger.AppendDebugf("patched source principal with trust domain aliases %v", b.trustDomainBundle.TrustDomains)
			}
			generated, err := m.Generate(forTCP, !b.option.UseFilterState, action)
			if err != nil {
				logger.AppendDebugf("skipped rule %s on TCP filter chain: %v", name, err)
				continue
			}
			if generated != nil {
				currentRule.Policies[name] = generated
				logger.AppendDebugf("generated config from rule %s on %s filter chain successfully", name, filterType)
			}
		}
		if len(policy.Spec.Rules) == 0 {
			// Generate an explicit policy that never matches.
			name := policyName(policy.Namespace, policy.Name, 0, b.option)
			logger.AppendDebugf("generated config from policy %s on %s filter chain successfully", name, filterType)
			currentRule.Policies[name] = rbacPolicyMatchNever
		}
	}

	if !hasEnforcePolicy {
		enforceRules = nil
	}
	if !hasDryRunPolicy {
		shadowRules = nil
	}
	return &builtRule{
		rules:       enforceRules,
		shadowRules: shadowRules,
		providers:   providers,
	}
}

func (b Builder) buildHTTP(rule *builtRule, logger *AuthzLogger) []*hcm.HttpFilter {
	rules := rule.rules
	shadowRules := rule.shadowRules
	providers := rule.providers
	if !b.option.IsCustomBuilder {
		rbac := &rbachttp.RBAC{
			Rules:                 rules,
			ShadowRules:           shadowRules,
			ShadowRulesStatPrefix: shadowRuleStatPrefix(shadowRules),
		}
		return []*hcm.HttpFilter{
			{
				Name:       wellknown.HTTPRoleBasedAccessControl,
				ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
			},
		}
	}

	extauthz, err := getExtAuthz(b.extensions, providers)
	if err != nil {
		logger.AppendError(multierror.Prefix(err, "failed to process CUSTOM action, will generate deny configs for the specified rules:"))
		rbac := &rbachttp.RBAC{Rules: getBadCustomDenyRules(rules)}
		return []*hcm.HttpFilter{
			{
				Name:       wellknown.HTTPRoleBasedAccessControl,
				ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
			},
		}
	}
	// Add the RBAC filter in shadow mode so that it only evaluates the matching rules for CUSTOM action but not enforce it.
	// The evaluation result is stored in the dynamic metadata keyed by the policy name. And then the ext_authz filter
	// can utilize these metadata to trigger the enforcement conditionally.
	// See https://docs.google.com/document/d/1V4mCQCw7mlGp0zSQQXYoBdbKMDnkPOjeyUb85U07iSI/edit#bookmark=kix.jdq8u0an2r6s
	// for more details.
	rbac := &rbachttp.RBAC{
		ShadowRules:           rules,
		ShadowRulesStatPrefix: authzmodel.RBACExtAuthzShadowRulesStatPrefix,
	}
	return []*hcm.HttpFilter{
		{
			Name:       wellknown.HTTPRoleBasedAccessControl,
			ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
		},
		{
			Name:       wellknown.HTTPExternalAuthorization,
			ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(extauthz.http)},
		},
	}
}

func (b Builder) buildTCP(rule *builtRule, logger *AuthzLogger) []*listener.Filter {
	rules := rule.rules
	shadowRules := rule.shadowRules
	providers := rule.providers
	if !b.option.IsCustomBuilder {
		rbac := &rbactcp.RBAC{
			Rules:                 rules,
			StatPrefix:            authzmodel.RBACTCPFilterStatPrefix,
			ShadowRules:           shadowRules,
			ShadowRulesStatPrefix: shadowRuleStatPrefix(shadowRules),
		}
		return []*listener.Filter{
			{
				Name:       wellknown.RoleBasedAccessControl,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
			},
		}
	}

	extauthz, err := getExtAuthz(b.extensions, providers)
	if err != nil {
		logger.AppendError(multierror.Prefix(err, "failed to process CUSTOM action, will generate deny configs for the specified rules:"))
		rbac := &rbactcp.RBAC{
			Rules:      getBadCustomDenyRules(rules),
			StatPrefix: authzmodel.RBACTCPFilterStatPrefix,
		}
		return []*listener.Filter{
			{
				Name:       wellknown.RoleBasedAccessControl,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
			},
		}
	} else if extauthz.tcp == nil {
		logger.AppendDebugf("ignored CUSTOM action with HTTP provider on TCP filter chain")
		return nil
	}

	rbac := &rbactcp.RBAC{
		ShadowRules:           rules,
		StatPrefix:            authzmodel.RBACTCPFilterStatPrefix,
		ShadowRulesStatPrefix: authzmodel.RBACExtAuthzShadowRulesStatPrefix,
	}
	return []*listener.Filter{
		{
			Name:       wellknown.RoleBasedAccessControl,
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(rbac)},
		},
		{
			Name:       wellknown.ExternalAuthorization,
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(extauthz.tcp)},
		},
	}
}

func getBadCustomDenyRules(rules *rbacpb.RBAC) *rbacpb.RBAC {
	rbac := &rbacpb.RBAC{
		Action:   rbacpb.RBAC_DENY,
		Policies: map[string]*rbacpb.Policy{},
	}
	for _, key := range maps.Keys(rules.Policies) {
		rbac.Policies[key+badCustomActionSuffix] = rules.Policies[key]
	}
	return rbac
}

func policyName(namespace, name string, rule int, option Option) string {
	prefix := ""
	if option.IsCustomBuilder {
		prefix = extAuthzMatchPrefix + "-"
	}
	return fmt.Sprintf("%sns[%s]-policy[%s]-rule[%d]", prefix, namespace, name, rule)
}
