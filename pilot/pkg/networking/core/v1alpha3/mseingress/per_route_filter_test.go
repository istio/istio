package mseingress

import (
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	authpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
)

func TestConstructTypedPerFilterConfigForRoute(t *testing.T) {
	testCases := []struct {
		globalHTTPFilter *GlobalHTTPFilters
		input            *networking.HTTPRoute
		expect           *rbachttppb.RBAC
	}{
		{
			globalHTTPFilter: nil,
			input: &networking.HTTPRoute{
				RouteHTTPFilters: []*networking.HTTPFilter{
					{
						Name: IPAccessControl,
						Filter: &networking.HTTPFilter_IpAccessControl{
							IpAccessControl: &networking.IPAccessControl{
								RemoteIpBlocks: []string{"1.1.1.1", "2.2.2.2/8"},
							},
						},
					},
				},
			},
			expect: &rbachttppb.RBAC{
				Rules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						IPAccessControl: {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Any{
										Any: true,
									},
								},
							},
							Principals: []*rbacpb.Principal{
								{
									Identifier: &rbacpb.Principal_NotId{
										NotId: &rbacpb.Principal{
											Identifier: &rbacpb.Principal_OrIds{
												OrIds: &rbacpb.Principal_Set{
													Ids: []*rbacpb.Principal{
														{
															Identifier: &rbacpb.Principal_RemoteIp{
																RemoteIp: &v3corepb.CidrRange{
																	AddressPrefix: "1.1.1.1",
																	PrefixLen: &wrappers.UInt32Value{
																		Value: 32,
																	},
																},
															},
														},
														{
															Identifier: &rbacpb.Principal_RemoteIp{
																RemoteIp: &v3corepb.CidrRange{
																	AddressPrefix: "2.2.2.2",
																	PrefixLen: &wrappers.UInt32Value{
																		Value: 8,
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
						},
					},
				},
			},
		},
		{
			globalHTTPFilter: nil,
			input: &networking.HTTPRoute{
				RouteHTTPFilters: []*networking.HTTPFilter{
					{
						Name: IPAccessControl,
						Filter: &networking.HTTPFilter_IpAccessControl{
							IpAccessControl: &networking.IPAccessControl{
								NotRemoteIpBlocks: []string{"3.3.3.3", "4.4.4.4/8"},
							},
						},
					},
				},
			},
			expect: &rbachttppb.RBAC{
				Rules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						IPAccessControl: {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Any{
										Any: true,
									},
								},
							},
							Principals: []*rbacpb.Principal{
								{
									Identifier: &rbacpb.Principal_OrIds{
										OrIds: &rbacpb.Principal_Set{
											Ids: []*rbacpb.Principal{
												{
													Identifier: &rbacpb.Principal_RemoteIp{
														RemoteIp: &v3corepb.CidrRange{
															AddressPrefix: "3.3.3.3",
															PrefixLen: &wrappers.UInt32Value{
																Value: 32,
															},
														},
													},
												},
												{
													Identifier: &rbacpb.Principal_RemoteIp{
														RemoteIp: &v3corepb.CidrRange{
															AddressPrefix: "4.4.4.4",
															PrefixLen: &wrappers.UInt32Value{
																Value: 8,
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
				},
			},
		},
		{
			globalHTTPFilter: &GlobalHTTPFilters{
				rbac: &rbachttppb.RBAC{
					Rules: &rbacpb.RBAC{
						Action: rbacpb.RBAC_DENY,
						Policies: map[string]*rbacpb.Policy{
							"jwt": {
								Permissions: []*rbacpb.Permission{
									{
										Rule: &rbacpb.Permission_Any{
											Any: true,
										},
									},
								},
								Principals: []*rbacpb.Principal{
									{
										Identifier: &rbacpb.Principal_Any{
											Any: true,
										},
									},
								},
							},
						},
					},
				},
			},
			input: &networking.HTTPRoute{
				RouteHTTPFilters: []*networking.HTTPFilter{
					{
						Name: IPAccessControl,
						Filter: &networking.HTTPFilter_IpAccessControl{
							IpAccessControl: &networking.IPAccessControl{
								NotRemoteIpBlocks: []string{"3.3.3.3", "4.4.4.4/8"},
							},
						},
					},
				},
			},
			expect: &rbachttppb.RBAC{
				Rules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						IPAccessControl: {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Any{
										Any: true,
									},
								},
							},
							Principals: []*rbacpb.Principal{
								{
									Identifier: &rbacpb.Principal_OrIds{
										OrIds: &rbacpb.Principal_Set{
											Ids: []*rbacpb.Principal{
												{
													Identifier: &rbacpb.Principal_RemoteIp{
														RemoteIp: &v3corepb.CidrRange{
															AddressPrefix: "3.3.3.3",
															PrefixLen: &wrappers.UInt32Value{
																Value: 32,
															},
														},
													},
												},
												{
													Identifier: &rbacpb.Principal_RemoteIp{
														RemoteIp: &v3corepb.CidrRange{
															AddressPrefix: "4.4.4.4",
															PrefixLen: &wrappers.UInt32Value{
																Value: 8,
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
						"jwt": {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Any{
										Any: true,
									},
								},
							},
							Principals: []*rbacpb.Principal{
								{
									Identifier: &rbacpb.Principal_Any{
										Any: true,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run("", func(t *testing.T) {
			filters := ConstructTypedPerFilterConfigForRoute(testCase.globalHTTPFilter, config.Config{}, testCase.input)
			any, exist := filters[wellknown.HTTPRoleBasedAccessControl]
			if !exist {
				t.Fatalf("should be existing.")
			}
			filter := &rbachttppb.RBACPerRoute{}
			_ = any.UnmarshalTo(filter)
			if !proto.Equal(filter.Rbac, testCase.expect) {
				t.Fatalf("should be equal.")
			}
		})
	}
}

func TestExtractGlobalHTTPFilters(t *testing.T) {
	node := &model.Proxy{
		ConfigNamespace: "test",
		Metadata: &model.NodeMetadata{
			Labels: map[string]string{},
		},
	}
	pushContext := &model.PushContext{
		Mesh: &meshconfig.MeshConfig{},
		AuthzPolicies: &model.AuthorizationPolicies{
			NamespaceToPolicies: map[string][]model.AuthorizationPolicy{
				"test": {
					{
						Name:      "gw-123-istio-jwt",
						Namespace: "test",
						Spec: &authpb.AuthorizationPolicy{
							Action: authpb.AuthorizationPolicy_ALLOW,
							Rules: []*authpb.Rule{
								{
									From: []*authpb.Rule_From{
										{
											Source: &authpb.Source{
												RequestPrincipals: []string{"test/test"},
											},
										},
									},
								},
								{
									To: []*authpb.Rule_To{
										{
											Operation: &authpb.Operation{
												Hosts: []string{"foo.com"},
												Paths: []string{"/a", "/b"},
											},
										},
										{
											Operation: &authpb.Operation{
												Hosts: []string{"bar.com"},
												Paths: []string{"/a", "/b"},
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

	globalHTTPFilters := ExtractGlobalHTTPFilters(node, pushContext)
	if globalHTTPFilters.rbac == nil {
		t.Fatalf("Should not be nil")
	}
}
