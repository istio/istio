package mseingress

import (
	"testing"

	v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	ext_authzv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	ratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ratelimit/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	authpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestIsUsingExtensionPath(t *testing.T) {
	testCases := []struct {
		input  model.AuthorizationPolicy
		expect bool
	}{
		{
			input: model.AuthorizationPolicy{
				Spec: &authpb.AuthorizationPolicy{},
			},
			expect: false,
		},
		{
			input: model.AuthorizationPolicy{
				Spec: &authpb.AuthorizationPolicy{
					Rules: []*authpb.Rule{
						{
							From: []*authpb.Rule_From{
								{
									Source: &authpb.Source{
										Principals: []string{"test"},
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			input: model.AuthorizationPolicy{
				Spec: &authpb.AuthorizationPolicy{
					Rules: []*authpb.Rule{
						{
							From: []*authpb.Rule_From{
								{
									Source: &authpb.Source{
										Principals: []string{"test"},
									},
								},
							},
						},
						{
							To: []*authpb.Rule_To{
								{
									Operation: &authpb.Operation{
										Paths: []string{"/test"},
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			input: model.AuthorizationPolicy{
				Spec: &authpb.AuthorizationPolicy{
					Rules: []*authpb.Rule{
						{
							From: []*authpb.Rule_From{
								{
									Source: &authpb.Source{
										Principals: []string{"test"},
									},
								},
							},
						},
						{
							To: []*authpb.Rule_To{
								{
									Operation: &authpb.Operation{
										ExtensionPaths: []*authpb.StringMatch{
											{
												MatchType: &authpb.StringMatch_Exact{
													Exact: "test",
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
			expect: true,
		},
	}

	for _, testCase := range testCases {
		t.Run("", func(t *testing.T) {
			if isUsingExtensionPath(testCase.input) != testCase.expect {
				t.Fatal("should be equal")
			}
		})
	}
}

var (
	anyPermissions = []*rbacpb.Permission{
		{
			Rule: &rbacpb.Permission_AndRules{
				AndRules: &rbacpb.Permission_Set{
					Rules: []*rbacpb.Permission{
						{
							Rule: &rbacpb.Permission_Any{
								Any: true,
							},
						},
					},
				},
			},
		},
	}

	principals = []*rbacpb.Principal{
		{
			Identifier: &rbacpb.Principal_AndIds{
				AndIds: &rbacpb.Principal_Set{
					Ids: []*rbacpb.Principal{
						{
							Identifier: &rbacpb.Principal_NotId{
								NotId: &rbacpb.Principal{
									Identifier: &rbacpb.Principal_OrIds{
										OrIds: &rbacpb.Principal_Set{
											Ids: []*rbacpb.Principal{
												{
													Identifier: &rbacpb.Principal_Metadata{
														Metadata: &matcher.MetadataMatcher{
															Filter: "istio_authn",
															Path: []*matcher.MetadataMatcher_PathSegment{
																{
																	Segment: &matcher.MetadataMatcher_PathSegment_Key{
																		Key: "request.auth.principal",
																	},
																},
															},
															Value: &matcher.ValueMatcher{
																MatchPattern: &matcher.ValueMatcher_StringMatch{
																	StringMatch: &matcher.StringMatcher{
																		MatchPattern: &matcher.StringMatcher_Exact{
																			Exact: "test/test",
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
		},
	}
)

const (
	extAuthz = "type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz"
	rbac     = "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC"
)

func TestGenerateRBACFilters(t *testing.T) {
	env := &model.Environment{}
	store := model.NewFakeStore()

	extAuthzECDSDiscovery := &http_conn.HttpFilter{
		Name: "ext-authz-test",
		ConfigType: &http_conn.HttpFilter_ConfigDiscovery{
			ConfigDiscovery: &v3.ExtensionConfigSource{
				ConfigSource: &v3.ConfigSource{
					ConfigSourceSpecifier: &v3.ConfigSource_Ads{},
				},
				TypeUrls: []string{extAuthz},
			},
		},
	}
	rawExtAuthzECDSDiscovery, _ := conversion.MessageToStruct(extAuthzECDSDiscovery)

	extAuthz := &ext_authzv3.ExtAuthz{
		FailureModeAllow: true,
	}
	extAuthzConfig := &v3.TypedExtensionConfig{
		Name:        "ext-authz-test",
		TypedConfig: protoconv.MessageToAny(extAuthz),
	}
	rawExtAuthz, _ := conversion.MessageToStruct(extAuthzConfig)

	rbacECDSDiscovery := &http_conn.HttpFilter{
		Name: "rbac-test",
		ConfigType: &http_conn.HttpFilter_ConfigDiscovery{
			ConfigDiscovery: &v3.ExtensionConfigSource{
				ConfigSource: &v3.ConfigSource{
					ConfigSourceSpecifier: &v3.ConfigSource_Ads{},
				},
				TypeUrls: []string{rbac},
			},
		},
	}
	rawRBACECDSDiscovery, _ := conversion.MessageToStruct(rbacECDSDiscovery)

	rbac := &rbachttppb.RBAC{
		ShadowRules: &rbacpb.RBAC{
			Action: rbacpb.RBAC_DENY,
			Policies: map[string]*rbacpb.Policy{
				"envoyfilter": {
					Permissions: []*rbacpb.Permission{
						{
							Rule: &rbacpb.Permission_Header{},
						},
					},
				},
			},
		},
	}
	rbacConfig := &v3.TypedExtensionConfig{
		Name:        "rbac-test",
		TypedConfig: protoconv.MessageToAny(rbac),
	}
	rawRBAC, _ := conversion.MessageToStruct(rbacConfig)

	envoyFilters := []config.Config{
		{
			Meta: config.Meta{Name: "authz-test1", Namespace: "wakanda", GroupVersionKind: gvk.EnvoyFilter},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
					{
						ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Context: networking.EnvoyFilter_GATEWAY,
							ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
								Listener: &networking.EnvoyFilter_ListenerMatch{
									FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
										Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
											Name: "envoy.http_connection_manager",
											SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{
												Name: "envoy.filters.http.cors",
											},
										},
									},
								},
							},
						},
						Patch: &networking.EnvoyFilter_Patch{
							Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
							Value:     rawExtAuthzECDSDiscovery,
						},
					},
					{
						ApplyTo: networking.EnvoyFilter_EXTENSION_CONFIG,
						Patch: &networking.EnvoyFilter_Patch{
							Operation: networking.EnvoyFilter_Patch_ADD,
							Value:     rawExtAuthz,
						},
					},
					{
						ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
						Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
							Context: networking.EnvoyFilter_GATEWAY,
							ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
								Listener: &networking.EnvoyFilter_ListenerMatch{
									FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
										Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
											Name: "envoy.http_connection_manager",
											SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{
												Name: "ext-authz-test",
											},
										},
									},
								},
							},
						},
						Patch: &networking.EnvoyFilter_Patch{
							Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
							Value:     rawRBACECDSDiscovery,
						},
					},
					{
						ApplyTo: networking.EnvoyFilter_EXTENSION_CONFIG,
						Patch: &networking.EnvoyFilter_Patch{
							Operation: networking.EnvoyFilter_Patch_ADD,
							Value:     rawRBAC,
						},
					},
				},
			},
		},
	}

	allowPolicyWithEmptyRules := &authpb.AuthorizationPolicy{
		Action: authpb.AuthorizationPolicy_ALLOW,
		Rules:  []*authpb.Rule{{}},
	}

	allowPolicyOnlyWithPrincipals := &authpb.AuthorizationPolicy{
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
		},
	}

	allowPolicyWithPaths := &authpb.AuthorizationPolicy{
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
							Hosts: []string{"test.com"},
							Paths: []string{"/path"},
						},
					},
					{
						Operation: &authpb.Operation{
							Hosts: []string{"*"},
							Paths: []string{"/foo"},
						},
					},
				},
			},
		},
	}

	allowPolicyWithExtensionPaths := &authpb.AuthorizationPolicy{
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
							Hosts: []string{"test.com"},
							ExtensionPaths: []*authpb.StringMatch{
								{
									MatchType: &authpb.StringMatch_Exact{
										Exact: "/test",
									},
								},
							},
						},
					},
					{
						Operation: &authpb.Operation{
							Hosts: []string{"*"},
							ExtensionPaths: []*authpb.StringMatch{
								{
									MatchType: &authpb.StringMatch_Prefix{
										Prefix: "/prefix",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	testCases := []struct {
		config config.Config
		expect *rbachttppb.RBAC
	}{
		{
			config: newConfig("gw-xxx-istio-jwt", "wakanda", allowPolicyWithEmptyRules),
			expect: &rbachttppb.RBAC{
				Rules: &rbacpb.RBAC{
					Action:   rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{},
				},
				ShadowRules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						"envoyfilter": {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Header{},
								},
							},
						},
					},
				},
			},
		},
		{
			config: newConfig("gw-xxx-istio-jwt", "wakanda", allowPolicyOnlyWithPrincipals),
			expect: &rbachttppb.RBAC{
				Rules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						"jwt": {
							Permissions: anyPermissions,
							Principals:  principals,
						},
					},
				},
				ShadowRules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						"envoyfilter": {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Header{},
								},
							},
						},
					},
				},
			},
		},
		{
			config: newConfig("gw-xxx-istio-jwt", "wakanda", allowPolicyWithPaths),
			expect: &rbachttppb.RBAC{
				ShadowRules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						"envoyfilter": {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Header{},
								},
							},
						},
					},
				},
				Rules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						"jwt": {
							Principals: principals,
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_AndRules{
										AndRules: &rbacpb.Permission_Set{
											Rules: []*rbacpb.Permission{
												{
													Rule: &rbacpb.Permission_OrRules{
														OrRules: &rbacpb.Permission_Set{
															Rules: []*rbacpb.Permission{
																{
																	Rule: &rbacpb.Permission_Header{
																		Header: &routev3.HeaderMatcher{
																			Name: ":authority",
																			HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
																				StringMatch: &matcher.StringMatcher{
																					MatchPattern: &matcher.StringMatcher_Exact{
																						Exact: "test.com",
																					},
																					IgnoreCase: true,
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
													Rule: &rbacpb.Permission_NotRule{
														NotRule: &rbacpb.Permission{
															Rule: &rbacpb.Permission_OrRules{
																OrRules: &rbacpb.Permission_Set{
																	Rules: []*rbacpb.Permission{
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Exact{
																								Exact: "/path",
																							},
																						},
																					},
																				},
																			},
																		},
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Exact{
																								Exact: "/foo",
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
									Rule: &rbacpb.Permission_AndRules{
										AndRules: &rbacpb.Permission_Set{
											Rules: []*rbacpb.Permission{
												{
													Rule: &rbacpb.Permission_NotRule{
														NotRule: &rbacpb.Permission{
															Rule: &rbacpb.Permission_OrRules{
																OrRules: &rbacpb.Permission_Set{
																	Rules: []*rbacpb.Permission{
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Exact{
																								Exact: "/path",
																							},
																						},
																					},
																				},
																			},
																		},
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Exact{
																								Exact: "/foo",
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
							},
						},
					},
				},
			},
		},
		{
			config: newConfig("gw-xxx-istio-jwt", "wakanda", allowPolicyWithExtensionPaths),
			expect: &rbachttppb.RBAC{
				ShadowRules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						"envoyfilter": {
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_Header{},
								},
							},
						},
					},
				},
				Rules: &rbacpb.RBAC{
					Action: rbacpb.RBAC_DENY,
					Policies: map[string]*rbacpb.Policy{
						"jwt": {
							Principals: principals,
							Permissions: []*rbacpb.Permission{
								{
									Rule: &rbacpb.Permission_AndRules{
										AndRules: &rbacpb.Permission_Set{
											Rules: []*rbacpb.Permission{
												{
													Rule: &rbacpb.Permission_OrRules{
														OrRules: &rbacpb.Permission_Set{
															Rules: []*rbacpb.Permission{
																{
																	Rule: &rbacpb.Permission_Header{
																		Header: &routev3.HeaderMatcher{
																			Name: ":authority",
																			HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
																				StringMatch: &matcher.StringMatcher{
																					MatchPattern: &matcher.StringMatcher_Exact{
																						Exact: "test.com",
																					},
																					IgnoreCase: true,
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
													Rule: &rbacpb.Permission_NotRule{
														NotRule: &rbacpb.Permission{
															Rule: &rbacpb.Permission_OrRules{
																OrRules: &rbacpb.Permission_Set{
																	Rules: []*rbacpb.Permission{
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Exact{
																								Exact: "/test",
																							},
																						},
																					},
																				},
																			},
																		},
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Prefix{
																								Prefix: "/prefix",
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
									Rule: &rbacpb.Permission_AndRules{
										AndRules: &rbacpb.Permission_Set{
											Rules: []*rbacpb.Permission{
												{
													Rule: &rbacpb.Permission_NotRule{
														NotRule: &rbacpb.Permission{
															Rule: &rbacpb.Permission_OrRules{
																OrRules: &rbacpb.Permission_Set{
																	Rules: []*rbacpb.Permission{
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Exact{
																								Exact: "/test",
																							},
																						},
																					},
																				},
																			},
																		},
																		{
																			Rule: &rbacpb.Permission_UrlPath{
																				UrlPath: &matcher.PathMatcher{
																					Rule: &matcher.PathMatcher_Path{
																						Path: &matcher.StringMatcher{
																							MatchPattern: &matcher.StringMatcher_Prefix{
																								Prefix: "/prefix",
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
							},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run("", func(t *testing.T) {
			for _, cfg := range envoyFilters {
				_, _ = store.Create(cfg)
			}
			env.ConfigStore = store
			serviceDiscovery := aggregate.NewController(aggregate.Options{})
			env.ServiceDiscovery = serviceDiscovery
			m := mesh.DefaultMeshConfig()
			env.Watcher = mesh.NewFixedWatcher(m)
			env.Init()

			// Init a new push context
			pc := model.NewPushContext()
			pc.Mesh = m
			if err := pc.InitContext(env, nil, nil); err != nil {
				t.Fatal(err)
			}

			authz, _ := createFakeAuthorizationPolicies(testCase.config, t)
			pc.AuthzPolicies = authz
			node := &model.Proxy{
				Metadata:        &model.NodeMetadata{},
				ConfigNamespace: "wakanda",
			}

			result := generateRBACFilters(node, pc)
			if !proto.Equal(testCase.expect, result) {
				t.Fatalf("should be equal. got %v, want %v", result, testCase.expect)
			}
		})
	}
}

func createFakeAuthorizationPolicies(config config.Config, t *testing.T) (*model.AuthorizationPolicies, *model.Environment) {
	store := memory.Make(collections.Pilot)
	store.Create(config)
	environment := &model.Environment{
		ConfigStore: store,
		Watcher:     mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
	}
	authzPolicies := model.GetAuthorizationPolicies(environment)
	return authzPolicies, environment
}

func newConfig(name, ns string, spec config.Spec) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.AuthorizationPolicy.GroupVersionKind(),
			Name:             name,
			Namespace:        ns,
		},
		Spec: spec,
	}
}

func TestGetRBACFilter(t *testing.T) {
	rbac := &rbachttppb.RBAC{
		Rules: &rbacpb.RBAC{},
	}
	rbacFilter := &http_conn.HttpFilter{
		Name: "rbac",
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(rbac),
		},
	}

	if !proto.Equal(GetRBACFilter(rbacFilter), rbac) {
		t.Fatal("should be equal")
	}

	rateLimit := &ratelimitv3.RateLimit{}
	rateLimitFilter := &http_conn.HttpFilter{
		Name: "rate-limit",
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(rateLimit),
		},
	}

	if GetRBACFilter(rateLimitFilter) != nil {
		t.Fatal("should be nil")
	}

	rbacECDS := &http_conn.HttpFilter{
		Name: "ecds",
		ConfigType: &http_conn.HttpFilter_ConfigDiscovery{
			ConfigDiscovery: &v3.ExtensionConfigSource{
				TypeUrls: []string{RBACTypeUrl},
			},
		},
	}

	if GetRBACFilter(rbacECDS) != nil {
		t.Fatal("should be nil")
	}
}

func TestHasRBACFilter(t *testing.T) {
	rbac := &rbachttppb.RBAC{
		Rules: &rbacpb.RBAC{},
	}
	rbacFilter := &http_conn.HttpFilter{
		Name: "rbac",
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(rbac),
		},
	}

	if !HasRBACFilter(rbacFilter) {
		t.Fatal("should have")
	}

	rateLimit := &ratelimitv3.RateLimit{}
	rateLimitFilter := &http_conn.HttpFilter{
		Name: "rate-limit",
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(rateLimit),
		},
	}

	if HasRBACFilter(rateLimitFilter) {
		t.Fatal("should not have")
	}

	rbacECDS := &http_conn.HttpFilter{
		Name: "ecds",
		ConfigType: &http_conn.HttpFilter_ConfigDiscovery{
			ConfigDiscovery: &v3.ExtensionConfigSource{
				TypeUrls: []string{RBACTypeUrl},
			},
		},
	}

	if !HasRBACFilter(rbacECDS) {
		t.Fatal("should have")
	}
}
