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

package loadbalancer

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/util/assert"
)

func TestDynamicSubsetKeys(t *testing.T) {
	assert.Equal(t, DynamicSubsetKeys(nil), nil)
	assert.Equal(t, DynamicSubsetKeys(&networking.DynamicSubsetLB{
		SubsetSelectors: []*networking.DynamicSubsetLB_SubsetSelector{
			{Keys: []string{"version", "stage"}},
			{Keys: []string{"version"}},
			{Keys: []string{constants.DynamicSubsetIPKey}},
		},
		DefaultSubset: map[string]string{"track": "stable", "version": "v1"},
	}), []string{constants.DynamicSubsetIPKey, "stage", "track", "version"})
}

func TestApplyDynamicSubset(t *testing.T) {
	eds := func() *cluster.Cluster {
		return &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}}
	}
	settings := &networking.DynamicSubsetLB{
		SubsetSelectors: []*networking.DynamicSubsetLB_SubsetSelector{
			{Keys: []string{"version", "stage"}},
			{Keys: []string{constants.DynamicSubsetIPKey}},
			{Keys: []string{constants.DynamicSubsetHostnameKey}},
		},
	}
	selectors := []*cluster.Cluster_LbSubsetConfig_LbSubsetSelector{
		{Keys: []string{"stage", "version"}},
		{Keys: []string{constants.DynamicSubsetIPKey}, SingleHostPerSubset: true},
		{Keys: []string{constants.DynamicSubsetHostnameKey}, SingleHostPerSubset: true},
	}
	existing := &cluster.Cluster_LbSubsetConfig{FallbackPolicy: cluster.Cluster_LbSubsetConfig_NO_FALLBACK}
	cases := []struct {
		name     string
		cluster  *cluster.Cluster
		settings *networking.DynamicSubsetLB
		want     *cluster.Cluster_LbSubsetConfig
	}{
		{
			name:     "no settings",
			cluster:  eds(),
			settings: nil,
			want:     nil,
		},
		{
			name:     "non eds type of cluster",
			cluster:  &cluster.Cluster{Name: "foo", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS}},
			settings: settings,
			want:     nil,
		},
		{
			name:     "defaults to any endpoint fallback",
			cluster:  eds(),
			settings: settings,
			want: &cluster.Cluster_LbSubsetConfig{
				FallbackPolicy:  cluster.Cluster_LbSubsetConfig_ANY_ENDPOINT,
				SubsetSelectors: selectors,
			},
		},
		{
			name:    "no fallback",
			cluster: eds(),
			settings: &networking.DynamicSubsetLB{
				SubsetSelectors: settings.SubsetSelectors,
				FallbackPolicy:  networking.DynamicSubsetLB_NO_FALLBACK,
			},
			want: &cluster.Cluster_LbSubsetConfig{
				FallbackPolicy:  cluster.Cluster_LbSubsetConfig_NO_FALLBACK,
				SubsetSelectors: selectors,
			},
		},
		{
			name:    "default subset",
			cluster: eds(),
			settings: &networking.DynamicSubsetLB{
				SubsetSelectors: settings.SubsetSelectors,
				FallbackPolicy:  networking.DynamicSubsetLB_DEFAULT_SUBSET,
				DefaultSubset:   map[string]string{"version": "v1"},
			},
			want: &cluster.Cluster_LbSubsetConfig{
				FallbackPolicy:  cluster.Cluster_LbSubsetConfig_DEFAULT_SUBSET,
				DefaultSubset:   &structpb.Struct{Fields: map[string]*structpb.Value{"version": structpb.NewStringValue("v1")}},
				SubsetSelectors: selectors,
			},
		},
		{
			name: "locality weighted",
			cluster: &cluster.Cluster{
				Name:                 "foo",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				CommonLbConfig: &cluster.Cluster_CommonLbConfig{
					LocalityConfigSpecifier: &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig_{
						LocalityWeightedLbConfig: &cluster.Cluster_CommonLbConfig_LocalityWeightedLbConfig{},
					},
				},
			},
			settings: settings,
			want: &cluster.Cluster_LbSubsetConfig{
				FallbackPolicy:      cluster.Cluster_LbSubsetConfig_ANY_ENDPOINT,
				LocalityWeightAware: true,
				SubsetSelectors:     selectors,
			},
		},
		{
			name: "typed load balancing policy",
			cluster: &cluster.Cluster{
				Name:                 "foo",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				LoadBalancingPolicy:  &cluster.LoadBalancingPolicy{},
			},
			settings: settings,
			want:     nil,
		},
		{
			name: "existing subset config",
			cluster: &cluster.Cluster{
				Name:                 "foo",
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				LbSubsetConfig:       existing,
			},
			settings: settings,
			want:     existing,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ApplyDynamicSubset(tt.cluster, tt.settings)
			assert.Equal(t, tt.cluster.LbSubsetConfig, tt.want)
		})
	}
}
