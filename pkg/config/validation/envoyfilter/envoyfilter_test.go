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

package envoyfilter

import (
	"strings"
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/wellknown"
)

const (
	// Config name for testing
	someName = "foo"
	// Config namespace for testing.
	someNamespace = "bar"
)

func stringOrEmpty(v error) string {
	if v == nil {
		return ""
	}
	return v.Error()
}

func checkValidationMessage(t *testing.T, gotWarning Warning, gotError error, wantWarning string, wantError string) {
	t.Helper()
	if (gotError == nil) != (wantError == "") {
		t.Fatalf("got err=%v but wanted err=%v", gotError, wantError)
	}
	if !strings.Contains(stringOrEmpty(gotError), wantError) {
		t.Fatalf("got err=%v but wanted err=%v", gotError, wantError)
	}

	if (gotWarning == nil) != (wantWarning == "") {
		t.Fatalf("got warning=%v but wanted warning=%v", gotWarning, wantWarning)
	}
	if !strings.Contains(stringOrEmpty(gotWarning), wantWarning) {
		t.Fatalf("got warning=%v but wanted warning=%v", gotWarning, wantWarning)
	}
}

func TestValidateEnvoyFilter(t *testing.T) {
	tests := []struct {
		name    string
		in      proto.Message
		error   string
		warning string
	}{
		{name: "empty filters", in: &networking.EnvoyFilter{}, error: ""},
		{name: "labels not defined in workload selector", in: &networking.EnvoyFilter{
			WorkloadSelector: &networking.WorkloadSelector{},
		}, error: "", warning: "Envoy filter: workload selector specified without labels, will be applied to all services in namespace"},
		{name: "invalid applyTo", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: 0,
				},
			},
		}, error: "Envoy filter: missing applyTo"},
		{name: "nil patch", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch:   nil,
				},
			},
		}, error: "Envoy filter: missing patch"},
		{name: "invalid patch operation", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch:   &networking.EnvoyFilter_Patch{},
				},
			},
		}, error: "Envoy filter: missing patch operation"},
		{name: "nil patch value", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
					},
				},
			},
		}, error: "Envoy filter: missing patch value for non-remove operation"},
		{name: "match with invalid regex", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						Proxy: &networking.EnvoyFilter_ProxyMatch{
							ProxyVersion: "%#@~++==`24c234`",
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: invalid regex for proxy version, [error parsing regexp: invalid nested repetition operator: `++`]"},
		{name: "match with valid regex", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						Proxy: &networking.EnvoyFilter_ProxyMatch{
							ProxyVersion: `release-1\.2-23434`,
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: ""},
		{name: "listener with invalid match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: applyTo for listener class objects cannot have non listener match"},
		{name: "listener with invalid filter match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Sni:    "124",
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: filter match has no name to match on"},
		{name: "listener with sub filter match and invalid applyTo", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
										Name:      "random",
										SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{},
									},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: subfilter match can be used with applyTo HTTP_FILTER only"},
		{name: "listener with sub filter match and invalid filter name", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
										Name:      "random",
										SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{},
									},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: subfilter match requires filter match with envoy.filters.network.http_connection_manager"},
		{name: "listener with sub filter match and no sub filter name", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
										Name:      wellknown.HTTPConnectionManager,
										SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{},
									},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: subfilter match has no name to match on"},
		{name: "route configuration with invalid match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_VIRTUAL_HOST,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: applyTo for http route class objects cannot have non route configuration match"},
		{name: "cluster with invalid match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: applyTo for cluster class objects cannot have non cluster match"},
		{name: "upstream http filter with invalid match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: applyTo for cluster class objects cannot have non cluster match"},
		{name: "cluster with invalid filter match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{
								Filter: &networking.EnvoyFilter_ClusterMatch_UpstreamFilterMatch{
									Name: "random",
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: upstream filter match has no effect when used with CLUSTER"},
		{name: "upstream http filter with invalid filter match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{
								Filter: &networking.EnvoyFilter_ClusterMatch_UpstreamFilterMatch{
									Name: "",
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: upstream filter match has no name to match on"},
		{name: "invalid patch value", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"name": {
									Kind: &structpb.Value_BoolValue{BoolValue: false},
								},
							},
						},
					},
				},
			},
		}, error: `Envoy filter: json: cannot unmarshal bool into Go value of type string`},
		{name: "happy config", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"lb_policy": {
									Kind: &structpb.Value_StringValue{StringValue: "RING_HASH"},
								},
							},
						},
					},
				},
				{
					ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Name: "envoy.tcp_proxy",
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"typed_config": {
									Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{
										Fields: map[string]*structpb.Value{
											"@type": {
												Kind: &structpb.Value_StringValue{
													StringValue: "type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz",
												},
											},
										},
									}},
								},
							},
						},
					},
				},
				{
					ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Name: "envoy.tcp_proxy",
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"typed_config": {
									Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{
										Fields: map[string]*structpb.Value{
											"@type": {
												Kind: &structpb.Value_StringValue{
													StringValue: "type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz",
												},
											},
										},
									}},
								},
							},
						},
					},
				},
			},
		}, error: ""},
		{name: "deprecated config", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Name: "envoy.tcp_proxy",
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"typed_config": {
									Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{
										Fields: map[string]*structpb.Value{
											"@type": {
												Kind: &structpb.Value_StringValue{
													StringValue: "type.googleapis.com/envoy.config.filter.network.ext_authz.v2.ExtAuthz",
												},
											},
										},
									}},
								},
							},
						},
					},
				},
			},
		}, error: "referenced type unknown (hint: try using the v3 XDS API)"},
		{name: "deprecated type", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
										Name: "envoy.http_connection_manager",
									},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
						Value:     &structpb.Struct{},
					},
				},
			},
		}, error: "", warning: "using deprecated filter name"},
		// Regression test for https://github.com/golang/protobuf/issues/1374
		{name: "duration marshal", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"dns_refresh_rate": {
									Kind: &structpb.Value_StringValue{
										StringValue: "500ms",
									},
								},
							},
						},
					},
				},
			},
		}, error: "", warning: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warn, err := validateEnvoyFilter(config.Config{
				Meta: config.Meta{
					Name:      someName,
					Namespace: someNamespace,
				},
				Spec: tt.in,
			}, Validation{})
			checkValidationMessage(t, warn, err, tt.warning, tt.error)
		})
	}
}

func TestRecurseMissingTypedConfig(t *testing.T) {
	good := &listener.Filter{
		Name:       wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: nil},
	}
	ecds := &hcm.HttpFilter{
		Name:       "something",
		ConfigType: &hcm.HttpFilter_ConfigDiscovery{},
	}
	bad := &listener.Filter{
		Name: wellknown.TCPProxy,
	}
	assert.Equal(t, recurseMissingTypedConfig(good.ProtoReflect()), []string{}, "typed config set")
	assert.Equal(t, recurseMissingTypedConfig(ecds.ProtoReflect()), []string{}, "config discovery set")
	assert.Equal(t, recurseMissingTypedConfig(bad.ProtoReflect()), []string{wellknown.TCPProxy}, "typed config not set")
}
