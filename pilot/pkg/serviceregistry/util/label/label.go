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

package label

import (
	"strings"

	"istio.io/api/label"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/network"
)

// copied from https://github.com/kubernetes/api/blob/master/core/v1/well_known_labels.go
// It is to remove dependency on k8s.io/api/core/v1
const (
	LabelHostname = "kubernetes.io/hostname"

	LabelTopologyZone    = "topology.kubernetes.io/zone"
	LabelTopologySubzone = "topology.istio.io/subzone"
	LabelTopologyRegion  = "topology.kubernetes.io/region"
)

// AugmentLabels adds additional labels to the those provided.
func AugmentLabels(in labels.Instance, clusterID cluster.ID, locality, k8sNode string, networkID network.ID) labels.Instance {
	// Copy the original labels to a new map.
	out := make(labels.Instance, len(in)+6)
	for k, v := range in {
		out[k] = v
	}

	region, zone, subzone := SplitLocalityLabel(locality)
	if len(region) > 0 {
		out[LabelTopologyRegion] = region
	}
	if len(zone) > 0 {
		out[LabelTopologyZone] = zone
	}
	if len(subzone) > 0 {
		out[label.TopologySubzone.Name] = subzone
	}
	if len(clusterID) > 0 {
		out[label.TopologyCluster.Name] = clusterID.String()
	}
	if len(k8sNode) > 0 {
		out[LabelHostname] = k8sNode
	}
	// In c.Network(), we already set the network label in priority order pod labels > namespace label > mesh Network
	// We won't let proxy.Metadata.Network override the above.
	if len(networkID) > 0 && out[label.TopologyNetwork.Name] == "" {
		out[label.TopologyNetwork.Name] = networkID.String()
	}
	return out
}

// SplitLocalityLabel splits a locality label into region, zone and subzone strings.
func SplitLocalityLabel(locality string) (region, zone, subzone string) {
	items := strings.Split(locality, "/")
	switch len(items) {
	case 1:
		return items[0], "", ""
	case 2:
		return items[0], items[1], ""
	default:
		return items[0], items[1], items[2]
	}
}
