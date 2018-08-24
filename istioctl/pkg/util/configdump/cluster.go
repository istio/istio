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

package configdump

import (
	"fmt"
	"sort"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	proto "github.com/gogo/protobuf/types"
)

// GetDynamicClusterDump retrieves a cluster dump with just dynamic active clusters in it
func (w *Wrapper) GetDynamicClusterDump(stripVersions bool) (*adminapi.ClustersConfigDump, error) {
	clusterDump, err := w.GetClusterConfigDump()
	if err != nil {
		return nil, err
	}
	dac := clusterDump.GetDynamicActiveClusters()
	sort.Slice(dac, func(i, j int) bool {
		return dac[i].Cluster.Name < dac[j].Cluster.Name
	})
	if stripVersions {
		for i := range dac {
			dac[i].VersionInfo = ""
			dac[i].LastUpdated = nil
		}
	}
	return &adminapi.ClustersConfigDump{DynamicActiveClusters: dac}, nil
}

// GetClusterConfigDump retrieves the cluster config dump from the ConfigDump
func (w *Wrapper) GetClusterConfigDump() (*adminapi.ClustersConfigDump, error) {
	// The cluster dump is the second one in the list.
	// See https://www.envoyproxy.io/docs/envoy/latest/api-v2/admin/v2alpha/config_dump.proto
	if len(w.Configs) < 2 {
		return nil, fmt.Errorf("config dump has no cluster dump")
	}
	clusterDumpAny := w.Configs[1]
	clusterDump := &adminapi.ClustersConfigDump{}
	err := proto.UnmarshalAny(&clusterDumpAny, clusterDump)
	if err != nil {
		return nil, err
	}
	return clusterDump, nil
}
