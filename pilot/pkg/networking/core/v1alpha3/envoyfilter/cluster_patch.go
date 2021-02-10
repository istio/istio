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
	"fmt"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

// ApplyClusterMerge processes the MERGE operation and merges the supplied configuration to the matched clusters.
func ApplyClusterMerge(pctx networking.EnvoyFilter_PatchContext, efw *model.EnvoyFilterWrapper, c *cluster.Cluster, hosts []host.Name) (out *cluster.Cluster) {
	defer runtime.HandleCrash(runtime.LogPanic, func(interface{}) {
		log.Errorf("clusters patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the clusters generated before to reduce the influence.
	out = c
	if efw == nil {
		return
	}
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if cp.Operation != networking.EnvoyFilter_Patch_MERGE {
			continue
		}
		if commonConditionMatch(pctx, cp) && clusterMatch(c, cp, hosts) {

			ret, err := mergeTransportSocketCluster(c, cp)
			if err != nil {
				log.Debugf("Merge of transport socket failed for cluster: %v", err)
				continue
			}
			if !ret {
				proto.Merge(c, cp.Value)
			}
		}
	}
	return c
}

// Test if the patch contains a config for TransportSocket
func mergeTransportSocketCluster(c *cluster.Cluster, cp *model.EnvoyFilterConfigPatchWrapper) (bool, error) {
	cpValueCast, okCpCast := (cp.Value).(*cluster.Cluster)
	if !okCpCast {
		return false, fmt.Errorf("cast of cp.Value failed: %v", okCpCast)
	}

	var tsmPatch *core.TransportSocket

	// Test if the patch contains a config for TransportSocket
	// and if the cluster contains a config for Transport Socket Matches
	if cpValueCast.GetTransportSocket() != nil && c.GetTransportSocketMatches() != nil {
		for _, tsm := range c.GetTransportSocketMatches() {
			if tsm.GetTransportSocket() != nil && cpValueCast.GetTransportSocket().Name == tsm.GetTransportSocket().Name {
				tsmPatch = tsm.GetTransportSocket()
				break
			}
		}
	} else if cpValueCast.GetTransportSocket() != nil && c.GetTransportSocket() != nil {
		if cpValueCast.GetTransportSocket().Name == c.GetTransportSocket().Name {
			tsmPatch = c.GetTransportSocket()
		}
	}

	if tsmPatch != nil {
		// Merge the patch and the cluster at a lower level
		dstCluster := tsmPatch.GetTypedConfig()
		srcPatch := cpValueCast.GetTransportSocket().GetTypedConfig()

		if dstCluster != nil && srcPatch != nil {

			retVal, errMerge := util.MergeAnyWithAny(dstCluster, srcPatch)
			if errMerge != nil {
				return false, fmt.Errorf("function MergeAnyWithAny failed for ApplyClusterMerge: %v", errMerge)
			}

			// Merge the above result with the whole cluster
			proto.Merge(dstCluster, retVal)
			return true, nil
		}
	}

	return false, nil
}

// ShouldKeepCluster checks if there is a REMOVE patch on the cluster, returns false if there is on so that it is removed.
func ShouldKeepCluster(pctx networking.EnvoyFilter_PatchContext, efw *model.EnvoyFilterWrapper, c *cluster.Cluster, hosts []host.Name) bool {
	if efw == nil {
		return true
	}
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if cp.Operation != networking.EnvoyFilter_Patch_REMOVE {
			continue
		}
		if commonConditionMatch(pctx, cp) && clusterMatch(c, cp, hosts) {
			return false
		}
	}
	return true
}

// InsertedClusters collects all clusters that are added via ADD operation and match the patch context.
func InsertedClusters(pctx networking.EnvoyFilter_PatchContext, efw *model.EnvoyFilterWrapper) []*cluster.Cluster {
	if efw == nil {
		return nil
	}
	var result []*cluster.Cluster
	// Add cluster if the operation is add, and patch context matches
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if cp.Operation == networking.EnvoyFilter_Patch_ADD {
			if commonConditionMatch(pctx, cp) {
				result = append(result, proto.Clone(cp.Value).(*cluster.Cluster))
			}
		}
	}
	return result
}

func clusterMatch(cluster *cluster.Cluster, cp *model.EnvoyFilterConfigPatchWrapper, hosts []host.Name) bool {
	cMatch := cp.Match.GetCluster()
	if cMatch == nil {
		return true
	}

	if cMatch.Name != "" {
		return cMatch.Name == cluster.Name
	}

	direction, subset, hostname, port := model.ParseSubsetKey(cluster.Name)

	hostMatches := []host.Name{hostname}
	// For inbound clusters, host parsed from subset key will be empty. Use the passed in service name.
	if direction == model.TrafficDirectionInbound && len(hosts) > 0 {
		hostMatches = hosts
	}

	if cMatch.Subset != "" && cMatch.Subset != subset {
		return false
	}

	if cMatch.Service != "" && !hostContains(hostMatches, host.Name(cMatch.Service)) {
		return false
	}

	// FIXME: Ports on a cluster can be 0. the API only takes uint32 for ports
	// We should either make that field in API as a wrapper type or switch to int
	if cMatch.PortNumber != 0 && int(cMatch.PortNumber) != port {
		return false
	}
	return true
}

func hostContains(hosts []host.Name, service host.Name) bool {
	for _, h := range hosts {
		if h == service {
			return true
		}
	}
	return false
}
