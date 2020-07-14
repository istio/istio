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

package clusters

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

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
	address            string
	port               int
	cluster            string
	status             core.HealthStatus
	failedOutlierCheck bool
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
	addr := host.Address.GetSocketAddress()
	if addr != nil {
		return addr.Address
	}
	return "unix://" + host.Address.GetPipe().Path
}

func retrieveEndpointPort(l *adminapi.HostStatus) uint32 {
	addr := l.Address.GetSocketAddress()
	if addr != nil {
		return addr.GetPortValue()
	}
	return 0
}

func retrieveEndpointStatus(l *adminapi.HostStatus) core.HealthStatus {
	return l.HealthStatus.GetEdsHealthStatus()
}

func retrieveFailedOutlierCheck(l *adminapi.HostStatus) bool {
	return l.HealthStatus.GetFailedOutlierCheck()
}

// Verify returns true if the passed host matches the filter fields
func (e *EndpointFilter) Verify(host *adminapi.HostStatus, cluster string) bool {
	if e.Address == "" && e.Port == 0 && e.Cluster == "" && e.Status == "" {
		return true
	}
	if e.Address != "" && !strings.EqualFold(retrieveEndpointAddress(host), e.Address) {
		return false
	}
	if e.Port != 0 && retrieveEndpointPort(host) != e.Port {
		return false
	}
	if e.Cluster != "" && !strings.EqualFold(cluster, e.Cluster) {
		return false
	}
	status := retrieveEndpointStatus(host)
	if e.Status != "" && !strings.EqualFold(core.HealthStatus_name[int32(status)], e.Status) {
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
				outlierCheck := retrieveFailedOutlierCheck(host)
				clusterEndpoint = append(clusterEndpoint, EndpointCluster{addr, int(port), cluster.Name, status, outlierCheck})
			}
		}
	}

	clusterEndpoint = retrieveSortedEndpointClusterSlice(clusterEndpoint)
	fmt.Fprintln(w, "ENDPOINT\tSTATUS\tOUTLIER CHECK\tCLUSTER")
	for _, ce := range clusterEndpoint {
		var endpoint string
		if ce.port != 0 {
			endpoint = ce.address + ":" + strconv.Itoa(ce.port)
		} else {
			endpoint = ce.address
		}
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", endpoint, core.HealthStatus_name[int32(ce.status)], printFailedOutlierCheck(ce.failedOutlierCheck), ce.cluster)
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

func printFailedOutlierCheck(b bool) string {
	if b {
		return "FAILED"
	}
	return "OK"
}
