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

package v1alpha3

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	"github.com/envoyproxy/go-control-plane/envoy/type"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
)

const (
	fileAccessLog = "envoy.file_access_log"

	envoyHTTPConnectionManager = "envoy.http_connection_manager"

	// RDSHttpProxy is the special name for HTTP PROXY route
	RDSHttpProxy = "http_proxy"

	// VirtualListenerName is the name for traffic capture listener
	VirtualListenerName = "virtual"

	// WildcardAddress binds to all IP addresses
	WildcardAddress = "0.0.0.0"

	// LocalhostAddress for local binding
	LocalhostAddress = "127.0.0.1"
)

var (
	// Very verbose output in the logs - full LDS response logged for each sidecar.
	// Use /debug/ldsz instead.
	verboseDebug = os.Getenv("PILOT_DUMP_ALPHA3") != ""

	// TODO: gauge should be reset on refresh, not the best way to represent errors but better
	// than nothing.
	// TODO: add dimensions - namespace of rule, service, rule name
	conflictingOutbound = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_conf_out_listeners",
		Help: "Number of conflicting listeners.",
	})
	invalidOutboundListeners = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_invalid_out_listeners",
		Help: "Number of invalid outbound listeners.",
	})
	filterChainsConflict = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_conf_filter_chains",
		Help: "Number of conflicting filter chains.",
	})
)

func init() {
	prometheus.MustRegister(conflictingOutbound)
	prometheus.MustRegister(invalidOutboundListeners)
	prometheus.MustRegister(filterChainsConflict)
}

// ListenersALPNProtocols denotes the the list of ALPN protocols that the listener
// should expose
var ListenersALPNProtocols = []string{"h2", "http/1.1"}

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func (configgen *ConfigGeneratorImpl) BuildListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {
	switch node.Type {
	case model.Sidecar:
		return configgen.buildSidecarListeners(env, node)
	case model.Router, model.Ingress:
		return configgen.buildGatewayListeners(env, node)
	}
	return nil, nil
}

// buildSidecarListeners produces a list of listeners for sidecar proxies
func (configgen *ConfigGeneratorImpl) buildSidecarListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {

	mesh := env.Mesh
	managementPorts := env.ManagementPorts(node.IPAddress)

	proxyInstances, err := env.GetProxyServiceInstances(&node)
	if err != nil {
		return nil, err
	}

	services, err := env.Services()
	if err != nil {
		return nil, err
	}

	// ensure services are ordered to simplify generation logic
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })

	listeners := make([]*xdsapi.Listener, 0)

	if mesh.ProxyListenPort > 0 {
		inbound := configgen.buildSidecarInboundListeners(env, node, proxyInstances)
		outbound := configgen.buildSidecarOutboundListeners(env, node, proxyInstances, services)

		listeners = append(listeners, inbound...)
		listeners = append(listeners, outbound...)

		mgmtListeners := buildSidecarInboundMgmtListeners(managementPorts, node.IPAddress)
		// If management listener port and service port are same, bad things happen
		// when running in kubernetes, as the probes stop responding. So, append
		// non overlapping listeners only.
		for i := range mgmtListeners {
			m := mgmtListeners[i]
			l := util.GetByAddress(listeners, m.Address.String())
			if l != nil {
				log.Warnf("Omitting listener for management address %s (%s) due to collision with service listener %s (%s)",
					m.Name, m.Address, l.Name, l.Address)
				continue
			}
			listeners = append(listeners, m)
		}

		// We need a dummy filter to fill in the filter stack for orig_dst listener
		// TODO: Move to Listener filters and set up original dst filter there.
		dummyTCPProxy := &tcp_proxy.TcpProxy{
			StatPrefix: util.BlackHoleCluster,
			Cluster:    util.BlackHoleCluster,
		}

		var transparent *google_protobuf.BoolValue
		if mode := node.Metadata["INTERCEPTION_MODE"]; mode == "TPROXY" {
			transparent = &google_protobuf.BoolValue{true}
		}

		// add an extra listener that binds to the port that is the recipient of the iptables redirect
		listeners = append(listeners, &xdsapi.Listener{
			Name:           VirtualListenerName,
			Address:        util.BuildAddress(WildcardAddress, uint32(mesh.ProxyListenPort)),
			Transparent:    transparent,
			UseOriginalDst: &google_protobuf.BoolValue{true},
			FilterChains: []listener.FilterChain{
				{
					Filters: []listener.Filter{
						{
							Name:   xdsutil.TCPProxy,
							Config: util.MessageToStruct(dummyTCPProxy),
						},
					},
				},
			},
		})
	}

	// enable HTTP PROXY port if necessary; this will add an RDS route for this port
	if mesh.ProxyHttpPort > 0 {
		useRemoteAddress := false
		traceOperation := http_conn.EGRESS
		listenAddress := LocalhostAddress

		if node.Type == model.Router {
			useRemoteAddress = true
			traceOperation = http_conn.INGRESS
			listenAddress = WildcardAddress
		}

		opts := buildListenerOpts{
			env:            env,
			proxy:          node,
			proxyInstances: proxyInstances,
			ip:             listenAddress,
			port:           int(mesh.ProxyHttpPort),
			protocol:       model.ProtocolHTTP,
			filterChainOpts: []*filterChainOpts{{
				httpOpts: &httpListenerOpts{
					routeConfig: configgen.buildSidecarOutboundHTTPRouteConfig(env, node, proxyInstances,
						services, RDSHttpProxy),
					rds:              RDSHttpProxy,
					useRemoteAddress: useRemoteAddress,
					direction:        traceOperation,
					connectionManager: &http_conn.HttpConnectionManager{
						HttpProtocolOptions: &core.Http1ProtocolOptions{
							AllowAbsoluteUrl: &google_protobuf.BoolValue{
								Value: true,
							},
						},
					},
				},
			}},
			bindToPort: true,
		}
		l := buildListener(opts)
		if err := marshalFilters(l, opts, []plugin.FilterChain{{}}); err != nil {
			log.Warna("buildSidecarListeners ", err.Error())
		} else {
			listeners = append(listeners, l)
		}
		// TODO: need inbound listeners in HTTP_PROXY case, with dedicated ingress listener.
	}

	return listeners, nil
}

// buildSidecarInboundListeners creates listeners for the server-side (inbound)
// configuration for co-located service proxyInstances.
func (configgen *ConfigGeneratorImpl) buildSidecarInboundListeners(env model.Environment, node model.Proxy,
	proxyInstances []*model.ServiceInstance) []*xdsapi.Listener {

	var listeners []*xdsapi.Listener
	listenerMap := make(map[string]*xdsapi.Listener)
	// inbound connections/requests are redirected to the endpoint address but appear to be sent
	// to the service address.
	for _, instance := range proxyInstances {
		endpoint := instance.Endpoint
		protocol := endpoint.ServicePort.Protocol

		// Local service instances can be accessed through one of three
		// addresses: localhost, endpoint IP, and service
		// VIP. Localhost bypasses the proxy and doesn't need any TCP
		// route config. Endpoint IP is handled below and Service IP is handled
		// by outbound routes.
		// Traffic sent to our service VIP is redirected by remote
		// services' kubeproxy to our specific endpoint IP.
		var listenerType plugin.ListenerProtocol
		listenerOpts := buildListenerOpts{
			env:            env,
			proxy:          node,
			proxyInstances: proxyInstances,
			ip:             endpoint.Address,
			port:           endpoint.Port,
			protocol:       protocol,
		}

		for _, p := range configgen.Plugins {
			if authnPolicy, ok := p.(authn.Plugin); ok {
				if authnPolicy.RequireTLSMultiplexing(env.Mesh, env.IstioConfigStore, instance.Service.Hostname, instance.Endpoint.ServicePort) {
					listenerOpts.tlsMultiplexed = true
					log.Infof("Uses TLS multiplexing for %v %v\n", instance.Service.Hostname.String(), *instance.Endpoint.ServicePort)
				}
			}
		}

		listenerMapKey := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
		if l, exists := listenerMap[listenerMapKey]; exists {
			log.Warnf("Conflicting inbound listeners on %s: previous listener %s", listenerMapKey, l.Name)
			// Skip building listener for the same ip port
			continue
		}
		listenerType = plugin.ModelProtocolToListenerProtocol(protocol)
		switch listenerType {
		case plugin.ListenerProtocolHTTP:
			httpOpts := &httpListenerOpts{
				routeConfig:      configgen.buildSidecarInboundHTTPRouteConfig(env, node, instance),
				rds:              "", // no RDS for inbound traffic
				useRemoteAddress: false,
				direction:        http_conn.INGRESS,
			}
			if listenerOpts.tlsMultiplexed {
				listenerOpts.filterChainOpts = []*filterChainOpts{
					{
						httpOpts:          httpOpts,
						transportProtocol: authn.EnvoyRawBufferMatch,
					}, {
						httpOpts:          httpOpts,
						transportProtocol: authn.EnvoyTLSMatch,
					},
				}
			} else {
				listenerOpts.filterChainOpts = []*filterChainOpts{
					{
						httpOpts: httpOpts,
					},
				}
			}
		case plugin.ListenerProtocolTCP:
			listenerOpts.filterChainOpts = []*filterChainOpts{{
				networkFilters: buildInboundNetworkFilters(instance),
			}}

		default:
			log.Warnf("Unsupported inbound protocol %v for port %#v", protocol, instance.Endpoint.ServicePort)
			continue
		}

		// call plugins
		l := buildListener(listenerOpts)
		mutable := &plugin.MutableObjects{
			Listener:     l,
			FilterChains: make([]plugin.FilterChain, len(l.FilterChains)),
		}
		for _, p := range configgen.Plugins {
			params := &plugin.InputParams{
				ListenerProtocol: listenerType,
				Env:              &env,
				Node:             &node,
				ProxyInstances:   proxyInstances,
				ServiceInstance:  instance,
				Port:             endpoint.ServicePort,
			}
			if err := p.OnInboundListener(params, mutable); err != nil {
				log.Warn(err.Error())
			}
		}
		// Filters are serialized one time into an opaque struct once we have the complete list.
		if err := marshalFilters(mutable.Listener, listenerOpts, mutable.FilterChains); err != nil {
			log.Warna("buildSidecarInboundListeners ", err.Error())
		} else {
			listeners = append(listeners, mutable.Listener)
			listenerMap[listenerMapKey] = mutable.Listener
		}
	}
	return listeners
}

type listenerEntry struct {
	service     *model.Service
	servicePort *model.Port
	listener    *xdsapi.Listener
}

func getClusterName(service *model.Service, servicePort *model.Port) string {
	return model.BuildSubsetKey(model.TrafficDirectionOutbound, "",
		service.Hostname, servicePort.Port)
}

func handleOutboundListenerConflict(service *model.Service, servicePort *model.Port, current listenerEntry, listenerMapKey string) {
	conflictingOutbound.Add(1)
	log.Warnf("buildSidecarOutboundListener: Omitting service %s due to conflict while "+
		"adding to listener %s on %s. Current Protocol: %s, incoming service protocol %s",
		getClusterName(service, servicePort),
		current.listener.Name,
		listenerMapKey,
		current.servicePort.Protocol,
		servicePort.Protocol)
}

// sortServicesByCreationTime sorts the list of services in ascending order by their creation time (if available).
func sortServicesByCreationTime(services []*model.Service) []*model.Service {
	sort.SliceStable(services, func(i, j int) bool {
		return services[i].CreationTime.Before(services[j].CreationTime)
	})
	return services
}

// buildSidecarOutboundListeners generates http and tcp listeners for outbound connections from the service instance
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
func (configgen *ConfigGeneratorImpl) buildSidecarOutboundListeners(env model.Environment, node model.Proxy,
	proxyInstances []*model.ServiceInstance, services []*model.Service) []*xdsapi.Listener {

	// Sort the services in order of creation.
	services = sortServicesByCreationTime(services)

	var proxyLabels model.LabelsCollection
	for _, w := range proxyInstances {
		proxyLabels = append(proxyLabels, w.Labels)
	}

	meshGateway := map[string]bool{model.IstioMeshGateway: true}
	configs := env.VirtualServices(meshGateway)

	var tcpListeners, httpListeners []*xdsapi.Listener
	var currentListener *xdsapi.Listener
	listenerMap := make(map[string]listenerEntry)
	for _, service := range services {
		for _, servicePort := range service.Ports {
			listenAddress := WildcardAddress
			var addresses []string
			var listenerMapKey string
			listenerOpts := buildListenerOpts{
				env:            env,
				proxy:          node,
				proxyInstances: proxyInstances,
				ip:             WildcardAddress,
				port:           servicePort.Port,
				protocol:       servicePort.Protocol,
			}

			currentListener = nil

			switch plugin.ModelProtocolToListenerProtocol(servicePort.Protocol) {
			case plugin.ListenerProtocolHTTP:
				listenerMapKey = fmt.Sprintf("%s:%d", listenAddress, servicePort.Port)
				if current, exists := listenerMap[listenerMapKey]; exists {
					if !current.servicePort.Protocol.IsHTTP() {
						handleOutboundListenerConflict(service, servicePort, current, listenerMapKey)
					}
					// Skip building listener for the same http port
					continue
				}

				operation := http_conn.EGRESS
				useRemoteAddress := false

				listenerOpts.protocol = servicePort.Protocol
				listenerOpts.filterChainOpts = []*filterChainOpts{{
					httpOpts: &httpListenerOpts{
						rds: fmt.Sprintf("%d", servicePort.Port),
						routeConfig: configgen.buildSidecarOutboundHTTPRouteConfig(
							env, node, proxyInstances, services, fmt.Sprintf("%d", servicePort.Port)),
						useRemoteAddress: useRemoteAddress,
						direction:        operation,
					},
				}}
			case plugin.ListenerProtocolTCP:
				if service.Resolution != model.Passthrough {
					listenAddress = service.GetServiceAddressForProxy(&node)
					addresses = []string{listenAddress}
				}

				listenerMapKey = fmt.Sprintf("%s:%d", listenAddress, servicePort.Port)
				currentListener = nil
				if current, exists := listenerMap[listenerMapKey]; exists {
					currentListener = current.listener
					// Check for port collisions between TCP/TLS and HTTP.
					// If configured correctly, TCP/TLS ports may not collide.
					// We'll need to do additional work to find out if there is a collision within TCP/TLS.
					if !current.servicePort.Protocol.IsTCP() {
						handleOutboundListenerConflict(service, servicePort, current, listenerMapKey)
						continue
					}
				}

				listenerOpts.filterChainOpts = buildOutboundTCPFilterChainOpts(env, configs, addresses, service, servicePort, proxyLabels, meshGateway)
			default:
				// UDP or other protocols: no need to log, it's too noisy
				continue
			}

			// Even if we have a non empty current listener, lets build the new listener with the filter chains
			// In the end, we will merge the filter chains

			// call plugins
			listenerOpts.ip = listenAddress
			l := buildListener(listenerOpts)
			mutable := &plugin.MutableObjects{
				Listener:     l,
				FilterChains: make([]plugin.FilterChain, len(l.FilterChains)),
			}

			for _, p := range configgen.Plugins {
				params := &plugin.InputParams{
					ListenerProtocol: plugin.ModelProtocolToListenerProtocol(servicePort.Protocol),
					Env:              &env,
					Node:             &node,
					ProxyInstances:   proxyInstances,
					Service:          service,
					Port:             servicePort,
				}

				if err := p.OnOutboundListener(params, mutable); err != nil {
					log.Warn(err.Error())
				}
			}

			// Filters are serialized one time into an opaque struct once we have the complete list.
			if err := marshalFilters(mutable.Listener, listenerOpts, mutable.FilterChains); err != nil {
				log.Warna("buildSidecarOutboundListeners: ", err.Error())
				continue
			}

			if currentListener != nil {
				// merge the newly built listener with the existing listener
				newFilterChains := make([]listener.FilterChain, 0, len(currentListener.FilterChains)+len(mutable.Listener.FilterChains))
				newFilterChains = append(newFilterChains, currentListener.FilterChains...)
				newFilterChains = append(newFilterChains, mutable.Listener.FilterChains...)
				currentListener.FilterChains = newFilterChains
			} else {
				listenerMap[listenerMapKey] = listenerEntry{
					service:     service,
					servicePort: servicePort,
					listener:    mutable.Listener,
				}
			}

			if log.DebugEnabled() && len(mutable.Listener.FilterChains) > 1 || currentListener != nil {
				var numChains int
				if currentListener != nil {
					numChains = len(currentListener.FilterChains)
				} else {
					numChains = len(mutable.Listener.FilterChains)
				}
				log.Debugf("buildSidecarOutboundListeners: multiple filter chain listener %s with %d chains", mutable.Listener.Name, numChains)
			}
		}
	}

	for name, l := range listenerMap {
		if err := l.listener.Validate(); err != nil {
			log.Warnf("buildSidecarOutboundListeners: error validating listener %s (type %v): %v", name, l.servicePort.Protocol, err)
			invalidOutboundListeners.Add(1)
			continue
		}
		if l.servicePort.Protocol.IsTCP() {
			tcpListeners = append(tcpListeners, l.listener)
		} else {
			httpListeners = append(httpListeners, l.listener)
		}
	}

	listeners := append(tcpListeners, httpListeners...)

	// trim conflicting filter chains
	// If there are two headless services on the same port, this loop will
	// detect it and remove one of the listeners. This is fine because headless
	// services (resolution NONE) are typically established with original dst clusters
	// So, even though there is only one listener shared across two headless services,
	// traffic will continue to go to the requested destination. The only problem here
	// is that stats will end up being attributed to the incorrect service.
	for _, l := range listeners {
		filterChainMatches := make(map[string]bool)

		trimmedFilterChains := make([]listener.FilterChain, 0, len(l.FilterChains))
		for _, filterChain := range l.FilterChains {
			key := "" // for filter chains without matches or SNI domains
			if filterChain.FilterChainMatch != nil {
				sniDomains := make([]string, len(filterChain.FilterChainMatch.ServerNames))
				copy(sniDomains, filterChain.FilterChainMatch.ServerNames)
				sort.Strings(sniDomains)
				key = strings.Join(sniDomains, ",") // sni domains is the only thing set in FilterChainMatch right now
			}
			if !filterChainMatches[key] {
				trimmedFilterChains = append(trimmedFilterChains, filterChain)
				filterChainMatches[key] = true
			} else {
				log.Warnf("omitting filterchain with duplicate filterchainmatch: %v", key)
				filterChainsConflict.Add(1)
			}
		}
		l.FilterChains = trimmedFilterChains
	}

	return listeners
}

// buildSidecarInboundMgmtListeners creates inbound TCP only listeners for the management ports on
// server (inbound). Management port listeners are slightly different from standard Inbound listeners
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
func buildSidecarInboundMgmtListeners(managementPorts model.PortList, managementIP string) []*xdsapi.Listener {
	listeners := make([]*xdsapi.Listener, 0, len(managementPorts))

	if managementIP == "" {
		managementIP = "127.0.0.1"
	}

	// assumes that inbound connections/requests are sent to the endpoint address
	for _, mPort := range managementPorts {
		switch mPort.Protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC, model.ProtocolTCP,
			model.ProtocolHTTPS, model.ProtocolTLS, model.ProtocolMongo, model.ProtocolRedis:

			instance := &model.ServiceInstance{
				Endpoint: model.NetworkEndpoint{
					Address:     managementIP,
					Port:        mPort.Port,
					ServicePort: mPort,
				},
				Service: &model.Service{
					Hostname: ManagementClusterHostname,
				},
			}
			listenerOpts := buildListenerOpts{
				ip:       managementIP,
				port:     mPort.Port,
				protocol: model.ProtocolTCP,
				filterChainOpts: []*filterChainOpts{{
					networkFilters: buildInboundNetworkFilters(instance),
				}},
			}
			l := buildListener(listenerOpts)
			// TODO: should we call plugins for the admin port listeners too? We do everywhere else we contruct listeners.
			if err := marshalFilters(l, listenerOpts, []plugin.FilterChain{{}}); err != nil {
				log.Warna("buildSidecarInboundMgmtListeners ", err.Error())
			} else {
				listeners = append(listeners, l)
			}
		default:
			log.Warnf("Unsupported inbound protocol %v for management port %#v",
				mPort.Protocol, mPort)
		}
	}

	return listeners
}

// httpListenerOpts are options for an HTTP listener
type httpListenerOpts struct {
	//nolint: maligned
	routeConfig      *xdsapi.RouteConfiguration
	rds              string
	useRemoteAddress bool
	direction        http_conn.HttpConnectionManager_Tracing_OperationName
	// If set, use this as a basis
	connectionManager *http_conn.HttpConnectionManager
	// stat prefix for the http connection manager
	// DO not set this field. Will be overridden by marshalFilters
	statPrefix string
}

// filterChainOpts describes a filter chain: a set of filters with the same TLS context
type filterChainOpts struct {
	sniHosts          []string
	transportProtocol string
	tlsContext        *auth.DownstreamTlsContext
	httpOpts          *httpListenerOpts
	networkFilters    []listener.Filter
}

// buildListenerOpts are the options required to build a Listener
type buildListenerOpts struct {
	// nolint: maligned
	env             model.Environment
	proxy           model.Proxy
	proxyInstances  []*model.ServiceInstance
	ip              string
	port            int
	protocol        model.Protocol
	bindToPort      bool
	filterChainOpts []*filterChainOpts
	tlsMultiplexed  bool
}

func buildHTTPConnectionManager(env model.Environment, httpOpts *httpListenerOpts, httpFilters []*http_conn.HttpFilter) *http_conn.HttpConnectionManager {
	filters := append(httpFilters,
		&http_conn.HttpFilter{Name: xdsutil.CORS},
		&http_conn.HttpFilter{Name: xdsutil.Fault},
		&http_conn.HttpFilter{Name: xdsutil.Router},
	)

	if httpOpts.connectionManager == nil {
		httpOpts.connectionManager = &http_conn.HttpConnectionManager{}
	}

	connectionManager := httpOpts.connectionManager
	connectionManager.CodecType = http_conn.AUTO
	connectionManager.AccessLog = []*accesslog.AccessLog{}
	connectionManager.HttpFilters = filters
	connectionManager.StatPrefix = httpOpts.statPrefix
	connectionManager.UseRemoteAddress = &google_protobuf.BoolValue{httpOpts.useRemoteAddress}

	if httpOpts.rds != "" {
		rds := &http_conn.HttpConnectionManager_Rds{
			Rds: &http_conn.Rds{
				ConfigSource: core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
				},
				RouteConfigName: httpOpts.rds,
			},
		}
		connectionManager.RouteSpecifier = rds
	} else {
		connectionManager.RouteSpecifier = &http_conn.HttpConnectionManager_RouteConfig{RouteConfig: httpOpts.routeConfig}
	}

	if env.Mesh.AccessLogFile != "" {
		fl := &fileaccesslog.FileAccessLog{
			Path: env.Mesh.AccessLogFile,
		}

		connectionManager.AccessLog = []*accesslog.AccessLog{
			{
				Config: util.MessageToStruct(fl),
				Name:   fileAccessLog,
			},
		}
	}

	if env.Mesh.EnableTracing {
		tc := model.GetTraceConfig()
		connectionManager.Tracing = &http_conn.HttpConnectionManager_Tracing{
			OperationName: httpOpts.direction,
			ClientSampling: &envoy_type.Percent{
				Value: tc.ClientSampling,
			},
			RandomSampling: &envoy_type.Percent{
				Value: tc.RandomSampling,
			},
			OverallSampling: &envoy_type.Percent{
				Value: tc.OverallSampling,
			},
		}
		connectionManager.GenerateRequestId = &google_protobuf.BoolValue{true}
	}

	if verboseDebug {
		connectionManagerJSON, _ := json.MarshalIndent(connectionManager, "  ", "  ")
		log.Infof("LDS: %s \n", string(connectionManagerJSON))
	}
	return connectionManager
}

// buildListener builds and initializes a Listener proto based on the provided opts. It does not set any filters.
func buildListener(opts buildListenerOpts) *xdsapi.Listener {
	filterChains := make([]listener.FilterChain, 0, len(opts.filterChainOpts))

	var listenerFilters []listener.ListenerFilter
	if opts.tlsMultiplexed {
		listenerFilters = []listener.ListenerFilter{
			{
				Name:   authn.EnvoyTLSInspectorFilterName,
				Config: &google_protobuf.Struct{},
			},
		}
	}

	for _, chain := range opts.filterChainOpts {
		match := &listener.FilterChainMatch{
			TransportProtocol: chain.transportProtocol,
		}
		if len(chain.sniHosts) > 0 {
			fullWildcardFound := false
			for _, h := range chain.sniHosts {
				if h == "*" {
					fullWildcardFound = true
					// If we have a host with *, it effectively means match anything, i.e.
					// no SNI based matching for this host.
					break
				}
			}
			if !fullWildcardFound {
				match.ServerNames = chain.sniHosts
			}
		}
		if reflect.DeepEqual(*match, listener.FilterChainMatch{}) {
			match = nil
		}
		filterChains = append(filterChains, listener.FilterChain{
			FilterChainMatch: match,
			TlsContext:       chain.tlsContext,
		})
	}

	var deprecatedV1 *xdsapi.Listener_DeprecatedV1
	if !opts.bindToPort {
		deprecatedV1 = &xdsapi.Listener_DeprecatedV1{
			BindToPort: boolFalse,
		}
	}

	return &xdsapi.Listener{
		Name:            fmt.Sprintf("%s_%d", opts.ip, opts.port),
		Address:         util.BuildAddress(opts.ip, uint32(opts.port)),
		ListenerFilters: listenerFilters,
		FilterChains:    filterChains,
		DeprecatedV1:    deprecatedV1,
	}
}

// marshalFilters adds the provided TCP and HTTP filters to the provided Listener and serializes them.
//
// TODO: should we change this from []plugins.FilterChains to [][]listener.Filter, [][]*http_conn.HttpFilter?
// TODO: given how tightly tied listener.FilterChains, opts.filterChainOpts, and mutable.FilterChains are to eachother
// we should encapsulate them some way to ensure they remain consistent (mainly that in each an index refers to the same
// chain)
func marshalFilters(l *xdsapi.Listener, opts buildListenerOpts, chains []plugin.FilterChain) error {
	if len(opts.filterChainOpts) == 0 {
		return fmt.Errorf("must have more than 0 chains in listener: %#v", l)
	}

	for i, chain := range chains {
		opt := opts.filterChainOpts[i]

		if len(chain.TCP) > 0 {
			l.FilterChains[i].Filters = append(l.FilterChains[i].Filters, chain.TCP...)
		}

		if len(opt.networkFilters) > 0 {
			l.FilterChains[i].Filters = append(l.FilterChains[i].Filters, opt.networkFilters...)
		}

		if log.DebugEnabled() {
			log.Debugf("attached %d network filters to listener %q filter chain %d", len(chain.TCP)+len(opt.networkFilters), l.Name, i)
		}

		if opt.httpOpts != nil {
			opt.httpOpts.statPrefix = l.Name
			connectionManager := buildHTTPConnectionManager(opts.env, opt.httpOpts, chain.HTTP)
			l.FilterChains[i].Filters = append(l.FilterChains[i].Filters, listener.Filter{
				Name:   envoyHTTPConnectionManager,
				Config: util.MessageToStruct(connectionManager),
			})
			log.Debugf("attached HTTP filter with %d http_filter options to listener %q filter chain %d", 1+len(chain.HTTP), l.Name, i)
		}
	}
	return nil
}
