// Copyright 2018 Istio Authors
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
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	policy "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
	"github.com/gogo/protobuf/types"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
)

// nolint:deadcode
func generateServiceRole(services, methods []string) *rbacproto.ServiceRole {
	return &rbacproto.ServiceRole{
		Rules: []*rbacproto.AccessRule{
			{
				Services: services,
				Methods:  methods,
			},
		},
	}
}

// nolint:deadcode
func generateServiceBinding(subject, serviceRoleRef string, mode rbacproto.EnforcementMode) *rbacproto.ServiceRoleBinding {
	return &rbacproto.ServiceRoleBinding{
		Mode: mode,
		Subjects: []*rbacproto.Subject{
			{
				User: subject,
			},
		},
		RoleRef: &rbacproto.RoleRef{
			Kind: "ServiceRole",
			Name: serviceRoleRef,
		},
	}
}

// nolint:deadcode,unparam
func generatePermission(headerName, matchSpecifier string) *policy.Permission {
	return &policy.Permission{
		Rule: &policy.Permission_AndRules{
			AndRules: &policy.Permission_Set{
				Rules: []*policy.Permission{
					{
						Rule: &policy.Permission_OrRules{
							OrRules: &policy.Permission_Set{
								Rules: []*policy.Permission{
									{
										Rule: &policy.Permission_Header{
											Header: &route.HeaderMatcher{
												Name: headerName,
												HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
													ExactMatch: matchSpecifier,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// nolint:deadcode
func generateHeaderRule(headers []*route.HeaderMatcher) *policy.Permission_OrRules {
	rules := &policy.Permission_OrRules{
		OrRules: &policy.Permission_Set{},
	}
	for _, header := range headers {
		rules.OrRules.Rules = append(rules.OrRules.Rules, &policy.Permission{Rule: &policy.Permission_Header{
			Header: header,
		}})
	}
	return rules
}

// nolint:deadcode
func generateDestinationPortRule(destinationPort []uint32) *policy.Permission_OrRules {
	rules := &policy.Permission_OrRules{
		OrRules: &policy.Permission_Set{},
	}
	for _, port := range destinationPort {
		rules.OrRules.Rules = append(rules.OrRules.Rules, &policy.Permission{
			Rule: &policy.Permission_DestinationPort{DestinationPort: port}})
	}
	return rules
}

// nolint:deadcode
func generateDestinationCidrRule(destinationPrefix []string, PrefixLen []uint32) *policy.Permission_OrRules {
	rules := &policy.Permission_OrRules{
		OrRules: &policy.Permission_Set{},
	}
	for i := range destinationPrefix {
		rules.OrRules.Rules = append(rules.OrRules.Rules, &policy.Permission{
			Rule: &policy.Permission_DestinationIp{
				DestinationIp: &core.CidrRange{
					AddressPrefix: destinationPrefix[i],
					PrefixLen:     &types.UInt32Value{Value: PrefixLen[i]},
				},
			}})
	}
	return rules
}

// nolint:deadcode
func generatePrincipal(principalName string) *policy.Principal {
	return &policy.Principal{
		Identifier: &policy.Principal_AndIds{
			AndIds: &policy.Principal_Set{
				Ids: []*policy.Principal{
					{
						Identifier: &policy.Principal_Metadata{
							Metadata: generateMetadataStringMatcher(
								"source.principal", &metadata.StringMatcher{
									MatchPattern: &metadata.StringMatcher_Exact{Exact: principalName}}, authn.AuthnFilterName),
						},
					},
				},
			},
		},
	}
}

// nolint:deadcode
func generatePolicyWithHTTPMethodAndGroupClaim(methodName, claimName string) *policy.Policy {
	return &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{
						{
							Rule: &policy.Permission_OrRules{
								OrRules: &policy.Permission_Set{
									Rules: []*policy.Permission{
										{
											Rule: &policy.Permission_Header{
												Header: &route.HeaderMatcher{
													Name: ":method",
													HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
														ExactMatch: methodName,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_Metadata{
								Metadata: generateMetadataListMatcher(authn.AuthnFilterName,
									[]string{attrRequestClaims, "groups"}, claimName),
							},
						},
					},
				},
			},
		}},
	}
}
