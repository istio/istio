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
	"strings"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
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

/* TODO: passthrough listeners are disabled since they must be provided through LDS/
// PassthroughPorts is a list of ports on the proxy IP address that must be
// open and allowed through the proxy to the co-located service instances.
// These ports are utilized by the underlying cluster platform for health
// checking, for example.
//
// The passthrough ports should be exposed irrespective of the services
// model. In case there is an overlap, that is the port is utilized by a
// service for a service instance and is also present in this list, the
// service model declaration takes precedence. That means any protocol
// upgrade (such as utilizng TLS for proxy-to-proxy traffic) will be applied
// to the passthrough port.
PassthroughPorts []int
// create passthrough listeners if they are missing
for _, port := range context.PassthroughPorts {
	cluster := buildInboundCluster(port, model.ProtocolTCP, mesh.ConnectTimeout)
	cluster.Name = fmt.Sprintf("passthrough_%d", port)
	// skip naming the listener to preserve it across updates
	listener := buildTCPListener(&TCPRouteConfig{
		Routes: []*TCPRoute{buildTCPRoute(cluster, []string{context.IPAddress})},
	}, "", context.IPAddress, port)
	listener.BindToPort = false
	listeners = append(listeners, listener)
	clusters = append(clusters, cluster)
}
*/

// buildConfig creates a proxy config with discovery services and admin port
func buildConfig(listeners Listeners, clusters Clusters, lds bool, mesh *proxyconfig.ProxyMeshConfig) *Config {
	out := &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Address:       fmt.Sprintf("tcp://%s:%d", WildcardAddress, mesh.ProxyAdminPort),
		},
		ClusterManager: ClusterManager{
			Clusters: append(clusters,
				buildCluster(mesh.DiscoveryAddress, RDSName, mesh.ConnectTimeout)),
			SDS: &DiscoveryCluster{
				Cluster:        buildCluster(mesh.DiscoveryAddress, SDSName, mesh.ConnectTimeout),
				RefreshDelayMs: protoDurationToMS(mesh.DiscoveryRefreshDelay),
			},
			CDS: &DiscoveryCluster{
				Cluster:        buildCluster(mesh.DiscoveryAddress, CDSName, mesh.ConnectTimeout),
				RefreshDelayMs: protoDurationToMS(mesh.DiscoveryRefreshDelay),
			},
		},
		StatsdUDPIPAddress: mesh.StatsdUdpAddress,
	}

	if lds {
		out.LDS = &LDSCluster{
			Cluster:        LDSName,
			RefreshDelayMs: protoDurationToMS(mesh.DiscoveryRefreshDelay),
		}
		out.ClusterManager.Clusters = append(out.ClusterManager.Clusters,
			buildCluster(mesh.DiscoveryAddress, LDSName, mesh.ConnectTimeout))
	}

	if mesh.ZipkinAddress != "" {
		out.ClusterManager.Clusters = append(out.ClusterManager.Clusters,
			buildCluster(mesh.ZipkinAddress, ZipkinCollectorCluster, mesh.ConnectTimeout))
		out.Tracing = buildZipkinTracing(mesh)
	}

	return out
}

// buildListeners produces a list of listeners and referenced clusters for all proxies
func buildListeners(env proxy.Environment, role proxy.Node) Listeners {
	switch role.Type {
	case proxy.Sidecar:
		listeners, _ := buildSidecar(env, role)
		return listeners
	case proxy.Ingress:
		return buildIngressListeners(env.Mesh, role)
	case proxy.Egress:
		return buildEgressListeners(env.Mesh, role)
	}
	return nil
}

func buildClusters(env proxy.Environment, role proxy.Node) (clusters Clusters) {
	switch role.Type {
	case proxy.Sidecar:
		_, clusters = buildSidecar(env, role)
	case proxy.Ingress:
		httpRouteConfigs, _ := buildIngressRoutes(env.Mesh, env.ServiceDiscovery, env.IstioConfigStore)
		clusters = httpRouteConfigs.clusters().normalize()
	case proxy.Egress:
		httpRouteConfigs := buildEgressRoutes(env.Mesh, env.ServiceDiscovery)
		clusters = httpRouteConfigs.clusters().normalize()
	}

	// apply custom policies for outbound clusters
	for _, cluster := range clusters {
		applyClusterPolicy(cluster, env.IstioConfigStore, env.Mesh, env.ServiceAccounts)
	}

	// append Mixer service definition if necessary
	if env.Mesh.MixerAddress != "" {
		clusters = append(clusters, buildMixerCluster(env.Mesh))
	}

	return clusters
}

// buildSidecar produces a list of listeners and referenced clusters for sidecar proxies
// TODO: this implementation is inefficient as it is recomputing all the routes for all proxies
// There is a lot of potential to cache and reuse cluster definitions across proxies and also
// skip computing the actual HTTP routes
func buildSidecar(env proxy.Environment, sidecar proxy.Node) (Listeners, Clusters) {
	instances := env.HostInstances(map[string]bool{sidecar.IPAddress: true})
	services := env.Services()

	inbound, inClusters := buildInboundListeners(env.Mesh, sidecar, instances)
	outbound, outClusters := buildOutboundListeners(env.Mesh, sidecar, instances, services, env)

	listeners := append(inbound, outbound...)
	clusters := append(inClusters, outClusters...)

	// set bind to port values for port redirection
	for _, listener := range listeners {
		listener.BindToPort = false
	}

	// add an extra listener that binds to the port that is the recipient of the iptables redirect
	listeners = append(listeners, &Listener{
		Name:           VirtualListenerName,
		Address:        fmt.Sprintf("tcp://%s:%d", WildcardAddress, env.Mesh.ProxyListenPort),
		BindToPort:     true,
		UseOriginalDst: true,
		Filters:        make([]*NetworkFilter, 0),
	})

	return listeners.normalize(), clusters.normalize()
}

// buildRDSRoutes supplies RDS-enabled HTTP routes
// TODO: this can be optimized by querying for a specific HTTP port in the table
func buildRDSRoutes(mesh *proxyconfig.ProxyMeshConfig, role proxy.Node,
	discovery model.ServiceDiscovery, config model.IstioConfigStore) HTTPRouteConfigs {
	switch role.Type {
	case proxy.Ingress:
		httpRouteConfigs, _ := buildIngressRoutes(mesh, discovery, config)
		return httpRouteConfigs

	case proxy.Egress:
		return buildEgressRoutes(mesh, discovery)

	case proxy.Sidecar:
		instances := discovery.HostInstances(map[string]bool{role.IPAddress: true})
		services := discovery.Services()
		return buildOutboundHTTPRoutes(mesh, role, instances, services, config)
	}

	return nil
}

// buildHTTPListener constructs a listener for the network interface address and port
// Use "0.0.0.0" IP address to listen on all interfaces
// RDS parameter controls whether to use RDS for the route updates.
func buildHTTPListener(mesh *proxyconfig.ProxyMeshConfig, role proxy.Node, routeConfig *HTTPRouteConfig,
	ip string, port int, rds bool, useRemoteAddress bool) *Listener {
	filters := buildFaultFilters(routeConfig)

	filters = append(filters, HTTPFilter{
		Type:   decoder,
		Name:   router,
		Config: FilterRouterConfig{},
	})

	if mesh.MixerAddress != "" {
		mixerConfig := mixerHTTPRouteConfig(role)
		filter := HTTPFilter{
			Type:   decoder,
			Name:   MixerFilter,
			Config: mixerConfig,
		}
		filters = append([]HTTPFilter{filter}, filters...)
	}

	config := &HTTPFilterConfig{
		CodecType:         auto,
		GenerateRequestID: true,
		UseRemoteAddress:  useRemoteAddress,
		StatPrefix:        "http",
		AccessLog: []AccessLog{{
			Path: DefaultAccessLog,
		}},
		Filters: filters,
	}

	if mesh.ZipkinAddress != "" {
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
		Name:       fmt.Sprintf("http_%s_%d", ip, port),
		Address:    fmt.Sprintf("tcp://%s:%d", ip, port),
		Filters: []*NetworkFilter{{
			Type:   read,
			Name:   HTTPConnectionManager,
			Config: config,
		}},
	}
}

func applyInboundAuth(listener *Listener, mesh *proxyconfig.ProxyMeshConfig) {
	switch mesh.AuthPolicy {
	case proxyconfig.ProxyMeshConfig_NONE:
	case proxyconfig.ProxyMeshConfig_MUTUAL_TLS:
		listener.SSLContext = buildListenerSSLContext(mesh.AuthCertsPath)
	}
}

// buildTCPListener constructs a listener for the TCP proxy
func buildTCPListener(tcpConfig *TCPRouteConfig, ip string, port int) *Listener {
	return &Listener{
		Name:    fmt.Sprintf("tcp_%s_%d", ip, port),
		Address: fmt.Sprintf("tcp://%s:%d", ip, port),
		Filters: []*NetworkFilter{{
			Type: read,
			Name: TCPProxyFilter,
			Config: &TCPProxyFilterConfig{
				StatPrefix:  "tcp",
				RouteConfig: tcpConfig,
			},
		}},
	}
}

// buildOutboundListeners combines HTTP routes and TCP listeners
func buildOutboundListeners(mesh *proxyconfig.ProxyMeshConfig, sidecar proxy.Node, instances []*model.ServiceInstance,
	services []*model.Service, config model.IstioConfigStore) (Listeners, Clusters) {
	listeners, clusters := buildOutboundTCPListeners(mesh, services)

	// note that outbound HTTP routes are supplied through RDS
	httpOutbound := buildOutboundHTTPRoutes(mesh, sidecar, instances, services, config)
	for port, routeConfig := range httpOutbound {
		listeners = append(listeners, buildHTTPListener(mesh, sidecar, routeConfig, WildcardAddress, port, true, false))
		clusters = append(clusters, routeConfig.clusters()...)
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
func buildOutboundHTTPRoutes(mesh *proxyconfig.ProxyMeshConfig, sidecar proxy.Node,
	instances []*model.ServiceInstance, services []*model.Service, config model.IstioConfigStore) HTTPRouteConfigs {
	httpConfigs := make(HTTPRouteConfigs)
	suffix := strings.Split(sidecar.Domain, ".")

	// get all the route rules applicable to the instances
	rules := config.RouteRulesBySource(instances)

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

				// there should be at most one occurrence of the service for the same
				// port since service port values are distinct; that means the virtual
				// host domains, which include the sole domain name for the service, do
				// not overlap for the same route config.
				// for example, a service "a" with two ports 80 and 8080, would have virtual
				// hosts on 80 and 8080 listeners that contain domain "a".
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
// TODO(github.com/istio/pilot/issues/237)
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
				cluster := buildOutboundCluster(service.Hostname, servicePort, nil)
				route := buildTCPRoute(cluster, []string{service.Address})
				config := &TCPRouteConfig{Routes: []*TCPRoute{route}}
				listener := buildTCPListener(config, service.Address, servicePort.Port)
				tcpClusters = append(tcpClusters, cluster)
				tcpListeners = append(tcpListeners, listener)
			}
		}
	}
	return tcpListeners, tcpClusters
}

// buildInboundListeners creates listeners for the server-side (inbound)
// configuration for co-located service instances. The function also returns
// all inbound clusters since they are statically declared in the proxy
// configuration and do not utilize CDS.
func buildInboundListeners(mesh *proxyconfig.ProxyMeshConfig, sidecar proxy.Node,
	instances []*model.ServiceInstance) (Listeners, Clusters) {
	listeners := make(Listeners, 0, len(instances))
	clusters := make(Clusters, 0, len(instances))

	// inbound connections/requests are redirected to the endpoint address but appear to be sent
	// to the service address
	// assumes that endpoint addresses/ports are unique in the instance set
	// TODO: validate that duplicated endpoints for services can be handled (e.g. above assumption)
	for _, instance := range instances {
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
				route.OpaqueConfig = buildMixerInboundOpaqueConfig()
			}

			host := &VirtualHost{
				Name:    fmt.Sprintf("inbound|%d", endpoint.Port),
				Domains: []string{"*"},
				Routes:  []*HTTPRoute{route},
			}

			config := &HTTPRouteConfig{VirtualHosts: []*VirtualHost{host}}
			listeners = append(listeners,
				buildHTTPListener(mesh, sidecar, config, endpoint.Address, endpoint.Port, false, false))

		case model.ProtocolTCP, model.ProtocolHTTPS:
			listeners = append(listeners, buildTCPListener(&TCPRouteConfig{
				Routes: []*TCPRoute{buildTCPRoute(cluster, []string{endpoint.Address})},
			}, endpoint.Address, endpoint.Port))

		default:
			glog.Warningf("Unsupported inbound protocol %v for port %#v", protocol, servicePort)
		}
	}

	for _, listener := range listeners {
		applyInboundAuth(listener, mesh)
	}

	return listeners, clusters
}
