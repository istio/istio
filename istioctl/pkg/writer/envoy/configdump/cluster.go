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

package configdump

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/golang/protobuf/ptypes"

	protio "istio.io/istio/istioctl/pkg/util/proto"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
)

// ClusterFilter is used to pass filter information into cluster based config writer print functions
type ClusterFilter struct {
	FQDN      host.Name
	Port      int
	Subset    string
	Direction model.TrafficDirection
}

// Verify returns true if the passed cluster matches the filter fields
func (c *ClusterFilter) Verify(cluster *cluster.Cluster) bool {
	name := cluster.Name
	if c.FQDN == "" && c.Port == 0 && c.Subset == "" && c.Direction == "" {
		return true
	}
	if c.FQDN != "" && !strings.Contains(name, string(c.FQDN)) {
		return false
	}
	if c.Direction != "" && !strings.Contains(name, string(c.Direction)) {
		return false
	}
	if c.Subset != "" && !strings.Contains(name, c.Subset) {
		return false
	}
	if c.Port != 0 {
		p := fmt.Sprintf("|%v|", c.Port)
		if !strings.Contains(name, p) {
			return false
		}
	}
	return true
}

// PrintClusterSummary prints a summary of the relevant clusters in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintClusterSummary(filter ClusterFilter) error {
	w, clusters, err := c.setupClusterConfigWriter()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w, "SERVICE FQDN\tPORT\tSUBSET\tDIRECTION\tTYPE\tDESTINATION RULE")
	for _, c := range clusters {
		if filter.Verify(c) {
			if len(strings.Split(c.Name, "|")) > 3 {
				direction, subset, fqdn, port := model.ParseSubsetKey(c.Name)
				if subset == "" {
					subset = "-"
				}
				_, _ = fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%s\t%s\n", fqdn, port, subset, direction, c.GetType(),
					describeManagement(c.GetMetadata()))
			} else {
				_, _ = fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%s\t%s\n", c.Name, "-", "-", "-", c.GetType(),
					describeManagement(c.GetMetadata()))
			}
		}
	}
	return w.Flush()
}

// PrintClusterDump prints the relevant clusters in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintClusterDump(filter ClusterFilter) error {
	_, clusters, err := c.setupClusterConfigWriter()
	if err != nil {
		return err
	}
	filteredClusters := protio.MessageSlice{}
	for _, cluster := range clusters {
		if filter.Verify(cluster) {
			filteredClusters = append(filteredClusters, cluster)
		}
	}
	out, err := json.MarshalIndent(filteredClusters, "", "    ")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) setupClusterConfigWriter() (*tabwriter.Writer, []*cluster.Cluster, error) {
	clusters, err := c.retrieveSortedClusterSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)
	return w, clusters, nil
}

func (c *ConfigWriter) retrieveSortedClusterSlice() ([]*cluster.Cluster, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	clusterDump, err := c.configDump.GetClusterConfigDump()
	if err != nil {
		return nil, err
	}
	clusters := make([]*cluster.Cluster, 0)
	for _, c := range clusterDump.DynamicActiveClusters {
		if c.Cluster != nil {
			clusterTyped := &cluster.Cluster{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			c.Cluster.TypeUrl = v3.ClusterType
			err = ptypes.UnmarshalAny(c.Cluster, clusterTyped)
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, clusterTyped)
		}
	}
	for _, c := range clusterDump.StaticClusters {
		if c.Cluster != nil {
			clusterTyped := &cluster.Cluster{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			c.Cluster.TypeUrl = v3.ClusterType
			err = ptypes.UnmarshalAny(c.Cluster, clusterTyped)
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, clusterTyped)
		}
	}
	if len(clusters) == 0 {
		return nil, fmt.Errorf("no clusters found")
	}
	sort.Slice(clusters, func(i, j int) bool {
		iDirection, iSubset, iName, iPort := safelyParseSubsetKey(clusters[i].Name)
		jDirection, jSubset, jName, jPort := safelyParseSubsetKey(clusters[j].Name)
		if iName == jName {
			if iSubset == jSubset {
				if iPort == jPort {
					return iDirection < jDirection
				}
				return iPort < jPort
			}
			return iSubset < jSubset
		}
		return iName < jName
	})
	return clusters, nil
}

func safelyParseSubsetKey(key string) (model.TrafficDirection, string, host.Name, int) {
	if len(strings.Split(key, "|")) > 3 {
		return model.ParseSubsetKey(key)
	}
	name := host.Name(key)
	return "", "", name, 0
}
