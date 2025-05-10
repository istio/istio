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
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/security/authz/matcher"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/spiffe"
)

type generator interface {
	permission(key, value string, forTCP bool) (*rbacpb.Permission, error)
	principal(key, value string, forTCP bool, useAuthenticated bool) (*rbacpb.Principal, error)
}

type extendedGenerator interface {
	extendedPermission(key string, value []string, forTCP bool) (*rbacpb.Permission, error)
	extendedPrincipal(key string, value []string, forTCP bool) (*rbacpb.Principal, error)
}

type destIPGenerator struct{}

func (destIPGenerator) permission(_, value string, _ bool) (*rbacpb.Permission, error) {
	cidrRange, err := util.AddrStrToCidrRange(value)
	if err != nil {
		return nil, err
	}
	return permissionDestinationIP(cidrRange), nil
}

func (destIPGenerator) principal(_, _ string, _ bool, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}

type destPortGenerator struct{}

func (destPortGenerator) permission(_, value string, _ bool) (*rbacpb.Permission, error) {
	portValue, err := convertToPort(value)
	if err != nil {
		return nil, err
	}
	return permissionDestinationPort(portValue), nil
}

func (destPortGenerator) principal(_, _ string, _ bool, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}

type connSNIGenerator struct{}

func (connSNIGenerator) permission(_, value string, _ bool) (*rbacpb.Permission, error) {
	m := matcher.StringMatcher(value)
	return permissionRequestedServerName(m), nil
}

func (connSNIGenerator) principal(_, _ string, _ bool, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}

type envoyFilterGenerator struct{}

func (efg envoyFilterGenerator) permission(key, value string, _ bool) (*rbacpb.Permission, error) {
	// Split key of format "experimental.envoy.filters.a.b[c]" to "envoy.filters.a.b" and "c".
	parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(key, "experimental."), "]"), "[", 2)

	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid key: %v", key)
	}

	// If value is of format [v], create a list matcher.
	// Else, if value is of format v, create a string matcher.
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		m := matcher.MetadataListMatcher(parts[0], parts[1:], matcher.StringMatcher(strings.Trim(value, "[]")), false)
		return permissionMetadata(m), nil
	}
	m := matcher.MetadataStringMatcher(parts[0], parts[1], matcher.StringMatcher(value))
	return permissionMetadata(m), nil
}

func (efg envoyFilterGenerator) extendedPermission(key string, values []string, _ bool) (*rbacpb.Permission, error) {
	// Split key of format "experimental.envoy.filters.a.b[c]" to "envoy.filters.a.b" and "c".
	parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(key, "experimental."), "]"), "[", 2)

	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid key: %v", key)
	}

	matchers := []*matcherpb.ValueMatcher{}
	for _, value := range values {
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			matchers = append(matchers, &matcherpb.ValueMatcher{
				MatchPattern: &matcherpb.ValueMatcher_ListMatch{
					ListMatch: &matcherpb.ListMatcher{
						MatchPattern: &matcherpb.ListMatcher_OneOf{
							OneOf: &matcherpb.ValueMatcher{
								MatchPattern: &matcherpb.ValueMatcher_StringMatch{
									StringMatch: matcher.StringMatcher(strings.Trim(value, "[]")),
								},
							},
						},
					},
				},
			})
		} else {
			matchers = append(matchers, &matcherpb.ValueMatcher{
				MatchPattern: &matcherpb.ValueMatcher_StringMatch{
					StringMatch: matcher.StringMatcher(value),
				},
			})
		}
	}
	m := matcher.MetadataValueMatcher(parts[0], parts[1], matcher.OrMatcher(matchers))
	return permissionMetadata(m), nil
}

func (envoyFilterGenerator) principal(_, _ string, _ bool, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (envoyFilterGenerator) extendedPrincipal(_ string, _ []string, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}

type srcIPGenerator struct{}

func (srcIPGenerator) permission(_, _ string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (srcIPGenerator) principal(_, value string, _ bool, _ bool) (*rbacpb.Principal, error) {
	cidr, err := util.AddrStrToCidrRange(value)
	if err != nil {
		return nil, err
	}
	return principalDirectRemoteIP(cidr), nil
}

type remoteIPGenerator struct{}

func (remoteIPGenerator) permission(_, _ string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (remoteIPGenerator) principal(_, value string, _ bool, _ bool) (*rbacpb.Principal, error) {
	cidr, err := util.AddrStrToCidrRange(value)
	if err != nil {
		return nil, err
	}
	return principalRemoteIP(cidr), nil
}

type srcNamespaceGenerator struct{}

func (srcNamespaceGenerator) permission(_, _ string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (srcNamespaceGenerator) principal(_, value string, _ bool, useAuthenticated bool) (*rbacpb.Principal, error) {
	v := strings.Replace(value, "*", ".*", -1)
	m := matcher.StringMatcherRegex(fmt.Sprintf(".*/ns/%s/.*", v))
	return principalAuthenticated(m, useAuthenticated), nil
}

type srcServiceAccountGenerator struct {
	policyName types.NamespacedName
}

func (g srcServiceAccountGenerator) permission(_, _ string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (g srcServiceAccountGenerator) principal(_, value string, _ bool, useAuthenticated bool) (*rbacpb.Principal, error) {
	regex := serviceAccountRegex(g.policyName.Namespace, value)
	m := matcher.StringMatcherRegex(regex)
	return principalAuthenticated(m, useAuthenticated), nil
}

// serviceAccountRegex builds a regex that will match the SA specifier.
// The specifier takes a `<name>` or `<namespace>/<name>` value. If namespace is elided, defaultNamespace is used.
func serviceAccountRegex(defaultNamespace string, value string) string {
	ns, sa, ok := strings.Cut(value, "/")
	if !ok {
		ns = defaultNamespace
		sa = value
	}
	// Format should follow...
	// 'spiffe://' then a trust domain + arbitrary k/v pairs
	// '/ns/<namespace>/'
	// optional arbitrary k/v pairs
	// '/sa/<serviceAccount>'
	// Either end of string OR / + arbitrary k/v pairs (the / ensures we do not match <service account>-some-junk)
	return fmt.Sprintf("spiffe://.+/ns/%s/(.+/|)sa/%s(/.+)?", ns, sa)
}

type srcPrincipalGenerator struct{}

func (srcPrincipalGenerator) permission(_, _ string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (srcPrincipalGenerator) principal(key, value string, _ bool, useAuthenticated bool) (*rbacpb.Principal, error) {
	m := matcher.StringMatcherWithPrefix(value, spiffe.URIPrefix)
	return principalAuthenticated(m, useAuthenticated), nil
}

type requestPrincipalGenerator struct{}

func (requestPrincipalGenerator) extendedPermission(_ string, _ []string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

var matchAny = matcher.StringMatcherRegex(".+")

func (rpg requestPrincipalGenerator) extendedPrincipal(key string, values []string, forTCP bool) (*rbacpb.Principal, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}
	var or []*rbacpb.Principal
	for _, value := range values {
		// Use the last index of "/" since issuer may be an URL
		idx := strings.LastIndex(value, "/")
		found := idx >= 0
		iss, sub := "", ""
		if found {
			iss, sub = value[:idx], value[idx+1:]
		} else {
			iss = value
		}
		var matchIss, matchSub *matcherpb.StringMatcher
		switch {
		case value == "*":
			matchIss = matchAny
			matchSub = matchAny
		case strings.HasPrefix(value, "*"):
			if found {
				if iss == "*" {
					matchIss = matchAny
				} else {
					matchIss = matcher.StringMatcherSuffix(strings.TrimPrefix(iss, "*"), false)
				}
				matchSub = matcher.StringMatcherExact(sub, false)
			} else {
				matchIss = matchAny
				matchSub = matcher.StringMatcherSuffix(strings.TrimPrefix(value, "*"), false)
			}
		case strings.HasSuffix(value, "*"):
			if found {
				matchIss = matcher.StringMatcherExact(iss, false)
				if sub == "*" {
					matchSub = matchAny
				} else {
					matchSub = matcher.StringMatcherPrefix(strings.TrimSuffix(sub, "*"), false)
				}
			} else {
				matchIss = matcher.StringMatcherPrefix(strings.TrimSuffix(value, "*"), false)
				matchSub = matchAny
			}
		default:
			matchSub = matcher.StringMatcherExact(sub, false)
			matchIss = matcher.StringMatcherExact(iss, false)
		}
		im := MetadataStringMatcherForJWTClaim("iss", matchIss)
		sm := MetadataStringMatcherForJWTClaim("sub", matchSub)
		or = append(or, principalAnd([]*rbacpb.Principal{principalMetadata(im), principalMetadata(sm)}))
	}
	if len(or) == 1 {
		return or[0], nil
	} else if len(or) > 0 {
		return principalOr(or), nil
	}
	return nil, nil
}

type requestAudiencesGenerator struct{}

func (requestAudiencesGenerator) extendedPermission(key string, values []string, forTCP bool) (*rbacpb.Permission, error) {
	return requestPrincipalGenerator{}.extendedPermission(key, values, forTCP)
}

func (rag requestAudiencesGenerator) extendedPrincipal(key string, values []string, forTCP bool) (*rbacpb.Principal, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}
	return principalMetadata(MetadataListValueMatcherForJWTClaims([]string{"aud"}, matcher.StringOrMatcher(values))), nil
}

type requestPresenterGenerator struct{}

func (requestPresenterGenerator) extendedPermission(key string, values []string, forTCP bool) (*rbacpb.Permission, error) {
	return requestPrincipalGenerator{}.extendedPermission(key, values, forTCP)
}

func (rpg requestPresenterGenerator) extendedPrincipal(key string, values []string, forTCP bool) (*rbacpb.Principal, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}
	return principalMetadata(MetadataListValueMatcherForJWTClaims([]string{"azp"}, matcher.StringOrMatcher(values))), nil
}

type requestHeaderGenerator struct{}

func (requestHeaderGenerator) permission(_, _ string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (requestHeaderGenerator) principal(key, value string, forTCP bool, _ bool) (*rbacpb.Principal, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}

	header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestHeader))
	if err != nil {
		return nil, err
	}
	m := matcher.HeaderMatcher(header, value)
	return principalHeader(m), nil
}

type requestInlineHeaderGenerator struct{}

func (requestInlineHeaderGenerator) permission(_, _ string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (requestInlineHeaderGenerator) principal(key, value string, forTCP bool, _ bool) (*rbacpb.Principal, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}

	header, err := extractNameInBrackets(strings.TrimPrefix(key, attrRequestInlineHeader))
	if err != nil {
		return nil, err
	}
	m := matcher.InlineHeaderMatcher(header, value)
	return principalHeader(m), nil
}

type requestClaimGenerator struct{}

func (requestClaimGenerator) extendedPermission(_ string, _ []string, _ bool) (*rbacpb.Permission, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (rcg requestClaimGenerator) extendedPrincipal(key string, values []string, forTCP bool) (*rbacpb.Principal, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}

	claims, err := extractNameInNestedBrackets(strings.TrimPrefix(key, attrRequestClaims))
	if err != nil {
		return nil, err
	}
	m := MetadataListValueMatcherForJWTClaims(claims, matcher.StringOrMatcher(values))
	return principalMetadata(m), nil
}

type hostGenerator struct{}

func (hg hostGenerator) permission(key, value string, forTCP bool) (*rbacpb.Permission, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}

	return permissionHeader(matcher.HostMatcher(hostHeader, value)), nil
}

func (hostGenerator) principal(key, value string, forTCP bool, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}

type pathGenerator struct{}

func (g pathGenerator) permission(key, value string, forTCP bool) (*rbacpb.Permission, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}

	if security.ContainsPathTemplate(value) {
		m := matcher.PathTemplateMatcher(value)
		return permissionPathTemplate(m), nil
	}

	m := matcher.PathMatcher(value)
	return permissionPath(m), nil
}

func (pathGenerator) principal(key, value string, forTCP bool, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}

type methodGenerator struct{}

func (methodGenerator) permission(key, value string, forTCP bool) (*rbacpb.Permission, error) {
	if forTCP {
		return nil, fmt.Errorf("%q is HTTP only", key)
	}

	m := matcher.HeaderMatcher(methodHeader, value)
	return permissionHeader(m), nil
}

func (methodGenerator) principal(key, value string, forTCP bool, _ bool) (*rbacpb.Principal, error) {
	return nil, fmt.Errorf("unimplemented")
}
