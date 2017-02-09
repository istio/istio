// Copyright 2017 Google Inc.
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

package envoy

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/manager/model"
	"istio.io/manager/model/proxy/alphav1/config"
)

// WriteFile saves config to a file
func (conf *Config) WriteFile(fname string) error {
	file, err := os.Create(fname)
	if err != nil {
		return err
	}

	if err := conf.Write(file); err != nil {
		err = multierror.Append(err, file.Close())
		return err
	}

	return file.Close()
}

func (conf *Config) Write(w io.Writer) error {
	out, err := json.MarshalIndent(&conf, "", "  ")
	if err != nil {
		return err
	}

	_, err = w.Write(out)
	return err
}

const (
	// OutboundClusterPrefix is the prefix for service clusters external to the proxy instance
	OutboundClusterPrefix = "outbound:"

	// InboundClusterPrefix is the prefix for service clusters co-hosted on the proxy instance
	InboundClusterPrefix = "inbound:"
)

// TODO: these values used in the Envoy configuration will be configurable
const (
	DefaultTimeoutMs = 1000
	DefaultLbType    = LbTypeRoundRobin
	DefaultAccessLog = "/dev/stdout"
)

// Generate Envoy configuration for service instances co-located with Envoy and all services in the mesh
func Generate(instances []*model.ServiceInstance, services []*model.Service, rules []*config.RouteRule,
	upstreams []*config.UpstreamCluster, mesh *MeshConfig) (*Config, error) {

	// Generate all upstream blocks (called clusters) in Envoy terminology
	// service name and hostname are used interchangeably
	hostToUpstreamsMap, upstreamNameToInternalNameMap := enumerateServiceVersions(services, rules)
	// flatten the map of host->[clusters] into array of clusters
	serviceUpstreams := make([]*model.Service, 0)
	for _, val := range hostToUpstreamsMap {
		serviceUpstreams = append(serviceUpstreams, val...)
	}
	// this is what goes into Envoy
	proxyUpstreams := buildClusters(serviceUpstreams)

	// add the mixer cluster if configured
	if len(mesh.MixerAddress) > 0 {
		proxyUpstreams = append(proxyUpstreams, Cluster{
			Name:             "mixer",
			Type:             "strict_dns",
			ConnectTimeoutMs: DefaultTimeoutMs,
			LbType:           DefaultLbType,
			Hosts: []Host{
				{
					URL: "tcp://" + mesh.MixerAddress,
				},
			},
		})
	}

	// Rules indexed by hostname
	// There can be more than one rule per destination
	// TODO: rules sorting - explicit priorities or some other means
	rulesMap := make(map[string][]*config.RouteRule, 0)
	for _, r := range rules {
		_, prs := rulesMap[r.Destination]
		if !prs {
			rulesMap[r.Destination] = make([]*config.RouteRule, 0)
		}
		rulesMap[r.Destination] = append(rulesMap[r.Destination], r)
	}

	listeners, localClusters := buildListeners(instances, services, rulesMap, upstreamNameToInternalNameMap, mesh)
	proxyUpstreams = append(proxyUpstreams, localClusters...)

	sort.Sort(ListenersByPort(listeners))
	sort.Sort(ClustersByName(proxyUpstreams))

	// TODO: add catch-all filters to prevent Envoy from crashing
	listeners = append(listeners, Listener{
		Port:           mesh.ProxyPort,
		BindToPort:     true,
		UseOriginalDst: true,
		Filters:        make([]NetworkFilter, 0),
	})

	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Port:          mesh.AdminPort,
		},
		ClusterManager: ClusterManager{
			Clusters: proxyUpstreams,
			SDS: SDS{
				Cluster: Cluster{
					Name:             "sds",
					Type:             "strict_dns",
					ConnectTimeoutMs: DefaultTimeoutMs,
					LbType:           DefaultLbType,
					Hosts: []Host{
						{
							URL: "tcp://" + mesh.DiscoveryAddress,
						},
					},
				},
				RefreshDelayMs: 1000,
			},
		},
	}, nil
}

// buildListeners uses iptables port redirect to route traffic either into the
// pod or outside the pod to service clusters based on the traffic metadata.
func buildListeners(instances []*model.ServiceInstance,
	services []*model.Service, rulesMap map[string][]*config.RouteRule,
	upstreamNameToInternalNameMap map[string]string,
	mesh *MeshConfig) ([]Listener, []Cluster) {

	localClusters := make([]Cluster, 0)
	listeners := make([]Listener, 0)

	hostnames := make([][]string, 0)
	for _, instance := range instances {
		hostnames = append(hostnames, strings.Split(instance.Service.Hostname, "."))
	}
	suffix := sharedHost(hostnames...)

	// group by port values to service with the declared port
	type listener struct {
		instances map[model.Protocol][]*model.Service
		services  map[model.Protocol][]*model.Service
	}

	ports := make(map[int]*listener, 0)

	// helper function to work with multi-maps
	ensure := func(port int) {
		if _, ok := ports[port]; !ok {
			ports[port] = &listener{
				instances: make(map[model.Protocol][]*model.Service),
				services:  make(map[model.Protocol][]*model.Service),
			}
		}
	}

	// group all service instances by (target-)port values
	// (assumption: traffic gets redirected from service port to instance port)
	for _, instance := range instances {
		port := instance.Endpoint.Port
		ensure(port)
		ports[port].instances[instance.Endpoint.ServicePort.Protocol] = append(
			ports[port].instances[instance.Endpoint.ServicePort.Protocol], &model.Service{
				Hostname: instance.Service.Hostname,
				Address:  instance.Service.Address,
				Ports:    []*model.Port{instance.Endpoint.ServicePort},
			})
	}

	// group all services by (service-)port values for outgoing traffic
	for _, svc := range services {
		for _, port := range svc.Ports {
			ensure(port.Port)
			ports[port.Port].services[port.Protocol] = append(
				ports[port.Port].services[port.Protocol], &model.Service{
					Hostname: svc.Hostname,
					Address:  svc.Address,
					Ports:    []*model.Port{port},
				})
		}
	}

	// generate listener for each port
	for port, lst := range ports {
		listener := Listener{
			Port:       port,
			BindToPort: false,
		}

		// append localhost redirect cluster
		localhost := fmt.Sprintf("%s%d", InboundClusterPrefix, port)
		if len(lst.instances) > 0 {
			localClusters = append(localClusters, Cluster{
				Name:             localhost,
				Type:             "static",
				ConnectTimeoutMs: DefaultTimeoutMs,
				LbType:           DefaultLbType,
				Hosts:            []Host{{URL: fmt.Sprintf("tcp://%s:%d", "127.0.0.1", port)}},
			})
		}

		// Envoy uses L4 and L7 filters for TCP and HTTP traffic.
		// In practice, no port has two protocols used by both filters, but we
		// should be careful with not stepping on our feet.

		// The order of the filter insertion is important.
		if len(lst.instances[model.ProtocolTCP]) > 0 {
			listener.Filters = append(listener.Filters, NetworkFilter{
				Type: "read",
				Name: "tcp_proxy",
				Config: NetworkFilterConfig{
					Cluster:    localhost,
					StatPrefix: "inbound_tcp",
				},
			})
		}

		// TODO: TCP routing for outbound based on dst IP
		// TODO: HTTPS protocol for inbound and outbound configuration using TCP routing or SNI
		// TODO: if two service ports have same port or same target port values but
		// different names, we will get duplicate host routes.  Envoy prohibits
		// duplicate entries with identical domains.

		// For HTTP, the routing decision is based on the virtual host.
		hosts := make(map[string]VirtualHost, 0)
		faultsByPort := make([]Filter, 0)
		for _, proto := range []model.Protocol{model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC} {
			for _, svc := range lst.services[proto] {
				routes, faultsByHost := buildHTTPRoutesAndFaults(svc, rulesMap,
					upstreamNameToInternalNameMap)
				host := buildVirtualHost(svc, suffix, routes)
				hosts[svc.String()] = host
				faultsByPort = append(faultsByPort, faultsByHost...)
			}

			// If the traffic is sent to a service that has instances co-located with the proxy,
			// we choose the local service instance since we cannot distinguish between inbound and outbound packets.
			// Note that this may not be a problem if the service port and its endpoint port are distinct.
			for _, svc := range lst.instances[proto] {
				host := buildVirtualHost(svc, suffix, []Route{{Prefix: "/", Cluster: localhost}})
				hosts[svc.String()] = host
			}
		}

		if len(hosts) > 0 {
			// sort hosts by key (should be non-overlapping domains)
			vhosts := make([]VirtualHost, 0)
			for _, host := range hosts {
				vhosts = append(vhosts, host)
			}
			sort.Sort(HostsByName(vhosts))

			listener.Filters = append(listener.Filters, NetworkFilter{
				Type: "read",
				Name: "http_connection_manager",
				Config: NetworkFilterConfig{
					CodecType:   "auto",
					StatPrefix:  "http",
					AccessLog:   []AccessLog{{Path: DefaultAccessLog}},
					RouteConfig: RouteConfig{VirtualHosts: vhosts},
					// TODO decide if mesh filter should come before faults or vice versa
					Filters: append(buildFilters(mesh), faultsByPort...),
				},
			})
		}

		if len(listener.Filters) > 0 {
			listeners = append(listeners, listener)
		}
	}

	return listeners, localClusters
}

// sharedHost computes the shared host name suffix for instances.
func sharedHost(parts ...[]string) []string {
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return parts[0]
	default:
		// longest common suffix
		out := make([]string, 0)
		for i := 1; i <= len(parts[0]); i++ {
			part := ""
			all := true
			for j, host := range parts {
				hostpart := host[len(host)-i]
				if j == 0 {
					part = hostpart
				} else if part != hostpart {
					all = false
					break
				}
			}
			if all {
				out = append(out, part)
			} else {
				break
			}
		}

		// reverse
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out
	}
}

// buildVirtualHost constructs an entry for VirtualHost for a given service.
// Service contains name, namespace and a single port declaration.
func buildVirtualHost(svc *model.Service, suffix []string, routes []Route) VirtualHost {
	hosts := make([]string, 0)
	domains := make([]string, 0)
	parts := strings.Split(svc.Hostname, ".")
	shared := sharedHost(suffix, parts)

	// if shared is "svc.cluster.local", then we can add "name.namespace", "name.namespace.svc", etc
	host := strings.Join(parts[0:len(parts)-len(shared)], ".")
	if len(host) > 0 {
		hosts = append(hosts, host)
	}
	for _, part := range shared {
		if len(host) > 0 {
			host = host + "."
		}
		host = host + part
		hosts = append(hosts, host)
	}

	// add cluster IP host name
	if len(svc.Address) > 0 {
		hosts = append(hosts, svc.Address)
	}

	// add ports
	if len(svc.Ports) > 0 {
		port := svc.Ports[0].Port
		for _, host := range hosts {
			domains = append(domains, fmt.Sprintf("%s:%d", host, port))

			// default port 80 does not need to be specified
			if port == 80 {
				domains = append(domains, host)
			}
		}
	}

	return VirtualHost{
		Name:    svc.String(),
		Domains: domains,
		Routes:  routes,
	}
}

// buildHttpRoutesAndFaults adds one or more route entries in a virtual host based on the routing rules
// and generates a list of fault injection filters to be injected into the filter array in Envoy's config.
func buildHTTPRoutesAndFaults(svc *model.Service, rulesMap map[string][]*config.RouteRule,
	upstreamNameToInternalNameMap map[string]string) ([]Route, []Filter) {

	routes := make([]Route, 0)
	faultsByDestination := make([]Filter, 0)
	ruleByDestination, prs := rulesMap[svc.Hostname]
	if prs {
		for _, rule := range ruleByDestination {
			httpRule := rule.GetHttp()
			route := Route{}
			if httpRule != nil {
				match := httpRule.GetMatch()

				route.Headers = buildHeaders(match.GetHeaders())
				route.Path, route.Prefix = buildPathAndPrefix(match)
				if httpRule.WeightedClusters != nil {
					route.WeightedClusters = buildWeightedClusters(httpRule.WeightedClusters,
						upstreamNameToInternalNameMap)
				} else {
					route.Cluster = OutboundClusterPrefix + svc.String()
				}

				if httpRule.Fault != nil {
					faultsByRoute := buildFaultFilters(&route, httpRule.Fault)
					faultsByDestination = append(faultsByDestination, faultsByRoute...)
				}
			}
			routes = append(routes, route)
		}
	} else {
		// services without routing rules
		routes = append(routes, Route{Prefix: "/", Cluster: OutboundClusterPrefix + svc.String()})
	}

	return routes, faultsByDestination
}

// buildFaultFilters builds a list of fault filters for the http route. If the route points to a single
// cluster, an array of size 1 is returned. If the route points to a weighted cluster, an array of fault
// filters (one per cluster entry in the weighted cluster) is returned.
func buildFaultFilters(route *Route, faultRule *config.HttpFaultInjection) []Filter {
	faults := make([]Filter, 0)
	if route.WeightedClusters != nil {
		for _, cluster := range route.WeightedClusters.Clusters {
			faults = append(faults, buildFaultFilter(cluster.Name, faultRule))
		}
	} else {
		faults = append(faults, buildFaultFilter(route.Cluster, faultRule))
	}
	return faults
}

// buildFaultFilter builds a single fault filter
func buildFaultFilter(internalUpstreamName string, faultRule *config.HttpFaultInjection) Filter {
	return Filter{
		Type: "decoder",
		Name: "fault",
		Config: FilterFaultConfig{
			UpstreamCluster: internalUpstreamName,
			Headers:         buildHeaders(faultRule.Headers),
			Abort:           buildAbortConfig(faultRule.Abort),
			Delay:           buildDelayConfig(faultRule.Delay),
		},
	}
}

// buildAbortConfig builds the envoy config related to abort spec in a fault filter
func buildAbortConfig(abortRule *config.HttpFaultInjection_Abort) *AbortFilter {
	if abortRule == nil || abortRule.GetHttpStatus() == 0 {
		return nil
	}

	return &AbortFilter{
		Percent:    int(abortRule.Percent),
		HTTPStatus: int(abortRule.GetHttpStatus()),
	}
}

// buildDelayConfig builds the envoy config related to delay spec in a fault filter
func buildDelayConfig(delayRule *config.HttpFaultInjection_Delay) *DelayFilter {
	if delayRule == nil || delayRule.GetFixedDelay() == nil {
		return nil
	}

	return &DelayFilter{
		Type:     "fixed",
		Percent:  int(delayRule.GetFixedDelay().Percent),
		Duration: int(delayRule.GetFixedDelay().FixedDelaySeconds * 1000),
	}
}

func buildHeaders(headerMatches map[string]*config.StringMatch) []Header {
	headers := make([]Header, 0, len(headerMatches))
	for name, stringMatch := range headerMatches {
		header := buildHeader(name, stringMatch)
		headers = append(headers, header)
	}
	sort.Sort(HeadersByNameValue(headers))
	return headers
}

func buildHeader(name string, match *config.StringMatch) Header {
	var value string
	var regex bool

	if match.GetExact() != "" {
		value = match.GetExact()
	} else if match.GetPrefix() != "" {
		value = fmt.Sprintf("^%v.*", match.GetPrefix())
		regex = true
	} else if match.GetRegex() != "" {
		value = match.GetRegex()
		regex = true
	}

	return Header{
		Name:  name,
		Value: value,
		Regex: regex,
	}
}

// buildPathAndPrefix returns the path and prefix fields in HTTP route based on routing rule
func buildPathAndPrefix(match *config.HttpMatchCondition) (string, string) {
	path := ""
	prefix := ""
	if match != nil && match.GetUri() != nil {
		// TODO Error check. Either path/prefix, but not both
		// TODO Error check. No regex
		prefix = match.Uri.GetPrefix()
		path = match.Uri.GetExact()
	}
	if prefix == "" && path == "" {
		prefix = "/"
	}
	return path, prefix
}

// buildWeightedClusters returns the weighted_clusters block for envoy http route entry
// The returned block has list of clusters and their weights
func buildWeightedClusters(wcRules []*config.WeightedCluster,
	upstreamNameToInternalNameMap map[string]string) (wc *WeightedCluster) {

	// The user provides the destination service and tags in the routing rule.
	// We convert this into the appropriate upstream cluster name
	weightedClusters := &WeightedCluster{}
	weightedClusters.Clusters = make([]WeightedClusterEntry, 0)
	for _, wcRuleItem := range wcRules {
		wcEntry := WeightedClusterEntry{
			Name: OutboundClusterPrefix +
				upstreamNameToInternalNameMap[toInternalUpstreamName(wcRuleItem.DstCluster)],
			Weight: int(wcRuleItem.Weight),
		}
		weightedClusters.Clusters = append(weightedClusters.Clusters, wcEntry)
	}

	return weightedClusters
}

// ruleTagsToSvcTag converts array of string tags from routing rule into internal Tag representation
func ruleTagsToSvcTag(tags []string) []model.Tag {
	svcTags := make([]model.Tag, 0)
	for _, t := range tags {
		svcTags = append(svcTags, model.ParseTagString(t))
	}
	return svcTags
}

// enumerateServiceVersions splits up the default service objects created by the underlying platform
// into finer-grained services based on routing rules specified by the user. Routing rules delineate
// different versions of a service through a set of tags.
// returns a map of hostname to array of upstreams, and a map of users upstreamName to internalName
func enumerateServiceVersions(services []*model.Service,
	rules []*config.RouteRule) (map[string][]*model.Service, map[string]string) {

	servicesMap := make(map[string]*model.Service, len(services))
	for _, svc := range services {
		servicesMap[svc.Hostname] = svc
	}

	serviceVersionsMap := make(map[string][]*model.Service, 0)
	upstreamNameToInternalNameMap := make(map[string]string, 0) // Hostname + tags -> svc.String()
	for _, rule := range rules {
		var weightedClusters []*config.WeightedCluster
		if httpRule := rule.GetHttp(); httpRule != nil {
			weightedClusters = httpRule.GetWeightedClusters()
		} else if l4Rule := rule.GetLayer4(); l4Rule != nil {
			weightedClusters = l4Rule.GetWeightedClusters()
		}

		for _, w := range weightedClusters {
			name := w.DstCluster.Name
			tags := ruleTagsToSvcTag(w.DstCluster.Tags)
			svcVersion := model.Service{
				Hostname: name,
				Tags:     tags,
				Ports:    servicesMap[name].Ports,
				Address:  servicesMap[name].Address,
			}
			serviceVersionsMap[name] = append(serviceVersionsMap[w.DstCluster.Name], &svcVersion)
			upstreamNameToInternalNameMap[toInternalUpstreamName(w.DstCluster)] = svcVersion.String()
		}
	}

	// Now merge the maps, giving preference to serviceVersion over service
	for svc := range servicesMap {
		if _, exists := serviceVersionsMap[svc]; !exists {
			serviceVersionsMap[svc] = append(serviceVersionsMap[svc], servicesMap[svc])
		}
	}

	return serviceVersionsMap, upstreamNameToInternalNameMap
}

// buildClusters creates a cluster for every (service, port)
func buildClusters(services []*model.Service) []Cluster {
	clusters := make([]Cluster, 0)
	for _, svc := range services {
		for _, port := range svc.Ports {
			clusterSvc := model.Service{
				Hostname: svc.Hostname,
				Ports:    []*model.Port{port},
				Tags:     svc.Tags,
			}
			cluster := Cluster{
				Name:             OutboundClusterPrefix + clusterSvc.String(),
				ServiceName:      clusterSvc.String(),
				Type:             "sds",
				LbType:           DefaultLbType,
				ConnectTimeoutMs: DefaultTimeoutMs,
			}
			if port.Protocol == model.ProtocolGRPC ||
				port.Protocol == model.ProtocolHTTP2 {
				cluster.Features = "http2"
			}
			clusters = append(clusters, cluster)
		}
	}
	sort.Sort(ClustersByName(clusters))
	return clusters
}

// buildFilter adds a filter for the the mixer and fault injection if specified by routing rule
// TODO: fault injection filter needs to go here
func buildFilters(mesh *MeshConfig) []Filter {
	filters := make([]Filter, 0)

	if len(mesh.MixerAddress) > 0 {
		filters = append(filters, Filter{
			Type: "both",
			Name: "esp",
			Config: FilterEndpointsConfig{
				ServiceConfig: "/etc/generic_service_config.json",
				ServerConfig:  "/etc/server_config.pb.txt",
			},
		})
	}

	filters = append(filters, Filter{
		Type:   "decoder",
		Name:   "router",
		Config: FilterRouterConfig{},
	})

	return filters
}

// toInternalUpstreamName converts the clusterIdentifier in a route rule to a string representation
// used by internal model.Service
func toInternalUpstreamName(identifier *config.ClusterIdentifier) string {
	s := model.Service{
		Hostname: identifier.Name,
		Tags:     ruleTagsToSvcTag(identifier.Tags),
	}

	return s.String()
}
