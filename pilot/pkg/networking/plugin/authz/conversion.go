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

package authz

import (
	"fmt"
	"strings"

	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"

	"istio.io/istio/pilot/pkg/networking/plugin/authz/matcher"
	"istio.io/istio/pilot/pkg/networking/plugin/authz/rbacfilter"
	authn_v1alpha1 "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	"istio.io/istio/pkg/spiffe"
)

// permissionForKeyValues converts a key-values pair to an envoy RBAC permission. The key specify the
// type of the permission (e.g. destination IP, header, SNI, etc.), the values specify the allowed
// value of the key, multiple values are ORed together.
func permissionForKeyValues(key string, values []string) *envoy_rbac.Permission {
	var converter func(string) (*envoy_rbac.Permission, error)
	switch {
	case key == attrDestIP:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			cidr, err := matcher.CidrRange(v)
			if err != nil {
				return nil, err
			}
			return rbacfilter.PermissionDestinationIP(cidr), nil
		}
	case key == attrDestPort:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			portValue, err := convertToPort(v)
			if err != nil {
				return nil, err
			}
			return rbacfilter.PermissionDestinationPort(portValue), nil
		}
	case key == pathHeader || key == methodHeader || key == hostHeader:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			m := matcher.HeaderMatcher(key, v)
			return rbacfilter.PermissionHeader(m), nil
		}
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		converter = func(v string) (*envoy_rbac.Permission, error) {
			m := matcher.HeaderMatcher(header, v)
			return rbacfilter.PermissionHeader(m), nil
		}
	case key == attrConnSNI:
		converter = func(v string) (*envoy_rbac.Permission, error) {
			m := matcher.StringMatcher(v)
			return rbacfilter.PermissionRequestedServerName(m), nil
		}
	case strings.HasPrefix(key, "experimental.envoy.filters.") && isKeyBinary(key):
		// Split key of format experimental.envoy.filters.a.b[c] to [envoy.filters.a.b, c].
		parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(key, "experimental."), "]"), "[", 2)
		converter = func(v string) (*envoy_rbac.Permission, error) {
			// If value is of format [v], create a list matcher.
			// Else, if value is of format v, create a string matcher.
			var m *envoy_matcher.MetadataMatcher
			if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
				m = matcher.MetadataListMatcher(parts[0], parts[1:], strings.Trim(v, "[]"))
			} else {
				m = matcher.MetadataStringMatcher(parts[0], parts[1], matcher.StringMatcher(v))
			}
			return rbacfilter.PermissionMetadata(m), nil
		}
	default:
		if !found(key, []string{attrDestName, attrDestNamespace, attrDestUser}) &&
			!strings.HasPrefix(key, attrDestLabel) {
			// The attribute is neither matched here nor in previous stage, this means it's something we
			// don't understand, most likely a user typo.
			rbacLog.Errorf("ignored unsupported constraint: %s", key)
		}
		return nil
	}

	pg := rbacfilter.PermissionGenerator{}
	for _, v := range values {
		if permission, err := converter(v); err != nil {
			rbacLog.Errorf("ignored invalid constraint value: %v", err)
		} else {
			pg.Append(permission)
		}
	}
	return pg.OrPermissions()
}

// principalForKeyValues converts a key-values pair to envoy RBAC principal. The key specify the
// type of the principal (e.g. source IP, source principals, etc.), the values specify the allowed
// value of the key, multiple values are ORed together.
func principalForKeyValues(key string, values []string, forTCPFilter bool) *envoy_rbac.Principal {
	pg := rbacfilter.PrincipalGenerator{}
	for _, value := range values {
		principal := principalForKeyValue(key, value, forTCPFilter)
		pg.Append(principal)
	}
	return pg.OrPrincipals()
}

func principalForKeyValue(key, value string, forTCPFilter bool) *envoy_rbac.Principal {
	switch {
	case attrSrcIP == key:
		cidr, err := matcher.CidrRange(value)
		if err != nil {
			rbacLog.Errorf("ignored invalid source ip value: %v", err)
			return nil
		}
		return rbacfilter.PrincipalSourceIP(cidr)
	case attrSrcNamespace == key:
		if forTCPFilter {
			regex := fmt.Sprintf(".*/ns/%s/.*", value)
			m := matcher.StringMatcherRegex(regex)
			return rbacfilter.PrincipalAuthenticated(m)
		}
		// Proxy doesn't have attrSrcNamespace directly, but the information is encoded in attrSrcPrincipal
		// with format: cluster.local/ns/{NAMESPACE}/sa/{SERVICE-ACCOUNT}.
		value = strings.Replace(value, "*", ".*", -1)
		m := matcher.StringMatcherRegex(fmt.Sprintf(".*/ns/%s/.*", value))
		metadata := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, attrSrcPrincipal, m)
		return rbacfilter.PrincipalMetadata(metadata)
	case attrSrcPrincipal == key:
		if value == allUsers || value == "*" {
			return rbacfilter.PrincipalAny(true)
		}
		// We don't allow users to use "*" in names or not_names. However, we will use "*" internally to
		// refer to authenticated users, since existing code using regex to map "*" to all authenticated
		// users.
		if value == allAuthenticatedUsers {
			value = "*"
		}

		if forTCPFilter {
			m := matcher.StringMatcherWithPrefix(value, spiffe.URIPrefix)
			return rbacfilter.PrincipalAuthenticated(m)
		}
		metadata := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, key, matcher.StringMatcher(value))
		return rbacfilter.PrincipalMetadata(metadata)
	case found(key, []string{attrRequestPrincipal, attrRequestAudiences, attrRequestPresenter, attrSrcUser}):
		m := matcher.MetadataStringMatcher(authn_v1alpha1.AuthnFilterName, key, matcher.StringMatcher(value))
		return rbacfilter.PrincipalMetadata(m)
	case strings.HasPrefix(key, attrRequestHeader):
		header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
		if err != nil {
			rbacLog.Errorf("ignored invalid %s: %v", attrRequestHeader, err)
			return nil
		}
		m := matcher.HeaderMatcher(header, value)
		return rbacfilter.PrincipalHeader(m)
	case strings.HasPrefix(key, attrRequestClaims):
		claim, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestClaims))
		if err != nil {
			return nil
		}
		// Generate a metadata list matcher for the given path keys and value.
		// On proxy side, the value should be of list type.
		m := matcher.MetadataListMatcher(authn_v1alpha1.AuthnFilterName, []string{attrRequestClaims, claim}, value)
		return rbacfilter.PrincipalMetadata(m)
	default:
		rbacLog.Debugf("generated dynamic metadata matcher for custom property: %s", key)
		filterName := rbacHTTPFilterName
		if forTCPFilter {
			filterName = rbacTCPFilterName
		}
		metadata := matcher.MetadataStringMatcher(filterName, key, matcher.StringMatcher(value))
		return rbacfilter.PrincipalMetadata(metadata)
	}
}
