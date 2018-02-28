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

// Package v2 is a port of package v1 from using the Envoy v1 API (JSON based) to v2 API (proto based).
package v2

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/api/filter/accesslog"
	"github.com/envoyproxy/go-control-plane/api/filter/network"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pkg/log"

	_ "github.com/golang/glog"

	google_protobuf "github.com/gogo/protobuf/types"
	meshconfig "istio.io/api/mesh/v1alpha1"
	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
)

const (
	FilterNameRouter = "router"
)

func ListListenersResponse(env model.Environment, node model.Proxy) (*api.DiscoveryResponse, error) {
	ls, err := BuildListeners(env, node)
	if err != nil {
		return nil, err
	}

	resp := &api.DiscoveryResponse{}
	for _, ll := range ls {
		lr, _ := types.MarshalAny(ll)
		resp.Resources = append(resp.Resources, lr)
	}

	return resp, nil
}

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func BuildListeners(env model.Environment, node model.Proxy) ([]*api.Listener, error) {
	switch node.Type {
	case model.Sidecar:
		proxyInstances, err := env.GetProxyServiceInstances(node)
		if err != nil {
			return nil, err
		}
		services, err := env.Services()
		if err != nil {
			return nil, err
		}
		listeners, _ := buildSidecarListenersClusters(env.Mesh, proxyInstances,
			services, env.ManagementPorts(node.IPAddress), node, env.IstioConfigStore)
		return listeners, nil
	case model.Router:
		// TODO: add listeners for other protocols too
		return buildGatewayHTTPListeners(env.Mesh, env.IstioConfigStore, node)
	case model.Ingress:
		services, err := env.Services()
		if err != nil {
			return nil, err
		}
		var svc *model.Service
		for _, s := range services {
			if strings.HasPrefix(s.Hostname, "istio-ingress") {
				svc = s
				break
			}
		}
		insts := make([]*model.ServiceInstance, 0, 1)
		if svc != nil {
			insts = append(insts, &model.ServiceInstance{Service: svc})
		}
		return buildIngressListeners(env.Mesh, insts, env.ServiceDiscovery, env.IstioConfigStore, node), nil
	}
	return nil, nil
}

// Ported from v1/ingress.go
func buildIngressListeners(mesh *meshconfig.MeshConfig, proxyInstances []*model.ServiceInstance, discovery model.ServiceDiscovery,
	config model.IstioConfigStore,
	ingress model.Proxy) []*api.Listener {

	opts := buildHTTPListenerOpts{
		mesh:             mesh,
		proxy:            ingress,
		proxyInstances:   proxyInstances,
		routeConfig:      nil,
		ip:               v1.WildcardAddress,
		port:             80,
		rds:              "80",
		useRemoteAddress: true,
		direction:        network.HttpConnectionManager_Tracing_EGRESS,
		outboundListener: false,
		store:            config,
	}

	listeners := []*api.Listener{buildHTTPListener(opts)}

	// lack of SNI in Envoy implies that TLS secrets are attached to listeners
	// therefore, we should first check that TLS endpoint is needed before shipping TLS listener
	_, secret := v1.BuildIngressRoutes(mesh, ingress, proxyInstances, discovery, config)
	if secret != "" {
		opts.port = 443
		opts.rds = "443"
		listener := buildHTTPListener(opts)
		// TODO(mostrowski)
		/*listener.SSLContext = &SSLContext{
			CertChainFile:  path.Join(model.IngressCertsPath, model.IngressCertFilename),
			PrivateKeyFile: path.Join(model.IngressCertsPath, model.IngressKeyFilename),
			ALPNProtocols:  strings.Join(ListenersALPNProtocols, ","),
		} */
		listeners = append(listeners, listener)
	}

	return listeners
}

func buildClusters(env model.Environment, node model.Proxy) (v1.Clusters, error) {
	var clusters v1.Clusters
	var proxyInstances []*model.ServiceInstance
	var err error
	switch node.Type {
	case model.Sidecar, model.Router:
		proxyInstances, err = env.GetProxyServiceInstances(node)
		if err != nil {
			return clusters, err
		}
		services, err := env.Services() // nolint: vetshadow
		if err != nil {
			return clusters, err
		}
		_, clusters = buildSidecarListenersClusters(env.Mesh, proxyInstances,
			services, env.ManagementPorts(node.IPAddress), node, env.IstioConfigStore)
	case model.Ingress:
		httpRouteConfigs, _ := v1.BuildIngressRoutes(env.Mesh, node, nil, env.ServiceDiscovery, env.IstioConfigStore)
		clusters = httpRouteConfigs.Clusters().Normalize()
	}

	if err != nil {
		return clusters, err
	}

	// apply custom policies for outbound clusters
	for _, cluster := range clusters {
		v1.ApplyClusterPolicy(cluster, proxyInstances, env.IstioConfigStore, env.Mesh, env.ServiceAccounts, node.Domain)
	}

	// append Mixer service definition if necessary
	if env.Mesh.MixerCheckServer != "" || env.Mesh.MixerReportServer != "" {
		clusters = append(clusters, v1.BuildMixerClusters(env.Mesh, node, env.MixerSAN)...)
		clusters = append(clusters, v1.BuildMixerAuthFilterClusters(env.IstioConfigStore, env.Mesh, proxyInstances)...)
	}

	return clusters, nil
}

// buildSidecarListenersClusters produces a list of listeners and referenced clusters for sidecar proxies
// TODO: this implementation is inefficient as it is recomputing all the routes for all proxies
// There is a lot of potential to cache and reuse cluster definitions across proxies and also
// skip computing the actual HTTP routes
func buildSidecarListenersClusters(
	mesh *meshconfig.MeshConfig,
	proxyInstances []*model.ServiceInstance,
	services []*model.Service,
	managementPorts model.PortList,
	node model.Proxy,
	config model.IstioConfigStore) ([]*api.Listener, v1.Clusters) {

	// ensure services are ordered to simplify generation logic
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })

	listeners := make([]*api.Listener, 0)
	clusters := make(v1.Clusters, 0)

	if node.Type == model.Router {
		outbound, outClusters := buildOutboundListeners(mesh, node, proxyInstances, services, config)
		listeners = append(listeners, outbound...)
		clusters = append(clusters, outClusters...)
	} else if mesh.ProxyListenPort > 0 {
		inbound, inClusters := buildInboundListeners(mesh, node, proxyInstances, config)
		outbound, outClusters := buildOutboundListeners(mesh, node, proxyInstances, services, config)
		mgmtListeners, mgmtClusters := buildMgmtPortListeners(mesh, managementPorts, node.IPAddress)

		listeners = append(listeners, inbound...)
		listeners = append(listeners, outbound...)
		clusters = append(clusters, inClusters...)
		clusters = append(clusters, outClusters...)

		// If management listener port and service port are same, bad things happen
		// when running in kubernetes, as the probes stop responding. So, append
		// non overlapping listeners only.
		for i := range mgmtListeners {
			m := mgmtListeners[i]
			c := mgmtClusters[i]
			l := GetByAddress(listeners, m.Address.String())
			if l != nil {
				log.Warnf("Omitting listener for management address %s (%s) due to collision with service listener %s (%s)",
					m.Name, m.Address, l.Name, l.Address)
				continue
			}
			listeners = append(listeners, m)
			clusters = append(clusters, c)
		}

		// BindToPort is deprecated in v2, always true.

		// add an extra listener that binds to the port that is the recipient of the iptables redirect
		listeners = append(listeners, &api.Listener{
			Name:           v1.VirtualListenerName,
			Address:        BuildAddress(v1.WildcardAddress, uint32(mesh.ProxyListenPort)),
			UseOriginalDst: &google_protobuf.BoolValue{true},
			FilterChains:   make([]*api.FilterChain, 0),
		})
	}

	// enable HTTP PROXY port if necessary; this will add an RDS route for this port
	if mesh.ProxyHttpPort > 0 {
		useRemoteAddress := false
		traceOperation := network.HttpConnectionManager_Tracing_EGRESS
		listenAddress := v1.LocalhostAddress

		if node.Type == model.Router {
			useRemoteAddress = true
			traceOperation = network.HttpConnectionManager_Tracing_INGRESS
			listenAddress = v1.WildcardAddress
		}

		// only HTTP outbound clusters are needed
		httpOutbound := buildOutboundHTTPRoutes(mesh, node, proxyInstances, services, config)
		httpOutbound = buildEgressHTTPRoutes(mesh, node, proxyInstances, config, httpOutbound)
		httpOutbound = v1.BuildExternalServiceHTTPRoutes(mesh, node, proxyInstances, config, httpOutbound)
		clusters = append(clusters, httpOutbound.Clusters()...)
		listeners = append(listeners, buildHTTPListener(buildHTTPListenerOpts{
			mesh:             mesh,
			proxy:            node,
			proxyInstances:   proxyInstances,
			routeConfig:      nil,
			ip:               listenAddress,
			port:             int(mesh.ProxyHttpPort),
			rds:              v1.RDSAll,
			useRemoteAddress: useRemoteAddress,
			direction:        traceOperation,
			outboundListener: true,
			store:            config,
		}))
		// TODO: need inbound listeners in HTTP_PROXY case, with dedicated ingress listener.
	}

	return normalizeListeners(listeners), clusters.Normalize()
}

// GetByAddress returns a listener by its address
func GetByAddress(listeners []*api.Listener, addr string) *api.Listener {
	for _, listener := range listeners {
		if listener.Address.String() == addr {
			return listener
		}
	}
	return nil
}

// options required to build an HTTPListener
type buildHTTPListenerOpts struct {
	// nolint: maligned
	config           model.Config
	env              model.Environment
	mesh             *meshconfig.MeshConfig
	proxy            model.Proxy
	proxyInstances   []*model.ServiceInstance
	routeConfig      *v1.HTTPRouteConfig
	rdsConfig        *network.Rds
	ip               string
	port             int
	rds              string
	useRemoteAddress bool
	direction        network.HttpConnectionManager_Tracing_OperationName
	outboundListener bool
	store            model.IstioConfigStore
}

// buildFaultFilters builds fault filters in v2 Envoy format for the given config, env and node.
func buildFaultFilters(config model.Config, env model.Environment, node model.Proxy) []*network.HttpFilter {
	rule := config.Spec.(*routing.RouteRule)
	// TODO(mostrowski): need a lightweight function to get list of cluster names.
	clusters, err := buildClusters(env, node)
	if err != nil {
		panic(err)
	}
	headerMatchers := buildHTTPHeaderMatcher(rule.Match)
	var out []*network.HttpFilter
	// Add the fault filters, one per cluster defined in weighted cluster or cluster
	if rule.HttpFault != nil {
		out = make([]*network.HttpFilter, 0, len(clusters))
		for _, c := range clusters {
			if fault := buildHTTPFaultFilter(c.Name, rule.HttpFault, headerMatchers); fault != nil {
				out = append(out, fault)
			}
		}
	}

	return out
}

// buildHTTPListener constructs a listener for the network interface address and port.
// Set RDS parameter to a non-empty value to enable RDS for the matching route name.
func buildHTTPListener(opts buildHTTPListenerOpts) *api.Listener {
	filters := []*network.HttpFilter{buildHTTPFilterConfig(v1.CORSFilter, "")}
	filters = append(filters, buildFaultFilters(opts.config, opts.env, opts.proxy)...)
	filters = append(filters, buildHTTPFilterConfig(FilterNameRouter, ""))

	if opts.mesh.MixerCheckServer != "" || opts.mesh.MixerReportServer != "" {
		mixerConfig := v1.BuildHTTPMixerFilterConfig(opts.mesh, opts.proxy, opts.proxyInstances, opts.outboundListener, opts.store)
		filter := buildHTTPFilterConfig(v1.MixerFilter, mustMarshalToString(mixerConfig))
		filters = append([]*network.HttpFilter{filter}, filters...)
	}

	rds := &network.Rds{}
	if opts.rds != "" {
		rds = &network.Rds{
			ConfigSource: api.ConfigSource{
				ConfigSourceSpecifier: &api.ConfigSource_ApiConfigSource{
					ApiConfigSource: &api.ApiConfigSource{
						ApiType:      api.ApiConfigSource_GRPC,
						ClusterNames: []string{v1.RDSName},
					},
				},
			},
		}
	} else {
		rds = opts.rdsConfig
	}

	manager := &network.HttpConnectionManager{
		CodecType: network.HttpConnectionManager_AUTO,
		AccessLog: []*accesslog.AccessLog{
			{
				Config: nil,
			},
		},
		HttpFilters: filters,
		StatPrefix:  "http",
		RouteSpecifier: &network.HttpConnectionManager_Rds{
			Rds: rds,
		},
		UseRemoteAddress: &google_protobuf.BoolValue{opts.useRemoteAddress},
	}

	if opts.mesh.AccessLogFile != "" {
		fl := &accesslog.FileAccessLog{
			Path: opts.mesh.AccessLogFile,
		}
		manager.AccessLog = []*accesslog.AccessLog{{Config: buildProtoStruct("file_access_log", fl.String())}}
	}

	if opts.mesh.EnableTracing {
		manager.Tracing = &network.HttpConnectionManager_Tracing{
			OperationName: opts.direction,
		}
		manager.GenerateRequestId = &google_protobuf.BoolValue{true}
	}

	return &api.Listener{
		Address: &api.Address{
			Address: &api.Address_SocketAddress{
				SocketAddress: &api.SocketAddress{
					Address: opts.ip,
					PortSpecifier: &api.SocketAddress_PortValue{
						PortValue: uint32(opts.port),
					},
				},
			},
		},
		Name: fmt.Sprintf("http_%s_%d", opts.ip, opts.port),
		FilterChains: []*api.FilterChain{
			{
				Filters: []*api.Filter{
					{
						Name:   v1.HTTPConnectionManager,
						Config: buildProtoStruct(v1.HTTPConnectionManager, manager.String()),
					},
				},
			},
		},
	}
}

// consolidateAuthPolicy returns service auth policy, if it's not INHERIT. Else,
// returns mesh policy.
func consolidateAuthPolicy(mesh *meshconfig.MeshConfig, serviceAuthPolicy meshconfig.AuthenticationPolicy) meshconfig.AuthenticationPolicy {
	if serviceAuthPolicy != meshconfig.AuthenticationPolicy_INHERIT {
		return serviceAuthPolicy
	}
	// TODO: use AuthenticationPolicy for mesh policy and remove this conversion
	switch mesh.AuthPolicy {
	case meshconfig.MeshConfig_MUTUAL_TLS:
		return meshconfig.AuthenticationPolicy_MUTUAL_TLS
	case meshconfig.MeshConfig_NONE:
		return meshconfig.AuthenticationPolicy_NONE
	default:
		// Never get here, there are no other enum value for mesh.AuthPolicy.
		panic(fmt.Sprintf("Unknown mesh auth policy: %v\n", mesh.AuthPolicy))
	}
}

// mayApplyInboundAuth adds ssl_context to the listener if consolidateAuthPolicy.
func mayApplyInboundAuth(listener *api.Listener, mesh *meshconfig.MeshConfig,
	serviceAuthPolicy meshconfig.AuthenticationPolicy) {
	// TODO(mostrowski): figure out SSL
	/*	if consolidateAuthPolicy(mesh, serviceAuthPolicy) == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
			listener.SSLContext = buildListenerSSLContext(model.AuthCertsPath)
		}
	*/
}

// buildTCPListener constructs a listener for the TCP proxy
// in addition, it enables mongo proxy filter based on the protocol
func buildTCPListener(tcpConfig *v1.TCPRouteConfig, ip string, port uint32, protocol model.Protocol) *api.Listener {

	config := network.TcpProxy{
		StatPrefix: "tcp",
		// RouteConfig is deprecated.
	}
	baseTCPProxy := &api.Filter{
		Name:   v1.TCPProxyFilter,
		Config: buildProtoStruct(v1.TCPProxyFilter, config.String()),
	}

	// Use Envoy's TCP proxy for TCP and Redis protocols. Currently, Envoy does not support CDS clusters
	// for Redis proxy. Once Envoy supports CDS clusters, remove the following lines
	if protocol == model.ProtocolRedis {
		protocol = model.ProtocolTCP
	}

	switch protocol {
	case model.ProtocolMongo:
		// TODO: add a watcher for /var/lib/istio/mongo/certs
		// if certs are found use, TLS or mTLS clusters for talking to MongoDB.
		// User is responsible for mounting those certs in the pod.
		config := &network.MongoProxy{
			StatPrefix: "mongo",
		}
		return &api.Listener{
			Name:    fmt.Sprintf("mongo_%s_%d", ip, port),
			Address: BuildAddress(ip, port),
			FilterChains: []*api.FilterChain{
				{
					Filters: []*api.Filter{
						{
							Name:   v1.MongoProxyFilter,
							Config: buildProtoStruct(v1.MongoProxyFilter, config.String()),
						},
						baseTCPProxy,
					},
				},
			},
		}

	case model.ProtocolRedis:
		// Redis filter requires the cluster name to be specified
		// as part of the filter. We extract the cluster from the
		// TCPRoute. Since TCPRoute has only one route, we take the
		// cluster from the first route. The moment this route array
		// has multiple routes, we need a fallback. For the moment,
		// fallback to base TCP.

		// Unlike Mongo, Redis is a standalone filter, that is not
		// stacked on top of tcp_proxy
		var td time.Duration = v1.RedisDefaultOpTimeout
		if len(tcpConfig.Routes) == 1 {
			config := &network.RedisProxy{
				Cluster:    tcpConfig.Routes[0].Cluster,
				StatPrefix: "redis",
				Settings: &network.RedisProxy_ConnPoolSettings{
					OpTimeout: durationToProto(td),
				},
			}
			//			func durationToProto(d time.Duration) *dpb.Duration {

			return &api.Listener{
				Name:    fmt.Sprintf("redis_%s_%d", ip, port),
				Address: BuildAddress(ip, port),
				FilterChains: []*api.FilterChain{
					{
						Filters: []*api.Filter{
							{
								Name:   v1.RedisProxyFilter,
								Config: buildProtoStruct(v1.RedisProxyFilter, config.String()),
							},
							baseTCPProxy,
						},
					},
				},
			}
		}
	}

	return &api.Listener{
		Name:         fmt.Sprintf("tcp_%s_%d", ip, port),
		Address:      BuildAddress(ip, port),
		FilterChains: []*api.FilterChain{{Filters: []*api.Filter{baseTCPProxy}}},
	}
}

// buildOutboundListeners combines HTTP routes and TCP listeners
func buildOutboundListeners(mesh *meshconfig.MeshConfig, node model.Proxy, proxyInstances []*model.ServiceInstance,
	services []*model.Service, config model.IstioConfigStore) ([]*api.Listener, v1.Clusters) {
	listeners, clusters := buildOutboundTCPListeners(mesh, node, services)

	egressTCPListeners, egressTCPClusters := buildEgressTCPListeners(mesh, node, config)
	listeners = append(listeners, egressTCPListeners...)
	clusters = append(clusters, egressTCPClusters...)

	externalServiceTCPListeners, externalServiceTCPClusters := buildExternalServiceTCPListeners(mesh, config)
	listeners = append(listeners, externalServiceTCPListeners...)
	clusters = append(clusters, externalServiceTCPClusters...)

	// note that outbound HTTP routes are supplied through RDS
	httpOutbound := buildOutboundHTTPRoutes(mesh, node, proxyInstances, services, config)
	httpOutbound = buildEgressHTTPRoutes(mesh, node, proxyInstances, config, httpOutbound)
	httpOutbound = v1.BuildExternalServiceHTTPRoutes(mesh, node, proxyInstances, config, httpOutbound)

	for port, routeConfig := range httpOutbound {
		operation := network.HttpConnectionManager_Tracing_EGRESS
		useRemoteAddress := false

		if node.Type == model.Router {
			// if this is in Router mode, then use ingress style trace operation, and remote address settings
			useRemoteAddress = true
			operation = network.HttpConnectionManager_Tracing_INGRESS
		}

		listeners = append(listeners, buildHTTPListener(buildHTTPListenerOpts{
			mesh:             mesh,
			proxy:            node,
			proxyInstances:   proxyInstances,
			routeConfig:      routeConfig,
			ip:               v1.WildcardAddress,
			port:             port,
			rds:              fmt.Sprintf("%d", port),
			useRemoteAddress: useRemoteAddress,
			direction:        operation,
			outboundListener: true,
			store:            config,
		}))
		clusters = append(clusters, routeConfig.Clusters()...)
	}

	return listeners, clusters
}

func buildExternalServiceTCPListeners(mesh *meshconfig.MeshConfig, config model.IstioConfigStore) ([]*api.Listener, v1.Clusters) {
	listeners := make([]*api.Listener, 0)
	clusters := make(v1.Clusters, 0)

	for _, externalServiceConfig := range config.ExternalServices() {
		externalService := externalServiceConfig.Spec.(*routingv2.ExternalService)
		for _, port := range externalService.Ports {
			modelPort := v1.BuildExternalServicePort(port)
			switch modelPort.Protocol {
			case model.ProtocolTCP, model.ProtocolMongo, model.ProtocolRedis, model.ProtocolHTTPS:
				routes := make([]*v1.TCPRoute, 0)

				for _, host := range externalService.Hosts {
					cluster := v1.BuildExternalServiceCluster(mesh, host, port.Name, modelPort, nil,
						externalService.Discovery, externalService.Endpoints)
					route := v1.BuildTCPRoute(cluster, []string{host})

					clusters = append(clusters, cluster)
					routes = append(routes, route)
				}

				// TODO: handle port conflicts
				routeConfig := &v1.TCPRouteConfig{Routes: routes}
				listeners = append(listeners,
					buildTCPListener(routeConfig, v1.WildcardAddress, port.Number, modelPort.Protocol))
			default:
				// handled elsewhere
			}
		}
	}

	return listeners, clusters
}

// buildDestinationHTTPRoutes creates HTTP route for a service and a port from rules
func buildDestinationHTTPRoutes(node model.Proxy, service *model.Service,
	servicePort *model.Port,
	proxyInstances []*model.ServiceInstance,
	config model.IstioConfigStore,
	buildCluster v1.BuildClusterFunc,
) []*v1.HTTPRoute {
	protocol := servicePort.Protocol
	switch protocol {
	case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
		routes := make([]*v1.HTTPRoute, 0)

		// collect route rules
		useDefaultRoute := true
		rules := config.RouteRules(proxyInstances, service.Hostname, node.Domain)
		// sort for output uniqueness
		// if v1alpha2 rules are returned, len(rules) <= 1 is guaranteed
		// because v1alpha2 rules are unique per host.
		model.SortRouteRules(rules)

		for _, rule := range rules {
			httpRoutes := v1.BuildHTTPRoutes(config, rule, service, servicePort, proxyInstances, node.Domain, buildCluster)
			routes = append(routes, httpRoutes...)

			// User can provide timeout/retry policies without any match condition,
			// or specific route. User could also provide a single default route, in
			// which case, we should not be generating another default route.
			// For every HTTPRoute we build, the return value also provides a boolean
			// "catchAll" flag indicating if the route that was built was a catch all route.
			// When such a route is encountered, we stop building further routes for the
			// destination and we will not add the default route after the for loop.
			for _, httpRoute := range httpRoutes {
				if httpRoute.CatchAll() {
					useDefaultRoute = false
					break
				}
			}

			if !useDefaultRoute {
				break
			}
		}

		if useDefaultRoute {
			// default route for the destination is always the lowest priority route
			cluster := buildCluster(service.Hostname, servicePort, nil, service.External())
			routes = append(routes, v1.BuildDefaultRoute(cluster))
		}

		return routes

	case model.ProtocolHTTPS:
		// as an exception, external name HTTPS port is sent in plain-text HTTP/1.1
		if service.External() {
			cluster := buildCluster(service.Hostname, servicePort, nil, service.External())
			return []*v1.HTTPRoute{v1.BuildDefaultRoute(cluster)}
		}

	case model.ProtocolTCP, model.ProtocolMongo, model.ProtocolRedis:
		// handled by buildOutboundTCPListeners

	default:
		log.Debugf("Unsupported outbound protocol %v for port %#v", protocol, servicePort)
	}

	return nil
}

// buildOutboundHTTPRoutes creates HTTP route configs indexed by ports for the
// traffic outbound from the proxy instance
func buildOutboundHTTPRoutes(_ *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, services []*model.Service, config model.IstioConfigStore) v1.HTTPRouteConfigs {
	httpConfigs := make(v1.HTTPRouteConfigs)
	suffix := strings.Split(node.Domain, ".")

	// outbound connections/requests are directed to service ports; we create a
	// map for each service port to define filters
	for _, service := range services {
		for _, servicePort := range service.Ports {
			routes := buildDestinationHTTPRoutes(node, service, servicePort, proxyInstances, config, v1.BuildOutboundCluster)

			if len(routes) > 0 {
				host := v1.BuildVirtualHost(service, servicePort, suffix, routes)
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

	return httpConfigs.Normalize()
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
//
// Connections to the ports of non-load balanced services are directed to
// the connection's original destination. This avoids costly queries of instance
// IPs and ports, but requires that ports of non-load balanced service be unique.
func buildOutboundTCPListeners(mesh *meshconfig.MeshConfig, node model.Proxy,
	services []*model.Service) ([]*api.Listener, v1.Clusters) {
	tcpListeners := make([]*api.Listener, 0)
	tcpClusters := make(v1.Clusters, 0)

	var originalDstCluster *v1.Cluster
	wildcardListenerPorts := make(map[int]bool)
	for _, service := range services {
		if service.External() {
			continue // TODO TCP external services not currently supported
		}
		for _, servicePort := range service.Ports {
			switch servicePort.Protocol {
			case model.ProtocolTCP, model.ProtocolHTTPS, model.ProtocolMongo, model.ProtocolRedis:
				if service.LoadBalancingDisabled || service.Address == "" ||
					node.Type == model.Router {
					// ensure only one wildcard listener is created per port if its headless service
					// or if its for a Router (where there is one wildcard TCP listener per port)
					// or if this is in environment where services don't get a dummy load balancer IP.
					if wildcardListenerPorts[servicePort.Port] {
						log.Debugf("Multiple definitions for port %d", servicePort.Port)
						continue
					}
					wildcardListenerPorts[servicePort.Port] = true

					var cluster *v1.Cluster
					// Router mode cannot handle headless services
					if service.LoadBalancingDisabled && node.Type != model.Router {
						if originalDstCluster == nil {
							originalDstCluster = v1.BuildOriginalDSTCluster(
								"orig-dst-cluster-tcp", mesh.ConnectTimeout)
							tcpClusters = append(tcpClusters, originalDstCluster)
						}
						cluster = originalDstCluster
					} else {
						cluster = v1.BuildOutboundCluster(service.Hostname, servicePort, nil,
							service.External())
						tcpClusters = append(tcpClusters, cluster)
					}
					route := v1.BuildTCPRoute(cluster, nil)
					config := &v1.TCPRouteConfig{Routes: []*v1.TCPRoute{route}}
					listener := buildTCPListener(
						config, v1.WildcardAddress, uint32(servicePort.Port), servicePort.Protocol)
					tcpListeners = append(tcpListeners, listener)
				} else {
					cluster := v1.BuildOutboundCluster(service.Hostname, servicePort, nil, service.External())
					route := v1.BuildTCPRoute(cluster, []string{service.Address})
					config := &v1.TCPRouteConfig{Routes: []*v1.TCPRoute{route}}
					listener := buildTCPListener(
						config, service.Address, uint32(servicePort.Port), servicePort.Protocol)
					tcpClusters = append(tcpClusters, cluster)
					tcpListeners = append(tcpListeners, listener)
				}
			}
		}
	}

	return tcpListeners, tcpClusters
}

// buildInboundListeners creates listeners for the server-side (inbound)
// configuration for co-located service proxyInstances. The function also returns
// all inbound clusters since they are statically declared in the proxy
// configuration and do not utilize CDS.
func buildInboundListeners(mesh *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, config model.IstioConfigStore) ([]*api.Listener, v1.Clusters) {
	listeners := make([]*api.Listener, 0, len(proxyInstances))
	clusters := make(v1.Clusters, 0, len(proxyInstances))

	// inbound connections/requests are redirected to the endpoint address but appear to be sent
	// to the service address
	// assumes that endpoint addresses/ports are unique in the instance set
	// TODO: validate that duplicated endpoints for services can be handled (e.g. above assumption)
	for _, instance := range proxyInstances {
		endpoint := instance.Endpoint
		servicePort := endpoint.ServicePort
		protocol := servicePort.Protocol
		cluster := v1.BuildInboundCluster(endpoint.Port, protocol, mesh.ConnectTimeout)
		clusters = append(clusters, cluster)

		var listener *api.Listener

		// Local service instances can be accessed through one of three
		// addresses: localhost, endpoint IP, and service
		// VIP. Localhost bypasses the proxy and doesn't need any TCP
		// route config. Endpoint IP is handled below and Service IP is handled
		// by outbound routes.
		// Traffic sent to our service VIP is redirected by remote
		// services' kubeproxy to our specific endpoint IP.
		switch protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
			defaultRoute := v1.BuildDefaultRoute(cluster)

			// set server-side mixer filter config for inbound HTTP routes
			if mesh.MixerCheckServer != "" || mesh.MixerReportServer != "" {
				defaultRoute.OpaqueConfig = v1.BuildMixerOpaqueConfig(!mesh.DisablePolicyChecks, false, instance.Service.Hostname)
			}

			host := &v1.VirtualHost{
				Name:    fmt.Sprintf("inbound|%d", endpoint.Port),
				Domains: []string{"*"},
				Routes:  []*v1.HTTPRoute{},
			}

			// Websocket enabled routes need to have an explicit use_websocket : true
			// This setting needs to be enabled on Envoys at both sender and receiver end
			if protocol == model.ProtocolHTTP {
				// get all the route rules applicable to the proxyInstances
				rules := config.RouteRulesByDestination(proxyInstances, node.Domain)

				// sort for output uniqueness
				// if v1alpha2 rules are returned, len(rules) <= 1 is guaranteed
				// because v1alpha2 rules are unique per host.
				model.SortRouteRules(rules)
				for _, config := range rules {
					switch config.Spec.(type) {
					case *routing.RouteRule:
						rule := config.Spec.(*routing.RouteRule)
						if route := v1.BuildInboundRoute(config, rule, cluster); route != nil {
							// set server-side mixer filter config for inbound HTTP routes
							// Note: websocket routes do not call the filter chain. Will be
							// resolved in future.
							if mesh.MixerCheckServer != "" || mesh.MixerReportServer != "" {
								route.OpaqueConfig = v1.BuildMixerOpaqueConfig(!mesh.DisablePolicyChecks, false,
									instance.Service.Hostname)
							}

							host.Routes = append(host.Routes, route)
						}
					case *routingv2.RouteRule:
						rule := config.Spec.(*routingv2.RouteRule)

						// if no routes are returned, it is a TCP RouteRule
						routes := v1.BuildInboundRoutesV2(proxyInstances, config, rule, cluster)
						for _, route := range routes {
							// set server-side mixer filter config for inbound HTTP routes
							// Note: websocket routes do not call the filter chain. Will be
							// resolved in future.
							if mesh.MixerCheckServer != "" || mesh.MixerReportServer != "" {
								route.OpaqueConfig = v1.BuildMixerOpaqueConfig(!mesh.DisablePolicyChecks, false,
									instance.Service.Hostname)
							}
						}

						host.Routes = append(host.Routes, routes...)
					default:
						panic("unsupported rule")
					}
				}
			}

			host.Routes = append(host.Routes, defaultRoute)

			routeConfig := &v1.HTTPRouteConfig{VirtualHosts: []*v1.VirtualHost{host}}
			listener = buildHTTPListener(buildHTTPListenerOpts{
				mesh:             mesh,
				proxy:            node,
				proxyInstances:   proxyInstances,
				routeConfig:      routeConfig,
				ip:               endpoint.Address,
				port:             endpoint.Port,
				rds:              "",
				useRemoteAddress: false,
				direction:        network.HttpConnectionManager_Tracing_INGRESS,
				outboundListener: false,
				store:            config,
			})

		case model.ProtocolTCP, model.ProtocolHTTPS, model.ProtocolMongo, model.ProtocolRedis:
			listener = buildTCPListener(&v1.TCPRouteConfig{
				Routes: []*v1.TCPRoute{v1.BuildTCPRoute(cluster, []string{endpoint.Address})},
			}, endpoint.Address, uint32(endpoint.Port), protocol)

			// set server-side mixer filter config
			if mesh.MixerCheckServer != "" || mesh.MixerReportServer != "" {
				config := v1.BuildTCPMixerFilterConfig(mesh, node, instance)
				// TODO(mostrowski): append to HTTPConnectionManager?
				listener.FilterChains = append(listener.FilterChains, &api.FilterChain{
					Filters: []*api.Filter{
						{
							Config: buildProtoStruct(v1.MixerFilter, mustMarshalToString(config)),
						},
					},
				})
			}

		default:
			log.Debugf("Unsupported inbound protocol %v for port %#v", protocol, servicePort)
		}

		if listener != nil {
			mayApplyInboundAuth(listener, mesh, endpoint.ServicePort.AuthenticationPolicy)
			listeners = append(listeners, listener)
		}
	}

	return listeners, clusters
}

func appendPortToDomains(domains []string, port int) []string {
	domainsWithPorts := make([]string, len(domains), 2*len(domains))
	copy(domainsWithPorts, domains)

	for _, domain := range domains {
		domainsWithPorts = append(domainsWithPorts, domain+":"+strconv.Itoa(port))
	}

	return domainsWithPorts
}

func buildEgressVirtualHost(serviceName string, destination string,
	mesh *meshconfig.MeshConfig, node model.Proxy, port *model.Port, proxyInstances []*model.ServiceInstance,
	config model.IstioConfigStore) *v1.VirtualHost {
	var externalTrafficCluster *v1.Cluster

	protocolToHandle := port.Protocol
	if protocolToHandle == model.ProtocolGRPC {
		protocolToHandle = model.ProtocolHTTP2
	}

	// Create a unique orig dst cluster for each service defined by egress rule
	// So that we can apply circuit breakers, outlier detections, etc., later.
	svc := model.Service{Hostname: destination}
	key := svc.Key(port, nil)
	externalTrafficCluster = v1.BuildOriginalDSTCluster(key, mesh.ConnectTimeout)
	externalTrafficCluster.ServiceName = key
	externalTrafficCluster.Hostname = destination
	externalTrafficCluster.Port = port
	if protocolToHandle == model.ProtocolHTTPS {
		externalTrafficCluster.SSLContext = &v1.SSLContextExternal{}
	}

	if protocolToHandle == model.ProtocolHTTP2 {
		externalTrafficCluster.Features = v1.ClusterFeatureHTTP2
	}

	if protocolToHandle == model.ProtocolHTTPS {
		// temporarily set the protocol to HTTP because we require applications
		// to use http to talk to external services (and we do TLS origination).
		// buildDestinationHTTPRoutes does not generate route blocks for HTTPS services
		port.Protocol = model.ProtocolHTTP
	}

	dest := &model.Service{Hostname: destination}
	routes := buildDestinationHTTPRoutes(node, dest, port, proxyInstances, config, v1.BuildOutboundCluster)
	// reset the protocol to the original value
	port.Protocol = protocolToHandle

	if mesh.MixerCheckServer != "" || mesh.MixerReportServer != "" {
		oc := v1.BuildMixerConfig(node, serviceName, dest, proxyInstances, config, mesh.DisablePolicyChecks, false)
		for _, route := range routes {
			route.OpaqueConfig = oc
		}
	}

	// Set the destination clusters to the cluster we computed above.
	// Services defined via egress rules do not have labels and hence no weighted clusters
	for _, route := range routes {
		// redirect rules must have empty Cluster name
		if !route.Redirect() {
			route.Cluster = externalTrafficCluster.Name
		}
		// cluster for default route must be defined
		route.Clusters = []*v1.Cluster{externalTrafficCluster}
	}

	virtualHostName := fmt.Sprintf("%s:%d", destination, port.Port)
	return &v1.VirtualHost{
		Name:    virtualHostName,
		Domains: appendPortToDomains([]string{destination}, port.Port),
		Routes:  routes,
	}
}

func buildEgressHTTPRoutes(mesh *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, config model.IstioConfigStore,
	httpConfigs v1.HTTPRouteConfigs) v1.HTTPRouteConfigs {

	if node.Type == model.Router {
		// No egress rule support for Routers. As semantics are not clear.
		return httpConfigs
	}

	egressRules, errs := model.RejectConflictingEgressRules(config.EgressRules())
	if errs != nil {
		log.Warnf("Rejected rules: %v", errs)
	}

	for _, r := range egressRules {
		rule, _ := r.Spec.(*routing.EgressRule)
		meshName := r.Name + "." + r.Namespace + "." + r.Domain

		for _, port := range rule.Ports {
			protocol := model.ConvertCaseInsensitiveStringToProtocol(port.Protocol)
			if !model.IsEgressRulesSupportedHTTPProtocol(protocol) {
				continue
			}
			intPort := int(port.Port)
			modelPort := &model.Port{Name: fmt.Sprintf("external-%v-%d", protocol, intPort),
				Port: intPort, Protocol: protocol}
			httpConfig := httpConfigs.EnsurePort(intPort)
			httpConfig.VirtualHosts = append(httpConfig.VirtualHosts,
				buildEgressVirtualHost(meshName, rule.Destination.Service, mesh, node, modelPort, proxyInstances, config))
		}
	}

	return httpConfigs.Normalize()
}

// buildEgressTCPListeners builds a listener on 0.0.0.0 per each distinct port of all TCP egress
// rules and a cluster per each TCP egress rule
func buildEgressTCPListeners(mesh *meshconfig.MeshConfig, node model.Proxy,
	config model.IstioConfigStore) ([]*api.Listener, v1.Clusters) {

	tcpListeners := make([]*api.Listener, 0)
	tcpClusters := make(v1.Clusters, 0)

	if node.Type == model.Router {
		// No egress rule support for Routers. As semantics are not clear.
		return tcpListeners, tcpClusters
	}

	egressRules, errs := model.RejectConflictingEgressRules(config.EgressRules())
	if errs != nil {
		log.Warnf("Rejected rules: %v", errs)
	}

	tcpRulesByPort := make(map[int][]*routing.EgressRule)
	tcpProtocolByPort := make(map[int]model.Protocol)

	for _, r := range egressRules {
		rule, _ := r.Spec.(*routing.EgressRule)
		for _, port := range rule.Ports {
			protocol := model.ConvertCaseInsensitiveStringToProtocol(port.Protocol)
			if !model.IsEgressRulesSupportedTCPProtocol(protocol) {
				continue
			}
			intPort := int(port.Port)
			tcpRulesByPort[intPort] = append(tcpRulesByPort[intPort], rule)
			tcpProtocolByPort[intPort] = protocol
		}
	}

	for intPort, rules := range tcpRulesByPort {
		protocol := tcpProtocolByPort[intPort]
		modelPort := &model.Port{Name: fmt.Sprintf("external-%v-%d", protocol, intPort),
			Port: intPort, Protocol: protocol}

		tcpRoutes := make([]*v1.TCPRoute, 0)
		for _, rule := range rules {
			tcpRoute, tcpCluster := buildEgressTCPRoute(rule.Destination.Service, mesh, modelPort)
			tcpRoutes = append(tcpRoutes, tcpRoute)
			tcpClusters = append(tcpClusters, tcpCluster)
		}

		config := &v1.TCPRouteConfig{Routes: tcpRoutes}
		tcpListener := buildTCPListener(config, v1.WildcardAddress, uint32(intPort), protocol)
		tcpListeners = append(tcpListeners, tcpListener)
	}

	return tcpListeners, tcpClusters
}

// buildEgressTCPRoute builds a tcp route and a cluster per port of a TCP egress service
// see comment to buildOutboundTCPListeners
func buildEgressTCPRoute(destination string,
	mesh *meshconfig.MeshConfig, port *model.Port) (*v1.TCPRoute, *v1.Cluster) {

	// Create a unique orig dst cluster for each service defined by egress rule
	// So that we can apply circuit breakers, outlier detections, etc., later.
	svc := model.Service{Hostname: destination}
	key := svc.Key(port, nil)
	externalTrafficCluster := v1.BuildOriginalDSTCluster(key, mesh.ConnectTimeout)
	externalTrafficCluster.Port = port
	externalTrafficCluster.ServiceName = key
	externalTrafficCluster.Hostname = destination

	route := v1.BuildTCPRoute(externalTrafficCluster, []string{destination})
	return route, externalTrafficCluster
}

// buildMgmtPortListeners creates inbound TCP only listeners for the management ports on
// server (inbound). The function also returns all inbound clusters since
// they are statically declared in the proxy configuration and do not
// utilize CDS.
// Management port listeners are slightly different from standard Inbound listeners
// in that, they do not have mixer filters nor do they have inbound auth.
// N.B. If a given management port is same as the service instance's endpoint port
// the pod will fail to start in Kubernetes, because the mixer service tries to
// lookup the service associated with the Pod. Since the pod is yet to be started
// and hence not bound to the service), the service lookup fails causing the mixer
// to fail the health check call. This results in a vicious cycle, where kubernetes
// restarts the unhealthy pod after successive failed health checks, and the mixer
// continues to reject the health checks as there is no service associated with
// the pod.
// So, if a user wants to use kubernetes probes with Istio, she should ensure
// that the health check ports are distinct from the service ports.
func buildMgmtPortListeners(mesh *meshconfig.MeshConfig, managementPorts model.PortList,
	managementIP string) ([]*api.Listener, v1.Clusters) {
	listeners := make([]*api.Listener, 0, len(managementPorts))
	clusters := make(v1.Clusters, 0, len(managementPorts))

	// assumes that inbound connections/requests are sent to the endpoint address
	for _, mPort := range managementPorts {
		switch mPort.Protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC, model.ProtocolTCP,
			model.ProtocolHTTPS, model.ProtocolMongo, model.ProtocolRedis:
			cluster := v1.BuildInboundCluster(mPort.Port, model.ProtocolTCP, mesh.ConnectTimeout)
			listener := buildTCPListener(&v1.TCPRouteConfig{
				Routes: []*v1.TCPRoute{v1.BuildTCPRoute(cluster, []string{managementIP})},
			}, managementIP, uint32(mPort.Port), model.ProtocolTCP)

			clusters = append(clusters, cluster)
			listeners = append(listeners, listener)
		default:
			log.Warnf("Unsupported inbound protocol %v for management port %#v",
				mPort.Protocol, mPort)
		}
	}

	return listeners, clusters
}

func buildHTTPFilterConfig(name, protoStr string) *network.HttpFilter {
	return &network.HttpFilter{
		Name:   name,
		Config: buildProtoStruct(name, protoStr),
	}
}

func buildProtoStruct(name, value string) *google_protobuf.Struct {
	return &google_protobuf.Struct{
		Fields: map[string]*google_protobuf.Value{
			name: {
				Kind: &google_protobuf.Value_StringValue{
					StringValue: value,
				},
			},
		},
	}
}
