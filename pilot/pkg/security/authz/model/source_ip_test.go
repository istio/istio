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
	"testing"

	xdscore "github.com/cncf/xds/go/xds/core/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/test/util/assert"
)

// Older Envoy proxies (before 1.38) ignore AddressMatcher.invert_match. Match negative CIDRs using RBAC NOT
// together with an explicit peer-presence guard that matches both IPv4 and IPv6.
// A missing or non-address filter state object cannot match any positive CIDR, including the guard.
//
//	           ipBlocks X                     notIpBlocks X
//	ALLOW      peer in X                      peer present and not in X
//	DENY/LOG   peer in X or peer absent       not (peer in X)
//
// A missing peer therefore never matches an ALLOW policy and always matches a DENY or LOG policy.

func peerIP(ranges ...*xdscore.CidrRange) *rbacpb.Principal {
	return &rbacpb.Principal{Identifier: &rbacpb.Principal_FilterState{FilterState: &matcherv3.FilterStateMatcher{
		Key: util.PeerDirectRemoteIPFilterStateKey,
		Matcher: &matcherv3.FilterStateMatcher_AddressMatch{AddressMatch: &matcherv3.AddressMatcher{
			Ranges: ranges,
		}},
	}}}
}

func peerPresent() *rbacpb.Principal {
	return peerIP(xdsCIDR("0.0.0.0", 0), xdsCIDR("::", 0))
}

func notPrincipal(p *rbacpb.Principal) *rbacpb.Principal {
	return &rbacpb.Principal{Identifier: &rbacpb.Principal_NotId{NotId: p}}
}

func xdsCIDR(prefix string, length uint32) *xdscore.CidrRange {
	return &xdscore.CidrRange{AddressPrefix: prefix, PrefixLen: wrapperspb.UInt32(length)}
}

func coreCIDR(prefix string, length uint32) *core.CidrRange {
	return &core.CidrRange{AddressPrefix: prefix, PrefixLen: wrapperspb.UInt32(length)}
}

func peerPrincipal(principal string) *rbacpb.Principal {
	return &rbacpb.Principal{Identifier: &rbacpb.Principal_FilterState{FilterState: &matcherv3.FilterStateMatcher{
		Key: "io.istio.peer_principal",
		Matcher: &matcherv3.FilterStateMatcher_StringMatch{StringMatch: &matcherv3.StringMatcher{
			MatchPattern: &matcherv3.StringMatcher_Exact{Exact: "spiffe://" + principal},
		}},
	}}}
}

// generatePrincipals returns the principals of the policy generated from a single "from" or "when" rule.
func generatePrincipals(t *testing.T, ruleYAML string, useAuthenticated bool, forTCP bool, action rbacpb.RBAC_Action) []*rbacpb.Principal {
	t.Helper()
	m, err := New(types.NamespacedName{Name: "policy", Namespace: "default"}, yamlRule(t, ruleYAML))
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Generate(forTCP, useAuthenticated, action)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.GetPrincipals()) != 1 {
		t.Fatalf("expected 1 principal, got %v", p.GetPrincipals())
	}
	return p.GetPrincipals()[0].GetAndIds().GetIds()
}

// TestSourceIPOnInternalListener checks the source IP principals generated for internal listeners, where the downstream
// peer is read from filter state. See https://github.com/istio/istio/issues/62522.
func TestSourceIPOnInternalListener(t *testing.T) {
	cases := []struct {
		name   string
		rule   string
		action rbacpb.RBAC_Action
		want   []*rbacpb.Principal
	}{
		{
			name:   "allow ipBlocks",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.1, "2001:db8::/32"]}}]}`,
			action: rbacpb.RBAC_ALLOW,
			want:   []*rbacpb.Principal{peerIP(xdsCIDR("10.0.0.1", 32), xdsCIDR("2001:db8::", 32))},
		},
		{
			name:   "allow notIpBlocks",
			rule:   `{from: [{source: {notIpBlocks: [10.0.0.1, "::/0"]}}]}`,
			action: rbacpb.RBAC_ALLOW,
			want:   []*rbacpb.Principal{principalAnd([]*rbacpb.Principal{peerPresent(), notPrincipal(peerIP(xdsCIDR("10.0.0.1", 32), xdsCIDR("::", 0)))})},
		},
		{
			name:   "deny ipBlocks",
			rule:   `{from: [{source: {ipBlocks: [0.0.0.0/0]}}]}`,
			action: rbacpb.RBAC_DENY,
			want:   []*rbacpb.Principal{principalOr([]*rbacpb.Principal{peerIP(xdsCIDR("0.0.0.0", 0)), notPrincipal(peerPresent())})},
		},
		{
			name:   "deny notIpBlocks",
			rule:   `{from: [{source: {notIpBlocks: [10.0.0.1]}}]}`,
			action: rbacpb.RBAC_DENY,
			want:   []*rbacpb.Principal{notPrincipal(peerIP(xdsCIDR("10.0.0.1", 32)))},
		},
		{
			name:   "audit ipBlocks and notIpBlocks",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.0/8], notIpBlocks: [10.1.0.0/16]}}]}`,
			action: rbacpb.RBAC_LOG,
			want: []*rbacpb.Principal{
				principalOr([]*rbacpb.Principal{peerIP(xdsCIDR("10.0.0.0", 8)), notPrincipal(peerPresent())}),
				notPrincipal(peerIP(xdsCIDR("10.1.0.0", 16))),
			},
		},
		{
			// The prefix is not masked here, as for direct_remote_ip; Envoy masks it.
			name:   "unmasked CIDR",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.1/8]}}]}`,
			action: rbacpb.RBAC_ALLOW,
			want:   []*rbacpb.Principal{peerIP(xdsCIDR("10.0.0.1", 8))},
		},
		{
			name:   "allow when source.ip",
			rule:   `{when: [{key: source.ip, values: [10.0.0.1], notValues: [10.0.0.2]}]}`,
			action: rbacpb.RBAC_ALLOW,
			want: []*rbacpb.Principal{
				peerIP(xdsCIDR("10.0.0.1", 32)),
				principalAnd([]*rbacpb.Principal{peerPresent(), notPrincipal(peerIP(xdsCIDR("10.0.0.2", 32)))}),
			},
		},
		{
			name:   "deny when source.ip",
			rule:   `{when: [{key: source.ip, notValues: [10.0.0.2]}]}`,
			action: rbacpb.RBAC_DENY,
			want:   []*rbacpb.Principal{notPrincipal(peerIP(xdsCIDR("10.0.0.2", 32)))},
		},
		{
			// Both conditions must hold, and the principal is read from filter state too.
			name:   "ipBlocks and principals",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.1], principals: [cluster.local/ns/default/sa/a]}}]}`,
			action: rbacpb.RBAC_ALLOW,
			want: []*rbacpb.Principal{
				principalOr([]*rbacpb.Principal{peerPrincipal("cluster.local/ns/default/sa/a")}),
				peerIP(xdsCIDR("10.0.0.1", 32)),
			},
		},
		{
			// remoteIpBlocks keep their forwarded address semantics.
			name:   "remoteIpBlocks",
			rule:   `{from: [{source: {remoteIpBlocks: [10.0.0.1], notRemoteIpBlocks: [10.0.0.2]}}]}`,
			action: rbacpb.RBAC_DENY,
			want: []*rbacpb.Principal{
				principalOr([]*rbacpb.Principal{principalRemoteIP(coreCIDR("10.0.0.1", 32))}),
				principalNot(principalOr([]*rbacpb.Principal{principalRemoteIP(coreCIDR("10.0.0.2", 32))})),
			},
		},
		{
			// An invalid value in a DENY policy is skipped, making the policy wider, as on other listeners.
			name:   "deny skips invalid value",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.1, "not-an-ip"], notIpBlocks: ["not-an-ip"]}}]}`,
			action: rbacpb.RBAC_DENY,
			want:   []*rbacpb.Principal{principalOr([]*rbacpb.Principal{peerIP(xdsCIDR("10.0.0.1", 32)), notPrincipal(peerPresent())})},
		},
	}
	for _, forTCP := range []bool{true, false} {
		for _, tc := range cases {
			name := tc.name + "/http"
			if forTCP {
				name = tc.name + "/tcp"
			}
			t.Run(name, func(t *testing.T) {
				got := generatePrincipals(t, tc.rule, false, forTCP, tc.action)
				assert.Equal(t, got, tc.want)
				for _, principal := range got {
					assertNoInvertedAddressMatch(t, principal)
				}
			})
		}
	}
}

// assertNoInvertedAddressMatch recursively checks that the older Envoy-compatible representation is used.
func assertNoInvertedAddressMatch(t *testing.T, principal *rbacpb.Principal) {
	t.Helper()
	if principal.GetFilterState().GetAddressMatch().GetInvertMatch() {
		t.Fatal("source-IP authorization must not use AddressMatcher.invert_match")
	}
	for _, p := range principal.GetAndIds().GetIds() {
		assertNoInvertedAddressMatch(t, p)
	}
	for _, p := range principal.GetOrIds().GetIds() {
		assertNoInvertedAddressMatch(t, p)
	}
	if p := principal.GetNotId(); p != nil {
		assertNoInvertedAddressMatch(t, p)
	}
}

// TestSourceIPOnNormalListener checks that listeners that read the downstream peer from the connection keep matching
// the direct remote IP.
func TestSourceIPOnNormalListener(t *testing.T) {
	for _, action := range []rbacpb.RBAC_Action{rbacpb.RBAC_ALLOW, rbacpb.RBAC_DENY, rbacpb.RBAC_LOG} {
		t.Run(action.String(), func(t *testing.T) {
			got := generatePrincipals(t, `{from: [{source: {ipBlocks: [10.0.0.1], notIpBlocks: ["2001:db8::1"]}}]}`, true, true, action)
			assert.Equal(t, got, []*rbacpb.Principal{
				principalOr([]*rbacpb.Principal{principalDirectRemoteIP(coreCIDR("10.0.0.1", 32))}),
				principalNot(principalOr([]*rbacpb.Principal{principalDirectRemoteIP(coreCIDR("2001:db8::1", 128))})),
			})
		})
	}
}

// TestSourceIPOnInternalListenerInvalidAllow checks that an invalid value fails an ALLOW rule, as on other listeners.
func TestSourceIPOnInternalListenerInvalidAllow(t *testing.T) {
	r := rule{key: attrSrcIP, values: []string{"10.0.0.1", "not-an-ip"}, g: srcIPGenerator{}}
	if got, err := r.principal(false, false, rbacpb.RBAC_ALLOW); err == nil {
		t.Fatalf("expected an error, got %v", got)
	}
	r = rule{key: attrSrcIP, notValues: []string{"not-an-ip"}, g: srcIPGenerator{}}
	if got, err := r.principal(false, false, rbacpb.RBAC_ALLOW); err == nil {
		t.Fatalf("expected an error, got %v", got)
	}
}
