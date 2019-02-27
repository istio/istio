package authz

import (
	"reflect"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	policy "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/matcher"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
)

func TestConvertRbacRulesToFilterConfigV2(t *testing.T) {
	roles := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-1"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services:   []string{"backup_service"},
						NotMethods: []string{"DELETE"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-2"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"service-2"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-3"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"productpage"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-4"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"secret-service"},
					},
				},
			},
		},
	}
	bindings := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "authz-policy-multiple-bindings"},
			Spec: &rbacproto.AuthorizationPolicy{
				Allow: []*rbacproto.ServiceRoleBinding{
					{
						Subjects: []*rbacproto.Subject{
							{
								NotNames: []string{"cluster.local/ns/testing/sa/unstable-service"},
							},
						},
						RoleRef: &rbacproto.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-3",
						},
					},
					{
						Subjects: []*rbacproto.Subject{
							{
								Namespaces: []string{"*"},
							},
						},
						RoleRef: &rbacproto.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-1",
						},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "authz-policy-single-binding"},
			Spec: &rbacproto.AuthorizationPolicy{
				Allow: []*rbacproto.ServiceRoleBinding{
					{
						Subjects: []*rbacproto.Subject{
							{
								NotNames: []string{"cluster.local/ns/testing/sa/unstable-service"},
							},
						},
						RoleRef: &rbacproto.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-2",
						},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "authz-policy-with-selector"},
			Spec: &rbacproto.AuthorizationPolicy{
				WorkloadSelector: &rbacproto.WorkloadSelector{
					Labels: map[string]string{
						"app": "productpage",
					},
				},
				Allow: []*rbacproto.ServiceRoleBinding{
					{
						Subjects: []*rbacproto.Subject{
							{
								NotNames: []string{"cluster.local/ns/testing/sa/unstable-service"},
							},
						},
						RoleRef: &rbacproto.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-3",
						},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "authz-policy-with-selector-no-match"},
			Spec: &rbacproto.AuthorizationPolicy{
				WorkloadSelector: &rbacproto.WorkloadSelector{
					Labels: map[string]string{
						"app": "notfound",
					},
				},
				Allow: []*rbacproto.ServiceRoleBinding{
					{
						Subjects: []*rbacproto.Subject{
							{
								NotNames: []string{"cluster.local/ns/testing/sa/unstable-service"},
							},
						},
						RoleRef: &rbacproto.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-4",
						},
					},
				},
			},
		},
	}

	policy1 := &policy.Policy{
		Permissions: []*policy.Permission{
			{
				Rule: &policy.Permission_AndRules{
					AndRules: &policy.Permission_Set{
						Rules: []*policy.Permission{
							{
								Rule: &policy.Permission_NotRule{
									NotRule: &policy.Permission{
										Rule: generateHeaderRule([]*route.HeaderMatcher{
											{Name: methodHeader, HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "DELETE"}},
										}),
									},
								},
							},
						},
					},
				},
			},
		},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_OrIds{
								OrIds: &policy.Principal_Set{
									Ids: []*policy.Principal{
										{
											Identifier: &policy.Principal_Metadata{
												Metadata: generateMetadataStringMatcher(
													attrSrcPrincipal, &metadata.StringMatcher{
														MatchPattern: &metadata.StringMatcher_Regex{
															Regex: ".*/ns/.*/.*",
														}},
													authn.AuthnFilterName),
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
	}
	policy2 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Rule: &policy.Permission_AndRules{
				AndRules: &policy.Permission_Set{
					Rules: []*policy.Permission{
						{
							Rule: &policy.Permission_Any{
								Any: true,
							},
						},
					},
				},
			},
		},
		},
		Principals: []*policy.Principal{{
			Identifier: &policy.Principal_AndIds{
				AndIds: &policy.Principal_Set{
					Ids: []*policy.Principal{
						{
							Identifier: &policy.Principal_NotId{
								NotId: &policy.Principal{
									Identifier: &policy.Principal_OrIds{
										OrIds: &policy.Principal_Set{
											Ids: []*policy.Principal{
												{
													Identifier: &policy.Principal_Metadata{
														Metadata: generateMetadataStringMatcher(
															attrSrcPrincipal, &metadata.StringMatcher{
																MatchPattern: &metadata.StringMatcher_Exact{Exact: "cluster.local/ns/testing/sa/unstable-service"}}, authn.AuthnFilterName),
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
	}
	policy3 := &policy.Policy{}

	expectRbac1 := generateExpectRBACForSinglePolicy("service-role-1", policy1)
	expectRbac2 := generateExpectRBACForSinglePolicy("service-role-2", policy2)
	expectRbac3 := generateExpectRBACForSinglePolicy("service-role-3", policy2)
	expectRbac4 := generateExpectRBACForSinglePolicy("" /*serviceRoleName*/, policy3)

	authzPolicies := newAuthzPoliciesWithRolesAndBindings(roles, bindings)
	option := rbacOption{authzPolicies: authzPolicies}
	testCases := []struct {
		name    string
		service *serviceMetadata
		rbac    *policy.RBAC
		option  rbacOption
	}{
		{
			name: "multiple bindings",
			service: &serviceMetadata{
				name: "backup_service",
			},
			rbac:   expectRbac1,
			option: option,
		},
		{
			name: "single binding",
			service: &serviceMetadata{
				name: "service-2",
			},
			rbac:   expectRbac2,
			option: option,
		},
		{
			name: "with workload selector",
			service: &serviceMetadata{
				name:   "productpage",
				labels: map[string]string{"app": "productpage"},
			},
			rbac:   expectRbac3,
			option: option,
		},
		{
			name: "with workload selector but no match",
			service: &serviceMetadata{
				name:   "secret-service",
				labels: map[string]string{"app": "productpage"},
			},
			rbac:   expectRbac4,
			option: option,
		},
	}

	for _, tc := range testCases {
		if tc.name == "with workload selector" {
			rbac := convertRbacRulesToFilterConfigV2(tc.service, tc.option)
			if !reflect.DeepEqual(*tc.rbac, *rbac.Rules) {
				t.Errorf("%s want:\n%v\nbut got:\n%v", tc.name, *tc.rbac, *rbac.Rules)
			}
		}
	}
}
