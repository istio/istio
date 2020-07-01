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
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/golang/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/config/host"
)

// ApplyClusterPatches applies patches to CDS clusters
func ApplyClusterPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	proxy *model.Proxy,
	push *model.PushContext,
	clusters []*cluster.Cluster) (out []*cluster.Cluster) {
	defer runtime.HandleCrash(func() {
		log.Errorf("clusters patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the clusters generated before to reduce the influence.
	out = clusters

	efw := push.EnvoyFilters(proxy)
	if efw == nil {
		return out
	}

	clustersRemoved := false
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if cp.Operation != networking.EnvoyFilter_Patch_REMOVE &&
			cp.Operation != networking.EnvoyFilter_Patch_MERGE {
			continue
		}
		for i := range clusters {
			if clusters[i] == nil {
				// deleted by the remove operation
				continue
			}

			if commonConditionMatch(patchContext, cp) && clusterMatch(clusters[i], cp) {
				if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
					clusters[i] = nil
					clustersRemoved = true
				} else {
					proto.Merge(clusters[i], cp.Value)
				}
			}
		}
	}

	// Add cluster if the operation is add, and patch context matches
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if cp.Operation == networking.EnvoyFilter_Patch_ADD {
			if commonConditionMatch(patchContext, cp) {
				clusters = append(clusters, proto.Clone(cp.Value).(*cluster.Cluster))
			}
		}
	}

	if clustersRemoved {
		trimmedClusters := make([]*cluster.Cluster, 0, len(clusters))
		for i := range clusters {
			if clusters[i] == nil {
				continue
			}
			trimmedClusters = append(trimmedClusters, clusters[i])
		}
		clusters = trimmedClusters
	}
	return clusters
}

func clusterMatch(cluster *cluster.Cluster, cp *model.EnvoyFilterConfigPatchWrapper) bool {
	cMatch := cp.Match.GetCluster()
	if cMatch == nil {
		return true
	}

	if cMatch.Name != "" {
		return cMatch.Name == cluster.Name
	}

	_, subset, hostname, port := model.ParseSubsetKey(cluster.Name)

	if cMatch.Subset != "" && cMatch.Subset != subset {
		return false
	}

	if cMatch.Service != "" && host.Name(cMatch.Service) != hostname {
		return false
	}

	// FIXME: Ports on a cluster can be 0. the API only takes uint32 for ports
	// We should either make that field in API as a wrapper type or switch to int
	if cMatch.PortNumber != 0 && int(cMatch.PortNumber) != port {
		return false
	}
	return true
}
