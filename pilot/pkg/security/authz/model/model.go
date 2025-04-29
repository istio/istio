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

package model

import (
	"fmt"
	"strings"

	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"k8s.io/apimachinery/pkg/types"

	authzpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/security/trustdomain"
)

const (
	RBACTCPFilterStatPrefix           = "tcp."
	RBACShadowEngineResult            = "shadow_engine_result"
	RBACShadowEffectivePolicyID       = "shadow_effective_policy_id"
	RBACShadowRulesAllowStatPrefix    = "istio_dry_run_allow_"
	RBACShadowRulesDenyStatPrefix     = "istio_dry_run_deny_"
	RBACExtAuthzShadowRulesStatPrefix = "istio_ext_authz_"

	attrRequestHeader       = "request.headers"                     // header name is surrounded by brackets, e.g. "request.headers[User-Agent]".
	attrRequestInlineHeader = "request.experimental.inline.headers" // header name is surrounded by brackets,
	// e.g. "request.experimental.inline.headers[User-Agent]".
	attrSrcIP             = "source.ip"                   // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrRemoteIP          = "remote.ip"                   // original client ip determined from x-forwarded-for or proxy protocol.
	attrSrcNamespace      = "source.namespace"            // e.g. "default".
	attrSrcServiceAccount = "source.serviceAccount"       // e.g. "default/productpage".
	attrSrcPrincipal      = "source.principal"            // source identity, e,g, "cluster.local/ns/default/sa/productpage".
	attrRequestPrincipal  = "request.auth.principal"      // authenticated principal of the request.
	attrRequestAudiences  = "request.auth.audiences"      // intended audience(s) for this authentication information.
	attrRequestPresenter  = "request.auth.presenter"      // authorized presenter of the credential.
	attrRequestClaims     = "request.auth.claims"         // claim name is surrounded by brackets, e.g. "request.auth.claims[iss]".
	attrDestIP            = "destination.ip"              // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrDestPort          = "destination.port"            // must be in the range [0, 65535].
	attrConnSNI           = "connection.sni"              // server name indication, e.g. "www.example.com".
	attrEnvoyFilter       = "experimental.envoy.filters." // an experimental attribute for checking Envoy Metadata directly.

	// Internal names used to generate corresponding Envoy matcher.
	methodHeader = ":method"
	pathMatcher  = "path-matcher"
	hostHeader   = ":authority"
)

type rule struct {
	key       string
	values    []string
	notValues []string
	g         generator
	// This generator aggregates value predicates
	extended extendedGenerator
}

type ruleList struct {
	rules []*rule
}

// Model represents a single rule from an authorization policy. The conditions of the rule are consolidated into
// permission or principal to align with the Envoy RBAC filter API.
type Model struct {
	permissions []ruleList
	principals  []ruleList
}

// New returns a model representing a single authorization policy.
func New(policyName types.NamespacedName, r *authzpb.Rule) (*Model, error) {
	m := Model{}

	basePermission := ruleList{}
	basePrincipal := ruleList{}

	// Each condition in the when needs to be consolidated into either permission or principal.
	for _, when := range r.When {
		k := when.Key
		switch {
		case k == attrDestIP:
			basePermission.appendLast(destIPGenerator{}, k, when.Values, when.NotValues)
		case k == attrDestPort:
			basePermission.appendLast(destPortGenerator{}, k, when.Values, when.NotValues)
		case k == attrConnSNI:
			basePermission.appendLast(connSNIGenerator{}, k, when.Values, when.NotValues)
		case strings.HasPrefix(k, attrEnvoyFilter):
			basePermission.appendLastExtended(envoyFilterGenerator{}, k, when.Values, when.NotValues)
		case k == attrSrcIP:
			basePrincipal.appendLast(srcIPGenerator{}, k, when.Values, when.NotValues)
		case k == attrRemoteIP:
			basePrincipal.appendLast(remoteIPGenerator{}, k, when.Values, when.NotValues)
		case k == attrSrcNamespace:
			basePrincipal.appendLast(srcNamespaceGenerator{}, k, when.Values, when.NotValues)
		case k == attrSrcServiceAccount:
			basePrincipal.appendLast(srcServiceAccountGenerator{policyName: policyName}, k, when.Values, when.NotValues)
		case k == attrSrcPrincipal:
			basePrincipal.appendLast(srcPrincipalGenerator{}, k, when.Values, when.NotValues)
		case k == attrRequestPrincipal:
			basePrincipal.appendLastExtended(requestPrincipalGenerator{}, k, when.Values, when.NotValues)
		case k == attrRequestAudiences:
			basePrincipal.appendLastExtended(requestAudiencesGenerator{}, k, when.Values, when.NotValues)
		case k == attrRequestPresenter:
			basePrincipal.appendLastExtended(requestPresenterGenerator{}, k, when.Values, when.NotValues)
		case strings.HasPrefix(k, attrRequestHeader):
			basePrincipal.appendLast(requestHeaderGenerator{}, k, when.Values, when.NotValues)
		case strings.HasPrefix(k, attrRequestInlineHeader):
			basePrincipal.appendLast(requestInlineHeaderGenerator{}, k, when.Values, when.NotValues)
		case strings.HasPrefix(k, attrRequestClaims):
			basePrincipal.appendLastExtended(requestClaimGenerator{}, k, when.Values, when.NotValues)
		default:
			return nil, fmt.Errorf("unknown attribute %s", when.Key)
		}
	}

	for _, from := range r.From {
		merged := basePrincipal.copy()
		if s := from.Source; s != nil {
			merged.insertFront(srcIPGenerator{}, attrSrcIP, s.IpBlocks, s.NotIpBlocks)
			merged.insertFront(remoteIPGenerator{}, attrRemoteIP, s.RemoteIpBlocks, s.NotRemoteIpBlocks)
			merged.insertFront(srcNamespaceGenerator{}, attrSrcNamespace, s.Namespaces, s.NotNamespaces)
			merged.insertFront(srcServiceAccountGenerator{policyName: policyName}, attrSrcServiceAccount, s.ServiceAccounts, s.NotServiceAccounts)
			merged.insertFrontExtended(requestPrincipalGenerator{}, attrRequestPrincipal, s.RequestPrincipals, s.NotRequestPrincipals)
			merged.insertFront(srcPrincipalGenerator{}, attrSrcPrincipal, s.Principals, s.NotPrincipals)
		}
		m.principals = append(m.principals, merged)
	}
	if len(r.From) == 0 {
		m.principals = append(m.principals, basePrincipal)
	}

	for _, to := range r.To {
		merged := basePermission.copy()
		if o := to.Operation; o != nil {
			merged.insertFront(destPortGenerator{}, attrDestPort, o.Ports, o.NotPorts)
			merged.insertFront(pathGenerator{}, pathMatcher, o.Paths, o.NotPaths)
			merged.insertFront(methodGenerator{}, methodHeader, o.Methods, o.NotMethods)
			merged.insertFront(hostGenerator{}, hostHeader, o.Hosts, o.NotHosts)
		}
		m.permissions = append(m.permissions, merged)
	}
	if len(r.To) == 0 {
		m.permissions = append(m.permissions, basePermission)
	}

	return &m, nil
}

// MigrateTrustDomain replaces the trust domain in source principal based on the trust domain aliases information.
func (m *Model) MigrateTrustDomain(tdBundle trustdomain.Bundle) {
	for _, p := range m.principals {
		for _, r := range p.rules {
			if r.key == attrSrcPrincipal {
				if len(r.values) != 0 {
					r.values = tdBundle.ReplaceTrustDomainAliases(r.values)
				}
				if len(r.notValues) != 0 {
					r.notValues = tdBundle.ReplaceTrustDomainAliases(r.notValues)
				}
			}
		}
	}
}

// Generate generates the Envoy RBAC config from the model.
func (m Model) Generate(forTCP bool, useAuthenticated bool, action rbacpb.RBAC_Action) (*rbacpb.Policy, error) {
	var permissions []*rbacpb.Permission
	for _, rl := range m.permissions {
		permission, err := generatePermission(rl, forTCP, action)
		if err != nil {
			return nil, err
		}
		permissions = append(permissions, permission)
	}
	if len(permissions) == 0 {
		return nil, fmt.Errorf("must have at least 1 permission")
	}

	var principals []*rbacpb.Principal
	for _, rl := range m.principals {
		principal, err := generatePrincipal(rl, forTCP, useAuthenticated, action)
		if err != nil {
			return nil, err
		}
		principals = append(principals, principal)
	}
	if len(principals) == 0 {
		return nil, fmt.Errorf("must have at least 1 principal")
	}

	return &rbacpb.Policy{
		Permissions: permissions,
		Principals:  principals,
	}, nil
}

func generatePermission(rl ruleList, forTCP bool, action rbacpb.RBAC_Action) (*rbacpb.Permission, error) {
	var and []*rbacpb.Permission
	for _, r := range rl.rules {
		ret, err := r.permission(forTCP, action)
		if err != nil {
			return nil, err
		}
		and = append(and, ret...)
	}
	if len(and) == 0 {
		and = append(and, permissionAny())
	}
	return permissionAnd(and), nil
}

func generatePrincipal(rl ruleList, forTCP bool, useAuthenticated bool, action rbacpb.RBAC_Action) (*rbacpb.Principal, error) {
	var and []*rbacpb.Principal
	for _, r := range rl.rules {
		ret, err := r.principal(forTCP, useAuthenticated, action)
		if err != nil {
			return nil, err
		}
		and = append(and, ret...)
	}
	if len(and) == 0 {
		and = append(and, principalAny())
	}
	return principalAnd(and), nil
}

func (r rule) permission(forTCP bool, action rbacpb.RBAC_Action) ([]*rbacpb.Permission, error) {
	var permissions []*rbacpb.Permission
	if r.extended != nil {
		if len(r.values) > 0 {
			p, err := r.extended.extendedPermission(r.key, r.values, forTCP)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				permissions = append(permissions, p)
			}
		}
	} else {
		var or []*rbacpb.Permission
		for _, value := range r.values {
			p, err := r.g.permission(r.key, value, forTCP)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				or = append(or, p)
			}
		}
		if len(or) > 0 {
			permissions = append(permissions, permissionOr(or))
		}
	}

	if r.extended != nil {
		if len(r.notValues) > 0 {
			p, err := r.extended.extendedPermission(r.key, r.notValues, forTCP)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				permissions = append(permissions, permissionNot(p))
			}
		}
	} else {
		var or []*rbacpb.Permission
		for _, notValue := range r.notValues {
			p, err := r.g.permission(r.key, notValue, forTCP)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				or = append(or, p)
			}
		}
		if len(or) > 0 {
			permissions = append(permissions, permissionNot(permissionOr(or)))
		}
	}
	return permissions, nil
}

func (r rule) principal(forTCP bool, useAuthenticated bool, action rbacpb.RBAC_Action) ([]*rbacpb.Principal, error) {
	var principals []*rbacpb.Principal
	if r.extended != nil {
		if len(r.values) > 0 {
			p, err := r.extended.extendedPrincipal(r.key, r.values, forTCP)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				principals = append(principals, p)
			}
		}
	} else {
		var or []*rbacpb.Principal
		for _, value := range r.values {
			p, err := r.g.principal(r.key, value, forTCP, useAuthenticated)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				or = append(or, p)
			}
		}
		if len(or) > 0 {
			principals = append(principals, principalOr(or))
		}
	}

	if r.extended != nil {
		if len(r.notValues) > 0 {
			p, err := r.extended.extendedPrincipal(r.key, r.notValues, forTCP)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				principals = append(principals, principalNot(p))
			}
		}
	} else {
		var or []*rbacpb.Principal
		for _, notValue := range r.notValues {
			p, err := r.g.principal(r.key, notValue, forTCP, useAuthenticated)
			if err := r.checkError(action, err); err != nil {
				return nil, err
			}
			if p != nil {
				or = append(or, p)
			}
		}
		if len(or) > 0 {
			principals = append(principals, principalNot(principalOr(or)))
		}
	}
	return principals, nil
}

func (r rule) checkError(action rbacpb.RBAC_Action, err error) error {
	if action == rbacpb.RBAC_ALLOW {
		// Return the error as-is for allow policy. This will make all rules in the current permission ignored, effectively
		// result in a smaller allow policy (i.e. less likely to allow a request).
		return err
	}

	// Ignore the error for a deny or audit policy. This will make the current rule ignored and continue the generation of
	// the next rule, effectively resulting in a wider deny or audit policy (i.e. more likely to deny or audit a request).
	return nil
}

func (p *ruleList) copy() ruleList {
	r := ruleList{}
	r.rules = append([]*rule{}, p.rules...)
	return r
}

func (p *ruleList) insertFront(g generator, key string, values, notValues []string) {
	if len(values) == 0 && len(notValues) == 0 {
		return
	}
	r := &rule{
		key:       key,
		values:    values,
		notValues: notValues,
		g:         g,
	}

	p.rules = append([]*rule{r}, p.rules...)
}

func (p *ruleList) insertFrontExtended(g extendedGenerator, key string, values, notValues []string) {
	if len(values) == 0 && len(notValues) == 0 {
		return
	}
	r := &rule{
		key:       key,
		values:    values,
		notValues: notValues,
		extended:  g,
	}

	p.rules = append([]*rule{r}, p.rules...)
}

func (p *ruleList) appendLast(g generator, key string, values, notValues []string) {
	if len(values) == 0 && len(notValues) == 0 {
		return
	}
	r := &rule{
		key:       key,
		values:    values,
		notValues: notValues,
		g:         g,
	}

	p.rules = append(p.rules, r)
}

func (p *ruleList) appendLastExtended(g extendedGenerator, key string, values, notValues []string) {
	if len(values) == 0 && len(notValues) == 0 {
		return
	}
	r := &rule{
		key:       key,
		values:    values,
		notValues: notValues,
		extended:  g,
	}
	p.rules = append(p.rules, r)
}
