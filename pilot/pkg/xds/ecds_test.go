// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package xds_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/spiffe"
	istiotest "istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

func TestECDSWasm(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "./testdata/ecds.yaml"),
	})

	ads := s.ConnectADS().WithType(v3.ExtensionConfigurationType)
	wantExtensionConfigName := "extension-config"
	md := model.NodeMetadata{
		ClusterID: "Kubernetes",
	}
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		Node: &core.Node{
			Id:       ads.ID,
			Metadata: md.ToStruct(),
		},
		ResourceNames: []string{wantExtensionConfigName},
	})

	var ec core.TypedExtensionConfig
	err := res.Resources[0].UnmarshalTo(&ec)
	if err != nil {
		t.Fatal("Failed to unmarshal extension config", err)
		return
	}
	if ec.Name != wantExtensionConfigName {
		t.Errorf("extension config name got %v want %v", ec.Name, wantExtensionConfigName)
	}
}

func TestECDSJwtAuthn(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "./testdata/ecds_jwt.yaml"),
	})
	ads := s.ConnectADS().WithType(v3.ExtensionConfigurationType)
	wantExtensionConfigName := "envoy.filters.http.jwt_authn"
	md := model.NodeMetadata{
		ClusterID: "Kubernetes",
	}
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		Node: &core.Node{
			Id:       ads.ID,
			Metadata: md.ToStruct(),
		},
		ResourceNames: []string{wantExtensionConfigName},
	})
	var ec core.TypedExtensionConfig
	err := res.Resources[0].UnmarshalTo(&ec)
	if err != nil {
		t.Fatal("Failed to unmarshal extension config", err)
		return
	}
	if ec.Name != wantExtensionConfigName {
		t.Errorf("extension config name got %v want %v", ec.Name, wantExtensionConfigName)
	}
}
func makeDockerCredentials(name, namespace string, data map[string]string, secretType corev1.SecretType) *corev1.Secret {
	bdata := map[string][]byte{}
	for k, v := range data {
		bdata[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: bdata,
		Type: secretType,
	}
}

func makeWasmPlugin(name, namespace, secret string) config.Config {
	spec := &extensions.WasmPlugin{
		Phase: extensions.PluginPhase_AUTHN,
	}
	if secret != "" {
		spec.ImagePullSecret = secret
	}
	return config.Config{
		Meta: config.Meta{
			Name:             name,
			Namespace:        namespace,
			GroupVersionKind: gvk.WasmPlugin,
		},
		Spec: spec,
	}
}

var (
	// Secrets
	defaultPullSecret = makeDockerCredentials("default-pull-secret", "default", map[string]string{
		corev1.DockerConfigJsonKey: "default-docker-credential",
	}, corev1.SecretTypeDockerConfigJson)
	rootPullSecret = makeDockerCredentials("root-pull-secret", "istio-system", map[string]string{
		corev1.DockerConfigJsonKey: "root-docker-credential",
	}, corev1.SecretTypeDockerConfigJson)
	wrongTypeSecret = makeDockerCredentials("wrong-type-pull-secret", "default", map[string]string{
		corev1.DockerConfigJsonKey: "wrong-type-docker-credential",
	}, corev1.SecretTypeTLS)

	wasmPlugin             = makeWasmPlugin("default-plugin", "default", "")
	wasmPluginWithSec      = makeWasmPlugin("default-plugin-with-sec", "default", "default-pull-secret")
	wasmPluginWrongSec     = makeWasmPlugin("default-plugin-wrong-sec", "default", "wrong-secret")
	wasmPluginWrongSecType = makeWasmPlugin("default-plugin-wrong-sec-type", "default", "wrong-type-pull-secret")
	rootWasmPluginWithSec  = makeWasmPlugin("root-plugin", "istio-system", "root-pull-secret")
)

func TestECDSWasmGenerate(t *testing.T) {
	cases := []struct {
		name             string
		proxyNamespace   string
		request          *model.PushRequest
		watchedResources []string
		wantExtensions   sets.String
		wantSecrets      sets.String
	}{
		{
			name:             "simple",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin"},
			wantExtensions:   sets.String{"default.default-plugin": {}},
			wantSecrets:      sets.String{},
		},
		{
			name:             "simple_with_secret",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		{
			name:             "miss_secret",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-wrong-sec"},
			wantExtensions:   sets.String{"default.default-plugin-wrong-sec": {}},
			wantSecrets:      sets.String{},
		},
		{
			name:             "wrong_secret_type",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-wrong-sec-type"},
			wantExtensions:   sets.String{"default.default-plugin-wrong-sec-type": {}},
			wantSecrets:      sets.String{},
		},
		{
			name:             "root_and_default",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-with-sec", "istio-system.root-plugin"},
			wantExtensions:   sets.String{"default.default-plugin-with-sec": {}, "istio-system.root-plugin": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}, "root-docker-credential": {}},
		},
		{
			name:             "only_root",
			proxyNamespace:   "somenamespace",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"istio-system.root-plugin"},
			wantExtensions:   sets.String{"istio-system.root-plugin": {}},
			wantSecrets:      sets.String{"root-docker-credential": {}},
		},
		{
			name:           "no_relevant_config_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.AuthorizationPolicy}),
			},
			watchedResources: []string{"default.default-plugin-with-sec", "istio-system.root-plugin"},
			wantExtensions:   sets.String{},
			wantSecrets:      sets.String{},
		},
		{
			name:           "has_relevant_config_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.AuthorizationPolicy}, model.ConfigKey{Kind: kind.WasmPlugin}),
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		{
			name:           "non_relevant_secret_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.AuthorizationPolicy}, model.ConfigKey{Kind: kind.Secret}),
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.String{},
			wantSecrets:      sets.String{},
		},
		{
			name:           "relevant_secret_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}),
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		{
			name:           "relevant_secret_update_non_full_push",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}),
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		// All the credentials should be sent to istio-agent even if one of them is only updated,
		// because `istio-agent` does not keep the credentials.
		{
			name:           "multi_wasmplugin_update_secret",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}),
			},
			watchedResources: []string{"default.default-plugin-with-sec", "istio-system.root-plugin"},
			wantExtensions:   sets.String{"default.default-plugin-with-sec": {}, "istio-system.root-plugin": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}, "root-docker-credential": {}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				KubernetesObjects: []runtime.Object{defaultPullSecret, rootPullSecret, wrongTypeSecret},
				Configs:           []config.Config{wasmPlugin, wasmPluginWithSec, wasmPluginWrongSec, wasmPluginWrongSecType, rootWasmPluginWithSec},
			})

			gen := s.Discovery.Generators[v3.ExtensionConfigurationType]
			tt.request.Start = time.Now()
			proxy := &model.Proxy{
				VerifiedIdentity: &spiffe.Identity{Namespace: tt.proxyNamespace},
				Type:             model.Router,
				Metadata: &model.NodeMetadata{
					ClusterID: "Kubernetes",
				},
			}
			tt.request.Push = s.PushContext()
			tt.request.Push.Mesh.RootNamespace = "istio-system"
			resources, _, _ := gen.Generate(s.SetupProxy(proxy),
				&model.WatchedResource{ResourceNames: tt.watchedResources}, tt.request)
			gotExtensions := sets.String{}
			gotSecrets := sets.String{}
			for _, res := range resources {
				gotExtensions.Insert(res.Name)
				ec := &core.TypedExtensionConfig{}
				res.Resource.UnmarshalTo(ec)
				wasm := &wasm.Wasm{}
				ec.TypedConfig.UnmarshalTo(wasm)
				gotsecret := wasm.GetConfig().GetVmConfig().GetEnvironmentVariables().GetKeyValues()[model.WasmSecretEnv]
				if gotsecret != "" {
					gotSecrets.Insert(gotsecret)
				}
			}
			if !reflect.DeepEqual(gotSecrets, tt.wantSecrets) {
				t.Errorf("got secrets %v, want secrets %v", gotSecrets, tt.wantSecrets)
			}
			if !reflect.DeepEqual(gotExtensions, tt.wantExtensions) {
				t.Errorf("got extensions %v, want extensions %v", gotExtensions, tt.wantExtensions)
			}
		})
	}
}

func getConfigMetaForJwks(name string) *config.Meta {
	return &config.Meta{
		Name:             name,
		Namespace:        "default",
		GroupVersionKind: gvk.RequestAuthentication,
	}
}

func TestECDSJwtGenerate(t *testing.T) {
	ms, err := test.StartNewServer()
	if err != nil {
		t.Fatal("failed to start a mock server")
	}
	jwksURI := ms.URL + "/oauth2/v3/certs"

	cases := []struct {
		name             string
		proxyNamespace   string
		request          *model.PushRequest
		config           []config.Config
		expectedResults  *envoy_jwt.JwtAuthentication
		enableRemoteJwks bool
	}{
		{
			name:           "Single JWT policy",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: jwksURI,
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
													AllowMissing: &emptypb.Empty{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey1,
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name:             "JWT policy with Mesh cluster as issuer and remote jwks enabled",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			enableRemoteJwks: true,
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "mesh cluster",
								JwksUri: "http://jwt-token-issuer.mesh:7443/jwks",
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
													AllowMissing: &emptypb.Empty{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "mesh cluster",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_RemoteJwks{
							RemoteJwks: &envoy_jwt.RemoteJwks{
								HttpUri: &core.HttpUri{
									Uri: "http://jwt-token-issuer.mesh:7443/jwks",
									HttpUpstreamType: &core.HttpUri_Cluster{
										Cluster: "outbound|7443||jwt-token-issuer.mesh.svc.cluster.local",
									},
									Timeout: &durationpb.Duration{Seconds: 5},
								},
								CacheDuration: &durationpb.Duration{Seconds: 5 * 60},
							},
						},
						Forward:           false,
						PayloadInMetadata: "mesh cluster",
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name:             "JWT policy with non Mesh cluster as issuer and remote jwks enabled",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			enableRemoteJwks: true,
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "invalid|7443|",
								JwksUri: jwksURI,
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
													AllowMissing: &emptypb.Empty{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "invalid|7443|",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey2,
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "invalid|7443|",
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name:           "Multi JWTs policy",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: jwksURI,
							},
						},
					},
				},
				{
					Meta: *getConfigMetaForJwks("policy2"),
					Spec: &v1beta1.RequestAuthentication{},
				},
				{
					Meta: *getConfigMetaForJwks("policy3"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer: "https://secret.bar.com",
								Jwks:   "jwks-inline-data",
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-1",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_RequiresAll{
													RequiresAll: &envoy_jwt.JwtRequirementAndList{
														Requirements: []*envoy_jwt.JwtRequirement{
															{
																RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
																	RequiresAny: &envoy_jwt.JwtRequirementOrList{
																		Requirements: []*envoy_jwt.JwtRequirement{
																			{
																				RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
																					ProviderName: "origins-0",
																				},
																			},
																			{
																				RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
																					AllowMissing: &emptypb.Empty{},
																				},
																			},
																		},
																	},
																},
															},
															{
																RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
																	RequiresAny: &envoy_jwt.JwtRequirementOrList{
																		Requirements: []*envoy_jwt.JwtRequirement{
																			{
																				RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
																					ProviderName: "origins-1",
																				},
																			},
																			{
																				RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
																					AllowMissing: &emptypb.Empty{},
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
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.bar.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: "jwks-inline-data",
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.bar.com",
					},
					"origins-1": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey2,
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name:           "JWT policy with inline Jwks",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer: "https://secret.foo.com",
								Jwks:   "inline-jwks-data",
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
													AllowMissing: &emptypb.Empty{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: "inline-jwks-data",
								},
							},
						},
						Forward:           false,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name:           "JWT policy with bad Jwks URI",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:  "https://secret.foo.com",
								JwksUri: "http://site.not.exist",
							},
						},
					},
				},
			},
			expectedResults: nil,
		},
		{
			name:           "Forward original token",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:               "https://secret.foo.com",
								JwksUri:              jwksURI,
								ForwardOriginalToken: true,
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
													AllowMissing: &emptypb.Empty{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey2,
								},
							},
						},
						Forward:           true,
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name:           "Output Payload",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:                "https://secret.foo.com",
								JwksUri:               jwksURI,
								ForwardOriginalToken:  true,
								OutputPayloadToHeader: "x-foo",
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
													AllowMissing: &emptypb.Empty{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey2,
								},
							},
						},
						Forward:              true,
						ForwardPayloadHeader: "x-foo",
						PayloadInMetadata:    "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
			},
		},
		{
			name:           "Output claim to header",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			config: []config.Config{
				{
					Meta: *getConfigMetaForJwks("policy1"),
					Spec: &v1beta1.RequestAuthentication{
						JwtRules: []*v1beta1.JWTRule{
							{
								Issuer:               "https://secret.foo.com",
								JwksUri:              jwksURI,
								ForwardOriginalToken: true,
								OutputClaimToHeaders: []*v1beta1.ClaimToHeader{
									{Header: "x-jwt-key1", Claim: "value1"},
									{Header: "x-jwt-key2", Claim: "value2"},
								},
							},
						},
					},
				},
			},
			expectedResults: &envoy_jwt.JwtAuthentication{
				Rules: []*envoy_jwt.RequirementRule{
					{
						Match: &route.RouteMatch{
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						RequirementType: &envoy_jwt.RequirementRule_Requires{
							Requires: &envoy_jwt.JwtRequirement{
								RequiresType: &envoy_jwt.JwtRequirement_RequiresAny{
									RequiresAny: &envoy_jwt.JwtRequirementOrList{
										Requirements: []*envoy_jwt.JwtRequirement{
											{
												RequiresType: &envoy_jwt.JwtRequirement_ProviderName{
													ProviderName: "origins-0",
												},
											},
											{
												RequiresType: &envoy_jwt.JwtRequirement_AllowMissing{
													AllowMissing: &emptypb.Empty{},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Providers: map[string]*envoy_jwt.JwtProvider{
					"origins-0": {
						Issuer: "https://secret.foo.com",
						JwksSourceSpecifier: &envoy_jwt.JwtProvider_LocalJwks{
							LocalJwks: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: test.JwtPubKey2,
								},
							},
						},
						Forward: true,
						ClaimToHeaders: []*envoy_jwt.JwtClaimToHeader{
							{HeaderName: "x-jwt-key1", ClaimName: "value1"},
							{HeaderName: "x-jwt-key2", ClaimName: "value2"},
						},
						PayloadInMetadata: "https://secret.foo.com",
					},
				},
				BypassCorsPreflight: true,
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			istiotest.SetForTest(t, &features.EnableRemoteJwks, tt.enableRemoteJwks)
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				Configs: tt.config,
			})

			gen := s.Discovery.Generators[v3.ExtensionConfigurationType]
			tt.request.Start = time.Now()
			proxy := &model.Proxy{
				VerifiedIdentity: &spiffe.Identity{Namespace: tt.proxyNamespace},
				Type:             model.Router,
				Metadata: &model.NodeMetadata{
					ClusterID: "Kubernetes",
				},
			}
			tt.request.Push = s.PushContext()
			tt.request.Push.Mesh.RootNamespace = "istio-system"
			resources, _, _ := gen.Generate(s.SetupProxy(proxy),
				&model.WatchedResource{}, tt.request)
			for _, res := range resources {
				ec := &core.TypedExtensionConfig{}
				res.Resource.UnmarshalTo(ec)
				reqAuth := &envoy_jwt.JwtAuthentication{}
				ec.TypedConfig.UnmarshalTo(reqAuth)
				if !reflect.DeepEqual(reqAuth.Providers, tt.expectedResults.Providers) {
					t.Errorf("got provider %v, want provider %v", spew.Sdump(reqAuth.Providers), spew.Sdump(tt.expectedResults.Providers))
				}
				if !reflect.DeepEqual(reqAuth.Rules, tt.expectedResults.Rules) {
					t.Errorf("got rules %v, want rules %v", spew.Sdump(reqAuth.Rules), spew.Sdump(tt.expectedResults.Rules))
				}
			}
		})
	}
	ms.Stop()
}
