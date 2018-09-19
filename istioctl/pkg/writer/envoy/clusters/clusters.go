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

package clusters

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/istio/istioctl/pkg/util/clusters"
	protio "istio.io/istio/istioctl/pkg/util/proto"
)

// EndpointFilter is used to pass filter information into route based config writer print functions
type EndpointFilter struct {
	Address string
	Port    uint32
	Cluster string
	Status  string
}

// ConfigWriter is a writer for processing responses from the Envoy Admin config_dump endpoint
type ConfigWriter struct {
	Stdout   io.Writer
	clusters *clusters.Wrapper
}

// EndpointCluster is used to store the endpoint and cluster
type EndpointCluster struct {
	address string
	port    int
	cluster string
	status  core.HealthStatus
}

// Prime loads the clusters output into the writer ready for printing
func (c *ConfigWriter) Prime(b []byte) error {
	cd := clusters.Wrapper{}
	err := json.Unmarshal(b, &cd)
	if err != nil {
		return fmt.Errorf("error unmarshalling config dump response from Envoy: %v", err)
	}
	c.clusters = &cd
	return nil
}

func retrieveEndpointAddress(host *adminapi.HostStatus) string {
	return host.Address.GetSocketAddress().Address
}

func retrieveEndpointPort(l *adminapi.HostStatus) uint32 {
	return l.Address.GetSocketAddress().GetPortValue()
}

func retrieveEndpointStatus(l *adminapi.HostStatus) core.HealthStatus {
	return l.HealthStatus.GetEdsHealthStatus()
}

// Verify returns true if the passed host matches the filter fields
func (e *EndpointFilter) Verify(host *adminapi.HostStatus, cluster string) bool {
	if e.Address == "" && e.Port == 0 && e.Cluster == "" && e.Status == "" {
		return true
	}
	if e.Address != "" && strings.ToLower(retrieveEndpointAddress(host)) != strings.ToLower(e.Address) {
		return false
	}
	if e.Port != 0 && retrieveEndpointPort(host) != e.Port {
		return false
	}
	if e.Cluster != "" && strings.ToLower(cluster) != strings.ToLower(e.Cluster) {
		return false
	}
	status := retrieveEndpointStatus(host)
	if e.Status != "" && strings.ToLower(core.HealthStatus_name[int32(status)]) != strings.ToLower(e.Status) {
		return false
	}
	return true
}

// PrintEndpointsSummary prints just the endpoints config summary to the ConfigWriter stdout
func (c *ConfigWriter) PrintEndpointsSummary(filter EndpointFilter) error {
	if c.clusters == nil {
		return fmt.Errorf("config writer has not been primed")
	}

	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)

	clusterEndpoint := make([]EndpointCluster, 0)
	for _, cluster := range c.clusters.ClusterStatuses {
		for _, host := range cluster.HostStatuses {
			if filter.Verify(host, cluster.Name) {
				addr := retrieveEndpointAddress(host)
				port := retrieveEndpointPort(host)
				status := retrieveEndpointStatus(host)

				clusterEndpoint = append(clusterEndpoint, EndpointCluster{addr, int(port), cluster.Name, status})
			}
		}
	}

	clusterEndpoint = retrieveSortedEndpointClusterSlice(clusterEndpoint)
	fmt.Fprintln(w, "ENDPOINT\tSTATUS\tCLUSTER")
	for _, ce := range clusterEndpoint {
		endpoint := ce.address + ":" + strconv.Itoa(int(ce.port))
		fmt.Fprintf(w, "%v\t%v\t%v\n", endpoint, core.HealthStatus_name[int32(ce.status)], ce.cluster)
	}

	return w.Flush()
}

// PrintEndpoints prints the endpoints config to the ConfigWriter stdout
func (c *ConfigWriter) PrintEndpoints(filter EndpointFilter) error {
	if c.clusters == nil {
		return fmt.Errorf("config writer has not been primed")
	}

	filteredClusters := protio.MessageSlice{}
	for _, cluster := range c.clusters.ClusterStatuses {
		for _, host := range cluster.HostStatuses {
			if filter.Verify(host, cluster.Name) {
				filteredClusters = append(filteredClusters, cluster)
				break
			}
		}

	}
	out, err := json.MarshalIndent(filteredClusters, "", "    ")
	if err != nil {
		return err
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func retrieveSortedEndpointClusterSlice(ec []EndpointCluster) []EndpointCluster {
	sort.Slice(ec, func(i, j int) bool {
		if ec[i].address == ec[j].address {
			if ec[i].port == ec[j].port {
				return ec[i].cluster < ec[j].cluster
			}
			return ec[i].port < ec[j].port
		}
		return ec[i].address < ec[j].address
	})
	return ec
}
