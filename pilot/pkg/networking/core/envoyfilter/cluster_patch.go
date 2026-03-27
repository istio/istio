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
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto/merge"
)

// ApplyClusterMerge processes the MERGE operation and merges the supplied configuration to the matched clusters.
func ApplyClusterMerge(pctx networking.EnvoyFilter_PatchContext, efw *model.MergedEnvoyFilterWrapper,
	c *cluster.Cluster, hosts []host.Name,
) (out *cluster.Cluster) {
	defer runtime.HandleCrash(runtime.LogPanic, func(any) {
		log.Errorf("clusters patch caused panic, so the patches did not take effect")
		IncrementEnvoyFilterErrorMetric(Cluster)
	})
	// In case the patches cause panic, use the clusters generated before to reduce the influence.
	out = c
	if efw == nil {
		return out
	}
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		applied := false
		// For removed patches, skip the merge if the patch matches.
		if cp.Operation == networking.EnvoyFilter_Patch_REMOVE &&
			commonConditionMatch(pctx, cp) &&
			clusterMatch(c, cp, hosts) {
			return nil
		}
		if cp.Operation != networking.EnvoyFilter_Patch_MERGE {
			IncrementEnvoyFilterMetric(cp.Key(), Cluster, applied)
			continue
		}
		if commonConditionMatch(pctx, cp) && clusterMatch(c, cp, hosts) {
			tsMerged, err := mergeTransportSocketCluster(c, cp)
			if err != nil {
				log.Debugf("Merge of transport socket failed for cluster: %v", err)
				continue
			}
			applied = true
			if !tsMerged {
				merge.Merge(c, cp.Value)
			}
		}
		IncrementEnvoyFilterMetric(cp.Key(), Cluster, applied)
	}
	return c
}

// Test if the patch contains a config for TransportSocket
// Returns a boolean indicating if the merge was handled by this function; if false, it should still be called
// outside of this function.
func mergeTransportSocketCluster(c *cluster.Cluster, cp *model.EnvoyFilterConfigPatchWrapper) (merged bool, err error) {
	cpValueCast, okCpCast := (cp.Value).(*cluster.Cluster)
	if !okCpCast {
		return false, fmt.Errorf("cast of cp.Value failed: %v", okCpCast)
	}

	// Check if cluster patch has a transport socket.
	if cpValueCast.GetTransportSocket() == nil {
		return false, nil
	}

	var ts *core.TransportSocket
	// First check if the transport socket matches with any cluster transport socket matches.
	if len(c.GetTransportSocketMatches()) > 0 {
		for _, tsm := range c.GetTransportSocketMatches() {
			if tsm.GetTransportSocket() != nil && cpValueCast.GetTransportSocket().Name == tsm.GetTransportSocket().Name {
				ts = tsm.GetTransportSocket()
				break
			}
		}
		if ts == nil {
			// If we merged we would get both a transport_socket and transport_socket_matches which is not valid
			// Drop the filter, but indicate that we handled the merge so that the outer function does not try
			// to merge it again
			return true, nil
		}
	} else if c.GetTransportSocket() != nil {
		if cpValueCast.GetTransportSocket().Name == c.GetTransportSocket().Name {
			ts = c.GetTransportSocket()
		}
	}
	// This means either there is a name mismatch or cluster does not have transport socket matches/transport socket.
	// We cannot do a deep merge. Instead just replace the transport socket
	if ts == nil {
		c.TransportSocket = cpValueCast.TransportSocket
	} else {
		// Merge the patch and the cluster at a lower level
		dst := ts.GetTypedConfig()
		srcPatch := cpValueCast.GetTransportSocket().GetTypedConfig()
		if dst != nil && srcPatch != nil {
			retVal, errMerge := util.MergeAnyWithAny(dst, srcPatch)
			if errMerge != nil {
				return false, fmt.Errorf("function MergeAnyWithAny failed for ApplyClusterMerge: %v", errMerge)
			}

			// Merge the above result with the whole cluster
			merge.Merge(dst, retVal)
		}
	}
	return true, nil
}

// InsertedClusters collects all clusters that are added via ADD operation and match the patch context.
func InsertedClusters(pctx networking.EnvoyFilter_PatchContext, efw *model.MergedEnvoyFilterWrapper) []*cluster.Cluster {
	if efw == nil {
		return nil
	}
	var result []*cluster.Cluster
	// Add cluster if the operation is add, and patch context matches
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if cp.Operation == networking.EnvoyFilter_Patch_ADD {
			// If cluster ADD patch does not specify a patch context, only add for sidecar outbound and gateway.
			if cp.Match.Context == networking.EnvoyFilter_ANY && pctx != networking.EnvoyFilter_SIDECAR_OUTBOUND &&
				pctx != networking.EnvoyFilter_GATEWAY {
				continue
			}
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
