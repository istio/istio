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

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
	"istio.io/manager/proxy"
)

// Config generation main functions.
// The general flow of the generation process consists of the following steps:
// - routes are created for each destination, with referenced clusters stored as a special field
// - routes are organized into listeners for inbound and outbound traffic
// - clusters are aggregated and normalized across routes
// - extra policies and filters are added by additional passes over abstract config structures
// - configuration elements are de-duplicated and ordered in a canonical way

// WriteFile saves config to a file
func (conf *Config) WriteFile(fname string) error {
	if glog.V(2) {
		glog.Infof("writing configuration to %s", fname)
		if err := conf.Write(os.Stderr); err != nil {
			glog.Error(err)
		}
	}

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
func Generate(context *proxy.Context) *Config {
	mesh := context.MeshConfig
	listeners, clusters := buildListeners(context)

	// set bind to port values for port redirection
	for _, listener := range listeners {
		listener.BindToPort = false
	}

	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	listeners = append(listeners, &Listener{
		Address:        fmt.Sprintf("tcp://%s:%d", WildcardAddress, mesh.ProxyListenPort),
		BindToPort:     true,
		UseOriginalDst: true,
		Filters:        make([]*NetworkFilter, 0),
	})

	return buildConfig(listeners, clusters, mesh)
}

// buildConfig creates a proxy config with discovery services and admin port
func buildConfig(listeners Listeners, clusters Clusters, mesh *proxyconfig.ProxyMeshConfig) *Config {
	if cluster := buildZipkinCluster(mesh); cluster != nil {
		clusters = append(clusters, cluster)
	}
	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Address:       fmt.Sprintf("tcp://%s:%d", WildcardAddress, mesh.ProxyAdminPort),
		},
		ClusterManager: ClusterManager{
			Clusters: append(clusters,
				buildDiscoveryCluster(mesh.DiscoveryAddress, RDSName, mesh.ConnectTimeout)),
			SDS: &DiscoveryCluster{
				Cluster:        buildDiscoveryCluster(mesh.DiscoveryAddress, SDSName, mesh.ConnectTimeout),
				RefreshDelayMs: protoDurationToMS(mesh.DiscoveryRefreshDelay),
			},
			CDS: &DiscoveryCluster{
				Cluster:        buildDiscoveryCluster(mesh.DiscoveryAddress, CDSName, mesh.ConnectTimeout),
				RefreshDelayMs: protoDurationToMS(mesh.DiscoveryRefreshDelay),
			},
		},
		Tracing: buildZipkinTracing(mesh),
	}
}

// buildListeners produces a list of listeners and referenced clusters
// (due to lack of RDS support for TCP proxy filter, all referenced clusters in TCP routes
// must be present)
func buildListeners(context *proxy.Context) (Listeners, Clusters) {
	// query the services model
	instances := context.Discovery.HostInstances(map[string]bool{context.IPAddress: true})
	services := context.Discovery.Services()

	inbound, inClusters := buildInboundListeners(instances, context.MeshConfig)
	outbound, outClusters := buildOutboundListeners(instances, services, context)

	listeners := append(inbound, outbound...)
	clusters := append(inClusters, outClusters...)

	// create passthrough listeners if they are missing
	for _, port := range context.PassthroughPorts {
		addr := fmt.Sprintf("tcp://%s:%d", context.IPAddress, port)
		if listeners.GetByAddress(addr) == nil {
			cluster := buildInboundCluster(port, model.ProtocolTCP, context.MeshConfig.ConnectTimeout)
			listeners = append(listeners, buildTCPListener(&TCPRouteConfig{
				Routes: []*TCPRoute{buildTCPRoute(cluster, []string{context.IPAddress})},
			}, context.IPAddress, port))
			clusters = append(clusters, cluster)
		}
	}

	listeners = listeners.normalize()
	clusters = clusters.normalize()

	// inject static Mixer filter with proxy identities for all HTTP filters
	insertMixerFilter(listeners, instances, context)

	return listeners, clusters
}

// buildHTTPListener constructs a listener for the network interface address and port
// Use "0.0.0.0" IP address to listen on all interfaces
// RDS parameter controls whether to use RDS for the route updates.
func buildHTTPListener(mesh *proxyconfig.ProxyMeshConfig, routeConfig *HTTPRouteConfig,
	ip string, port int, rds bool) *Listener {
	filters := buildFaultFilters(routeConfig)

	filters = append(filters, HTTPFilter{
		Type:   decoder,
		Name:   router,
		Config: FilterRouterConfig{},
	})

	config := &HTTPFilterConfig{
		CodecType:  auto,
		StatPrefix: "http",
		AccessLog: []AccessLog{{
			Path: DefaultAccessLog,
		}},
		Filters: filters,
	}

	if mesh.ZipkinAddress != "" {
		config.GenerateRequestID = true
		config.Tracing = &HTTPFilterTraceConfig{
			OperationName: IngressTraceOperation,
		}
	}

	if rds {
		config.RDS = &RDS{
			Cluster:         RDSName,
			RouteConfigName: fmt.Sprintf("%d", port),
			RefreshDelayMs:  protoDurationToMS(mesh.DiscoveryRefreshDelay),
		}
	} else {
		config.RouteConfig = routeConfig
	}

	return &Listener{
		BindToPort: true,
		Address:    fmt.Sprintf("tcp://%s:%d", ip, port),
		Filters: []*NetworkFilter{{
			Type:   "read",
			Name:   HTTPConnectionManager,
			Config: config,
		}},
	}
}

func applyInboundAuth(listener *Listener, mesh *proxyconfig.ProxyMeshConfig) *Listener {
	switch mesh.AuthPolicy {
	case proxyconfig.ProxyMeshConfig_NONE:
	case proxyconfig.ProxyMeshConfig_MUTUAL_TLS:
		listener.SSLContext = buildListenerSSLContext(mesh.AuthCertsPath)
	}
	return listener
}

// buildTCPListener constructs a listener for the TCP proxy
func buildTCPListener(tcpConfig *TCPRouteConfig, ip string, port int) *Listener {
	return &Listener{
		Address: fmt.Sprintf("tcp://%s:%d", ip, port),
		Filters: []*NetworkFilter{{
			Type: "read",
			Name: TCPProxyFilter,
			Config: TCPProxyFilterConfig{
				StatPrefix:  "tcp",
				RouteConfig: tcpConfig,
			},
		}},
	}
}

// buildOutboundListeners combines HTTP routes and TCP listeners
func buildOutboundListeners(instances []*model.ServiceInstance, services []*model.Service,
	context *proxy.Context) (Listeners, Clusters) {
	httpOutbound := buildOutboundHTTPRoutes(instances, services, context.Accounts, context.MeshConfig, context.Config)
	listeners, clusters := buildOutboundTCPListeners(context.MeshConfig, services)

	for port, routeConfig := range httpOutbound {
		listeners = append(listeners, buildHTTPListener(context.MeshConfig, routeConfig, WildcardAddress, port, true))
	}
	return listeners, clusters
}

// buildDestinationHTTPRoutes creates HTTP route for a service and a port from rules
func buildDestinationHTTPRoutes(service *model.Service,
	servicePort *model.Port,
	rules []*proxyconfig.RouteRule) []*HTTPRoute {
	protocol := servicePort.Protocol
	switch protocol {
	case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
		routes := make([]*HTTPRoute, 0)

		// collect route rules
		useDefaultRoute := true
		for _, rule := range rules {
			if rule.Destination == service.Hostname {
				httpRoute := buildHTTPRoute(rule, servicePort)
				routes = append(routes, httpRoute)

				// User can provide timeout/retry policies without any match condition,
				// or specific route. User could also provide a single default route, in
				// which case, we should not be generating another default route.
				// For every HTTPRoute we build, the return value also provides a boolean
				// "catchAll" flag indicating if the route that was built was a catch all route.
				// When such a route is encountered, we stop building further routes for the
				// destination and we will not add the default route after the for loop.
				if httpRoute.CatchAll() {
					useDefaultRoute = false
					break
				}
			}
		}

		if useDefaultRoute {
			// default route for the destination is always the lowest priority route
			cluster := buildOutboundCluster(service.Hostname, servicePort, nil)
			routes = append(routes, buildDefaultRoute(cluster))
		}

		return routes

	case model.ProtocolHTTPS:
		// as an exception, external name HTTPS port is sent in plain-text HTTP/1.1
		if service.External() {
			cluster := buildOutboundCluster(service.Hostname, servicePort, nil)
			return []*HTTPRoute{buildDefaultRoute(cluster)}
		}

	case model.ProtocolTCP:
		// handled by buildOutboundTCPListeners

	default:
		glog.Warningf("Unsupported outbound protocol %v for port %#v", protocol, servicePort)
	}

	return nil
}

// buildOutboundHTTPRoutes creates HTTP route configs indexed by ports for the
// traffic outbound from the proxy instance
func buildOutboundHTTPRoutes(
	instances []*model.ServiceInstance,
	services []*model.Service,
	accounts model.ServiceAccounts,
	mesh *proxyconfig.ProxyMeshConfig,
	config *model.IstioRegistry) HTTPRouteConfigs {
	httpConfigs := make(HTTPRouteConfigs)

	// used for shortcut domain names for outbound hostnames
	suffix := sharedInstanceHost(instances)

	// get all the route rules applicable to the instances
	rules := config.RouteRulesBySource("", instances)

	// outbound connections/requests are directed to service ports; we create a
	// map for each service port to define filters
	for _, service := range services {
		for _, servicePort := range service.Ports {
			// skip external services if the egress proxy is undefined
			if service.External() && mesh.EgressProxyAddress == "" {
				continue
			}

			routes := buildDestinationHTTPRoutes(service, servicePort, rules)

			if len(routes) > 0 {
				// must use egress proxy to route external name services
				if service.External() {
					for _, route := range routes {
						route.HostRewrite = service.Hostname
						for _, cluster := range route.clusters {
							cluster.ServiceName = ""
							cluster.Type = ClusterTypeStrictDNS
							cluster.Hosts = []Host{{URL: fmt.Sprintf("tcp://%s", mesh.EgressProxyAddress)}}
						}
					}
				}

				host := buildVirtualHost(service, servicePort, suffix, routes)
				http := httpConfigs.EnsurePort(servicePort.Port)
				http.VirtualHosts = append(http.VirtualHosts, host)
			}
		}
	}

	httpConfigs.normalize()
	return httpConfigs
}

// buildOutboundTCPListeners lists listeners and referenced clusters for TCP
// protocols (including HTTPS)
//
// TODO(github.com/istio/manager/issues/237)
//
// Sharing tcp_proxy and http_connection_manager filters on the same port for
// different destination services doesn't work with Envoy (yet). When the
// tcp_proxy filter's route matching fails for the http service the connection
// is closed without falling back to the http_connection_manager.
//
// Temporary workaround is to add a listener for each service IP that requires
// TCP routing
func buildOutboundTCPListeners(mesh *proxyconfig.ProxyMeshConfig, services []*model.Service) (Listeners, Clusters) {
	tcpListeners := make(Listeners, 0)
	tcpClusters := make(Clusters, 0)
	for _, service := range services {
		if service.External() {
			continue // TODO TCP external services not currently supported
		}
		for _, servicePort := range service.Ports {
			switch servicePort.Protocol {
			case model.ProtocolTCP, model.ProtocolHTTPS:
				// TODO: Enable SSL context for TCP and HTTPS services.
				cluster := buildOutboundCluster(service.Hostname, servicePort, nil)
				route := buildTCPRoute(cluster, []string{service.Address})
				config := &TCPRouteConfig{Routes: []*TCPRoute{route}}
				listener := buildTCPListener(config, service.Address, servicePort.Port)
				tcpClusters = append(tcpClusters, cluster)
				tcpListeners = append(tcpListeners, listener)
			}
		}
	}
	tcpClusters.setTimeout(mesh.ConnectTimeout)
	return tcpListeners, tcpClusters
}

// buildInboundListeners creates listeners for the server-side (inbound)
// configuration for co-located service instances. The function also returns
// all inbound clusters since they are statically declared in the proxy
// configuration and do not utilize CDS.
func buildInboundListeners(instances []*model.ServiceInstance,
	mesh *proxyconfig.ProxyMeshConfig) (Listeners, Clusters) {
	// used for shortcut domain names for hostnames
	suffix := sharedInstanceHost(instances)
	listeners := make(Listeners, 0)
	clusters := make(Clusters, 0)

	// inbound connections/requests are redirected to the endpoint address but appear to be sent
	// to the service address
	// assumes that endpoint addresses/ports are unique in the instance set
	for _, instance := range instances {
		service := instance.Service
		endpoint := instance.Endpoint
		servicePort := endpoint.ServicePort
		protocol := servicePort.Protocol
		cluster := buildInboundCluster(endpoint.Port, protocol, mesh.ConnectTimeout)
		clusters = append(clusters, cluster)

		// Local service instances can be accessed through one of three
		// addresses: localhost, endpoint IP, and service
		// VIP. Localhost bypasses the proxy and doesn't need any TCP
		// route config. Endpoint IP is handled below and Service IP is handled
		// by outbound routes.
		// Traffic sent to our service VIP is redirected by remote
		// services' kubeproxy to our specific endpoint IP.
		switch protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
			route := buildDefaultRoute(cluster)

			// set server-side mixer filter config for inbound routes
			if mesh.MixerAddress != "" {
				route.OpaqueConfig = map[string]string{
					"mixer_control": "on",
					"mixer_forward": "off",
				}
			}

			host := buildVirtualHost(service, servicePort, suffix, []*HTTPRoute{route})

			// insert explicit instance (pod) ip:port as a hostname field
			host.Domains = append(host.Domains, fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port))
			if endpoint.Port == 80 {
				host.Domains = append(host.Domains, endpoint.Address)
			}

			config := &HTTPRouteConfig{VirtualHosts: []*VirtualHost{host}}
			listeners = append(listeners,
				applyInboundAuth(buildHTTPListener(mesh, config, endpoint.Address, endpoint.Port, false), mesh))

		case model.ProtocolTCP, model.ProtocolHTTPS:
			listeners = append(listeners, buildTCPListener(&TCPRouteConfig{
				Routes: []*TCPRoute{buildTCPRoute(cluster, []string{endpoint.Address})},
			}, endpoint.Address, endpoint.Port))

		default:
			glog.Warningf("Unsupported inbound protocol %v for port %#v", protocol, servicePort)
		}
	}

	return listeners, clusters
}
