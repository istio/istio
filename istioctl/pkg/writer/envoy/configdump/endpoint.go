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
	"google.golang.org/protobuf/types/known/anypb"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/util/proto"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/slices"
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
	if e.Address != "" {
		found := slices.FindFunc(retrieveEndpointAddresses(ep), func(s string) bool {
			return strings.EqualFold(s, e.Address)
		}) != nil
		if !found {
			return false
		}
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

func retrieveEndpointAddresses(ep *endpoint.LbEndpoint) []string {
	addrs := []*core.Address{ep.GetEndpoint().GetAddress()}
	for _, a := range ep.GetEndpoint().GetAdditionalAddresses() {
		addrs = append(addrs, a.GetAddress())
	}
	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr := addr.GetSocketAddress(); addr != nil {
			result = append(result, addr.Address+":"+strconv.Itoa(int(addr.GetPortValue())))
			continue
		}
		if addr := addr.GetPipe(); addr != nil {
			result = append(result, addr.GetPath())
			continue
		}
		if internal := addr.GetEnvoyInternalAddress(); internal != nil {
			switch an := internal.GetAddressNameSpecifier().(type) {
			case *core.EnvoyInternalAddress_ServerListenerName:
				result = append(result, fmt.Sprintf("envoy://%s/%s", an.ServerListenerName, internal.EndpointId))
				continue
			}
		}
		result = append(result, "unknown")
	}

	return result
}

func (c *ConfigWriter) PrintEndpoints(filter EndpointFilter, outputFormat string) error {
	if c.configDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}
	dump, err := c.retrieveSortedEndpointsSlice(filter)
	if err != nil {
		return err
	}
	marshaller := make(proto.MessageSlice, 0, len(dump))
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

	fmt.Fprintln(w, "NAME\tSTATUS\tLOCALITY\tCLUSTER")
	dump, err := c.retrieveSortedEndpointsSlice(filter)
	if err != nil {
		return err
	}
	for _, eds := range dump {
		for _, llb := range eds.Endpoints {
			for _, ep := range llb.LbEndpoints {
				addr := strings.Join(retrieveEndpointAddresses(ep), ",")
				if includeConfigType {
					addr = fmt.Sprintf("endpoint/%s", addr)
				}
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\n",
					addr,
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
	if dump == nil {
		return nil, nil
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
