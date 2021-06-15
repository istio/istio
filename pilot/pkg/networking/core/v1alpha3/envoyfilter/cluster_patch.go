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

	xdscluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

func ApplyClusterPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	efw *model.EnvoyFilterWrapper,
	clusters []*xdscluster.Cluster,
) (out []*xdscluster.Cluster) {
	defer runtime.HandleCrash(runtime.LogPanic, func(interface{}) {
		IncrementEnvoyFilterErrorMetric(Cluster)
		log.Errorf("listeners patch caused panic, so the patches did not take effect")
	})
	// In case the patches cause panic, use the listeners generated before to reduce the influence.
	out = clusters
	if efw == nil {
		return
	}

	clusterRemoved := false
	// do all the changes for a single envoy filter crd object. [including adds]
	// then move on to the next one

	// only removes/merges plus next level object operations [add/remove/merge]
	for _, cluster := range clusters {
		if cluster.Name == "" {
			// removed by another op
			continue
		}
		patchCluster(patchContext, efw.Patches, cluster, &clusterRemoved)
	}

	// adds at cl level if enabled
	for _, lp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if lp.Operation == networking.EnvoyFilter_Patch_ADD {
			if !commonConditionMatch(patchContext, lp) {
				IncrementEnvoyFilterMetric(lp.Key(), Cluster, false)
				continue
			}

			// clone before append. Otherwise, subsequent operations on this listener will corrupt
			// the master value stored in CP..
			clusters = append(clusters, proto.Clone(lp.Value).(*xdscluster.Cluster))
			IncrementEnvoyFilterMetric(lp.Key(), Cluster, true)
		}
	}

	if clusterRemoved {
		tempArray := make([]*xdscluster.Cluster, 0, len(clusters))
		for _, c := range clusters {
			if c.Name != "" {
				tempArray = append(tempArray, c)
			}
		}
		return tempArray
	}

	return clusters
}

func patchCluster(patchContext networking.EnvoyFilter_PatchContext,
	patches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
	cluster *xdscluster.Cluster,
	listenersRemoved *bool) {
	for _, cp := range patches[networking.EnvoyFilter_CLUSTER] {
		if !commonConditionMatch(patchContext, cp) ||
			!clusterMatch(cluster, cp) {
			IncrementEnvoyFilterMetric(cp.Key(), Cluster, false)
			continue
		}
		IncrementEnvoyFilterMetric(cp.Key(), Cluster, true)
		if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			cluster.Name = ""
			*listenersRemoved = true
			// terminate the function here as we have nothing more do to for this listener
			return
		} else if cp.Operation == networking.EnvoyFilter_Patch_MERGE {
			applied := false
			ret, err := mergeTransportSocketCluster(cluster, cp)
			if err != nil {
				log.Debugf("Merge of transport socket failed for xdscluster: %v", err)
				continue
			}
			applied = true
			if !ret {
				proto.Merge(cluster, cp.Value)
			}
			IncrementEnvoyFilterMetric(cp.Key(), Cluster, applied)
		}
	}
}

// Test if the patch contains a config for TransportSocket
// Returns a boolean indicating if the merge was handled by this function; if false, it should still be called
// outside of this function.
func mergeTransportSocketCluster(c *xdscluster.Cluster, cp *model.EnvoyFilterConfigPatchWrapper) (merged bool, err error) {
	cpValueCast, okCpCast := (cp.Value).(*xdscluster.Cluster)
	if !okCpCast {
		return false, fmt.Errorf("cast of cp.Value failed: %v", okCpCast)
	}

	var tsmPatch *core.TransportSocket

	// Test if the patch contains a config for TransportSocket
	// and if the xdscluster contains a config for Transport Socket Matches
	if cpValueCast.GetTransportSocket() != nil && c.GetTransportSocketMatches() != nil {
		for _, tsm := range c.GetTransportSocketMatches() {
			if tsm.GetTransportSocket() != nil && cpValueCast.GetTransportSocket().Name == tsm.GetTransportSocket().Name {
				tsmPatch = tsm.GetTransportSocket()
				break
			}
		}
		if tsmPatch == nil && len(c.GetTransportSocketMatches()) > 0 {
			// If we merged we would get both a transport_socket and transport_socket_matches which is not valid
			// Drop the filter, but indicate that we handled the merge so that the outer function does not try
			// to merge it again
			return true, nil
		}
	} else if cpValueCast.GetTransportSocket() != nil && c.GetTransportSocket() != nil {
		if cpValueCast.GetTransportSocket().Name == c.GetTransportSocket().Name {
			tsmPatch = c.GetTransportSocket()
		} else {
			// There is a name mismatch, so we cannot do a deep merge. Instead just replace the transport socket
			c.TransportSocket = cpValueCast.TransportSocket
			return true, nil
		}
	}

	if tsmPatch != nil {
		// Merge the patch and the xdscluster at a lower level
		dstCluster := tsmPatch.GetTypedConfig()
		srcPatch := cpValueCast.GetTransportSocket().GetTypedConfig()

		if dstCluster != nil && srcPatch != nil {

			retVal, errMerge := util.MergeAnyWithAny(dstCluster, srcPatch)
			if errMerge != nil {
				return false, fmt.Errorf("function MergeAnyWithAny failed for ApplyClusterMerge: %v", errMerge)
			}

			// Merge the above result with the whole xdscluster
			proto.Merge(dstCluster, retVal)
			return true, nil
		}
	}

	return false, nil
}

func clusterMatch(cluster *xdscluster.Cluster, cp *model.EnvoyFilterConfigPatchWrapper) bool {
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
