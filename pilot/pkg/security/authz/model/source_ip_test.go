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

// Envoy evaluates the filter state address matcher used on internal listeners as follows (see
// FilterStateMatcher::match and FilterStateIpRangeMatcher::match in Envoy source/common/common): a missing object,
// or an object that is not an address, never matches, even with invert_match. So the generated principals are:
//
//	           ipBlocks X                     notIpBlocks X
//	ALLOW      peer in X                      peer present and not in X
//	DENY/AUDIT not (peer present and not in X) not (peer in X)
//
// so a missing peer never matches in an ALLOW policy, and always matches in a DENY or AUDIT policy.

func peerIP(invert bool, ranges ...*xdscore.CidrRange) *rbacpb.Principal {
	return &rbacpb.Principal{Identifier: &rbacpb.Principal_FilterState{FilterState: &matcherv3.FilterStateMatcher{
		Key: util.PeerDirectRemoteIPFilterStateKey,
		Matcher: &matcherv3.FilterStateMatcher_AddressMatch{AddressMatch: &matcherv3.AddressMatcher{
			Ranges:      ranges,
			InvertMatch: invert,
		}},
	}}}
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
			want:   []*rbacpb.Principal{peerIP(false, xdsCIDR("10.0.0.1", 32), xdsCIDR("2001:db8::", 32))},
		},
		{
			name:   "allow notIpBlocks",
			rule:   `{from: [{source: {notIpBlocks: [10.0.0.1, "::/0"]}}]}`,
			action: rbacpb.RBAC_ALLOW,
			want:   []*rbacpb.Principal{peerIP(true, xdsCIDR("10.0.0.1", 32), xdsCIDR("::", 0))},
		},
		{
			name:   "deny ipBlocks",
			rule:   `{from: [{source: {ipBlocks: [0.0.0.0/0]}}]}`,
			action: rbacpb.RBAC_DENY,
			want:   []*rbacpb.Principal{notPrincipal(peerIP(true, xdsCIDR("0.0.0.0", 0)))},
		},
		{
			name:   "deny notIpBlocks",
			rule:   `{from: [{source: {notIpBlocks: [10.0.0.1]}}]}`,
			action: rbacpb.RBAC_DENY,
			want:   []*rbacpb.Principal{notPrincipal(peerIP(false, xdsCIDR("10.0.0.1", 32)))},
		},
		{
			name:   "audit ipBlocks and notIpBlocks",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.0/8], notIpBlocks: [10.1.0.0/16]}}]}`,
			action: rbacpb.RBAC_LOG,
			want: []*rbacpb.Principal{
				notPrincipal(peerIP(true, xdsCIDR("10.0.0.0", 8))),
				notPrincipal(peerIP(false, xdsCIDR("10.1.0.0", 16))),
			},
		},
		{
			// The prefix is not masked here, as for direct_remote_ip; Envoy masks it.
			name:   "unmasked CIDR",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.1/8]}}]}`,
			action: rbacpb.RBAC_ALLOW,
			want:   []*rbacpb.Principal{peerIP(false, xdsCIDR("10.0.0.1", 8))},
		},
		{
			name:   "allow when source.ip",
			rule:   `{when: [{key: source.ip, values: [10.0.0.1], notValues: [10.0.0.2]}]}`,
			action: rbacpb.RBAC_ALLOW,
			want: []*rbacpb.Principal{
				peerIP(false, xdsCIDR("10.0.0.1", 32)),
				peerIP(true, xdsCIDR("10.0.0.2", 32)),
			},
		},
		{
			name:   "deny when source.ip",
			rule:   `{when: [{key: source.ip, notValues: [10.0.0.2]}]}`,
			action: rbacpb.RBAC_DENY,
			want:   []*rbacpb.Principal{notPrincipal(peerIP(false, xdsCIDR("10.0.0.2", 32)))},
		},
		{
			// Both conditions must hold, and the principal is read from filter state too.
			name:   "ipBlocks and principals",
			rule:   `{from: [{source: {ipBlocks: [10.0.0.1], principals: [cluster.local/ns/default/sa/a]}}]}`,
			action: rbacpb.RBAC_ALLOW,
			want: []*rbacpb.Principal{
				principalOr([]*rbacpb.Principal{peerPrincipal("cluster.local/ns/default/sa/a")}),
				peerIP(false, xdsCIDR("10.0.0.1", 32)),
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
			want:   []*rbacpb.Principal{notPrincipal(peerIP(true, xdsCIDR("10.0.0.1", 32)))},
		},
	}
	for _, forTCP := range []bool{true, false} {
		for _, tc := range cases {
			name := tc.name + "/http"
			if forTCP {
				name = tc.name + "/tcp"
			}
			t.Run(name, func(t *testing.T) {
				assert.Equal(t, generatePrincipals(t, tc.rule, false, forTCP, tc.action), tc.want)
			})
		}
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
