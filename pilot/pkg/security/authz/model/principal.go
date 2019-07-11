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

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"

	authn_v1alpha1 "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	"istio.io/istio/pilot/pkg/security/authz/model/matcher"
	"istio.io/istio/pkg/spiffe"
)

type Principal struct {
	User          string // For backward-compatible only.
	Names         []string
	NotNames      []string
	Group         string // For backward-compatible only.
	Groups        []string
	NotGroups     []string
	Namespaces    []string
	NotNamespaces []string
	IPs           []string
	NotIPs        []string
	Properties    []KeyValues
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

	for _, p := range principal.Properties {
		for key := range p {
			if strings.HasPrefix(key, "request.auth.") || key == attrSrcUser {
				return fmt.Errorf("property(%v)", p)
			}
		}
	}
	return nil
}

func (principal *Principal) Generate(forTCPFilter bool) (*envoy_rbac.Principal, error) {
	if principal == nil {
		return nil, nil
	}

	if err := principal.ValidateForTCP(forTCPFilter); err != nil {
		return nil, err
	}

	pg := principalGenerator{}
	if principal.User != "" {
		principal := principalForKeyValue(attrSrcPrincipal, principal.User, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.Names) > 0 {
		principal := principalForKeyValues(attrSrcPrincipal, principal.Names, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.NotNames) > 0 {
		principal := principalForKeyValues(attrSrcPrincipal, principal.NotNames, forTCPFilter)
		pg.append(principalNot(principal))
	}

	if principal.Group != "" {
		principal := principalForKeyValue(attrRequestClaimGroups, principal.Group, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.Groups) > 0 {
		// TODO: Validate attrRequestClaimGroups and principal.Groups are not used at the same time.
		principal := principalForKeyValues(attrRequestClaimGroups, principal.Groups, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.NotGroups) > 0 {
		principal := principalForKeyValues(attrRequestClaimGroups, principal.NotGroups, forTCPFilter)
		pg.append(principalNot(principal))
	}

	if len(principal.Namespaces) > 0 {
		principal := principalForKeyValues(attrSrcNamespace, principal.Namespaces, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.NotNamespaces) > 0 {
		principal := principalForKeyValues(attrSrcNamespace, principal.NotNamespaces, forTCPFilter)
		pg.append(principalNot(principal))
	}

	if len(principal.IPs) > 0 {
		principal := principalForKeyValues(attrSrcIP, principal.IPs, forTCPFilter)
		pg.append(principal)
	}

	if len(principal.NotIPs) > 0 {
		principal := principalForKeyValues(attrSrcIP, principal.NotIPs, forTCPFilter)
		pg.append(principalNot(principal))
	}

	for _, p := range principal.Properties {
		// Use a separate key list to make sure the map iteration order is stable, so that the generated
		// config is stable.
		var keys []string
		for key := range p {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, k := range keys {
			// TODO: Validate attrSrcPrincipal and principal.Names are not used at the same time.
			principal := principalForKeyValues(k, p[k], forTCPFilter)
			if len(p[k]) == 1 {
				// FIXME: Temporary hack to avoid changing unit tests during code refactor. Remove once
				// we finish the code refactor with new unit tests.
				principal = principalForKeyValue(k, p[k][0], forTCPFilter)
			}
			pg.append(principal)
		}
	}

	if pg.isEmpty() {
		// None of above principal satisfied means nobody has the permission.
		principal := principalNot(principalAny(true))
		pg.append(principal)
	}

	return pg.andPrincipals(), nil
}

// principalForKeyValues converts a key-values pair to envoy RBAC principal. The key specify the
// type of the principal (e.g. source IP, source principals, etc.), the values specify the allowed
// value of the key, multiple values are ORed together.
func principalForKeyValues(key string, values []string, forTCPFilter bool) *envoy_rbac.Principal {
	pg := principalGenerator{}
	for _, value := range values {
		principal := principalForKeyValue(key, value, forTCPFilter)
		pg.append(principal)
	}
	return pg.orPrincipals()
}

func principalForKeyValue(key, value string, forTCPFilter bool) *envoy_rbac.Principal {
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
		metadata := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, attrSrcPrincipal, m)
		return principalMetadata(metadata)
	case attrSrcPrincipal == key:
		if value == allUsers || value == "*" {
			return principalAny(true)
		}
		// We don't allow users to use "*" in names or not_names. However, we will use "*" internally to
		// refer to authenticated users, since existing code using regex to map "*" to all authenticated
		// users.
		if value == allAuthenticatedUsers {
			value = "*"
		}

		if forTCPFilter {
			m := matcher.StringMatcherWithPrefix(value, spiffe.URIPrefix)
			return principalAuthenticated(m)
		}
		metadata := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, key, matcher.StringMatcher(value))
		return principalMetadata(metadata)
	case found(key, []string{attrRequestPrincipal, attrRequestAudiences, attrRequestPresenter, attrSrcUser}):
		m := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, key, matcher.StringMatcher(value))
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
		m := matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName, []string{attrRequestClaims, claim}, value)
		return principalMetadata(m)
	default:
		rbacLog.Debugf("generated dynamic metadata matcher for custom property: %s", key)
		filterName := RBACHTTPFilterName
		if forTCPFilter {
			filterName = RBACTCPFilterName
		}
		metadata := matcher.MetadataStringMatcher(filterName, key, matcher.StringMatcher(value))
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

func principalAny(any bool) *envoy_rbac.Principal {
	return &envoy_rbac.Principal{
		Identifier: &envoy_rbac.Principal_Any{
			Any: any,
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
