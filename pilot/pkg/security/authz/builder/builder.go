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
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
)

var (
	authzLog = log.RegisterScope("authorization", "Istio Authorization Policy", 0)
)

// General setting to control behavior
type Option struct {
	IsIstioVersionGE15     bool
	IsOnInboundPassthrough bool
	IsCustomBuilder        bool
}

// Builder builds Istio authorization policy to Envoy filters.
type Builder struct {
	trustDomainBundle trustdomain.Bundle
	option            Option

	// populated when building for CUSTOM action.
	customPolicies []model.AuthorizationPolicy
	extensions     []*meshconfig.MeshConfig_ExtensionProvider

	// populated when building for ALLOW/DENY/AUDIT action.
	denyPolicies  []model.AuthorizationPolicy
	allowPolicies []model.AuthorizationPolicy
	auditPolicies []model.AuthorizationPolicy
}

// New returns a new builder for the given workload with the authorization policy.
// Returns nil if none of the authorization policies are enabled for the workload.
func New(trustDomainBundle trustdomain.Bundle, in *plugin.InputParams, option Option) *Builder {
	namespace := in.Node.ConfigNamespace
	workload := in.Node.Metadata.Labels
	policies := in.Push.AuthzPolicies.ListAuthorizationPolicies(namespace, labels.Collection{workload})

	if option.IsCustomBuilder {
		if len(policies.Custom) == 0 {
			authzLog.Debugf("No CUSTOM policy for workload %v in %s", workload, namespace)
			return nil
		}
		return &Builder{
			customPolicies:    policies.Custom,
			extensions:        in.Push.Mesh.ExtensionProviders,
			trustDomainBundle: trustDomainBundle,
			option:            option,
		}
	}

	if len(policies.Deny) == 0 && len(policies.Allow) == 0 && len(policies.Audit) == 0 {
		authzLog.Debugf("No ALLOW/DENY/AUDIT policy for workload %v in %s", workload, namespace)
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
			return configs.http
		}
		return nil
	}

	var filters []*httppb.HttpFilter
	if configs := b.build(b.auditPolicies, rbacpb.RBAC_LOG, false); configs != nil {
		filters = append(filters, configs.http...)
	}
	if configs := b.build(b.denyPolicies, rbacpb.RBAC_DENY, false); configs != nil {
		filters = append(filters, configs.http...)
	}
	if configs := b.build(b.allowPolicies, rbacpb.RBAC_ALLOW, false); configs != nil {
		filters = append(filters, configs.http...)
	}
	return filters
}

// BuildTCP returns the TCP filters built from the authorization policy.
func (b Builder) BuildTCP() []*tcppb.Filter {
	if b.option.IsCustomBuilder {
		if configs := b.build(b.customPolicies, rbacpb.RBAC_DENY, true); configs != nil {
			return configs.tcp
		}
		return nil
	}

	var filters []*tcppb.Filter
	if configs := b.build(b.auditPolicies, rbacpb.RBAC_LOG, true); configs != nil {
		filters = append(filters, configs.tcp...)
	}
	if configs := b.build(b.denyPolicies, rbacpb.RBAC_DENY, true); configs != nil {
		filters = append(filters, configs.tcp...)
	}
	if configs := b.build(b.allowPolicies, rbacpb.RBAC_ALLOW, true); configs != nil {
		filters = append(filters, configs.tcp...)
	}
	return filters
}

type builtConfigs struct {
	http []*httppb.HttpFilter
	tcp  []*tcppb.Filter
}

func (b Builder) build(policies []model.AuthorizationPolicy, action rbacpb.RBAC_Action, forTCP bool) *builtConfigs {
	if len(policies) == 0 {
		return nil
	}

	rules := &rbacpb.RBAC{
		Action:   action,
		Policies: map[string]*rbacpb.Policy{},
	}

	var providers []string
	for _, policy := range policies {
		if b.option.IsCustomBuilder {
			providers = append(providers, policy.Spec.GetProvider().GetName())
		}
		for i, rule := range policy.Spec.Rules {
			name := fmt.Sprintf("ns[%s]-policy[%s]-rule[%d]", policy.Namespace, policy.Name, i)
			if b.option.IsCustomBuilder {
				// The name will later be used by ext_authz filter to get the evaluation result from dynamic metadata.
				name = fmt.Sprintf("%s-%s", extAuthzMatchPrefix, name)
			}
			if rule == nil {
				authzLog.Errorf("Skipped nil rule %s", name)
				continue
			}
			m, err := authzmodel.New(rule, b.option.IsIstioVersionGE15)
			if err != nil {
				authzLog.Errorf("Skipped rule %s: %v", name, err)
				continue
			}
			m.MigrateTrustDomain(b.trustDomainBundle)
			generated, err := m.Generate(forTCP, action)
			if err != nil {
				if forTCP && b.option.IsOnInboundPassthrough {
					authzLog.Debugf("On TCP inbound passthrough filter chain, skipped rule %s: %v", name, err)
				} else {
					authzLog.Errorf("Skipped rule %s: %v", name, err)
				}
				continue
			}
			if generated != nil {
				rules.Policies[name] = generated
				authzLog.Debugf("Rule %s generated policy: %+v", name, generated)
			}
		}
	}

	if forTCP {
		return &builtConfigs{tcp: b.buildTCP(rules, providers)}
	}
	return &builtConfigs{http: b.buildHTTP(rules, providers)}
}

func (b Builder) buildHTTP(rules *rbacpb.RBAC, providers []string) []*httppb.HttpFilter {
	if !b.option.IsCustomBuilder {
		rbac := &rbachttppb.RBAC{Rules: rules}
		return []*httppb.HttpFilter{
			{
				Name:       authzmodel.RBACHTTPFilterName,
				ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	}

	extauthz, err := buildExtAuthz(b.extensions, providers)
	if err != nil {
		authzLog.Errorf("Failed parsing CUSTOM action, will generate a deny all config: %v", err)
		rbac := &rbachttppb.RBAC{Rules: rbacDefaultDenyAll}
		return []*httppb.HttpFilter{
			{
				Name:       authzmodel.RBACHTTPFilterName,
				ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	}
	// Add the RBAC filter in shadow mode so that it only evaluates the matching rules for CUSTOM action but not enforce it.
	// The evaluation result is stored in the dynamic metadata keyed by the policy name.
	// See https://docs.google.com/document/d/1V4mCQCw7mlGp0zSQQXYoBdbKMDnkPOjeyUb85U07iSI/edit#bookmark=kix.jdq8u0an2r6s
	// for more details.
	rbac := &rbachttppb.RBAC{ShadowRules: rules}
	return []*httppb.HttpFilter{
		{
			Name:       authzmodel.RBACHTTPFilterName,
			ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
		},
		{
			Name:       wellknown.HTTPExternalAuthorization,
			ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(extauthz.http)},
		},
	}
}

func (b Builder) buildTCP(rules *rbacpb.RBAC, providers []string) []*tcppb.Filter {
	if !b.option.IsCustomBuilder {
		rbac := &rbactcppb.RBAC{Rules: rules, StatPrefix: authzmodel.RBACTCPFilterStatPrefix}
		return []*tcppb.Filter{
			{
				Name:       authzmodel.RBACTCPFilterName,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	}

	if extauthz, err := buildExtAuthz(b.extensions, providers); err != nil {
		authzLog.Errorf("Failed parsing CUSTOM action, will generate a deny all config: %v", err)
		rbac := &rbactcppb.RBAC{Rules: rbacDefaultDenyAll, StatPrefix: authzmodel.RBACTCPFilterStatPrefix}
		return []*tcppb.Filter{
			{
				Name:       authzmodel.RBACTCPFilterName,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
		}
	} else if extauthz.tcp == nil {
		authzLog.Warnf("Ignored CUSTOM action with HTTP provider on TCP filter chain")
		return nil
	} else {
		rbac := &rbactcppb.RBAC{ShadowRules: rules, StatPrefix: authzmodel.RBACTCPFilterStatPrefix}
		return []*tcppb.Filter{
			{
				Name:       authzmodel.RBACTCPFilterName,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbac)},
			},
			{
				Name:       wellknown.ExternalAuthorization,
				ConfigType: &tcppb.Filter_TypedConfig{TypedConfig: util.MessageToAny(extauthz.tcp)},
			},
		}
	}
}
