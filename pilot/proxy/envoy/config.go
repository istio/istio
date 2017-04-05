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
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
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
// - HTTP pod port collision creates duplicate virtual host entries
// - (bug) two service ports with the same target port create two virtual hosts with same domains
//   (not allowed by envoy). FIXME - need to detect and eliminate such ports in validation
// TODO: no RDS for TCP

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
func Generate(context *ProxyContext) *Config {
	mesh := context.MeshConfig
	listeners, clusters := buildListeners(context)

	// set bind to port values to values for port redirection
	for _, listener := range listeners {
		listener.BindToPort = false
	}

	// add an extra listener that binds to a port
	listeners = append(listeners, &Listener{
		Address:        fmt.Sprintf("tcp://%s:%d", WildcardAddress, mesh.ProxyListenPort),
		BindToPort:     true,
		UseOriginalDst: true,
		Filters:        make([]*NetworkFilter, 0),
	})

	clusters = append(clusters, buildDiscoveryCluster(mesh.DiscoveryAddress, RDSName, mesh.ConnectTimeout))
	return &Config{
		Listeners: listeners,
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Address:       fmt.Sprintf("tcp://%s:%d", WildcardAddress, mesh.ProxyAdminPort),
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: &SDS{
				Cluster:        buildDiscoveryCluster(mesh.DiscoveryAddress, "sds", mesh.ConnectTimeout),
				RefreshDelayMs: int(convertDuration(mesh.DiscoveryRefreshDelay) / time.Millisecond),
			},
			CDS: &CDS{
				Cluster:        buildDiscoveryCluster(mesh.DiscoveryAddress, "cds", mesh.ConnectTimeout),
				RefreshDelayMs: int(convertDuration(mesh.DiscoveryRefreshDelay) / time.Millisecond),
			},
		},
	}
}

// buildListeners produces a list of listeners and referenced clusters
// (due to lack of RDS support for TCP proxy filter, all referenced clusters in TCP routes
// must be present)
func buildListeners(context *ProxyContext) (Listeners, Clusters) {
	// query the services model
	instances := context.Discovery.HostInstances(map[string]bool{context.IPAddress: true})
	services := context.Discovery.Services()

	inbound, inClusters := buildInboundListeners(instances, context.MeshConfig)
	outbound, outClusters := buildOutboundListeners(instances, services, context)

	listeners := append(inbound, outbound...)
	listeners.normalize()

	clusters := append(inClusters, outClusters...).normalize()

	// inject Mixer filter with proxy identities
	insertMixerFilter(listeners, instances, context)

	return listeners, clusters
}

// buildHTTPListener constructs a listener for the network interface address and port
// Use "0.0.0.0" IP address to listen on all interfaces
// RDS parameter controls whether to use RDS for the route updates.
func buildHTTPListener(mesh *proxyconfig.ProxyMeshConfig, routeConfig *HTTPRouteConfig,
	ip string, port int, rds bool, inbound bool) *Listener {
	filters := buildFaultFilters(routeConfig)

	filters = append(filters, HTTPFilter{
		Type:   "decoder",
		Name:   "router",
		Config: FilterRouterConfig{},
	})

	config := &HTTPFilterConfig{
		CodecType:  "auto",
		StatPrefix: "http",
		AccessLog: []AccessLog{{
			Path: DefaultAccessLog,
		}},
		Filters: filters,
	}

	if rds {
		config.RDS = &RDS{
			Cluster:         RDSName,
			RouteConfigName: fmt.Sprintf("%d", port),
			RefreshDelayMs:  (int)(convertDuration(mesh.DiscoveryRefreshDelay) / time.Millisecond),
		}
	} else {
		config.RouteConfig = routeConfig
	}

	listener := &Listener{
		Address: fmt.Sprintf("tcp://%s:%d", ip, port),
		Filters: []*NetworkFilter{{
			Type:   "read",
			Name:   HTTPConnectionManager,
			Config: config,
		}},
	}

	if inbound {
		switch mesh.AuthPolicy {
		case proxyconfig.ProxyMeshConfig_NONE:
		case proxyconfig.ProxyMeshConfig_MUTUAL_TLS:
			listener.SSLContext = buildListenerSSLContext(mesh.AuthCertsPath)
		default:
			glog.Warningf("Unknown auth policy: %v", mesh.AuthPolicy)
		}
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
	context *ProxyContext) (Listeners, Clusters) {
	httpOutbound := buildOutboundHTTPRoutes(instances, services, context)
	listeners, clusters := buildOutboundTCPListeners(context.MeshConfig, services)
	for port, routeConfig := range httpOutbound {
		listeners = append(listeners, buildHTTPListener(context.MeshConfig, routeConfig, WildcardAddress, port, true, false))
	}
	return listeners, clusters
}

// buildOutboundHTTPRoutes creates HTTP route configs indexed by ports for the
// traffic outbound from the proxy instance
func buildOutboundHTTPRoutes(instances []*model.ServiceInstance, services []*model.Service,
	context *ProxyContext) HTTPRouteConfigs {
	httpConfigs := make(HTTPRouteConfigs)

	// used for shortcut domain names for outbound hostnames
	suffix := sharedInstanceHost(instances)

	// get all the route rules applicable to the instances
	rules := context.Config.RouteRulesBySource("", instances)

	// outbound connections/requests are directed to service ports; we create a
	// map for each service port to define filters
	for _, service := range services {
		// clusters aggregate clusters across ports
		clusters := make(Clusters, 0)

		for _, servicePort := range service.Ports {
			protocol := servicePort.Protocol
			switch protocol {
			case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
				routes := make([]*HTTPRoute, 0)

				// User can provide timeout/retry policies without any match condition,
				// or specific route. User could also provide a single default route, in
				// which case, we should not be generating another default route.
				// For every HTTPRoute we build, the return value also provides a boolean
				// "catchAll" flag indicating if the route that was built was a catch all route.
				// When such a route is encountered, we stop building further routes for the
				// destination and we will not add the default route after of the for loop.

				catchAll := false
				var httpRoute *HTTPRoute

				// collect route rules
				for _, rule := range rules {
					if rule.Destination == service.Hostname {
						httpRoute, catchAll = buildHTTPRoute(rule, servicePort)
						routes = append(routes, httpRoute)
						if catchAll {
							break
						}
					}
				}

				if !catchAll {
					// default route for the destination
					cluster := buildOutboundCluster(service.Hostname, servicePort, nil)
					routes = append(routes, buildDefaultRoute(cluster))
				}

				host := buildVirtualHost(service, servicePort, suffix, routes)
				http := httpConfigs.EnsurePort(servicePort.Port)
				http.VirtualHosts = append(http.VirtualHosts, host)
				clusters = append(clusters, host.clusters()...)

			case model.ProtocolTCP, model.ProtocolHTTPS:
				// handled by buildOutboundTCPListeners

			default:
				glog.Warningf("Unsupported outbound protocol %v for port %#v", protocol, servicePort)
			}
		}

		clusters.setTimeout(context.MeshConfig.ConnectTimeout)

		// apply SSL context to outbound clusters for authentication policy
		switch context.MeshConfig.AuthPolicy {
		case proxyconfig.ProxyMeshConfig_NONE:
		case proxyconfig.ProxyMeshConfig_MUTUAL_TLS:
			serviceAccounts, _ := context.Discovery.GetIstioServiceAccounts(service.Hostname)
			sslContext := buildClusterSSLContext(context.MeshConfig.AuthCertsPath, serviceAccounts)
			for _, cluster := range clusters {
				cluster.SSLContext = sslContext
			}
		default:
			glog.Warningf("Unknown auth policy: %v", context.MeshConfig.AuthPolicy)
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
// configuration and do no utilize CDS.
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
		cluster := buildInboundCluster(service.Hostname, endpoint.Port, protocol)
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
				buildHTTPListener(mesh, config, endpoint.Address, endpoint.Port, false, true))

		case model.ProtocolTCP, model.ProtocolHTTPS:
			listeners = append(listeners, buildTCPListener(&TCPRouteConfig{
				Routes: []*TCPRoute{buildTCPRoute(cluster, []string{endpoint.Address})},
			}, endpoint.Address, endpoint.Port))

		default:
			glog.Warningf("Unsupported inbound protocol %v for port %#v", protocol, servicePort)
		}
	}

	clusters.setTimeout(mesh.ConnectTimeout)
	return listeners, clusters
}
