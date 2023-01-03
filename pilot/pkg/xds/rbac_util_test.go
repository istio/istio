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

package xds

import (
	"testing"

	authpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/rbacapi"
	"istio.io/istio/pkg/test/util/assert"
)

func TestRBACGenerator(t *testing.T) {
	t.Run("NamespacePolicy_withFromAndTo", func(t *testing.T) {
		/*
			- from:
			- source:
			namespaces: ["*goodns"]
			notPrincipals: ["badworkload1", "badworkload2"]
			- source:
			notNamespaces: ["badns*"]
			to:
			- operation:
			ports: ["1234"]
		*/

		authzPolicy := model.AuthorizationPolicy{
			Name:      "policy1",
			Namespace: "ns1",
			Spec: &authpb.AuthorizationPolicy{
				Rules: []*authpb.Rule{
					{
						From: []*authpb.Rule_From{
							{
								Source: &authpb.Source{
									Namespaces:    []string{"*goodns"},
									NotPrincipals: []string{"badworkload1", "badworkload2"},
								},
							},
							{
								Source: &authpb.Source{
									NotNamespaces: []string{"badns*"},
								},
							},
						},
						To: []*authpb.Rule_To{
							{
								Operation: &authpb.Operation{
									Ports: []string{"1234"},
								},
							},
						},
					},
				},
			},
		}

		expectedRBAC := &rbacapi.RBAC{
			Name:  "policy1/ns1",
			Scope: rbacapi.RBACScope_NAMESPACE,
			Groups: []*rbacapi.RBACPolicyRulesGroup{
				{
					Rules: []*rbacapi.RBACPolicyRules{
						{
							Matches: []*rbacapi.RBACPolicyRuleMatch{
								{
									Namespaces: []*rbacapi.StringMatch{
										{MatchType: &rbacapi.StringMatch_Suffix{Suffix: "goodns"}},
									},
									NotPrincipals: []*rbacapi.StringMatch{
										{MatchType: &rbacapi.StringMatch_Exact{Exact: "badworkload1"}},
										{MatchType: &rbacapi.StringMatch_Exact{Exact: "badworkload2"}},
									},
								},
								{
									NotNamespaces: []*rbacapi.StringMatch{
										{MatchType: &rbacapi.StringMatch_Prefix{Prefix: "badns"}},
									},
								},
							},
						},
						{
							Matches: []*rbacapi.RBACPolicyRuleMatch{
								{
									DestinationPorts: []uint32{1234},
								},
							},
						},
					},
				},
			},
		}

		rbac := authorizationPolicyToRBAC(authzPolicy, "istio-system")
		assert.Equal(t, rbac, expectedRBAC)
	})

	t.Run("NamespacePolicy_withToAndWhen", func(t *testing.T) {
		/*
			  - to:
				- operation:
					ports: ["1234"]
				when:
				- key: source.ip
					values: ["10.1.2.3", "10.2.0.4"]
				- key: source.namespace
					values: ["goodns"]
		*/

		authzPolicy := model.AuthorizationPolicy{
			Name:      "policy1",
			Namespace: "ns1",
			Spec: &authpb.AuthorizationPolicy{
				Rules: []*authpb.Rule{
					{
						To: []*authpb.Rule_To{
							{
								Operation: &authpb.Operation{
									Ports: []string{"1234"},
								},
							},
						},
						When: []*authpb.Condition{
							{
								Key:    "source.ip",
								Values: []string{"10.1.2.3", "10.2.0.4"},
							},
							{
								Key:    "source.namespace",
								Values: []string{"*"},
							},
						},
					},
				},
			},
		}

		expectedRBAC := &rbacapi.RBAC{
			Name:  "policy1/ns1",
			Scope: rbacapi.RBACScope_NAMESPACE,
			Groups: []*rbacapi.RBACPolicyRulesGroup{
				{
					Rules: []*rbacapi.RBACPolicyRules{
						{
							Matches: []*rbacapi.RBACPolicyRuleMatch{
								{
									DestinationPorts: []uint32{1234},
								},
							},
						},
						{
							Matches: []*rbacapi.RBACPolicyRuleMatch{
								{
									SourceIps: []*rbacapi.Address{
										{Address: []byte{10, 1, 2, 3}, Length: 4},
										{Address: []byte{10, 2, 0, 4}, Length: 4},
									},
								},
							},
						},
						{
							Matches: []*rbacapi.RBACPolicyRuleMatch{
								{
									Namespaces: []*rbacapi.StringMatch{
										{MatchType: &rbacapi.StringMatch_Presence{}},
									},
								},
							},
						},
					},
				},
			},
		}

		rbac := authorizationPolicyToRBAC(authzPolicy, "istio-system")
		assert.Equal(t, rbac, expectedRBAC)
	})

	t.Run("GlobalPolicy", func(t *testing.T) {
		authzPolicy := model.AuthorizationPolicy{
			Name:      "policy1",
			Namespace: "istio-system",
			Spec: &authpb.AuthorizationPolicy{
				Rules: []*authpb.Rule{
					{
						To: []*authpb.Rule_To{
							{
								Operation: &authpb.Operation{
									Ports: []string{"1234"},
								},
							},
						},
					},
				},
			},
		}

		expectedRBAC := &rbacapi.RBAC{
			Name:  "policy1/istio-system",
			Scope: rbacapi.RBACScope_GLOBAL,
			Groups: []*rbacapi.RBACPolicyRulesGroup{
				{
					Rules: []*rbacapi.RBACPolicyRules{
						{
							Matches: []*rbacapi.RBACPolicyRuleMatch{
								{
									DestinationPorts: []uint32{1234},
								},
							},
						},
					},
				},
			},
		}

		rbac := authorizationPolicyToRBAC(authzPolicy, "istio-system")
		assert.Equal(t, rbac, expectedRBAC)
	})
}

func TestIpBlockStringsToAddresses(t *testing.T) {
	t.Run("AllValid", func(t *testing.T) {
		ipBlocks := []string{"1.2.3.4", "2001:cb8::17"}
		want := []*rbacapi.Address{
			{
				Address: []byte{0x0001, 0x0002, 0x0003, 0x0004},
				Length:  4,
			},
			{
				Address: []byte{0x20, 0x01, 0x0c, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x17},
				Length:  16,
			},
		}
		addresses := ipBlockStringsToAddresses(ipBlocks)
		assert.Equal(t, addresses, want)
	})
	t.Run("HasInvalid", func(t *testing.T) {
		ipBlocks := []string{"1.2.3.4", "2001cb8::17"}
		want := []*rbacapi.Address{
			{
				Address: []byte{0x0001, 0x0002, 0x0003, 0x0004},
				Length:  4,
			},
		}
		addresses := ipBlockStringsToAddresses(ipBlocks)
		assert.Equal(t, addresses, want)
	})
}
