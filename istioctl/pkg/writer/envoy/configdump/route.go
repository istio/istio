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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"text/tabwriter"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	protio "istio.io/istio/istioctl/pkg/util/proto"
)

// RouteFilter is used to pass filter information into route based config writer print functions
type RouteFilter struct {
	Name string
}

// Verify returns true if the passed route matches the filter fields
func (r *RouteFilter) Verify(route *xdsapi.RouteConfiguration) bool {
	if r.Name != "" && r.Name != route.Name {
		return false
	}
	return true
}

// PrintRouteSummary prints a summary of the relevant routes in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintRouteSummary(filter RouteFilter) error {
	w, routes, err := c.setupRouteConfigWriter()
	if err != nil {
		return err
	}
	fmt.Fprintln(c.Stdout, "NOTE: This output only contains routes loaded via RDS.")
	fmt.Fprintln(w, "NAME\tVIRTUAL HOSTS")
	for _, route := range routes {
		if filter.Verify(route) {
			fmt.Fprintf(w, "%v\t%v\n", route.Name, len(route.GetVirtualHosts()))
		}
	}
	return w.Flush()
}

// PrintRouteDump prints the relevant routes in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintRouteDump(filter RouteFilter) error {
	_, routes, err := c.setupRouteConfigWriter()
	if err != nil {
		return err
	}
	filteredRoutes := protio.MessageSlice{}
	for _, route := range routes {
		if filter.Verify(route) {
			filteredRoutes = append(filteredRoutes, route)
		}
	}
	out, err := json.MarshalIndent(filteredRoutes, "", "    ")
	if err != nil {
		return err
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) setupRouteConfigWriter() (*tabwriter.Writer, []*xdsapi.RouteConfiguration, error) {
	routes, err := c.retrieveSortedRouteSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)
	return w, routes, nil
}

func (c *ConfigWriter) retrieveSortedRouteSlice() ([]*xdsapi.RouteConfiguration, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	routeDump, err := c.configDump.GetRouteConfigDump()
	if err != nil {
		return nil, err
	}
	routes := []*xdsapi.RouteConfiguration{}
	for _, route := range routeDump.DynamicRouteConfigs {
		routes = append(routes, route.RouteConfig)
	}
	for _, route := range routeDump.StaticRouteConfigs {
		routes = append(routes, route.RouteConfig)
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes found")
	}
	sort.Slice(routes, func(i, j int) bool {
		iName, err := strconv.Atoi(routes[i].Name)
		if err != nil {
			return false
		}
		jName, err := strconv.Atoi(routes[j].Name)
		if err != nil {
			return false
		}
		return iName < jName
	})
	return routes, nil
}
