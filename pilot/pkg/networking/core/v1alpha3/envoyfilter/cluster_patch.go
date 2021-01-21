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
	"strconv"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/golang/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

const (
	RemovePatch = "-"
	MergeAny    = "*"
	MergeOne    = "+"
	Service     = "service"
	Subset      = "subset"
	Port        = "port"
)

func GenerateMatchMap(pctx networking.EnvoyFilter_PatchContext, efw *model.EnvoyFilterWrapper) (map[string][]*model.EnvoyFilterConfigPatchWrapper, map[string][]*model.EnvoyFilterConfigPatchWrapper, map[string][]*model.EnvoyFilterConfigPatchWrapper, map[string][]*model.EnvoyFilterConfigPatchWrapper) {
	cpw := make(map[string][]*model.EnvoyFilterConfigPatchWrapper)
	serviceMap := make(map[string][]*model.EnvoyFilterConfigPatchWrapper)
	subsetMap := make(map[string][]*model.EnvoyFilterConfigPatchWrapper)
	portMap := make(map[string][]*model.EnvoyFilterConfigPatchWrapper)
	if efw == nil {
		return cpw, serviceMap, subsetMap, portMap
	}
	for _, cp := range efw.Patches[networking.EnvoyFilter_CLUSTER] {
		if cp.Operation != networking.EnvoyFilter_Patch_REMOVE &&
			cp.Operation != networking.EnvoyFilter_Patch_MERGE {
			continue
		}
		if !commonConditionMatch(pctx, cp) {
			continue
		}

		if cp.Match.GetCluster() == nil {
			if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				cpw[RemovePatch] = append(cpw[RemovePatch], cp)
				continue
			}
			cpw[MergeAny] = append(cpw[MergeAny], cp)
			continue
		}
		if cp.Match.GetCluster().Name != "" {
			if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
				cpw[cp.Match.GetCluster().Name+RemovePatch] = append(cpw[cp.Match.GetCluster().Name+RemovePatch], cp)
				continue
			}
			cpw[cp.Match.GetCluster().Name+MergeOne] = append(cpw[cp.Match.GetCluster().Name+MergeOne], cp)
			continue
		}

		service := cp.Match.GetCluster().Service
		subset := cp.Match.GetCluster().Subset
		port := cp.Match.GetCluster().PortNumber

		if service != "" {
			serviceMap[service] = append(serviceMap[service], cp)
		} else {
			serviceMap[MergeAny] = append(serviceMap[MergeAny], cp)
		}

		if subset != "" {
			subsetMap[subset] = append(subsetMap[subset], cp)
		} else {
			subsetMap[MergeAny] = append(subsetMap[MergeAny], cp)
		}

		if port != 0 {
			portMap[strconv.Itoa(int(port))] = append(portMap[strconv.Itoa(int(port))], cp)
		} else {
			portMap[MergeAny] = append(portMap[MergeAny], cp)
		}
	}
	return cpw, serviceMap, subsetMap, portMap
}

func ApplyClusterMergeOrRemove(c *cluster.Cluster, cpw, serviceMap, subsetMap, portMap map[string][]*model.EnvoyFilterConfigPatchWrapper) (out *cluster.Cluster, shouldKeep bool) {
	defer runtime.HandleCrash(func(interface{}) {
		log.Errorf("clusters patch caused panic, so the patches did not take effect")
	})

	shouldKeep = true
	if len(cpw[RemovePatch]) > 0 || len(cpw[c.Name+RemovePatch]) > 0 {
		c = nil
		shouldKeep = false
		return nil, shouldKeep
	}
	if len(cpw[MergeAny]) > 0 {
		for _, cp := range cpw[MergeAny] {
			proto.Merge(c, cp.Value)
		}
	}
	if len(cpw[c.Name+MergeOne]) > 0 {
		for _, cp := range cpw[c.Name+MergeOne] {
			proto.Merge(c, cp.Value)
		}
	}
	key, minMatchMap := findMinMatchMap(c.Name, serviceMap, subsetMap, portMap)

	switch key {
	case Service:
		for _, cp := range minMatchMap {
			c, shouldKeep = mergeOrRemove(c, cp)
		}
		for _, cp := range serviceMap[MergeAny] {
			c, shouldKeep = mergeOrRemove(c, cp)
		}

	case Subset:
		for _, cp := range minMatchMap {
			c, shouldKeep = mergeOrRemove(c, cp)
		}
		for _, cp := range subsetMap[MergeAny] {
			c, shouldKeep = mergeOrRemove(c, cp)
		}

	case Port:
		for _, cp := range minMatchMap {
			c, shouldKeep = mergeOrRemove(c, cp)
		}
		for _, cp := range portMap[MergeAny] {
			c, shouldKeep = mergeOrRemove(c, cp)
		}
	default:
		//do nothing
	}
	return c, shouldKeep
}

func mergeOrRemove(cluster *cluster.Cluster, cp *model.EnvoyFilterConfigPatchWrapper) (*cluster.Cluster, bool) {
	if clusterMatch(cluster, cp) {
		if cp.Operation == networking.EnvoyFilter_Patch_REMOVE {
			return nil, false
		}
		proto.Merge(cluster, cp.Value)
	}
	return cluster, true
}

func findMinMatchMap(name string,
	serviceMap map[string][]*model.EnvoyFilterConfigPatchWrapper,
	subsetMap map[string][]*model.EnvoyFilterConfigPatchWrapper,
	portMap map[string][]*model.EnvoyFilterConfigPatchWrapper) (string, []*model.EnvoyFilterConfigPatchWrapper) {

	_, subset, hostname, port := model.ParseSubsetKey(name)

	serviceMapLen := len(serviceMap[string(hostname)]) + len(serviceMap[MergeAny])
	subsetMapLen := len(subsetMap[subset]) + len(subsetMap[MergeAny])
	portMapLen := len(portMap[strconv.Itoa(port)]) + len(portMap[MergeAny])

	if serviceMapLen == 0 && subsetMapLen == 0 && portMapLen == 0 {
		return "", nil
	}
	var intArr = []int{serviceMapLen, subsetMapLen, portMapLen}
	minVal := intArr[0]
	minValIndex := 0
	for i := 1; i < len(intArr); i++ {
		if minVal == 0 {
			minVal = intArr[i]
			minValIndex = i
			continue
		}
		if minVal > intArr[i] && intArr[i] != 0 {
			minVal = intArr[i]
			minValIndex = i
		}
	}
	switch minValIndex {
	case 0:
		return Service, serviceMap[string(hostname)]
	case 1:
		return Subset, subsetMap[subset]
	case 2:
		return Port, portMap[strconv.Itoa(port)]
	default:
		return "", nil
	}
}

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
