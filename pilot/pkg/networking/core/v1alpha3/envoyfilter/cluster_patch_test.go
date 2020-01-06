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

package envoyfilter

import (
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/google/go-cmp/cmp"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
)

func Test_clusterMatch(t *testing.T) {
	type args struct {
		proxy          *model.Proxy
		cluster        *xdsapi.Cluster
		matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch
		operation      networking.EnvoyFilter_Patch_Operation
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "name mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{Name: "scooby"},
					},
				},
				cluster: &xdsapi.Cluster{Name: "scrappy"},
			},
			want: false,
		},
		{
			name: "subset mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &xdsapi.Cluster{Name: "outbound|80|v2|foo.bar"},
			},
			want: false,
		},
		{
			name: "service mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &xdsapi.Cluster{Name: "outbound|80|v1|google.com"},
			},
			want: false,
		},
		{
			name: "port mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &xdsapi.Cluster{Name: "outbound|90|v1|foo.bar"},
			},
			want: false,
		},
		{
			name: "full match",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &xdsapi.Cluster{Name: "outbound|80|v1|foo.bar"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterMatch(tt.args.cluster, &model.EnvoyFilterConfigPatchWrapper{Match: tt.args.matchCondition}); got != tt.want {
				t.Errorf("clusterMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyClusterPatches(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-cluster1"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-cluster2"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Service: "gateway.com",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{Operation: networking.EnvoyFilter_Patch_REMOVE},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						PortNumber: 9999,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{Operation: networking.EnvoyFilter_Patch_REMOVE},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"dns_lookup_family":"V6_ONLY"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"lb_policy":"RING_HASH"}`),
			},
		},
	}

	sidecarOutboundIn := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V4_ONLY, LbPolicy: xdsapi.Cluster_ROUND_ROBIN},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_MAGLEV,
		},
	}

	sidecarOutboundOut := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V6_ONLY, LbPolicy: xdsapi.Cluster_RING_HASH},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_RING_HASH, DnsLookupFamily: xdsapi.Cluster_V6_ONLY,
		},
		{Name: "new-cluster1"},
		{Name: "new-cluster2"},
	}

	sidecarInboundIn := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V4_ONLY, LbPolicy: xdsapi.Cluster_ROUND_ROBIN},
		{Name: "inbound|9999||mgmtCluster"},
	}
	sidecarInboundOut := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V6_ONLY, LbPolicy: xdsapi.Cluster_RING_HASH},
	}

	gatewayInput := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V4_ONLY, LbPolicy: xdsapi.Cluster_ROUND_ROBIN},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_MAGLEV,
		},
		{Name: "outbound|443||gateway.com"},
	}
	gatewayOutput := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V6_ONLY, LbPolicy: xdsapi.Cluster_RING_HASH},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_RING_HASH, DnsLookupFamily: xdsapi.Cluster_V6_ONLY,
		},
	}

	testCases := []struct {
		name         string
		input        []*xdsapi.Cluster
		proxy        *model.Proxy
		patchContext networking.EnvoyFilter_PatchContext
		output       []*xdsapi.Cluster
	}{
		{
			name:         "sidecar outbound cluster patch",
			input:        sidecarOutboundIn,
			proxy:        &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"},
			patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			output:       sidecarOutboundOut,
		},
		{
			name:         "sidecar inbound cluster patch",
			input:        sidecarInboundIn,
			patchContext: networking.EnvoyFilter_SIDECAR_INBOUND,
			proxy:        &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"},
			output:       sidecarInboundOut,
		},
		{
			name:         "gateway cds patch",
			input:        gatewayInput,
			patchContext: networking.EnvoyFilter_GATEWAY,
			proxy:        &model.Proxy{Type: model.Router, ConfigNamespace: "not-default"},
			output:       gatewayOutput,
		},
	}

	serviceDiscovery := &fakes.ServiceDiscovery{}
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(env, nil, nil)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyClusterPatches(tc.patchContext, tc.proxy, push, tc.input)
			if diff := cmp.Diff(tc.output, got); diff != "" {
				t.Errorf("ApplyClusterPatches(): %s mismatch (-want +got):\n%s", tc.name, diff)
			}
		})
	}
}
