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
	"time"

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
	prometheus.MustRegister(invalidOutboundListeners)
	prometheus.MustRegister(filterChainsConflict)
}

// ListenersALPNProtocols denotes the the list of ALPN protocols that the listener
// should expose
var ListenersALPNProtocols = []string{"h2", "http/1.1"}

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func (configgen *ConfigGeneratorImpl) BuildListeners(env *model.Environment, node *model.Proxy, push *model.PushContext) ([]*xdsapi.Listener, error) {
	switch node.Type {
	case model.Sidecar:
		return configgen.buildSidecarListeners(env, node, push)
	case model.Router, model.Ingress:
		return configgen.buildGatewayListeners(env, node, push)
	}
	return nil, nil
}

// buildSidecarListeners produces a list of listeners for sidecar proxies
func (configgen *ConfigGeneratorImpl) buildSidecarListeners(env *model.Environment, node *model.Proxy,
	push *model.PushContext) ([]*xdsapi.Listener, error) {

	mesh := env.Mesh
	managementPorts := env.ManagementPorts(node.IPAddress)

	proxyInstances, err := env.GetProxyServiceInstances(node)
	if err != nil {
		return nil, err
	}

	services := push.Services

	listeners := make([]*xdsapi.Listener, 0)

	if mesh.ProxyListenPort > 0 {
		inbound := configgen.buildSidecarInboundListeners(env, node, push, proxyInstances)
		outbound := configgen.buildSidecarOutboundListeners(env, node, push, proxyInstances, services)

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
			transparent = &google_protobuf.BoolValue{Value: true}
		}

		// add an extra listener that binds to the port that is the recipient of the iptables redirect
		listeners = append(listeners, &xdsapi.Listener{
			Name:           VirtualListenerName,
			Address:        util.BuildAddress(WildcardAddress, uint32(mesh.ProxyListenPort)),
			Transparent:    transparent,
			UseOriginalDst: &google_protobuf.BoolValue{Value: true},
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
func (configgen *ConfigGeneratorImpl) buildSidecarInboundListeners(env *model.Environment, node *model.Proxy, push *model.PushContext,
	proxyInstances []*model.ServiceInstance) []*xdsapi.Listener {

	var listeners []*xdsapi.Listener
	listenerMap := make(map[string]*model.ServiceInstance)
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

		listenerMapKey := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
		if old, exists := listenerMap[listenerMapKey]; exists {
			push.Add(model.ProxyStatusConflictInboundListener, node.ID, node,
				fmt.Sprintf("Rejected %s, used %s for %s", instance.Service.Hostname, old.Service.Hostname, listenerMapKey))
			// Skip building listener for the same ip port
			continue
		}
		listenerType = plugin.ModelProtocolToListenerProtocol(protocol)
		allChains := []plugin.FilterChain{}
		var httpOpts *httpListenerOpts
		var tcpNetworkFilters []listener.Filter
		switch listenerType {
		case plugin.ListenerProtocolHTTP:
			httpOpts = &httpListenerOpts{
				routeConfig:      configgen.buildSidecarInboundHTTPRouteConfig(env, node, push, instance),
				rds:              "", // no RDS for inbound traffic
				useRemoteAddress: false,
				direction:        http_conn.INGRESS,
			}
		case plugin.ListenerProtocolTCP:
			tcpNetworkFilters = buildInboundNetworkFilters(instance)

		default:
			log.Warnf("Unsupported inbound protocol %v for port %#v", protocol, instance.Endpoint.ServicePort)
			continue
		}
		for _, p := range configgen.Plugins {
			params := &plugin.InputParams{
				ListenerProtocol: listenerType,
				Env:              env,
				Node:             node,
				ProxyInstances:   proxyInstances,
				ServiceInstance:  instance,
				Port:             endpoint.ServicePort,
			}
			chains := p.OnInboundFilterChains(params)
			if len(chains) == 0 {
				continue
			}
			if len(allChains) != 0 {
				log.Warnf("Found two plugin setups inbound filter chains for listeners, FilterChainMatch may not work as intended!")
			}
			allChains = append(allChains, chains...)
		}
		// Construct the default filter chain.
		if len(allChains) == 0 {
			log.Infof("Use default filter chain for %v", endpoint)
			allChains = []plugin.FilterChain{plugin.FilterChain{}}
		}
		for _, chain := range allChains {
			listenerOpts.filterChainOpts = append(listenerOpts.filterChainOpts, &filterChainOpts{
				httpOpts:        httpOpts,
				networkFilters:  tcpNetworkFilters,
				tlsContext:      chain.TLSContext,
				match:           chain.FilterChainMatch,
				listenerFilters: chain.RequiredListenerFilters,
			})
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
				Env:              env,
				Node:             node,
				ProxyInstances:   proxyInstances,
				ServiceInstance:  instance,
				Port:             endpoint.ServicePort,
				Push:             push,
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
			listenerMap[listenerMapKey] = instance
		}
	}
	return listeners
}

type listenerEntry struct {
	// TODO: Clean this up
	services    []*model.Service
	servicePort *model.Port
	listener    *xdsapi.Listener
}

// sortServicesByCreationTime sorts the list of services in ascending order by their creation time (if available).
func sortServicesByCreationTime(services []*model.Service) []*model.Service {
	sort.SliceStable(services, func(i, j int) bool {
		return services[i].CreationTime.Before(services[j].CreationTime)
	})
	return services
}

func protocolName(p model.Protocol) string {
	switch plugin.ModelProtocolToListenerProtocol(p) {
	case plugin.ListenerProtocolHTTP:
		return "HTTP"
	case plugin.ListenerProtocolTCP:
		return "TCP"
	default:
		return "UNKNOWN"
	}
}

type outboundListenerConflict struct {
	metric          *model.PushMetric
	env             *model.Environment
	node            *model.Proxy
	listenerName    string
	currentProtocol model.Protocol
	currentServices []*model.Service
	newHostname     model.Hostname
	newProtocol     model.Protocol
}

func (c outboundListenerConflict) addMetric(push *model.PushContext) {
	currentHostnames := make([]string, len(c.currentServices))
	for i, s := range c.currentServices {
		currentHostnames[i] = string(s.Hostname)
	}
	concatHostnames := strings.Join(currentHostnames, ",")
	push.Add(c.metric,
		c.listenerName,
		c.node,
		fmt.Sprintf("Listener=%s Accepted%s=%s Rejected%s=%s %sServices=%d",
			c.listenerName,
			protocolName(c.currentProtocol),
			concatHostnames,
			protocolName(c.newProtocol),
			c.newHostname,
			protocolName(c.currentProtocol),
			len(c.currentServices)))
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
func (configgen *ConfigGeneratorImpl) buildSidecarOutboundListeners(env *model.Environment, node *model.Proxy, push *model.PushContext,
	proxyInstances []*model.ServiceInstance, services []*model.Service) []*xdsapi.Listener {

	// Sort the services in order of creation.
	services = sortServicesByCreationTime(services)

	var proxyLabels model.LabelsCollection
	for _, w := range proxyInstances {
		proxyLabels = append(proxyLabels, w.Labels)
	}

	meshGateway := map[string]bool{model.IstioMeshGateway: true}
	configs := push.VirtualServices(meshGateway)

	var tcpListeners, httpListeners []*xdsapi.Listener
	// For conflicit resolution
	var currentListenerEntry *listenerEntry
	listenerMap := make(map[string]*listenerEntry)
	for _, service := range services {
		for _, servicePort := range service.Ports {
			listenAddress := WildcardAddress
			var destinationIPAddress string
			var listenerMapKey string
			listenerOpts := buildListenerOpts{
				env:            env,
				proxy:          node,
				proxyInstances: proxyInstances,
				ip:             WildcardAddress,
				port:           servicePort.Port,
				protocol:       servicePort.Protocol,
			}

			currentListenerEntry = nil

			switch plugin.ModelProtocolToListenerProtocol(servicePort.Protocol) {
			case plugin.ListenerProtocolHTTP:
				listenerMapKey = fmt.Sprintf("%s:%d", listenAddress, servicePort.Port)
				var exists bool
				// Check if this HTTP listener conflicts with an existing wildcard TCP listener
				// i.e. one of NONE resolution type, since we collapse all HTTP listeners into
				// a single 0.0.0.0:port listener and use vhosts to distinguish individual http
				// services in that port
				if currentListenerEntry, exists = listenerMap[listenerMapKey]; exists {
					if !currentListenerEntry.servicePort.Protocol.IsHTTP() {
						outboundListenerConflict{
							metric:          model.ProxyStatusConflictOutboundListenerTCPOverHTTP,
							env:             env,
							node:            node,
							listenerName:    listenerMapKey,
							currentServices: currentListenerEntry.services,
							currentProtocol: currentListenerEntry.servicePort.Protocol,
							newHostname:     service.Hostname,
							newProtocol:     servicePort.Protocol,
						}.addMetric(push)
					}
					// Skip building listener for the same http port
					currentListenerEntry.services = append(currentListenerEntry.services, service)
					continue
				}

				operation := http_conn.EGRESS
				useRemoteAddress := false

				listenerOpts.protocol = servicePort.Protocol
				listenerOpts.filterChainOpts = []*filterChainOpts{{
					httpOpts: &httpListenerOpts{
						rds:              fmt.Sprintf("%d", servicePort.Port),
						useRemoteAddress: useRemoteAddress,
						direction:        operation,
					},
				}}
			case plugin.ListenerProtocolTCP:
				// Determine the listener address
				// we listen on the service VIP if and only
				// if the address is an IP address. If its a CIDR, we listen on
				// 0.0.0.0, and setup a filter chain match for the CIDR range.
				// As a small optimization, CIDRs with /32 prefix will be converted
				// into listener address so that there is a dedicated listener for this
				// ip:port. This will reduce the impact of a listener reload

				var svcListenAddress string
				// This is to maintain backward compatibility with 0.8 envoy
				if _, is10Proxy := node.GetProxyVersion(); !is10Proxy {
					if service.Resolution != model.Passthrough {
						svcListenAddress = service.GetServiceAddressForProxy(node)
					}
				} else {
					svcListenAddress = service.GetServiceAddressForProxy(node)
				}

				// We should never get an empty address.
				// This is a safety guard, in case some platform adapter isn't doing things
				// properly
				if len(svcListenAddress) > 0 {
					if !strings.Contains(svcListenAddress, "/") {
						listenAddress = svcListenAddress
					} else {
						// Address is a CIDR. Fall back to 0.0.0.0 and
						// filter chain match
						destinationIPAddress = svcListenAddress
					}
				}

				listenerMapKey = fmt.Sprintf("%s:%d", listenAddress, servicePort.Port)
				var exists bool
				// Check if this TCP listener conflicts with an existing HTTP listener on 0.0.0.0:Port
				if currentListenerEntry, exists = listenerMap[listenerMapKey]; exists {
					// Check for port collisions between TCP/TLS and HTTP.
					// If configured correctly, TCP/TLS ports may not collide.
					// We'll need to do additional work to find out if there is a collision within TCP/TLS.
					if !currentListenerEntry.servicePort.Protocol.IsTCP() {
						outboundListenerConflict{
							metric:          model.ProxyStatusConflictOutboundListenerHTTPOverTCP,
							env:             env,
							node:            node,
							listenerName:    listenerMapKey,
							currentServices: currentListenerEntry.services,
							currentProtocol: currentListenerEntry.servicePort.Protocol,
							newHostname:     service.Hostname,
							newProtocol:     servicePort.Protocol,
						}.addMetric(push)
						continue
					}
					// WE have a collision with another TCP port.
					// This can happen only if the service is listening on 0.0.0.0:<port>
					// which is the case for headless services, or non-k8s services that do not have a VIP.
					// Unfortunately we won't know if this is a real conflict or not
					// until we process the VirtualServices, etc.
					// The conflict resolution is done later in this code
				}

				listenerOpts.filterChainOpts = buildSidecarOutboundTCPTLSFilterChainOpts(node, push, configs,
					destinationIPAddress, service, servicePort, proxyLabels, meshGateway)
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
					Env:              env,
					Node:             node,
					ProxyInstances:   proxyInstances,
					Service:          service,
					Port:             servicePort,
					Push:             push,
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

			// TODO(rshriram) merge multiple identical filter chains with just a single destination CIDR based
			// filter chain matche, into a single filter chain and array of destinationcidr matches

			// We checked TCP over HTTP, and HTTP over TCP conflicts above.
			// The code below checks for TCP over TCP conflicts and merges listeners
			if currentListenerEntry != nil {
				// merge the newly built listener with the existing listener
				// if and only if the filter chains have distinct conditions
				// Extract the current filter chain matches
				// For every new filter chain match being added, check if any previous match is same
				// if so, skip adding this filter chain with a warning
				// This is very unoptimized.
				newFilterChains := make([]listener.FilterChain, 0,
					len(currentListenerEntry.listener.FilterChains)+len(mutable.Listener.FilterChains))
				newFilterChains = append(newFilterChains, currentListenerEntry.listener.FilterChains...)
				for _, incomingFilterChain := range mutable.Listener.FilterChains {
					conflictFound := false

				compareWithExisting:
					for _, existingFilterChain := range currentListenerEntry.listener.FilterChains {
						if existingFilterChain.FilterChainMatch == nil {
							// This is a catch all filter chain.
							// We can only merge with a non-catch all filter chain
							// Else mark it as conflict
							if incomingFilterChain.FilterChainMatch == nil {
								conflictFound = true
								outboundListenerConflict{
									metric:          model.ProxyStatusConflictOutboundListenerTCPOverTCP,
									env:             env,
									node:            node,
									listenerName:    listenerMapKey,
									currentServices: currentListenerEntry.services,
									currentProtocol: currentListenerEntry.servicePort.Protocol,
									newHostname:     service.Hostname,
									newProtocol:     servicePort.Protocol,
								}.addMetric(push)
								break compareWithExisting
							} else {
								continue
							}
						}
						if incomingFilterChain.FilterChainMatch == nil {
							continue
						}

						// We have two non-catch all filter chains. Check for duplicates
						if reflect.DeepEqual(*existingFilterChain.FilterChainMatch, *incomingFilterChain.FilterChainMatch) {
							conflictFound = true
							outboundListenerConflict{
								metric:          model.ProxyStatusConflictOutboundListenerTCPOverTCP,
								env:             env,
								node:            node,
								listenerName:    listenerMapKey,
								currentServices: currentListenerEntry.services,
								currentProtocol: currentListenerEntry.servicePort.Protocol,
								newHostname:     service.Hostname,
								newProtocol:     servicePort.Protocol,
							}.addMetric(push)
							break compareWithExisting
						}
					}

					if !conflictFound {
						// There is no conflict with any filter chain in the existing listener.
						// So append the new filter chains to the existing listener's filter chains
						newFilterChains = append(newFilterChains, incomingFilterChain)
						lEntry := listenerMap[listenerMapKey]
						lEntry.services = append(lEntry.services, service)
					}
				}
				currentListenerEntry.listener.FilterChains = newFilterChains
			} else {
				listenerMap[listenerMapKey] = &listenerEntry{
					services:    []*model.Service{service},
					servicePort: servicePort,
					listener:    mutable.Listener,
				}
			}

			if log.DebugEnabled() && len(mutable.Listener.FilterChains) > 1 || currentListenerEntry != nil {
				var numChains int
				if currentListenerEntry != nil {
					numChains = len(currentListenerEntry.listener.FilterChains)
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

	return append(tcpListeners, httpListeners...)
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
	sniHosts         []string
	destinationCIDRs []string
	tlsContext       *auth.DownstreamTlsContext
	httpOpts         *httpListenerOpts
	match            *listener.FilterChainMatch
	listenerFilters  []listener.ListenerFilter
	networkFilters   []listener.Filter
}

// buildListenerOpts are the options required to build a Listener
type buildListenerOpts struct {
	// nolint: maligned
	env             *model.Environment
	proxy           *model.Proxy
	proxyInstances  []*model.ServiceInstance
	ip              string
	port            int
	protocol        model.Protocol
	bindToPort      bool
	filterChainOpts []*filterChainOpts
}

func buildHTTPConnectionManager(env *model.Environment, node *model.Proxy, httpOpts *httpListenerOpts,
	httpFilters []*http_conn.HttpFilter) *http_conn.HttpConnectionManager {
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
	connectionManager.UseRemoteAddress = &google_protobuf.BoolValue{Value: httpOpts.useRemoteAddress}

	if _, is10Proxy := node.GetProxyVersion(); is10Proxy {
		// Allow websocket upgrades
		websocketUpgrade := &http_conn.HttpConnectionManager_UpgradeConfig{UpgradeType: "websocket"}
		connectionManager.UpgradeConfigs = []*http_conn.HttpConnectionManager_UpgradeConfig{websocketUpgrade}
		notimeout := 0 * time.Second
		// Setting IdleTimeout to 0 seems to break most tests, causing
		// envoy to disconnect.
		// connectionManager.IdleTimeout = &notimeout
		connectionManager.StreamIdleTimeout = &notimeout
	}

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
		connectionManager.GenerateRequestId = &google_protobuf.BoolValue{Value: true}
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

	// TODO(incfly): consider changing this to map to handle duplicated listener filters from different chains?
	var listenerFilters []listener.ListenerFilter

	for _, chain := range opts.filterChainOpts {
		listenerFilters = append(listenerFilters, chain.listenerFilters...)
		match := &listener.FilterChainMatch{}
		needMatch := false
		if chain.match != nil {
			needMatch = true
			match = chain.match
		}
		if len(chain.sniHosts) > 0 {
			sort.Strings(chain.sniHosts)
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
		if len(chain.destinationCIDRs) > 0 {
			sort.Strings(chain.destinationCIDRs)
			for _, d := range chain.destinationCIDRs {
				if len(d) == 0 {
					continue
				}
				cidr := util.ConvertAddressToCidr(d)
				if cidr != nil && cidr.AddressPrefix != model.UnspecifiedIP {
					match.PrefixRanges = append(match.PrefixRanges, cidr)
				}
			}
		}

		if !needMatch && reflect.DeepEqual(*match, listener.FilterChainMatch{}) {
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
			connectionManager := buildHTTPConnectionManager(opts.env, opts.proxy, opt.httpOpts, chain.HTTP)
			l.FilterChains[i].Filters = append(l.FilterChains[i].Filters, listener.Filter{
				Name:   envoyHTTPConnectionManager,
				Config: util.MessageToStruct(connectionManager),
			})
			log.Debugf("attached HTTP filter with %d http_filter options to listener %q filter chain %d", 1+len(chain.HTTP), l.Name, i)
		}
	}
	return nil
}
