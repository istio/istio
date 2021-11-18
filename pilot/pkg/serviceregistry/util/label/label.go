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
	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/network"
)

// copied from https://github.com/kubernetes/api/blob/master/core/v1/well_known_labels.go
// It is to remove dependency on k8s.io/api/core/v1
const (
	LabelTopologyZone   = "topology.kubernetes.io/zone"
	LabelTopologyRegion = "topology.kubernetes.io/region"
)

// AugmentLabels adds additional labels to the those provided.
func AugmentLabels(in labels.Instance, clusterID cluster.ID, locality string, networkID network.ID) labels.Instance {
	// Copy the original labels to a new map.
	out := make(labels.Instance, len(in)+5)
	for k, v := range in {
		out[k] = v
	}

	region, zone, subzone := model.SplitLocalityLabel(locality)
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
	if len(networkID) > 0 {
		out[label.TopologyNetwork.Name] = networkID.String()
	}
	return out
}
