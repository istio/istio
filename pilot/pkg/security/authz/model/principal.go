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

package model

import (
	"fmt"
	"sort"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"

	"istio.io/istio/pilot/pkg/security/authz/model/matcher"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/spiffe"
)

type Principal struct {
	Users                []string // For backward-compatible only.
	Names                []string
	NotNames             []string
	Group                string // For backward-compatible only.
	Groups               []string
	NotGroups            []string
	Namespaces           []string
	NotNamespaces        []string
	IPs                  []string
	NotIPs               []string
	RequestPrincipals    []string
	NotRequestPrincipals []string
	Properties           []KeyValues
	AllowAll             bool
	v1beta1              bool
}

// ValidateForTCP checks if the principal is valid for TCP filter. A principal is not valid for TCP
// filter if it includes any HTTP-only fields, e.g. group, etc.
func (principal *Principal) ValidateForTCP(forTCP bool) error {
	if principal == nil || !forTCP {
		return nil
	}

	if principal.Group != "" {
		return fmt.Errorf("group(%v)", principal.Groups)
	}
	if len(principal.Groups) != 0 {
		return fmt.Errorf("groups(%v)", principal.Groups)
	}
	if len(principal.NotGroups) != 0 {
		return fmt.Errorf("not_groups(%v)", principal.NotGroups)
	}
	if len(principal.RequestPrincipals) != 0 {
		return fmt.Errorf("request_principals(%v)", principal.RequestPrincipals)
	}
	if len(principal.NotRequestPrincipals) != 0 {
		return fmt.Errorf("not_request_principals(%v)", principal.NotRequestPrincipals)
	}

	for _, p := range principal.Properties {
		for key := range p {
			if !validPropertyForTCP(key) {
				return fmt.Errorf("property(%v)", p)
			}
		}
	}
	return nil
}

func validPropertyForTCP(p string) bool {
	// The "request.auth.*" and "request.headers" attributes are only available for HTTP.
	// See https://istio.io/docs/reference/config/security/conditions/.
	return !strings.HasPrefix(p, "request.") && p != attrSrcUser
}

// Generate generates the RBAC filter config for the given principal.
// When the policy uses HTTP fields for TCP filter (forTCPFilter is true):
// - If it's allow policy (forDenyPolicy is false), returns nil so that the allow policy is ignored to avoid granting more permissions in this case.
// - If it's deny policy (forDenyPolicy is true), returns a config that only includes the TCP fields (e.g. source.principal). This makes sure
//   the generated deny policy is more restrictive so that it never grants any extra permission in this case.
func (principal *Principal) Generate(forTCPFilter, forDenyPolicy bool) (*envoy_rbac.Principal, error) {
	if principal == nil {
		return nil, nil
	}

	// When true, the function will only handle the TCP fields in the principal.
	onlyTCPFields := false
	if err := principal.ValidateForTCP(forTCPFilter); err != nil {
		if !forDenyPolicy {
			return nil, err
		}
		// Set onlyTCPFields to true if the deny policy is using HTTP fields so that
		// we generate a deny policy with only TCP fields.
		onlyTCPFields = true
	}

	pg := principalGenerator{}

	if principal.AllowAll {
		pg.append(principalAny())
		return pg.andPrincipals(), nil
	}

	if !onlyTCPFields && len(principal.Users) != 0 {
		principal := principal.forKeyValues(attrSrcPrincipal, principal.Users, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.Names) > 0 {
		principal := principal.forKeyValues(attrSrcPrincipal, principal.Names, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.NotNames) > 0 {
		principal := principal.forKeyValues(attrSrcPrincipal, principal.NotNames, forTCPFilter)
		pg.append(principalNot(principal))
	}

	if !onlyTCPFields {
		if len(principal.RequestPrincipals) > 0 {
			principal := principal.forKeyValues(attrRequestPrincipal, principal.RequestPrincipals, forTCPFilter)
			pg.append(principal)
		}

		if len(principal.NotRequestPrincipals) > 0 {
			principal := principal.forKeyValues(attrRequestPrincipal, principal.NotRequestPrincipals, forTCPFilter)
			pg.append(principalNot(principal))
		}

		if principal.Group != "" {
			principal := principal.forKeyValue(attrRequestClaimGroups, principal.Group, forTCPFilter)
			pg.append(principal)
		}

		if len(principal.Groups) > 0 {
			// TODO: Validate attrRequestClaimGroups and principal.Groups are not used at the same time.
			principal := principal.forKeyValues(attrRequestClaimGroups, principal.Groups, forTCPFilter)
			pg.append(principal)
		}

		if len(principal.NotGroups) > 0 {
			principal := principal.forKeyValues(attrRequestClaimGroups, principal.NotGroups, forTCPFilter)
			pg.append(principalNot(principal))
		}
	}

	if len(principal.Namespaces) > 0 {
		principal := principal.forKeyValues(attrSrcNamespace, principal.Namespaces, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.NotNamespaces) > 0 {
		principal := principal.forKeyValues(attrSrcNamespace, principal.NotNamespaces, forTCPFilter)
		pg.append(principalNot(principal))
	}

	if len(principal.IPs) > 0 {
		principal := principal.forKeyValues(attrSrcIP, principal.IPs, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.NotIPs) > 0 {
		principal := principal.forKeyValues(attrSrcIP, principal.NotIPs, forTCPFilter)
		pg.append(principalNot(principal))
	}

	for _, p := range principal.Properties {
		// Use a separate key list to make sure the map iteration order is stable, so that the generated
		// config is stable.
		var keys []string
		for key := range p {
			if !onlyTCPFields || validPropertyForTCP(key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)

		for _, k := range keys {
			// TODO: Validate attrSrcPrincipal and principal.Names are not used at the same time.
			if len(p[k].Values) > 0 {
				newPrincipal := principal.forKeyValues(k, p[k].Values, forTCPFilter)
				pg.append(newPrincipal)
			}
			if len(p[k].NotValues) > 0 {
				newPrincipal := principal.forKeyValues(k, p[k].NotValues, forTCPFilter)
				pg.append(principalNot(newPrincipal))
			}
		}
	}

	if pg.isEmpty() {
		var principal *envoy_rbac.Principal
		if forDenyPolicy {
			// Deny everything
			principal = principalAny()
		} else {
			// None of above principal satisfied means nobody has the permission.
			principal = principalNot(principalAny())
		}
		pg.append(principal)
	}

	return pg.andPrincipals(), nil
}

// isSupportedPrincipal returns true if the key is supported to be used in principal.
func isSupportedPrincipal(key string) bool {
	switch {
	case attrSrcIP == key:
	case attrSrcNamespace == key:
	case attrSrcPrincipal == key:
	case found(key, []string{attrRequestPrincipal, attrRequestAudiences, attrRequestPresenter, attrSrcUser}):
	case strings.HasPrefix(key, attrRequestHeader):
	case strings.HasPrefix(key, attrRequestClaims):
	default:
		return false
	}
	return true
}

// forKeyValues converts a key-values pair to envoy RBAC principal. The key specify the
// type of the principal (e.g. source IP, source principals, etc.), the values specify the allowed
// value of the key, multiple values are ORed together.
func (principal *Principal) forKeyValues(key string, values []string, forTCPFilter bool) *envoy_rbac.Principal {
	pg := principalGenerator{}
	for _, value := range values {
		principal := principal.forKeyValue(key, value, forTCPFilter)
		pg.append(principal)
	}
	return pg.orPrincipals()
}

func (principal *Principal) forKeyValue(key, value string, forTCPFilter bool) *envoy_rbac.Principal {
	switch {
	case attrSrcIP == key:
		cidr, err := matcher.CidrRange(value)
		if err != nil {
			rbacLog.Errorf("ignored invalid source ip value: %v", err)
			return nil
		}
		return principalSourceIP(cidr)
	case attrSrcNamespace == key:
		if forTCPFilter {
			regex := fmt.Sprintf(".*/ns/%s/.*", value)
			m := matcher.StringMatcherRegex(regex)
			return principalAuthenticated(m)
		}
		// Proxy doesn't have attrSrcNamespace directly, but the information is encoded in attrSrcPrincipal
		// with format: cluster.local/ns/{NAMESPACE}/sa/{SERVICE-ACCOUNT}.
		value = strings.Replace(value, "*", ".*", -1)
		m := matcher.StringMatcherRegex(fmt.Sprintf(".*/ns/%s/.*", value))
		metadata := matcher.MetadataStringMatcher(authn_model.AuthnFilterName, attrSrcPrincipal, m)
		return principalMetadata(metadata)
	case attrSrcPrincipal == key:
		if !principal.v1beta1 {
			// Legacy support for the v1alpha1 policy. The v1beta1 doesn't support these.
			if value == allUsers || value == "*" {
				return principalAny()
			}
			// We don't allow users to use "*" in names or not_names. However, we will use "*" internally to
			// refer to authenticated users, since existing code using regex to map "*" to all authenticated
			// users.
			if value == allAuthenticatedUsers {
				value = "*"
			}
		}

		if forTCPFilter {
			m := matcher.StringMatcherWithPrefix(value, spiffe.URIPrefix, principal.v1beta1)
			return principalAuthenticated(m)
		}
		metadata := matcher.MetadataStringMatcher(authn_model.AuthnFilterName, key, matcher.StringMatcher(value, principal.v1beta1))
		return principalMetadata(metadata)
	case found(key, []string{attrRequestPrincipal, attrRequestAudiences, attrRequestPresenter, attrSrcUser}):
		m := matcher.MetadataStringMatcher(authn_model.AuthnFilterName, key, matcher.StringMatcher(value, principal.v1beta1))
		return principalMetadata(m)
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		m := matcher.HeaderMatcher(header, value)
		return principalHeader(m)
	case strings.HasPrefix(key, attrRequestClaims):
		claim, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestClaims))
		if err != nil {
			return nil
		}
		// Generate a metadata list matcher for the given path keys and value.
		// On proxy side, the value should be of list type.
		m := matcher.MetadataListMatcher(authn_model.AuthnFilterName, []string{attrRequestClaims, claim}, value, principal.v1beta1)
		return principalMetadata(m)
	default:
		rbacLog.Debugf("generated dynamic metadata matcher for custom property: %s", key)
		filterName := RBACHTTPFilterName
		if forTCPFilter {
			filterName = RBACTCPFilterName
		}
		metadata := matcher.MetadataStringMatcher(filterName, key, matcher.StringMatcher(value, principal.v1beta1))
		return principalMetadata(metadata)
	}
}

type principalGenerator struct {
	principals []*envoy_rbac.Principal
}

func (pg *principalGenerator) isEmpty() bool {
	return len(pg.principals) == 0
}

func (pg *principalGenerator) append(principal *envoy_rbac.Principal) {
	if principal == nil {
		return
	}
	pg.principals = append(pg.principals, principal)
}

func (pg *principalGenerator) andPrincipals() *envoy_rbac.Principal {
	if pg.isEmpty() {
		return nil
	}

	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_AndIds{
			AndIds: &envoy_rbac.Principal_Set{
				Ids: pg.principals,
			},
		},
	}
}

func (pg *principalGenerator) orPrincipals() *envoy_rbac.Principal {
	if pg.isEmpty() {
		return nil
	}

	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_OrIds{
			OrIds: &envoy_rbac.Principal_Set{
				Ids: pg.principals,
			},
		},
	}
}

func principalAny() *envoy_rbac.Principal {
	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_Any{
			Any: true,
		},
	}
}

func principalNot(principal *envoy_rbac.Principal) *envoy_rbac.Principal {
	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_NotId{
			NotId: principal,
		},
	}
}

func principalAuthenticated(name *envoy_matcher.StringMatcher) *envoy_rbac.Principal {
	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_Authenticated_{
			Authenticated: &envoy_rbac.Principal_Authenticated{
				PrincipalName: name,
			},
		},
	}
}

func principalSourceIP(cidr *core.CidrRange) *envoy_rbac.Principal {
	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_SourceIp{
			SourceIp: cidr,
		},
	}
}

func principalMetadata(metadata *envoy_matcher.MetadataMatcher) *envoy_rbac.Principal {
	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_Metadata{
			Metadata: metadata,
		},
	}
}

func principalHeader(header *route.HeaderMatcher) *envoy_rbac.Principal {
	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_Header{
			Header: header,
		},
	}
}
