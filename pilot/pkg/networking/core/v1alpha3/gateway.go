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

package v1alpha3

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	statefulsession "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/stateful_session/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/tunnelingconfig"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	alifeatures "istio.io/istio/pkg/ali/features"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/gateway"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/util/hash"
	"istio.io/istio/pkg/util/istiomultierror"
	"istio.io/istio/pkg/util/sets"
)

type mutableListenerOpts struct {
	mutable   *MutableGatewayListener
	opts      *gatewayListenerOpts
	transport istionetworking.TransportProtocol
}

// MutableGatewayListener represents a listener that is being built.
// Historically, this was used for all listener building. At this point, outbound and inbound have specialized code.
// This only applies to gateways now.
type MutableGatewayListener struct {
	istionetworking.MutableObjects
}

// build adds the provided TCP and HTTP filters to the provided Listener and serializes them.
// TODO: given how tightly tied listener.FilterChains, opts.filterChainOpts, and mutable.FilterChains
// are to each other we should encapsulate them some way to ensure they remain consistent (mainly that
// in each an index refers to the same chain).
func (ml *MutableGatewayListener) build(builder *ListenerBuilder, opts gatewayListenerOpts) error {
	if len(opts.filterChainOpts) == 0 {
		return fmt.Errorf("must have more than 0 chains in listener %q", ml.Listener.Name)
	}
	httpConnectionManagers := make([]*hcm.HttpConnectionManager, len(ml.FilterChains))
	for i := range ml.FilterChains {
		chain := ml.FilterChains[i]
		opt := opts.filterChainOpts[i]
		ml.Listener.FilterChains[i].Metadata = opt.metadata
		if opt.httpOpts == nil {
			// we are building a network filter chain (no http connection manager) for this filter chain
			// In HTTP, we need to have RBAC, etc. upfront so that they can enforce policies immediately
			// For network filters such as mysql, mongo, etc., we need the filter codec upfront. Data from this
			// codec is used by RBAC later.
			//
			// Currently, when transport is QUIC we assume HTTP3. So it should not come here.
			// When other protocols are used over QUIC, we have to revisit this assumption.

			if len(opt.networkFilters) > 0 {
				// this is the terminating filter
				lastNetworkFilter := opt.networkFilters[len(opt.networkFilters)-1]

				for n := 0; n < len(opt.networkFilters)-1; n++ {
					ml.Listener.FilterChains[i].Filters = append(ml.Listener.FilterChains[i].Filters, opt.networkFilters[n])
				}
				ml.Listener.FilterChains[i].Filters = append(ml.Listener.FilterChains[i].Filters, chain.TCP...)
				ml.Listener.FilterChains[i].Filters = append(ml.Listener.FilterChains[i].Filters, lastNetworkFilter)
			} else {
				ml.Listener.FilterChains[i].Filters = append(ml.Listener.FilterChains[i].Filters, chain.TCP...)
			}
			log.Debugf("attached %d network filters to listener %q filter chain %d", len(chain.TCP)+len(opt.networkFilters), ml.Listener.Name, i)
		} else {
			// Add the TCP filters first.. and then the HTTP connection manager.
			// Skip adding this if transport is not TCP (could be QUIC)
			if chain.TransportProtocol == istionetworking.TransportProtocolTCP {
				ml.Listener.FilterChains[i].Filters = append(ml.Listener.FilterChains[i].Filters, chain.TCP...)
			}

			// If statPrefix has been set before calling this method, respect that.
			if len(opt.httpOpts.statPrefix) == 0 {
				opt.httpOpts.statPrefix = strings.ToLower(ml.Listener.TrafficDirection.String()) + "_" + ml.Listener.Name
			}
			if opts.port != nil {
				opt.httpOpts.port = opts.port.Port
			}
			httpConnectionManagers[i] = builder.buildHTTPConnectionManager(opt.httpOpts)
			filter := &listener.Filter{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(httpConnectionManagers[i])},
			}
			ml.Listener.FilterChains[i].Filters = append(ml.Listener.FilterChains[i].Filters, filter)
			log.Debugf("attached HTTP filter with %d http_filter options to listener %q filter chain %d",
				len(httpConnectionManagers[i].HttpFilters), ml.Listener.Name, i)
		}
	}

	return nil
}

func (configgen *ConfigGeneratorImpl) buildGatewayListeners(builder *ListenerBuilder) *ListenerBuilder {
	if builder.node.MergedGateway == nil {
		log.Debugf("buildGatewayListeners: no gateways for router %v", builder.node.ID)
		return builder
	}

	mergedGateway := builder.node.MergedGateway
	log.Debugf("buildGatewayListeners: gateways after merging: %v", mergedGateway)

	actualWildcards, _ := getWildcardsAndLocalHost(builder.node.GetIPMode())
	errs := istiomultierror.New()
	// Mutable objects keyed by listener name so that we can build listeners at the end.
	mutableopts := make(map[string]mutableListenerOpts)
	proxyConfig := builder.node.Metadata.ProxyConfigOrDefault(builder.push.Mesh.DefaultConfig)
	// listener port -> host/bind
	tlsHostsByPort := map[uint32]map[string]string{}
	for _, port := range mergedGateway.ServerPorts {
		// Skip ports we cannot bind to. Note that MergeGateways will already translate Service port to
		// targetPort, which handles the common case of exposing ports like 80 and 443 but listening on
		// higher numbered ports.
		if builder.node.IsUnprivileged() && port.Number < 1024 {
			log.Warnf("buildGatewayListeners: skipping privileged gateway port %d for node %s as it is an unprivileged pod",
				port.Number, builder.node.ID)
			continue
		}
		var extraBind []string
		bind := actualWildcards[0]
		if features.EnableDualStack && len(actualWildcards) > 1 {
			extraBind = actualWildcards[1:]
		}
		if len(port.Bind) > 0 {
			bind = port.Bind
			extraBind = nil
		}

		// NOTE: There is no gating here to check for the value of the QUIC feature flag. However,
		// they are created in MergeGatways only when the flag is set. So when it is turned off, the
		// MergedQUICTransportServers would be nil so that no listener would be created. It is written this way
		// to make testing a little easier.
		transportToServers := map[istionetworking.TransportProtocol]map[model.ServerPort]*model.MergedServers{
			istionetworking.TransportProtocolTCP:  mergedGateway.MergedServers,
			istionetworking.TransportProtocolQUIC: mergedGateway.MergedQUICTransportServers,
		}

		for transport, gwServers := range transportToServers {
			if gwServers == nil {
				log.Debugf("buildGatewayListeners: no gateway-server for transport %s at port %d", transport.String(), port.Number)
				continue
			}

			needPROXYProtocol := transport != istionetworking.TransportProtocolQUIC &&
				proxyConfig.GatewayTopology != nil &&
				proxyConfig.GatewayTopology.ProxyProtocol != nil

			// on a given port, we can either have plain text HTTP servers or
			// HTTPS/TLS servers with SNI. We cannot have a mix of http and https server on same port.
			// We can also have QUIC on a given port along with HTTPS/TLS on a given port. It does not
			// cause port-conflict as they use different transport protocols
			opts := &gatewayListenerOpts{
				push:              builder.push,
				proxy:             builder.node,
				bind:              bind,
				extraBind:         extraBind,
				port:              &model.Port{Port: int(port.Number)},
				bindToPort:        true,
				needPROXYProtocol: needPROXYProtocol,
			}
			// Added by ingress
			opts.enableProxyProtocol = builder.push.Mesh.MseIngressGlobalConfig.GetEnableProxyProtocol()
			// End added by ingress
			lname := getListenerName(bind, int(port.Number), transport)
			p := protocol.Parse(port.Protocol)
			serversForPort := gwServers[port]
			if serversForPort == nil {
				continue
			}

			var newFilterChains []istionetworking.FilterChain
			switch transport {
			case istionetworking.TransportProtocolTCP:
				newFilterChains = configgen.buildGatewayTCPBasedFilterChains(builder, p, port, opts, serversForPort, proxyConfig, mergedGateway, tlsHostsByPort)
			case istionetworking.TransportProtocolQUIC:
				// Currently, we just assume that QUIC is HTTP/3 although that does not
				// have to be the case (it is just the most common case now, in the future
				// we will support more cases)
				newFilterChains = configgen.buildGatewayHTTP3FilterChains(builder, serversForPort, mergedGateway, proxyConfig, opts)
			}

			for cnum := range newFilterChains {
				if util.IsIstioVersionGE117(builder.node.IstioVersion) {
					newFilterChains[cnum].TCP = append(newFilterChains[cnum].TCP, xdsfilters.IstioNetworkAuthenticationFilter)
				}
				if newFilterChains[cnum].ListenerProtocol == istionetworking.ListenerProtocolTCP {
					newFilterChains[cnum].TCP = append(newFilterChains[cnum].TCP, builder.authzCustomBuilder.BuildTCP()...)
					newFilterChains[cnum].TCP = append(newFilterChains[cnum].TCP, builder.authzBuilder.BuildTCP()...)
				}
			}

			if mopts, exists := mutableopts[lname]; !exists {
				mutable := &MutableGatewayListener{
					MutableObjects: istionetworking.MutableObjects{
						// Note: buildGatewayListener creates filter chains but does not populate the filters in the chain; that's what
						// this is for.
						FilterChains: newFilterChains,
					},
				}
				mutableopts[lname] = mutableListenerOpts{mutable: mutable, opts: opts, transport: transport}
			} else {
				mopts.opts.filterChainOpts = append(mopts.opts.filterChainOpts, opts.filterChainOpts...)
				mopts.mutable.MutableObjects.FilterChains = append(mopts.mutable.MutableObjects.FilterChains, newFilterChains...)
			}
		}
	}
	listeners := make([]*listener.Listener, 0)
	for _, ml := range mutableopts {
		ml.mutable.Listener = buildGatewayListener(*ml.opts, ml.transport)
		log.Debugf("buildGatewayListeners: marshaling listener %q with %d filter chains",
			ml.mutable.Listener.GetName(), len(ml.mutable.Listener.GetFilterChains()))

		// Filters are serialized one time into an opaque struct once we have the complete list.
		if err := ml.mutable.build(builder, *ml.opts); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("gateway omitting listener %q due to: %v", ml.mutable.Listener.Name, err.Error()))
			continue
		}
		listeners = append(listeners, ml.mutable.Listener)
	}
	// We'll try to return any listeners we successfully marshaled; if we have none, we'll emit the error we built up
	err := errs.ErrorOrNil()
	if err != nil {
		// we have some listeners to return, but we also have some errors; log them
		log.Info(err.Error())
	}

	if len(mutableopts) == 0 {
		log.Warnf("gateway has zero listeners for node %v", builder.node.ID)
		return builder
	}

	builder.gatewayListeners = listeners
	return builder
}

func (configgen *ConfigGeneratorImpl) buildGatewayTCPBasedFilterChains(
	builder *ListenerBuilder,
	p protocol.Instance, port model.ServerPort,
	opts *gatewayListenerOpts,
	serversForPort *model.MergedServers,
	proxyConfig *meshconfig.ProxyConfig,
	mergedGateway *model.MergedGateway,
	tlsHostsByPort map[uint32]map[string]string,
) []istionetworking.FilterChain {
	newFilterChains := make([]istionetworking.FilterChain, 0)
	if p.IsHTTP() {
		// We have a list of HTTP servers on this port. Build a single listener for the server port.
		port := &networking.Port{Number: port.Number, Protocol: port.Protocol}
		opts.filterChainOpts = []*filterChainOpts{
			configgen.createGatewayHTTPFilterChainOpts(builder.node, port, nil, serversForPort.RouteName,
				proxyConfig, istionetworking.ListenerProtocolTCP, builder.push, nil),
		}
		newFilterChains = append(newFilterChains, istionetworking.FilterChain{
			ListenerProtocol: istionetworking.ListenerProtocolHTTP,
		})
	} else {
		// build http connection manager with TLS context, for HTTPS servers using simple/mutual TLS
		// build listener with tcp proxy, with or without TLS context, for TCP servers
		//   or TLS servers using simple/mutual/passthrough TLS
		//   or HTTPS servers using passthrough TLS
		// This process typically yields multiple filter chain matches (with SNI) [if TLS is used]
		tcpFilterChainOpts := make([]*filterChainOpts, 0)
		for _, server := range serversForPort.Servers {
			if gateway.IsHTTPSServerWithTLSTermination(server) {
				// Added by ingress
				gatewayConfig := builder.push.GetGatewayByName(mergedGateway.GatewayNameForServer[server])
				log.Debugf("[Listener] Get gatewayConfig %v", gatewayConfig)
				extraOpts := &buildListenerFilterChainExtraOpts{
					gatewayConfig: builder.push.GetGatewayByName(mergedGateway.GatewayNameForServer[server]),
					meshConfig:    builder.push.Mesh,
					proxyConfig:   proxyConfig,
				}
				// End added by ingress

				routeName := mergedGateway.TLSServerInfo[server].RouteName
				// This is a HTTPS server, where we are doing TLS termination. Build a http connection manager with TLS context
				tcpFilterChainOpts = append(tcpFilterChainOpts, configgen.createGatewayHTTPFilterChainOpts(builder.node, server.Port, server,
					routeName, proxyConfig, istionetworking.TransportProtocolTCP, builder.push, extraOpts))
				newFilterChains = append(newFilterChains, istionetworking.FilterChain{
					ListenerProtocol: istionetworking.ListenerProtocolHTTP,
				})
			} else {
				// This is the case of TCP or PASSTHROUGH.
				tcpChainOpts := configgen.createGatewayTCPFilterChainOpts(builder.node, builder.push,
					server, port.Number, mergedGateway.GatewayNameForServer[server], tlsHostsByPort)
				tcpFilterChainOpts = append(tcpFilterChainOpts, tcpChainOpts...)
				for i := 0; i < len(tcpChainOpts); i++ {
					newFilterChains = append(newFilterChains, istionetworking.FilterChain{
						ListenerProtocol: istionetworking.ListenerProtocolTCP,
					})
				}
			}
		}

		opts.filterChainOpts = tcpFilterChainOpts
	}
	return newFilterChains
}

func (configgen *ConfigGeneratorImpl) buildGatewayHTTP3FilterChains(
	builder *ListenerBuilder,
	serversForPort *model.MergedServers,
	mergedGateway *model.MergedGateway,
	proxyConfig *meshconfig.ProxyConfig,
	opts *gatewayListenerOpts,
) []istionetworking.FilterChain {
	newFilterChains := make([]istionetworking.FilterChain, 0)
	quicFilterChainOpts := make([]*filterChainOpts, 0)
	for _, server := range serversForPort.Servers {
		log.Debugf("buildGatewayListeners: creating QUIC filter chain for port %d(%s:%s)",
			server.GetPort().GetNumber(), server.GetPort().GetName(), server.GetPort().GetProtocol())

		// Here it is assumed that this HTTP/3 server is a mirror of an existing HTTPS
		// server. So the same route name would be reused instead of creating new one.
		routeName := mergedGateway.TLSServerInfo[server].RouteName
		quicFilterChainOpts = append(quicFilterChainOpts, configgen.createGatewayHTTPFilterChainOpts(builder.node, server.Port, server,
			routeName, proxyConfig, istionetworking.TransportProtocolQUIC, builder.push, nil))
		newFilterChains = append(newFilterChains, istionetworking.FilterChain{
			// Make sure that this is set to HTTP so that JWT and Authorization
			// filters that are applied to HTTPS are also applied to this chain.
			// Not doing so is a security hole as would allow bypassing auth.
			ListenerProtocol:  istionetworking.ListenerProtocolHTTP,
			TransportProtocol: istionetworking.TransportProtocolQUIC,
		})
	}
	opts.filterChainOpts = quicFilterChainOpts
	return newFilterChains
}

func getListenerName(bind string, port int, transport istionetworking.TransportProtocol) string {
	switch transport {
	case istionetworking.TransportProtocolTCP:
		return bind + "_" + strconv.Itoa(port)
	case istionetworking.TransportProtocolQUIC:
		return "udp_" + bind + "_" + strconv.Itoa(port)
	}
	return "unknown"
}

func buildNameToServiceMapForHTTPRoutes(node *model.Proxy, push *model.PushContext,
	virtualService config.Config,
) map[host.Name]*model.Service {
	vs := virtualService.Spec.(*networking.VirtualService)
	nameToServiceMap := map[host.Name]*model.Service{}

	addService := func(hostname host.Name) {
		if _, exist := nameToServiceMap[hostname]; exist {
			return
		}

		var service *model.Service
		// First, we obtain the service which has the same namespace as virtualService
		s, exist := push.ServiceIndex.HostnameAndNamespace[hostname][virtualService.Namespace]
		if exist {
			// We should check whether the selected service is visible to the proxy node.
			if push.IsServiceVisible(s, node.ConfigNamespace) {
				service = s
			}
		}
		// If we find no service for the namespace of virtualService or the selected service is not visible to the proxy node,
		// we should fallback to pick one service which is visible to the ConfigNamespace of node.
		if service == nil {
			service = push.ServiceForHostname(node, hostname)
		}
		nameToServiceMap[hostname] = service
	}

	for _, httpRoute := range vs.Http {
		if httpRoute.GetMirror() != nil {
			addService(host.Name(httpRoute.GetMirror().GetHost()))
		}

		for _, route := range httpRoute.GetRoute() {
			if route.GetDestination() != nil {
				addService(host.Name(route.GetDestination().GetHost()))
			}

			// Added by ingress
			for _, fallback := range route.FallbackClusters {
				addService(host.Name(fallback.GetHost()))
			}
			// End add by ingress
		}
	}

	return nameToServiceMap
}

// Added by ingress
func (configgen *ConfigGeneratorImpl) BuildScopedRoutes(node *model.Proxy, push *model.PushContext) []*route.ScopedRouteConfiguration {
	if node.MergedGateway == nil {
		log.Warnf("buildScopedRoutes: no gateways for router %v", node.ID)
		return nil
	}
	merged := node.MergedGateway
	var out []*route.ScopedRouteConfiguration
	gatewayVirtualServices := make(map[string][]config.Config)
	serverIterator := func(listenerPort int, mergedServers map[model.ServerPort]*model.MergedServers) sets.String {
		hostSet := sets.New[string]()
		for port, servers := range mergedServers {
			if port.Number != uint32(listenerPort) {
				continue
			}
			for _, server := range servers.Servers {
				gatewayName := merged.GatewayNameForServer[server]

				var virtualServices []config.Config
				var exists bool

				if virtualServices, exists = gatewayVirtualServices[gatewayName]; !exists {
					virtualServices = push.VirtualServicesForGateway(node.ConfigNamespace, gatewayName)
					gatewayVirtualServices[gatewayName] = virtualServices
				}
				for _, virtualService := range virtualServices {
					rule := virtualService.Spec.(*networking.VirtualService)
					if len(rule.Gateways) > 0 {
						if len(rule.Hosts) == 0 {
							hostSet.Insert(constants.GlobalWildcardHost)
							break
						}
						for _, host := range rule.Hosts {
							hostSet.Insert(host)
						}
					}
				}
			}
		}
		return hostSet
	}
	buildPortHostScopedRoute := func(listenerPort model.ServerPort) {
		p := protocol.Parse(listenerPort.Protocol)
		if !p.IsHTTP() && p != protocol.HTTPS {
			return
		}
		port := strconv.Itoa(int(listenerPort.Number))
		hostSet := serverIterator(int(listenerPort.Number), merged.MergedServers).
			Union(serverIterator(int(listenerPort.Number), merged.MergedQUICTransportServers))
		for host, _ := range hostSet {
			portKey := &route.ScopedRouteConfiguration_Key_Fragment{
				Type: &route.ScopedRouteConfiguration_Key_Fragment_StringKey{
					StringKey: port,
				},
			}
			hostKey := &route.ScopedRouteConfiguration_Key_Fragment{
				Type: &route.ScopedRouteConfiguration_Key_Fragment_StringKey{
					StringKey: host,
				},
			}
			name := strings.Join([]string{port, host}, ".")
			out = append(out, &route.ScopedRouteConfiguration{
				OnDemand:               alifeatures.OnDemandRDS,
				Name:                   name,
				RouteConfigurationName: constants.HigressHostRDSNamePrefix + name,
				Key: &route.ScopedRouteConfiguration_Key{
					Fragments: []*route.ScopedRouteConfiguration_Key_Fragment{portKey, hostKey},
				},
			})
		}
	}
	for _, port := range merged.ServerPorts {
		buildPortHostScopedRoute(port)
	}
	return out
}

type virtualServiceContext struct {
	virtualService    config.Config
	server            *networking.Server
	gatewayName       string
	intersectingHosts host.Names
}

func (configgen *ConfigGeneratorImpl) buildHostRDSConfig(
	node *model.Proxy,
	req *model.PushRequest,
	routeName string,
	vsCache map[int][]virtualServiceContext,
	efw *model.EnvoyFilterWrapper,
	efKeys []string,
) (*discovery.Resource, bool) {
	push := req.Push
	var (
		hostRDSPort string
		hostRDSHost string
	)
	portAndHost := strings.SplitN(strings.TrimPrefix(routeName, constants.HigressHostRDSNamePrefix), ".", 2)
	if len(portAndHost) != 2 {
		log.Errorf("Invalid route %s when using Higress hostRDS", routeName)
		return nil, false
	}
	hostRDSPort = portAndHost[0]
	hostRDSHost = portAndHost[1]

	ph := GetProxyHeaders(node, push, istionetworking.ListenerClassGateway)
	merged := node.MergedGateway
	log.Debugf("buildGatewayRoutes: gateways after merging: %v", merged)
	rdsPort, err := strconv.Atoi(hostRDSPort)
	if err != nil {
		log.Errorf("Invalid port %s of route %s when using Higress hostRDS", hostRDSPort, routeName)
		return nil, false
	}

	hostVs := push.VirtualServicesForHost(node, hostRDSHost)

	var httpRoutes []config.Config
	var vsDependent []config.Config

	cacheable := true

	for _, vs := range hostVs {
		vsSpec := vs.Spec.(*networking.VirtualService)
		for _, vsHttpRoute := range vsSpec.Http {
			// check if dynamic port exists, we should not cache RDS
			for _, vsRoute := range vsHttpRoute.Route {
				if vsRoute.Destination.Port == nil {
					cacheable = false
				}
				for _, fallbackDestination := range vsRoute.FallbackClusters {
					if fallbackDestination.Port == nil {
						cacheable = false
					}
				}
			}
			if vsHttpRoute.Mirror != nil && vsHttpRoute.Mirror.Port == nil {
				cacheable = false
			}
			if vsHttpRoute.Delegate != nil {
				vsDependent = append(vsDependent, config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.VirtualService,
						Name:             vsHttpRoute.Delegate.Name,
						Namespace:        vsHttpRoute.Delegate.Namespace,
					},
					Spec: networking.VirtualService{},
				})

			}
		}
		vsDependent = append(vsDependent, config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             vs.Name,
				Namespace:        vs.Namespace,
			},
			Spec: vs.Spec,
		})
		if len(vs.Annotations) == 0 {
			continue
		}
		if originName, ok := vs.Annotations[constants.MSEOriginName]; ok && originName != vs.Name {
			vsDependent = append(vsDependent, config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             originName,
					Namespace:        vs.Namespace,
				},
				Spec: vs.Spec,
			})
		}
		if parents, ok := vs.Annotations[constants.InternalParentNames]; ok {
			typeNames := strings.Split(parents, ",")
			for _, typeName := range typeNames {
				if !strings.HasPrefix(typeName, "HTTPRoute/") {
					continue
				}
				nsNameStr := strings.TrimPrefix(typeName, "HTTPRoute/")
				nsName := strings.SplitN(nsNameStr, ".", 2)
				if len(nsName) != 2 {
					continue
				}
				httpRoutes = append(httpRoutes, config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.HTTPRoute,
						Name:             nsName[0],
						Namespace:        nsName[1],
					},
				})
			}
		}
	}

	routeCache := &istio_route.Cache{
		RouteName:    routeName,
		ProxyVersion: node.Metadata.IstioVersion,
		ListenerPort: rdsPort,
		// Use same host vs to cache, although the cache can be cleared when the port is different, this can be accepted
		VirtualServices: vsDependent,
		HTTPRoutes:      httpRoutes,
		EnvoyFilterKeys: efKeys,
	}

	var resource *discovery.Resource
	if cacheable {
		resource = configgen.Cache.Get(routeCache)
		if resource != nil {
			return resource, true
		}
	} else {
		log.Warnf("route cache is disabled for RDS:%s", routeName)
	}

	listenerPort := uint32(rdsPort)

	isH3DiscoveryNeeded := false

	// When this is true, we add alt-svc header to the response to tell the client
	// that HTTP/3 over QUIC is available on the same port for this host. This is
	// very important for discovering HTTP/3 services
	for port, servers := range merged.MergedQUICTransportServers {
		if port.Number == listenerPort && len(servers.Servers) > 0 {
			isH3DiscoveryNeeded = true
			break
		}
	}
	gatewayRoutes := make(map[string]map[string][]*route.Route)
	gatewayVirtualServices := make(map[string][]config.Config)
	var listenerVirtualServices []virtualServiceContext
	var selectedVirtualServices []virtualServiceContext
	var vHost *route.VirtualHost
	serverIterator := func(mergedServers map[model.ServerPort]*model.MergedServers) {
		for port, servers := range mergedServers {
			if port.Number != listenerPort {
				continue
			}
			for _, server := range servers.Servers {
				gatewayName := merged.GatewayNameForServer[server]

				var virtualServices []config.Config
				var exists bool

				if virtualServices, exists = gatewayVirtualServices[gatewayName]; !exists {
					virtualServices = push.VirtualServicesForGateway(node.ConfigNamespace, gatewayName)
					gatewayVirtualServices[gatewayName] = virtualServices
				}
				for _, virtualService := range virtualServices {
					virtualServiceHosts := host.NewNames(virtualService.Spec.(*networking.VirtualService).Hosts)
					serverHosts := host.NamesForNamespace(server.Hosts, virtualService.Namespace)

					// We have two cases here:
					// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and server hosts are ns/*.foo.com
					// 2. virtualService hosts are *.foo.com, and server hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
					intersectingHosts := serverHosts.Intersection(virtualServiceHosts)
					if len(intersectingHosts) == 0 {
						continue
					}
					listenerVirtualServices = append(listenerVirtualServices, virtualServiceContext{
						virtualService:    virtualService,
						server:            server,
						gatewayName:       gatewayName,
						intersectingHosts: intersectingHosts,
					})
				}
			}
		}
	}
	var vsExists bool
	if listenerVirtualServices, vsExists = vsCache[rdsPort]; !vsExists {
		serverIterator(merged.MergedServers)
		serverIterator(merged.MergedQUICTransportServers)
		vsCache[rdsPort] = listenerVirtualServices
	}
	for _, vsCtx := range listenerVirtualServices {
		virtualService := vsCtx.virtualService
		hostMatch := false
		var selectHost string
		for _, hostname := range virtualService.Spec.(*networking.VirtualService).Hosts {
			// exact match
			if hostname == hostRDSHost {
				hostMatch = true
				selectHost = hostRDSHost
				break
			}
			if alifeatures.HostRDSMergeSubset {
				// subset match
				if host.Name(hostRDSHost).SubsetOf(host.Name(hostname)) {
					hostMatch = true
					selectHost = hostname
				}
			}
		}
		if !hostMatch {
			continue
		}
		if len(virtualService.Spec.(*networking.VirtualService).Hosts) > 1 {
			copiedVS := networking.VirtualService{}
			copiedVS = *(virtualService.Spec.(*networking.VirtualService))
			copiedVS.Hosts = []string{selectHost}
			selectedVirtualServices = append(selectedVirtualServices, virtualServiceContext{
				virtualService: config.Config{
					Meta:   virtualService.Meta,
					Spec:   &copiedVS,
					Status: virtualService.Status,
				},
				server:      vsCtx.server,
				gatewayName: vsCtx.gatewayName,
			})
		} else {
			selectedVirtualServices = append(selectedVirtualServices, vsCtx)
		}
	}
	sort.SliceStable(selectedVirtualServices, func(i, j int) bool {
		// Sort by subset
		// before: ["*.abc.com", "*.com", "www.abc.com"]
		// after: ["www.abc.com", "*.abc.com", "*.com"]
		if host.Name(selectedVirtualServices[i].virtualService.Spec.(*networking.VirtualService).Hosts[0]).SubsetOf(
			host.Name(selectedVirtualServices[j].virtualService.Spec.(*networking.VirtualService).Hosts[0])) {
			return true
		}
		// Sort by creationTimestamp after
		return selectedVirtualServices[i].virtualService.CreationTimestamp.Before(selectedVirtualServices[j].virtualService.CreationTimestamp)
	})

	port := int(listenerPort)
	for _, ctx := range selectedVirtualServices {
		virtualService := ctx.virtualService
		server := ctx.server
		gatewayName := ctx.gatewayName
		// Make sure we can obtain services which are visible to this virtualService as much as possible.
		nameToServiceMap := buildNameToServiceMapForHTTPRoutes(node, push, virtualService)

		var routes []*route.Route
		var exists bool
		var err error
		if _, exists = gatewayRoutes[gatewayName]; !exists {
			gatewayRoutes[gatewayName] = make(map[string][]*route.Route)
		}

		vskey := virtualService.Name + "/" + virtualService.Namespace

		if routes, exists = gatewayRoutes[gatewayName][vskey]; !exists {
			opts := istio_route.RouteOptions{
				IsTLS:                     server.Tls != nil,
				IsHTTP3AltSvcHeaderNeeded: isH3DiscoveryNeeded,
				Mesh:                      push.Mesh,
			}
			hashByDestination := istio_route.GetConsistentHashForVirtualService(push, node, virtualService)
			routes, err = istio_route.BuildHTTPRoutesForVirtualService(node, virtualService, nameToServiceMap,
				hashByDestination, port, sets.New(gatewayName), opts)
			if err != nil {
				log.Debugf("%s omitting routes for virtual service %v/%v due to error: %v", node.ID, virtualService.Namespace, virtualService.Name, err)
				continue
			}
			gatewayRoutes[gatewayName][vskey] = routes
		}

		// This is the service that is exposed on gateway using VirtualService.
		var gatewayService *model.Service
		for _, hostname := range ctx.intersectingHosts {
			if svc, exists := nameToServiceMap[hostname]; exists {
				gatewayService = svc
			}
		}

		if vHost != nil {
			vHost.Routes = append(vHost.Routes, routes...)
			if server.Tls != nil && server.Tls.HttpsRedirect {
				vHost.RequireTls = route.VirtualHost_ALL
			}
		} else {
			perRouteFilters := map[string]*anypb.Any{}
			if gatewayService != nil {
				// Build StatefulSession Filter if gateway service has persistence session label.
				if statefulConfig := util.MaybeBuildStatefulSessionFilterConfig(gatewayService); statefulConfig != nil {
					perRouteStatefulSession := &statefulsession.StatefulSessionPerRoute{
						Override: &statefulsession.StatefulSessionPerRoute_StatefulSession{
							StatefulSession: statefulConfig,
						},
					}
					perRouteFilters[util.StatefulSessionFilter] = protoconv.MessageToAny(perRouteStatefulSession)
				}
			}
			vHost = &route.VirtualHost{
				Name:                       util.DomainName(hostRDSHost, port),
				Domains:                    []string{hostRDSHost},
				Routes:                     routes,
				IncludeRequestAttemptCount: ph.IncludeRequestAttemptCount,
			}
			if server.Tls != nil && server.Tls.HttpsRedirect {
				vHost.RequireTls = route.VirtualHost_ALL
			}
		}

		// check all hostname if is not exist with HttpsRedirect set to true
		// create VirtualHost to redirect
		if server.GetTls().GetHttpsRedirect() {
			if vHost != nil {
				vHost.RequireTls = route.VirtualHost_ALL
			} else {
				vHost = &route.VirtualHost{
					Name:                       util.DomainName(hostRDSHost, port),
					Domains:                    []string{hostRDSHost},
					IncludeRequestAttemptCount: ph.IncludeRequestAttemptCount,
					RequireTls:                 route.VirtualHost_ALL,
				}
			}
		} else if vHost != nil {
			mode := server.GetTls().GetMode()
			if mode == networking.ServerTLSSettings_MUTUAL ||
				mode == networking.ServerTLSSettings_ISTIO_MUTUAL {
				vHost.AllowServerNames = append(vHost.AllowServerNames, server.Hosts...)
			}
		}
	}
	var virtualHosts []*route.VirtualHost
	if vHost == nil {
		log.Warnf("constructed http route config for route %s on port %d with no vhosts; Setting up a default 404 vhost", routeName, port)
		virtualHosts = []*route.VirtualHost{{
			Name:    util.DomainName("blackhole", port),
			Domains: []string{"*"},
			// Empty route list will cause Envoy to 404 NR any requests
			Routes: []*route.Route{},
		}}
	} else {
		sort.SliceStable(vHost.AllowServerNames, func(i, j int) bool {
			hostI := vHost.AllowServerNames[i]
			hostJ := vHost.AllowServerNames[j]
			if host.Name(hostI).SubsetOf(host.Name(hostJ)) {
				return true
			}
			return hostI < hostJ
		})
		var uniqueServerNames []string
		hasAllCatch := false
		for i, name := range vHost.AllowServerNames {
			if name == "*" {
				hasAllCatch = true
				break
			}
			if i == 0 || vHost.AllowServerNames[i-1] != name {
				uniqueServerNames = append(uniqueServerNames, name)
			}
		}
		if hasAllCatch {
			vHost.AllowServerNames = nil
		} else {
			vHost.AllowServerNames = uniqueServerNames
		}
		vHost.Routes = istio_route.SortVHostRoutes(vHost.Routes)
		virtualHosts = append(virtualHosts, vHost)
	}

	routeCfg := &route.RouteConfiguration{
		// Retain the routeName as its used by EnvoyFilter patching logic
		Name:                           routeName,
		VirtualHosts:                   virtualHosts,
		ValidateClusters:               proto.BoolFalse,
		IgnorePortInHostMatching:       !node.IsProxylessGrpc(),
		MaxDirectResponseBodySizeBytes: istio_route.DefaultMaxDirectResponseBodySizeBytes,
	}

	routeCfg = envoyfilter.ApplyRouteConfigurationPatches(networking.EnvoyFilter_GATEWAY, node, efw, routeCfg)
	resource = &discovery.Resource{
		Name:     routeName,
		Resource: protoconv.MessageToAny(routeCfg),
	}

	if features.EnableRDSCaching && cacheable {
		configgen.Cache.Add(routeCache, req, resource)
	}

	return resource, false
}

// End added by ingress

// Modifed by ingress
func (configgen *ConfigGeneratorImpl) buildGatewayHTTPRouteConfig(
	node *model.Proxy,
	req *model.PushRequest,
	routeName string,
	vsCache map[int][]virtualServiceContext,
	efw *model.EnvoyFilterWrapper,
	efKeys []string,
) (*discovery.Resource, bool) {
	if node.MergedGateway == nil {
		log.Warnf("buildGatewayRoutes: no gateways for router %v", node.ID)
		return nil, false
	}
	// Added by ingress
	push := req.Push
	if strings.HasPrefix(routeName, constants.HigressHostRDSNamePrefix) {
		resource, cacheHit := configgen.buildHostRDSConfig(node, req, routeName, vsCache, efw, efKeys)
		if resource == nil {
			return nil, false
		}
		return resource, cacheHit
	}
	// End added by ingress

	ph := GetProxyHeaders(node, push, istionetworking.ListenerClassGateway)
	merged := node.MergedGateway
	log.Debugf("buildGatewayRoutes: gateways after merging: %v", merged)

	// make sure that there is some server listening on this port
	if _, ok := merged.ServersByRouteName[routeName]; !ok {
		log.Warnf("Gateway missing for route %s. This is normal if gateway was recently deleted.", routeName)

		// This can happen when a gateway has recently been deleted. Envoy will still request route
		// information due to the draining of listeners, so we should not return an error.
		return nil, false
	}

	servers := merged.ServersByRouteName[routeName]

	// When this is true, we add alt-svc header to the response to tell the client
	// that HTTP/3 over QUIC is available on the same port for this host. This is
	// very important for discovering HTTP/3 services
	_, isH3DiscoveryNeeded := merged.HTTP3AdvertisingRoutes[routeName]

	gatewayRoutes := make(map[string]map[string][]*route.Route)
	gatewayVirtualServices := make(map[string][]config.Config)

	vHostDedupMap := make(map[host.Name]*route.VirtualHost)
	for _, server := range servers {
		gatewayName := merged.GatewayNameForServer[server]
		port := int(server.Port.Number)

		var virtualServices []config.Config
		var exists bool

		if virtualServices, exists = gatewayVirtualServices[gatewayName]; !exists {
			virtualServices = push.VirtualServicesForGateway(node.ConfigNamespace, gatewayName)
			gatewayVirtualServices[gatewayName] = virtualServices
		}

		for _, virtualService := range virtualServices {
			virtualServiceHosts := host.NewNames(virtualService.Spec.(*networking.VirtualService).Hosts)
			serverHosts := host.NamesForNamespace(server.Hosts, virtualService.Namespace)

			// We have two cases here:
			// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and server hosts are ns/*.foo.com
			// 2. virtualService hosts are *.foo.com, and server hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
			intersectingHosts := serverHosts.Intersection(virtualServiceHosts)
			if len(intersectingHosts) == 0 {
				continue
			}

			// Make sure we can obtain services which are visible to this virtualService as much as possible.
			nameToServiceMap := buildNameToServiceMapForHTTPRoutes(node, push, virtualService)

			var routes []*route.Route
			var exists bool
			var err error
			if _, exists = gatewayRoutes[gatewayName]; !exists {
				gatewayRoutes[gatewayName] = make(map[string][]*route.Route)
			}

			vskey := virtualService.Name + "/" + virtualService.Namespace

			if routes, exists = gatewayRoutes[gatewayName][vskey]; !exists {
				opts := istio_route.RouteOptions{
					IsTLS:                     server.Tls != nil,
					IsHTTP3AltSvcHeaderNeeded: isH3DiscoveryNeeded,
					Mesh:                      push.Mesh,
				}
				hashByDestination := istio_route.GetConsistentHashForVirtualService(push, node, virtualService)
				// update by ingress
				routes, err = istio_route.BuildHTTPRoutesForVirtualService(node, virtualService, nameToServiceMap,
					hashByDestination, port, sets.New(gatewayName), opts)
				// End update by ingress
				if err != nil {
					log.Debugf("%s omitting routes for virtual service %v/%v due to error: %v", node.ID, virtualService.Namespace, virtualService.Name, err)
					continue
				}
				gatewayRoutes[gatewayName][vskey] = routes
			}
			// This is the service that is exposed on gateway using VirtualService.
			var gatewayService *model.Service
			for _, hostname := range intersectingHosts {
				if svc, exists := nameToServiceMap[hostname]; exists {
					gatewayService = svc
				}
			}

			for _, hostname := range intersectingHosts {
				if vHost, exists := vHostDedupMap[hostname]; exists {
					vHost.Routes = append(vHost.Routes, routes...)
					if server.Tls != nil && server.Tls.HttpsRedirect {
						vHost.RequireTls = route.VirtualHost_ALL
					}
				} else {
					perRouteFilters := map[string]*anypb.Any{}
					if gatewayService != nil {
						// Build StatefulSession Filter if gateway service has persistence session label.
						if statefulConfig := util.MaybeBuildStatefulSessionFilterConfig(gatewayService); statefulConfig != nil {
							perRouteStatefulSession := &statefulsession.StatefulSessionPerRoute{
								Override: &statefulsession.StatefulSessionPerRoute_StatefulSession{
									StatefulSession: statefulConfig,
								},
							}
							perRouteFilters[util.StatefulSessionFilter] = protoconv.MessageToAny(perRouteStatefulSession)
						}
					}
					newVHost := &route.VirtualHost{
						Name:                       util.DomainName(string(hostname), port),
						Domains:                    []string{hostname.String()},
						Routes:                     routes,
						IncludeRequestAttemptCount: ph.IncludeRequestAttemptCount,
					}
					if server.Tls != nil && server.Tls.HttpsRedirect {
						newVHost.RequireTls = route.VirtualHost_ALL
					}
					vHostDedupMap[hostname] = newVHost
				}
			}
		}

		// check all hostname in vHostDedupMap and if is not exist with HttpsRedirect set to true
		// create VirtualHost to redirect
		for _, hostname := range server.Hosts {
			if !server.GetTls().GetHttpsRedirect() {
				continue
			}
			if vHost, exists := vHostDedupMap[host.Name(hostname)]; exists {
				vHost.RequireTls = route.VirtualHost_ALL
				continue
			}
			newVHost := &route.VirtualHost{
				Name:                       util.DomainName(hostname, port),
				Domains:                    []string{hostname},
				IncludeRequestAttemptCount: ph.IncludeRequestAttemptCount,
				RequireTls:                 route.VirtualHost_ALL,
			}
			vHostDedupMap[host.Name(hostname)] = newVHost
		}
	}

	var virtualHosts []*route.VirtualHost
	if len(vHostDedupMap) == 0 {
		port := int(servers[0].Port.Number)
		log.Warnf("constructed http route config for route %s on port %d with no vhosts; Setting up a default 404 vhost", routeName, port)
		virtualHosts = []*route.VirtualHost{{
			Name:    util.DomainName("blackhole", port),
			Domains: []string{"*"},
			// Empty route list will cause Envoy to 404 NR any requests
			Routes: []*route.Route{},
		}}
	} else {
		virtualHosts = make([]*route.VirtualHost, 0, len(vHostDedupMap))
		vHostDedupMap = collapseDuplicateRoutes(vHostDedupMap)
		for _, v := range vHostDedupMap {
			v.Routes = istio_route.SortVHostRoutes(v.Routes)
			virtualHosts = append(virtualHosts, v)
		}
	}

	util.SortVirtualHosts(virtualHosts)

	routeCfg := &route.RouteConfiguration{
		// Retain the routeName as its used by EnvoyFilter patching logic
		Name:                           routeName,
		VirtualHosts:                   virtualHosts,
		ValidateClusters:               proto.BoolFalse,
		IgnorePortInHostMatching:       !node.IsProxylessGrpc(),
		MaxDirectResponseBodySizeBytes: istio_route.DefaultMaxDirectResponseBodySizeBytes,
	}

	routeCfg = envoyfilter.ApplyRouteConfigurationPatches(networking.EnvoyFilter_GATEWAY, node, efw, routeCfg)
	resource := &discovery.Resource{
		Name:     routeName,
		Resource: protoconv.MessageToAny(routeCfg),
	}
	return resource, false
}

// End modified by ingress

// hashRouteList returns a hash of a list of pointers
func hashRouteList(r []*route.Route) uint64 {
	// nolint: gosec
	// Not security sensitive code
	h := hash.New()
	for _, v := range r {
		u := uintptr(unsafe.Pointer(v))
		size := unsafe.Sizeof(u)
		b := make([]byte, size)
		switch size {
		case 4:
			binary.LittleEndian.PutUint32(b, uint32(u))
		default:
			binary.LittleEndian.PutUint64(b, uint64(u))
		}
		h.Write(b)
	}
	return h.Sum64()
}

// collapseDuplicateRoutes prevents cardinality explosion when we have multiple hostnames defined for the same set of routes
// with virtual service: {hosts: [a, b], routes: [r1, r2]}
// before: [{vhosts: [a], routes: [r1, r2]},{vhosts: [b], routes: [r1, r2]}]
// after: [{vhosts: [a,b], routes: [r1, r2]}]
// Note: At this point in the code, r1 and r2 are just pointers. However, once we send them over the wire
// they are fully expanded and expensive, so the optimization is important.
func collapseDuplicateRoutes(input map[host.Name]*route.VirtualHost) map[host.Name]*route.VirtualHost {
	if !features.EnableRouteCollapse {
		return input
	}
	dedupe := make(map[host.Name]*route.VirtualHost, len(input))
	known := make(map[uint64]host.Name, len(input))

	// In order to ensure stable XDS, we need to sort things. First vhost alphabetically will be the "primary"
	var hostnameKeys host.Names = make([]host.Name, 0, len(input))
	for k := range input {
		hostnameKeys = append(hostnameKeys, k)
	}
	sort.Sort(hostnameKeys)
	for _, h := range hostnameKeys {
		vh := input[h]
		hash := hashRouteList(vh.Routes)
		eh, f := known[hash]
		if f && vhostMergeable(vh, dedupe[eh]) {
			// Merge domains, routes are identical. We check the hash *and* routesEqual so that we don't depend on not having
			// collisions.
			// routesEqual is fairly cheap, but not cheap enough to do n^2 checks, so both are needed
			dedupe[eh].Domains = append(dedupe[eh].Domains, vh.Domains...)
		} else {
			known[hash] = h
			dedupe[h] = vh
		}
	}
	return dedupe
}

// vhostMergeable checks if two virtual hosts can be merged
// We explicitly do not check domains or name, as those are the keys for the merge
func vhostMergeable(a, b *route.VirtualHost) bool {
	if a.IncludeRequestAttemptCount != b.IncludeRequestAttemptCount {
		return false
	}
	if a.RequireTls != b.RequireTls {
		return false
	}
	if !routesEqual(a.Routes, b.Routes) {
		return false
	}
	return true
}

func routesEqual(a, b []*route.Route) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// builds a HTTP connection manager for servers of type HTTP or HTTPS (mode: simple/mutual)
func (configgen *ConfigGeneratorImpl) createGatewayHTTPFilterChainOpts(node *model.Proxy, port *networking.Port, server *networking.Server,
	routeName string, proxyConfig *meshconfig.ProxyConfig, transportProtocol istionetworking.TransportProtocol,
	push *model.PushContext, extraOpts *buildListenerFilterChainExtraOpts,
) *filterChainOpts {
	serverProto := protocol.Parse(port.Protocol)
	ph := GetProxyHeadersFromProxyConfig(proxyConfig, istionetworking.ListenerClassGateway)
	if serverProto.IsHTTP() {
		return &filterChainOpts{
			// This works because we validate that only HTTPS servers can have same port but still different port names
			// and that no two non-HTTPS servers can be on same port or share port names.
			// Validation is done per gateway and also during merging
			sniHosts:   nil,
			tlsContext: nil,
			httpOpts: &httpListenerOpts{
				rds:                       routeName,
				useRemoteAddress:          true,
				connectionManager:         buildGatewayConnectionManager(proxyConfig, node, false /* http3SupportEnabled */, push),
				suppressEnvoyDebugHeaders: ph.SuppressDebugHeaders,
				protocol:                  serverProto,
				class:                     istionetworking.ListenerClassGateway,
			},
		}
	}

	// Build a filter chain for the HTTPS server
	// We know that this is a HTTPS server because this function is called only for ports of type HTTP/HTTPS
	// where HTTPS server's TLS mode is not passthrough and not nil
	http3Enabled := transportProtocol == istionetworking.TransportProtocolQUIC
	return &filterChainOpts{
		// This works because we validate that only HTTPS servers can have same port but still different port names
		// and that no two non-HTTPS servers can be on same port or share port names.
		// Validation is done per gateway and also during merging
		sniHosts:   node.MergedGateway.TLSServerInfo[server].SNIHosts,
		tlsContext: buildGatewayListenerTLSContext(push.Mesh, server, node, transportProtocol, extraOpts),
		httpOpts: &httpListenerOpts{
			rds:                       routeName,
			useRemoteAddress:          true,
			connectionManager:         buildGatewayConnectionManager(proxyConfig, node, http3Enabled, push),
			suppressEnvoyDebugHeaders: ph.SuppressDebugHeaders,
			protocol:                  serverProto,
			statPrefix:                server.Name,
			http3Only:                 http3Enabled,
			class:                     istionetworking.ListenerClassGateway,
		},
	}
}

func buildGatewayConnectionManager(proxyConfig *meshconfig.ProxyConfig, node *model.Proxy, http3SupportEnabled bool,
	push *model.PushContext,
) *hcm.HttpConnectionManager {
	ph := GetProxyHeadersFromProxyConfig(proxyConfig, istionetworking.ListenerClassGateway)
	httpProtoOpts := &core.Http1ProtocolOptions{}
	if features.HTTP10 || enableHTTP10(node.Metadata.HTTP10) {
		httpProtoOpts.AcceptHttp_10 = true
	}
	xffNumTrustedHops := uint32(0)

	// Gateways do not use ProxyHeaders for XFCC as there is an existing field in gateway topology that is used instead.
	forwardClientCertDetails := util.MeshConfigToEnvoyForwardClientCertDetails(meshconfig.ForwardClientCertDetails_SANITIZE_SET)
	if proxyConfig != nil && proxyConfig.GatewayTopology != nil {
		xffNumTrustedHops = proxyConfig.GatewayTopology.NumTrustedProxies
		if proxyConfig.GatewayTopology.ForwardClientCertDetails != meshconfig.ForwardClientCertDetails_UNDEFINED {
			forwardClientCertDetails = util.MeshConfigToEnvoyForwardClientCertDetails(proxyConfig.GatewayTopology.ForwardClientCertDetails)
		}
	}

	httpConnManager := &hcm.HttpConnectionManager{
		XffNumTrustedHops: xffNumTrustedHops,
		// Forward client cert if connection is mTLS
		ForwardClientCertDetails: forwardClientCertDetails,
		SetCurrentClientCertDetails: &hcm.HttpConnectionManager_SetCurrentClientCertDetails{
			Subject: proto.BoolTrue,
			Cert:    true,
			Uri:     true,
			Dns:     true,
		},
		ServerName:                 ph.ServerName,
		ServerHeaderTransformation: ph.ServerHeaderTransformation,
		GenerateRequestId:          ph.GenerateRequestID,
		HttpProtocolOptions:        httpProtoOpts,
	}
	if http3SupportEnabled {
		httpConnManager.Http3ProtocolOptions = &core.Http3ProtocolOptions{}
		httpConnManager.CodecType = hcm.HttpConnectionManager_HTTP3
	}
	if features.EnableHCMInternalNetworks && push.Networks != nil {
		for _, internalnetwork := range push.Networks.GetNetworks() {
			iac := &hcm.HttpConnectionManager_InternalAddressConfig{}
			for _, ne := range internalnetwork.Endpoints {
				if cidr := util.ConvertAddressToCidr(ne.GetFromCidr()); cidr != nil {
					iac.CidrRanges = append(iac.CidrRanges, cidr)
				}
			}
			httpConnManager.InternalAddressConfig = iac
		}
	}

	return httpConnManager
}

// sdsPath: is the path to the mesh-wide workload sds uds path, and it is assumed that if this path is unset, that sds is
// disabled mesh-wide
// metadata: map of miscellaneous configuration values sent from the Envoy instance back to Pilot, could include the field
//
// Below is a table of potential scenarios for the gateway configuration:
//
// TLS mode      | Mesh-wide SDS | Ingress SDS | Resulting Configuration
// SIMPLE/MUTUAL |    ENABLED    |   ENABLED   | support SDS at ingress gateway to terminate SSL communication outside the mesh
// ISTIO_MUTUAL  |    ENABLED    |   DISABLED  | support SDS at gateway to terminate workload mTLS, with internal workloads
//
//	| for egress or with another trusted cluster for ingress)
//
// ISTIO_MUTUAL  |    DISABLED   |   DISABLED  | use file-mounted secret paths to terminate workload mTLS from gateway
//
// Note that ISTIO_MUTUAL TLS mode and ingressSds should not be used simultaneously on the same ingress gateway.
func buildGatewayListenerTLSContext(
	mesh *meshconfig.MeshConfig, server *networking.Server, proxy *model.Proxy, transportProtocol istionetworking.TransportProtocol,
	extraOpts *buildListenerFilterChainExtraOpts,
) *tls.DownstreamTlsContext {
	// Server.TLS cannot be nil or passthrough. But as a safety guard, return nil
	if server.Tls == nil || gateway.IsPassThroughServer(server) {
		return nil // We don't need to setup TLS context for passthrough mode
	}

	server.Tls.CipherSuites = security.FilterCipherSuites(server.Tls.CipherSuites)
	return BuildListenerTLSContext(server.Tls, proxy, mesh, transportProtocol, gateway.IsTCPServerWithTLSTermination(server), extraOpts)
}

func convertTLSProtocol(in networking.ServerTLSSettings_TLSProtocol) tls.TlsParameters_TlsProtocol {
	out := tls.TlsParameters_TlsProtocol(in) // There should be a one-to-one enum mapping
	if out < tls.TlsParameters_TLS_AUTO || out > tls.TlsParameters_TLSv1_3 {
		log.Warnf("was not able to map TLS protocol to Envoy TLS protocol")
		return tls.TlsParameters_TLS_AUTO
	}
	return out
}

func (configgen *ConfigGeneratorImpl) createGatewayTCPFilterChainOpts(
	node *model.Proxy, push *model.PushContext, server *networking.Server, listenerPort uint32,
	gatewayName string, tlsHostsByPort map[uint32]map[string]string,
) []*filterChainOpts {
	// We have a TCP/TLS server. This could be TLS termination (user specifies server.TLS with simple/mutual)
	// or opaque TCP (server.TLS is nil). or it could be a TLS passthrough with SNI based routing.

	// This is opaque TCP server. Find matching virtual services with TCP blocks and forward
	if server.Tls == nil {
		if filters := buildGatewayNetworkFiltersFromTCPRoutes(node,
			push, server, gatewayName); len(filters) > 0 {
			return []*filterChainOpts{
				{
					sniHosts:       nil,
					tlsContext:     nil,
					networkFilters: filters,
				},
			}
		}
		log.Warnf("gateway %s:%d listener missed network filter", gatewayName, server.Port.Number)
	} else if !gateway.IsPassThroughServer(server) {
		// TCP with TLS termination and forwarding. Setup TLS context to terminate, find matching services with TCP blocks
		// and forward to backend
		// Validation ensures that non-passthrough servers will have certs
		if filters := buildGatewayNetworkFiltersFromTCPRoutes(node,
			push, server, gatewayName); len(filters) > 0 {
			return []*filterChainOpts{
				{
					sniHosts:       node.MergedGateway.TLSServerInfo[server].SNIHosts,
					tlsContext:     buildGatewayListenerTLSContext(push.Mesh, server, node, istionetworking.TransportProtocolTCP, nil),
					networkFilters: filters,
				},
			}
		}
		log.Warnf("gateway %s:%d listener missed network filter", gatewayName, server.Port.Number)
	} else {
		// Passthrough server.
		return buildGatewayNetworkFiltersFromTLSRoutes(node, push, server, listenerPort, gatewayName, tlsHostsByPort)
	}

	return []*filterChainOpts{}
}

// buildGatewayNetworkFiltersFromTCPRoutes builds tcp proxy routes for all VirtualServices with TCP blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services.
func buildGatewayNetworkFiltersFromTCPRoutes(node *model.Proxy, push *model.PushContext, server *networking.Server, gateway string) []*listener.Filter {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: protocol.Parse(server.Port.Protocol),
	}

	gatewayServerHosts := make(map[host.Name]bool, len(server.Hosts))
	for _, hostname := range server.Hosts {
		gatewayServerHosts[host.Name(hostname)] = true
	}

	virtualServices := push.VirtualServicesForGateway(node.ConfigNamespace, gateway)
	if len(virtualServices) == 0 {
		log.Warnf("no virtual service bound to gateway: %v", gateway)
	}
	for _, v := range virtualServices {
		vsvc := v.Spec.(*networking.VirtualService)
		// We have two cases here:
		// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and gateway's hosts are ns/*.foo.com
		// 2. virtualService hosts are *.foo.com, and gateway's hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
		// Since this is TCP, neither matters. We are simply looking for matching virtual service for this gateway
		matchingHosts := pickMatchingGatewayHosts(gatewayServerHosts, v)
		if len(matchingHosts) == 0 {
			// the VirtualService's hosts don't include hosts advertised by server
			continue
		}

		// ensure we satisfy the rule's l4 match conditions, if any exist
		// For the moment, there can be only one match that succeeds
		// based on the match port/server port and the gateway name
		for _, tcp := range vsvc.Tcp {
			if l4MultiMatch(tcp.Match, server, gateway) {
				out := make([]*listener.Filter, 0)
				if server.GetTls().GetMode() == networking.ServerTLSSettings_ISTIO_MUTUAL {
					out = append(out,
						buildMetadataExchangeNetworkFiltersForTCPIstioMTLSGateway()...)
				}
				return append(out, buildOutboundNetworkFilters(node, tcp.Route, push, port, v.Meta)...)
			}
		}
	}

	return nil
}

// buildGatewayNetworkFiltersFromTLSRoutes builds tcp proxy routes for all VirtualServices with TLS blocks.
// It first obtains all virtual services bound to the set of Gateways for this workload, filters them by this
// server's port and hostnames, and produces network filters for each destination from the filtered services
func buildGatewayNetworkFiltersFromTLSRoutes(node *model.Proxy, push *model.PushContext, server *networking.Server,
	listenerPort uint32, gatewayName string, tlsHostsByPort map[uint32]map[string]string,
) []*filterChainOpts {
	port := &model.Port{
		Name:     server.Port.Name,
		Port:     int(server.Port.Number),
		Protocol: protocol.Parse(server.Port.Protocol),
	}

	gatewayServerHosts := make(map[host.Name]bool, len(server.Hosts))
	for _, hostname := range server.Hosts {
		gatewayServerHosts[host.Name(hostname)] = true
	}

	filterChains := make([]*filterChainOpts, 0)

	if server.Tls.Mode == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
		filterChains = append(filterChains, builtAutoPassthroughFilterChains(push, node, node.MergedGateway.TLSServerInfo[server].SNIHosts)...)
	} else {
		virtualServices := push.VirtualServicesForGateway(node.ConfigNamespace, gatewayName)
		for _, v := range virtualServices {
			vsvc := v.Spec.(*networking.VirtualService)
			// We have two cases here:
			// 1. virtualService hosts are 1.foo.com, 2.foo.com, 3.foo.com and gateway's hosts are ns/*.foo.com
			// 2. virtualService hosts are *.foo.com, and gateway's hosts are ns/1.foo.com, ns/2.foo.com, ns/3.foo.com
			// The code below only handles 1.
			// TODO: handle case 2
			matchingHosts := pickMatchingGatewayHosts(gatewayServerHosts, v)
			if len(matchingHosts) == 0 {
				// the VirtualService's hosts don't include hosts advertised by server
				continue
			}

			// For every matching TLS block, generate a filter chain with sni match
			// TODO: Bug..if there is a single virtual service with *.foo.com, and multiple TLS block
			// matches, one for 1.foo.com, another for 2.foo.com, this code will produce duplicate filter
			// chain matches
			for _, tls := range vsvc.Tls {
				for i, match := range tls.Match {
					if l4SingleMatch(convertTLSMatchToL4Match(match), server, gatewayName) {
						// Envoy will reject config that has multiple filter chain matches with the same matching rules
						// To avoid this, we need to make sure we don't have duplicated SNI hosts, which will become
						// SNI filter chain matches
						if tlsHostsByPort[listenerPort] == nil {
							tlsHostsByPort[listenerPort] = make(map[string]string)
						}
						if duplicateSniHosts := model.CheckDuplicates(match.SniHosts, server.Bind, tlsHostsByPort[listenerPort]); len(duplicateSniHosts) != 0 {
							log.Warnf(
								"skipping VirtualService %s rule #%v on server port %d of gateway %s, duplicate SNI host names: %v",
								v.Meta.Name, i, port.Port, gatewayName, duplicateSniHosts)
							model.RecordRejectedConfig(gatewayName)
							continue
						}

						// the sni hosts in the match will become part of a filter chain match
						filterChains = append(filterChains, &filterChainOpts{
							sniHosts:       match.SniHosts,
							tlsContext:     nil, // NO TLS context because this is passthrough
							networkFilters: buildOutboundNetworkFilters(node, tls.Route, push, port, v.Meta),
						})
					}
				}
			}
		}
	}

	return filterChains
}

// builtAutoPassthroughFilterChains builds a set of filter chains for auto_passthrough gateway servers.
// These servers allow connecting to any SNI-DNAT upstream cluster that matches the server's hostname.
// To handle this, we generate a filter chain per upstream cluster
func builtAutoPassthroughFilterChains(push *model.PushContext, proxy *model.Proxy, hosts []string) []*filterChainOpts {
	filterChains := make([]*filterChainOpts, 0)
	for _, service := range proxy.SidecarScope.Services() {
		if service.MeshExternal {
			continue
		}
		for _, port := range service.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			matchFound := false
			for _, h := range hosts {
				if service.Hostname.SubsetOf(host.Name(h)) {
					matchFound = true
					break
				}
			}
			if !matchFound {
				continue
			}
			clusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
			statPrefix := clusterName
			if len(push.Mesh.OutboundClusterStatName) != 0 {
				statPrefix = telemetry.BuildStatPrefix(push.Mesh.OutboundClusterStatName, string(service.Hostname), "", port, 0, &service.Attributes)
			}
			destinationRule := CastDestinationRule(proxy.SidecarScope.DestinationRule(
				model.TrafficDirectionOutbound, proxy, service.Hostname).GetRule())

			// First, we build the standard cluster. We match on the SNI matching the cluster name
			// (per the spec of AUTO_PASSTHROUGH), as well as all possible Istio mTLS ALPNs. This,
			// along with filtering out plaintext destinations in EDS, ensures that our requests will
			// always hit an Istio mTLS filter chain on the inbound side. As a result, it should not
			// be possible for anyone to access a cluster without mTLS. Note that we cannot actually
			// check for mTLS here, as we are doing passthrough TLS.
			filterChains = append(filterChains, &filterChainOpts{
				sniHosts:             []string{clusterName},
				applicationProtocols: allIstioMtlsALPNs,
				tlsContext:           nil, // NO TLS context because this is passthrough
				networkFilters: buildOutboundNetworkFiltersWithSingleDestination(
					push, proxy, statPrefix, clusterName, "", port, destinationRule, tunnelingconfig.Skip),
			})

			// Do the same, but for each subset
			for _, subset := range destinationRule.GetSubsets() {
				subsetClusterName := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
				subsetStatPrefix := subsetClusterName
				// If stat name is configured, build the stat prefix from configured pattern.
				if len(push.Mesh.OutboundClusterStatName) != 0 {
					subsetStatPrefix = telemetry.BuildStatPrefix(push.Mesh.OutboundClusterStatName, string(service.Hostname), subset.Name, port, 0, &service.Attributes)
				}
				filterChains = append(filterChains, &filterChainOpts{
					sniHosts:             []string{subsetClusterName},
					applicationProtocols: allIstioMtlsALPNs,
					tlsContext:           nil, // NO TLS context because this is passthrough
					networkFilters: buildOutboundNetworkFiltersWithSingleDestination(
						push, proxy, subsetStatPrefix, subsetClusterName, subset.Name, port, destinationRule, tunnelingconfig.Skip),
				})
			}
		}
	}
	return filterChains
}

// Select the virtualService's hosts that match the ones specified in the gateway server's hosts
// based on the wildcard hostname match and the namespace match
func pickMatchingGatewayHosts(gatewayServerHosts map[host.Name]bool, virtualService config.Config) map[string]host.Name {
	matchingHosts := make(map[string]host.Name)
	virtualServiceHosts := virtualService.Spec.(*networking.VirtualService).Hosts
	for _, vsvcHost := range virtualServiceHosts {
		for gatewayHost := range gatewayServerHosts {
			gwHostnameForMatching := gatewayHost
			if strings.Contains(string(gwHostnameForMatching), "/") {
				// match the namespace first
				// gateway merging code ensures that we only have ns/host
				// and no ./* or */host
				parts := strings.Split(string(gwHostnameForMatching), "/")
				if parts[0] != virtualService.Namespace {
					continue
				}
				// strip the namespace
				gwHostnameForMatching = host.Name(parts[1])
			}
			if gwHostnameForMatching.Matches(host.Name(vsvcHost)) {
				// assign the actual gateway host because calling code uses it as a key
				// to locate TLS redirect servers
				matchingHosts[vsvcHost] = gatewayHost
			}
		}
	}
	return matchingHosts
}

func convertTLSMatchToL4Match(tlsMatch *networking.TLSMatchAttributes) *networking.L4MatchAttributes {
	return &networking.L4MatchAttributes{
		DestinationSubnets: tlsMatch.DestinationSubnets,
		Port:               tlsMatch.Port,
		SourceLabels:       tlsMatch.SourceLabels,
		Gateways:           tlsMatch.Gateways,
		SourceNamespace:    tlsMatch.SourceNamespace,
	}
}

func l4MultiMatch(predicates []*networking.L4MatchAttributes, server *networking.Server, gateway string) bool {
	// NB from proto definitions: each set of predicates is OR'd together; inside of a predicate all conditions are AND'd.
	// This means we can return as soon as we get any match of an entire predicate.
	for _, match := range predicates {
		if l4SingleMatch(match, server, gateway) {
			return true
		}
	}
	// If we had no predicates we match; otherwise we don't match since we'd have exited at the first match.
	return len(predicates) == 0
}

func l4SingleMatch(match *networking.L4MatchAttributes, server *networking.Server, gateway string) bool {
	// if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
	return isPortMatch(match.Port, server) && isGatewayMatch(gateway, match.Gateways)
}

func isPortMatch(port uint32, server *networking.Server) bool {
	// if there's no port predicate, portMatch is true; otherwise we evaluate the port predicate against the server's port
	portMatch := port == 0
	if port != 0 {
		portMatch = server.Port.Number == port
	}
	return portMatch
}

func isGatewayMatch(gateway string, gatewayNames []string) bool {
	// if there's no gateway predicate, gatewayMatch is true; otherwise we match against the gateways for this workload
	if len(gatewayNames) == 0 {
		return true
	}
	if len(gatewayNames) > 0 {
		for _, gatewayName := range gatewayNames {
			if gatewayName == gateway {
				return true
			}
		}
	}
	return false
}
