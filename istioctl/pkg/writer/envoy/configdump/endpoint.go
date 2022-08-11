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
	"strconv"
	"strings"
	"text/tabwriter"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	anypb "github.com/golang/protobuf/ptypes/any"
	"sigs.k8s.io/yaml"

	protio "istio.io/istio/istioctl/pkg/util/proto"
	"istio.io/istio/pilot/pkg/networking/util"
)

type EndpointFilter struct {
	Address string
	Port    uint32
	Cluster string
	Status  string
}

// Verify returns true if the passed host matches the filter fields
func (e *EndpointFilter) Verify(ep *endpoint.LbEndpoint, cluster string) bool {
	if e.Address == "" && e.Port == 0 && e.Cluster == "" && e.Status == "" {
		return true
	}
	if e.Address != "" && !strings.EqualFold(retrieveEndpointAddress(ep), e.Address) {
		return false
	}
	if e.Port != 0 && retrieveEndpointPort(ep) != e.Port {
		return false
	}
	if e.Cluster != "" && !strings.EqualFold(cluster, e.Cluster) {
		return false
	}
	status := retrieveEndpointStatus(ep)
	if e.Status != "" && !strings.EqualFold(core.HealthStatus_name[int32(status)], e.Status) {
		return false
	}
	return true
}

func retrieveEndpointStatus(ep *endpoint.LbEndpoint) core.HealthStatus {
	return ep.GetHealthStatus()
}

func retrieveEndpointPort(ep *endpoint.LbEndpoint) uint32 {
	return ep.GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()
}

func retrieveEndpointAddress(ep *endpoint.LbEndpoint) string {
	addr := ep.GetEndpoint().GetAddress()
	if addr := addr.GetSocketAddress(); addr != nil {
		return addr.Address + ":" + strconv.Itoa(int(addr.GetPortValue()))
	}
	if addr := addr.GetPipe(); addr != nil {
		return addr.GetPath()
	}
	return ""
}

func (c *ConfigWriter) PrintEndpoints(filter EndpointFilter, outputFormat string) error {
	if c.configDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}
	dump, err := c.retrieveSortedEndpointsSlice(filter)
	if err != nil {
		return err
	}
	marshaller := make(protio.MessageSlice, 0, len(dump))
	for _, eds := range dump {
		marshaller = append(marshaller, eds)
	}
	out, err := json.MarshalIndent(marshaller, "", "    ")
	if err != nil {
		return err
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) PrintEndpointsSummary(filter EndpointFilter) error {
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)

	fmt.Fprintln(w, "ENDPOINT\tSTATUS\tLOCALITY\tCLUSTER")
	dump, err := c.retrieveSortedEndpointsSlice(filter)
	if err != nil {
		return err
	}
	for _, eds := range dump {
		for _, llb := range eds.Endpoints {
			for _, ep := range llb.LbEndpoints {
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\n",
					retrieveEndpointAddress(ep),
					ep.GetHealthStatus().String(),
					util.LocalityToString(llb.Locality),
					eds.ClusterName,
				)
			}
		}
	}

	return w.Flush()
}

func (c *ConfigWriter) retrieveSortedEndpointsSlice(filter EndpointFilter) ([]*endpoint.ClusterLoadAssignment, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	dump, err := c.configDump.GetEndpointsConfigDump()
	if err != nil {
		return nil, err
	}
	endpoints := make([]*endpoint.ClusterLoadAssignment, 0, len(dump.DynamicEndpointConfigs))
	for _, e := range dump.GetDynamicEndpointConfigs() {
		cla, epCount := retrieveEndpoint(e.EndpointConfig, filter)
		if epCount != 0 {
			endpoints = append(endpoints, cla)
		}
	}
	for _, e := range dump.GetStaticEndpointConfigs() {
		cla, epCount := retrieveEndpoint(e.EndpointConfig, filter)
		if epCount != 0 {
			endpoints = append(endpoints, cla)
		}
	}
	sort.Slice(endpoints, func(i, j int) bool {
		iDirection, iSubset, iName, iPort := safelyParseSubsetKey(endpoints[i].ClusterName)
		jDirection, jSubset, jName, jPort := safelyParseSubsetKey(endpoints[j].ClusterName)
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
	return endpoints, nil
}

func retrieveEndpoint(epConfig *anypb.Any, filter EndpointFilter) (*endpoint.ClusterLoadAssignment, int) {
	cla := &endpoint.ClusterLoadAssignment{}
	if err := epConfig.UnmarshalTo(cla); err != nil {
		return nil, 0
	}
	filteredCount := 0
	for _, llb := range cla.Endpoints {
		filtered := make([]*endpoint.LbEndpoint, 0, len(llb.LbEndpoints))
		for _, ep := range llb.LbEndpoints {
			if !filter.Verify(ep, cla.ClusterName) {
				continue
			}
			filtered = append(filtered, ep)
		}
		llb.LbEndpoints = filtered
		filteredCount += len(llb.LbEndpoints)
	}

	return cla, filteredCount
}
