// Copyright 2017 Istio Authors
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

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/manager/model"
)

// Config generation main functions.
// The general flow of the generation process consists of the following steps:
// - routes are created for each destination, with referenced clusters stored as a special field
// - routes are grouped organized into listeners for inbound and outbound traffic
// - the outbound and inbound listeners are merged with preference given to the inbound traffic
// - clusters are aggregated and normalized.

// Requirements for the additions to the generation routines:
// - extra policies and filters should be added as additional passes over abstract config structures
// - lists in the config must be de-duplicated and ordered in a canonical way

// TODO: missing features in the config generation:
// - TCP routing
// - HTTPS protocol for inbound and outbound configuration using TCP routing or SNI
// - HTTP pod port collision creates duplicate virtual host entries
// - (bug) two service ports with the same target port create two virtual hosts with same domains
//   (not allowed by envoy). FIXME - need to detect and eliminate such ports in validation

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

// Generate Envoy sidecar proxy configuration
func Generate(instances []*model.ServiceInstance, services []*model.Service,
	config *model.IstioRegistry, mesh *MeshConfig) *Config {
	listeners, clusters := build(instances, services, config, mesh)

	// inject mixer filter
	if mesh.MixerAddress != "" {
		insertMixerFilter(listeners, mesh.MixerAddress)
	}

	// set bind to port values to values for port redirection
	for _, listener := range listeners {
		listener.BindToPort = false
	}

	// add an extra listener that binds to a port
	listeners = append(listeners, &Listener{
		Port:           mesh.ProxyPort,
		BindToPort:     true,
		UseOriginalDst: true,
		Filters:        make([]*NetworkFilter, 0),
	})

	// add SDS cluster
	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Port:          mesh.AdminPort,
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: SDS{
				Cluster:        buildSDSCluster(mesh),
				RefreshDelayMs: 1000,
			},
		},
	}
}

// build combines the outbound and inbound routes prioritizing the latter
func build(instances []*model.ServiceInstance, services []*model.Service,
	config *model.IstioRegistry, mesh *MeshConfig) ([]*Listener, Clusters) {
	httpOutbound, tcpOutbound := buildOutboundFilters(instances, services, config, mesh)
	httpInbound, tcpInbound := buildInboundFilters(instances)

	// merge the two sets of HTTP route configs
	httpRouteConfigs := make(HTTPRouteConfigs)
	for port, httpRouteConfig := range httpInbound {
		httpRouteConfigs[port] = httpRouteConfig
	}
	for port, outgoing := range httpOutbound {
		if incoming, ok := httpRouteConfigs[port]; ok {
			// If the traffic is sent to a service that has instances co-located with the proxy,
			// we choose the local service instance since we cannot distinguish between inbound and outbound packets.
			// Note that this may not be a problem if the service port and its endpoint port are distinct.
			httpRouteConfigs[port] = incoming.merge(outgoing)
		} else {
			httpRouteConfigs[port] = outgoing
		}
	}

	// merge the two sets of TCP route configs
	tcpRouteConfigs := make(TCPRouteConfigs)
	for port, tcpRouteConfig := range tcpInbound {
		tcpRouteConfigs[port] = tcpRouteConfig
	}
	for port, outgoing := range tcpOutbound {
		if incoming, ok := tcpRouteConfigs[port]; ok {
			// If the traffic is sent to a service that has instances co-located with the proxy,
			// we choose the local service instance since we cannot distinguish between inbound and outbound packets.
			// Note that this may not be a problem if the service port and its endpoint port are distinct.
			tcpRouteConfigs[port] = incoming.merge(outgoing)
		} else {
			tcpRouteConfigs[port] = outgoing
		}
	}

	// canonicalize listeners and collect clusters
	clusters := make(Clusters, 0)
	listeners := make([]*Listener, 0)

	for port, routeConfig := range httpRouteConfigs {
		sort.Sort(HostsByName(routeConfig.VirtualHosts))
		clusters = append(clusters, routeConfig.clusters()...)

		filters := buildFaultFilters(config, routeConfig)

		filters = append(filters, HTTPFilter{
			Type:   "decoder",
			Name:   "router",
			Config: FilterRouterConfig{},
		})

		listener := &Listener{
			Port: port,
			Filters: []*NetworkFilter{{
				Type: "read",
				Name: HTTPConnectionManager,
				Config: &HTTPFilterConfig{
					CodecType:  "auto",
					StatPrefix: "http",
					AccessLog: []AccessLog{{
						Path: DefaultAccessLog,
					}},
					RouteConfig: routeConfig,
					Filters:     filters,
				},
			}},
		}

		// TODO(github.com/istio/manager/issues/237)
		//
		// Sharing tcp_proxy and http_connection_manager filters on
		// the same port for different destination services doesn't work
		// with Envoy (yet). When the tcp_proxy filter's route matching
		// fails for the http service the connection is closed without
		// falling back to the http_connection_manager.
		//
		// Temporary workaround is to not share ports between
		// destination services. If the user does share ports, remove the
		// TCP service from the envoy config and print a warning.
		if config, ok := tcpRouteConfigs[port]; ok {
			glog.Warningf("TCP and HTTP services on same port not supported")
			glog.Warningf("Omitting tcp service %v on port %v", config, port)
			delete(tcpRouteConfigs, port)
		}
		listeners = append(listeners, listener)
	}

	for port, tcpConfig := range tcpRouteConfigs {
		clusters = append(clusters, tcpConfig.clusters()...)
		listener := &Listener{
			Port: port,
			Filters: []*NetworkFilter{{
				Type: "read",
				Name: TCPProxyFilter,
				Config: TCPProxyFilterConfig{
					StatPrefix:  "tcp",
					RouteConfig: tcpConfig,
				},
			}},
		}
		listeners = append(listeners, listener)
	}

	sort.Sort(ListenersByPort(listeners))

	clusters = clusters.Normalize()
	for _, cluster := range clusters {
		insertDestinationPolicy(config, cluster)
	}

	return listeners, clusters
}

// buildOutboundFilters creates route configs indexed by ports for the traffic outbound
// from the proxy instance
func buildOutboundFilters(instances []*model.ServiceInstance, services []*model.Service,
	config *model.IstioRegistry, mesh *MeshConfig) (HTTPRouteConfigs, TCPRouteConfigs) {
	// used for shortcut domain names for outbound hostnames
	suffix := sharedInstanceHost(instances)
	httpConfigs := make(HTTPRouteConfigs)
	tcpConfigs := make(TCPRouteConfigs)

	// get all the route rules applicable to the instances
	rules := config.RouteRulesBySource("", instances)

	// outbound connections/requests are redirected to service ports; we create a
	// map for each service port to define filters
	for _, service := range services {
		for _, port := range service.Ports {
			switch port.Protocol {
			case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
				routes := make([]*HTTPRoute, 0)

				// collect route rules
				for _, rule := range rules {
					if rule.Destination == service.Hostname {
						routes = append(routes, buildHTTPRoute(rule, port))
					}
				}

				// default route for the destination
				cluster := buildOutboundCluster(service.Hostname, port, nil)
				routes = append(routes, buildDefaultRoute(cluster))

				host := buildVirtualHost(service, port, suffix, routes)
				http := httpConfigs.EnsurePort(port.Port)
				http.VirtualHosts = append(http.VirtualHosts, host)
			case model.ProtocolTCP:
				cluster := buildOutboundCluster(service.Hostname, port, nil)
				route := buildTCPRoute(cluster, []string{service.Address}, port.Port)
				config := tcpConfigs.EnsurePort(port.Port)
				config.Routes = append(config.Routes, route)
			default:
				glog.Warningf("Unsupported outbound protocol %v for port %d", port.Protocol, port.Port)
			}
		}
	}
	return httpConfigs, tcpConfigs
}

// buildInboundFilters creates route configs indexed by ports for the traffic inbound
// to co-located service instances
func buildInboundFilters(instances []*model.ServiceInstance) (HTTPRouteConfigs, TCPRouteConfigs) {
	// used for shortcut domain names for hostnames
	suffix := sharedInstanceHost(instances)
	httpConfigs := make(HTTPRouteConfigs)
	tcpConfigs := make(TCPRouteConfigs)

	// inbound connections/requests are redirected to the endpoint port but appear to be sent
	// to the service port
	for _, instance := range instances {
		service := instance.Service
		endpoint := instance.Endpoint
		servicePort := endpoint.ServicePort
		protocol := servicePort.Protocol
		switch protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
			cluster := buildInboundCluster(service.Hostname, endpoint.Port, protocol)
			route := buildDefaultRoute(cluster)
			host := buildVirtualHost(service, servicePort, suffix, []*HTTPRoute{route})

			// insert explicit instance ip:port as a hostname field
			host.Domains = append(host.Domains, fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port))
			if endpoint.Port == 80 {
				host.Domains = append(host.Domains, endpoint.Address)
			}

			http := httpConfigs.EnsurePort(endpoint.Port)
			http.VirtualHosts = append(http.VirtualHosts, host)
		case model.ProtocolTCP:
			cluster := buildInboundCluster(service.Hostname, endpoint.Port, protocol)

			// Local service instances can be accessed through one of three
			// addresses: localhost, endpoint IP, and service
			// VIP. Localhost bypasses the proxy and doesn't need any TCP
			// route config. Endpoint IP and Service VIP routes are
			// handled below.
			//
			// Also, omit the destination port here since TCP routes are
			// already declared in the scope of a particular listener
			// port.

			// Traffic sent to our service VIP is redirected by remote
			// services' kubeproxy to our specific endpoint IP.
			config := tcpConfigs.EnsurePort(endpoint.Port)
			config.Routes = append(config.Routes,
				buildTCPRoute(cluster, []string{endpoint.Address}, -1),
			)

			// Traffic sent to our service VIP by a container
			// co-located in our same pod will be intercepted by envoy
			// proxy before it is redirected to the endpoint IP.
			config = tcpConfigs.EnsurePort(servicePort.Port)
			config.Routes = append(config.Routes,
				buildTCPRoute(cluster, []string{service.Address}, -1),
			)
		default:
			glog.Warningf("Unsupported inbound protocol %v for port %d", protocol, servicePort)
		}
	}

	return httpConfigs, tcpConfigs
}
