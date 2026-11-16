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
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

// DynamicSubsetKeys returns every key referenced by the dynamic subset settings, in a stable order.
// These are the keys endpoints must expose as metadata for the subsets to be computed.
func DynamicSubsetKeys(ds *networking.DynamicSubsetLB) []string {
	if ds == nil {
		return nil
	}
	var keys []string
	for _, s := range ds.GetSubsetSelectors() {
		for _, k := range s.GetKeys() {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	for k := range ds.GetDefaultSubset() {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	return slices.Sort(keys)
}

// ApplyDynamicSubset configures Envoy's subset load balancer on the cluster from the dynamic subset
// settings. Endpoints expose the selector keys as `envoy.lb` metadata (see DynamicSubsetKeys), and a
// request selects a subset by carrying `envoy.lb` metadata matching all keys of one selector.
//
// Only EDS clusters are supported, as the metadata is only attached to endpoints sent over EDS.
// Envoy rejects `lb_subset_config` together with `load_balancing_policy`, so clusters using a typed
// load balancing policy are left unchanged.
func ApplyDynamicSubset(c *cluster.Cluster, ds *networking.DynamicSubsetLB) {
	if ds == nil || len(ds.GetSubsetSelectors()) == 0 {
		return
	}
	if c.GetType() != cluster.Cluster_EDS || c.GetLoadBalancingPolicy() != nil || c.GetLbSubsetConfig() != nil {
		return
	}
	cfg := &cluster.Cluster_LbSubsetConfig{
		// Keep honoring locality weights within a subset, if the cluster uses them.
		LocalityWeightAware: c.GetCommonLbConfig().GetLocalityWeightedLbConfig() != nil,
		SubsetSelectors:     make([]*cluster.Cluster_LbSubsetConfig_LbSubsetSelector, 0, len(ds.GetSubsetSelectors())),
	}
	for _, s := range ds.GetSubsetSelectors() {
		keys := slices.Sort(slices.Clone(s.GetKeys()))
		cfg.SubsetSelectors = append(cfg.SubsetSelectors, &cluster.Cluster_LbSubsetConfig_LbSubsetSelector{
			Keys: keys,
			// A built-in key identifies one endpoint, which allows Envoy to use a cheaper host lookup.
			SingleHostPerSubset: len(keys) == 1 && isBuiltinDynamicSubsetKey(keys[0]),
		})
	}
	switch ds.GetFallbackPolicy() {
	case networking.DynamicSubsetLB_NO_FALLBACK:
		cfg.FallbackPolicy = cluster.Cluster_LbSubsetConfig_NO_FALLBACK
	case networking.DynamicSubsetLB_DEFAULT_SUBSET:
		cfg.FallbackPolicy = cluster.Cluster_LbSubsetConfig_DEFAULT_SUBSET
		fields := make(map[string]*structpb.Value, len(ds.GetDefaultSubset()))
		for _, k := range slices.Sort(maps.Keys(ds.GetDefaultSubset())) {
			fields[k] = structpb.NewStringValue(ds.GetDefaultSubset()[k])
		}
		cfg.DefaultSubset = &structpb.Struct{Fields: fields}
	default:
		cfg.FallbackPolicy = cluster.Cluster_LbSubsetConfig_ANY_ENDPOINT
	}
	c.LbSubsetConfig = cfg
}

func isBuiltinDynamicSubsetKey(k string) bool {
	return k == constants.DynamicSubsetIPKey || k == constants.DynamicSubsetHostnameKey
}
