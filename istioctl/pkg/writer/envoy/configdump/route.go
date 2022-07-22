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

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"sigs.k8s.io/yaml"

	protio "istio.io/istio/istioctl/pkg/util/proto"
	pilot_util "istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/util/sets"
)

// RouteFilter is used to pass filter information into route based config writer print functions
type RouteFilter struct {
	Name    string
	Verbose bool
}

// Verify returns true if the passed route matches the filter fields
func (r *RouteFilter) Verify(route *route.RouteConfiguration) bool {
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
	if filter.Verbose {
		fmt.Fprintln(w, "NAME\tDOMAINS\tMATCH\tVIRTUAL SERVICE")
	} else {
		fmt.Fprintln(w, "NAME\tVIRTUAL HOSTS")
	}
	for _, route := range routes {
		if filter.Verify(route) {
			if filter.Verbose {
				for _, vhosts := range route.GetVirtualHosts() {
					for _, r := range vhosts.Routes {
						if !isPassthrough(r.GetAction()) {
							fmt.Fprintf(w, "%v\t%s\t%s\t%s\n",
								route.Name,
								describeRouteDomains(vhosts.GetDomains()),
								describeMatch(r.GetMatch()),
								describeManagement(r.GetMetadata()))
						}
					}
					if len(vhosts.Routes) == 0 {
						fmt.Fprintf(w, "%v\t%s\t%s\t%s\n",
							route.Name,
							describeRouteDomains(vhosts.GetDomains()),
							"/*",
							"404")
					}
				}
			} else {
				fmt.Fprintf(w, "%v\t%v\n", route.Name, len(route.GetVirtualHosts()))
			}
		}
	}
	return w.Flush()
}

func describeRouteDomains(domains []string) string {
	if len(domains) == 0 {
		return ""
	}
	if len(domains) == 1 {
		return domains[0]
	}

	// Return the shortest non-numeric domain.  Count of domains seems uninteresting.
	max := 2
	withoutPort := make([]string, 0, len(domains))
	for _, d := range domains {
		if !strings.Contains(d, ":") {
			withoutPort = append(withoutPort, d)
			// if the domain contains IPv6, such as [fd00:10:96::7fc7] and [fd00:10:96::7fc7]:8090
		} else if strings.Count(d, ":") > 2 {
			// if the domain is only a IPv6 address, such as [fd00:10:96::7fc7], append it
			if strings.HasSuffix(d, "]") {
				withoutPort = append(withoutPort, d)
			}
		}
	}
	withoutPort = unexpandDomains(withoutPort)
	if len(withoutPort) > max {
		ret := strings.Join(withoutPort[:max], ", ")
		return fmt.Sprintf("%s + %d more...", ret, len(withoutPort)-max)
	}
	return strings.Join(withoutPort, ", ")
}

func unexpandDomains(domains []string) []string {
	unique := sets.New(domains...)
	shouldDelete := sets.New()
	for _, h := range domains {
		stripFull := strings.TrimSuffix(h, ".svc.cluster.local")
		if _, f := unique[stripFull]; f && stripFull != h {
			shouldDelete.Insert(h)
		}
		stripPartial := strings.TrimSuffix(h, ".svc")
		if _, f := unique[stripPartial]; f && stripPartial != h {
			shouldDelete.Insert(h)
		}
	}
	// Filter from original list to keep original order
	ret := make([]string, 0, len(domains))
	for _, h := range domains {
		if _, f := shouldDelete[h]; !f {
			ret = append(ret, h)
		}
	}
	return ret
}

func describeManagement(metadata *envoy_config_core_v3.Metadata) string {
	if metadata == nil {
		return ""
	}
	istioMetadata, ok := metadata.FilterMetadata[pilot_util.IstioMetadataKey]
	if !ok {
		return ""
	}
	config, ok := istioMetadata.Fields["config"]
	if !ok {
		return ""
	}
	return renderConfig(config.GetStringValue())
}

func renderConfig(configPath string) string {
	if strings.HasPrefix(configPath, "/apis/networking.istio.io/v1alpha3/namespaces/") {
		pieces := strings.Split(configPath, "/")
		if len(pieces) != 8 {
			return ""
		}
		return fmt.Sprintf("%s.%s", pieces[7], pieces[5])
	}
	return "<unknown>"
}

// PrintRouteDump prints the relevant routes in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintRouteDump(filter RouteFilter, outputFormat string) error {
	_, routes, err := c.setupRouteConfigWriter()
	if err != nil {
		return err
	}
	filteredRoutes := make(protio.MessageSlice, 0, len(routes))
	for _, route := range routes {
		if filter.Verify(route) {
			filteredRoutes = append(filteredRoutes, route)
		}
	}
	out, err := json.MarshalIndent(filteredRoutes, "", "    ")
	if err != nil {
		return err
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) setupRouteConfigWriter() (*tabwriter.Writer, []*route.RouteConfiguration, error) {
	routes, err := c.retrieveSortedRouteSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)
	return w, routes, nil
}

func (c *ConfigWriter) retrieveSortedRouteSlice() ([]*route.RouteConfiguration, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	routeDump, err := c.configDump.GetRouteConfigDump()
	if err != nil {
		return nil, err
	}
	routes := make([]*route.RouteConfiguration, 0)
	for _, r := range routeDump.DynamicRouteConfigs {
		if r.RouteConfig != nil {
			routeTyped := &route.RouteConfiguration{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			r.RouteConfig.TypeUrl = v3.RouteType
			err = r.RouteConfig.UnmarshalTo(routeTyped)
			if err != nil {
				return nil, err
			}
			routes = append(routes, routeTyped)
		}
	}
	for _, r := range routeDump.StaticRouteConfigs {
		if r.RouteConfig != nil {
			routeTyped := &route.RouteConfiguration{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			r.RouteConfig.TypeUrl = v3.RouteType
			err = r.RouteConfig.UnmarshalTo(routeTyped)
			if err != nil {
				return nil, err
			}
			routes = append(routes, routeTyped)
		}
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

func isPassthrough(action any) bool {
	a, ok := action.(*route.Route_Route)
	if !ok {
		return false
	}
	cl, ok := a.Route.ClusterSpecifier.(*route.RouteAction_Cluster)
	if !ok {
		return false
	}
	return cl.Cluster == "PassthroughCluster"
}
