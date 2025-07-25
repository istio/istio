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

package core

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoyquicv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/quic/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	extensions "istio.io/api/extensions/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/core/extension"
	"istio.io/istio/pilot/pkg/networking/util"
	authnmodel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/proto"
	secconst "istio.io/istio/pkg/security"
	"istio.io/istio/pkg/slices"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

const (
	NoConflict = iota
	// HTTPOverTCP represents incoming HTTP existing TCP
	HTTPOverTCP
	// TCPOverHTTP represents incoming TCP existing HTTP
	TCPOverHTTP
	// TCPOverTCP represents incoming TCP existing TCP
	TCPOverTCP
	// TCPOverAuto represents incoming TCP existing AUTO
	TCPOverAuto
	// AutoOverHTTP represents incoming AUTO existing HTTP
	AutoOverHTTP
	// AutoOverTCP represents incoming AUTO existing TCP
	AutoOverTCP
)

// A set of pre-allocated variables related to protocol sniffing logic for
// propagating the ALPN to upstreams
var (
	// These are sniffed by the HTTP Inspector in the outbound listener
	// We need to forward these ALPNs to upstream so that the upstream can
	// properly use an HTTP or TCP listener
	plaintextHTTPALPNs = func() []string {
		if features.HTTP10 {
			// If HTTP 1.0 is enabled, we will match it
			return []string{"http/1.0", "http/1.1", "h2c"}
		}
		// Otherwise, matching would just lead to immediate rejection. By not matching, we can let it pass
		// through as raw TCP at least.
		// NOTE: mtlsHTTPALPNs can always include 1.0, for simplicity, as it will only be sent if a client
		return []string{"http/1.1", "h2c"}
	}()
	mtlsHTTPALPNs = []string{"istio-http/1.0", "istio-http/1.1", "istio-h2"}

	allIstioMtlsALPNs = []string{"istio", "istio-peer-exchange", "istio-http/1.0", "istio-http/1.1", "istio-h2"}

	mtlsTCPWithMxcALPNs = []string{"istio-peer-exchange", "istio"}

	defaultGatewayTransportSocketConnectTimeout = 15 * time.Second
)

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func (configgen *ConfigGeneratorImpl) BuildListeners(node *model.Proxy,
	push *model.PushContext,
) []*listener.Listener {
	builder := NewListenerBuilder(node, push)
	switch node.Type {
	case model.SidecarProxy:
		builder = configgen.buildSidecarListeners(builder)
	case model.Waypoint:
		builder = configgen.buildWaypointListeners(builder)
	case model.Router:
		builder = configgen.buildGatewayListeners(builder)
	}

	builder.patchListeners()
	l := builder.getListeners()
	if features.EnableHBONESend && !builder.node.IsWaypointProxy() {
		class := istionetworking.ListenerClassSidecarOutbound
		if node.Type == model.Router {
			class = istionetworking.ListenerClassGateway
		}
		l = append(l, buildConnectOriginateListener(push, node, class))
	}

	return l
}

func BuildListenerTLSContext(serverTLSSettings *networking.ServerTLSSettings,
	proxy *model.Proxy, push *model.PushContext, transportProtocol istionetworking.TransportProtocol, gatewayTCPServerWithTerminatingTLS bool,
) *auth.DownstreamTlsContext {
	alpnByTransport := util.ALPNHttp
	if transportProtocol == istionetworking.TransportProtocolQUIC {
		alpnByTransport = util.ALPNHttp3OverQUIC
	} else if transportProtocol == istionetworking.TransportProtocolTCP &&
		serverTLSSettings.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL &&
		gatewayTCPServerWithTerminatingTLS {
		if features.DisableMxALPN {
			alpnByTransport = util.ALPNDownstream
		} else {
			alpnByTransport = util.ALPNDownstreamWithMxc
		}
	}

	ctx := &auth.DownstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{
			AlpnProtocols: alpnByTransport,
		},
	}

	ctx.RequireClientCertificate = proto.BoolFalse
	if serverTLSSettings.Mode == networking.ServerTLSSettings_MUTUAL ||
		serverTLSSettings.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL {
		ctx.RequireClientCertificate = proto.BoolTrue
	}
	if transportProtocol == istionetworking.TransportProtocolQUIC {
		// TODO(https://github.com/envoyproxy/envoy/issues/23809) support this in Envoy
		ctx.RequireClientCertificate = proto.BoolFalse
	}
	credentialSocketExist := false
	if proxy.Metadata != nil && proxy.Metadata.Raw[secconst.CredentialMetaDataName] == "true" {
		credentialSocketExist = true
	}
	validateClient := ctx.RequireClientCertificate.Value || serverTLSSettings.Mode == networking.ServerTLSSettings_OPTIONAL_MUTUAL

	switch {
	case serverTLSSettings.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL:
		authnmodel.ApplyToCommonTLSContext(
			ctx.CommonTlsContext, proxy, serverTLSSettings.SubjectAltNames, serverTLSSettings.CaCrl,
			[]string{}, validateClient, nil)
	// If credential name(s) are specified at gateway config, create SDS config for gateway to fetch key/cert from Istiod.
	case len(serverTLSSettings.GetCredentialNames()) > 0 || serverTLSSettings.CredentialName != "":
		authnmodel.ApplyCredentialSDSToServerCommonTLSContext(ctx.CommonTlsContext, serverTLSSettings, credentialSocketExist, push)
	default:
		certProxy := &model.Proxy{}
		certProxy.IstioVersion = proxy.IstioVersion
		certProxy.Metadata = &model.NodeMetadata{
			TLSServerCertChain: serverTLSSettings.ServerCertificate,
			TLSServerKey:       serverTLSSettings.PrivateKey,
			TLSServerRootCert:  serverTLSSettings.CaCertificates,
			Raw:                proxy.Metadata.Raw,
		}

		authnmodel.ApplyToCommonTLSContext(
			ctx.CommonTlsContext, certProxy, serverTLSSettings.SubjectAltNames, serverTLSSettings.CaCrl,
			[]string{}, validateClient, serverTLSSettings.TlsCertificates)
	}

	if isSimpleOrMutual(serverTLSSettings.Mode) {
		// If Mesh TLSDefaults are set, use them.
		applyDownstreamTLSDefaults(push.Mesh.GetTlsDefaults(), ctx.CommonTlsContext)
		applyServerTLSSettings(serverTLSSettings, ctx.CommonTlsContext)
	}

	// Compliance for Envoy TLS downstreams.
	authnmodel.EnforceCompliance(ctx.CommonTlsContext)
	return ctx
}

func applyDownstreamTLSDefaults(tlsDefaults *meshconfig.MeshConfig_TLSConfig, ctx *auth.CommonTlsContext) {
	if tlsDefaults == nil {
		return
	}

	if len(tlsDefaults.EcdhCurves) > 0 {
		tlsParamsOrNew(ctx).EcdhCurves = tlsDefaults.EcdhCurves
	}
	if len(tlsDefaults.CipherSuites) > 0 {
		tlsParamsOrNew(ctx).CipherSuites = tlsDefaults.CipherSuites
	}
	if tlsDefaults.MinProtocolVersion != meshconfig.MeshConfig_TLSConfig_TLS_AUTO {
		tlsParamsOrNew(ctx).TlsMinimumProtocolVersion = auth.TlsParameters_TlsProtocol(tlsDefaults.MinProtocolVersion)
	}
}

func applyServerTLSSettings(serverTLSSettings *networking.ServerTLSSettings, ctx *auth.CommonTlsContext) {
	if serverTLSSettings.MinProtocolVersion != networking.ServerTLSSettings_TLS_AUTO {
		tlsParamsOrNew(ctx).TlsMinimumProtocolVersion = convertTLSProtocol(serverTLSSettings.MinProtocolVersion)
	}
	if len(serverTLSSettings.CipherSuites) > 0 {
		tlsParamsOrNew(ctx).CipherSuites = serverTLSSettings.CipherSuites
	}
	if serverTLSSettings.MaxProtocolVersion != networking.ServerTLSSettings_TLS_AUTO {
		tlsParamsOrNew(ctx).TlsMaximumProtocolVersion = convertTLSProtocol(serverTLSSettings.MaxProtocolVersion)
	}
}

func isSimpleOrMutual(mode networking.ServerTLSSettings_TLSmode) bool {
	return mode == networking.ServerTLSSettings_SIMPLE || mode == networking.ServerTLSSettings_MUTUAL || mode == networking.ServerTLSSettings_OPTIONAL_MUTUAL
}

func tlsParamsOrNew(tlsContext *auth.CommonTlsContext) *auth.TlsParameters {
	if tlsContext.TlsParams == nil {
		tlsContext.TlsParams = &auth.TlsParameters{}
	}
	return tlsContext.TlsParams
}

// buildSidecarListeners produces a list of listeners for sidecar proxies
func (configgen *ConfigGeneratorImpl) buildSidecarListeners(builder *ListenerBuilder) *ListenerBuilder {
	if builder.push.Mesh.ProxyListenPort > 0 {
		// Any build order change need a careful code review
		builder.appendSidecarInboundListeners().
			appendSidecarOutboundListeners().
			buildHTTPProxyListener().
			buildVirtualOutboundListener()
	}
	return builder
}

// buildWaypointListeners produces a list of listeners for waypoint
func (configgen *ConfigGeneratorImpl) buildWaypointListeners(builder *ListenerBuilder) *ListenerBuilder {
	builder.inboundListeners = builder.buildWaypointInbound()
	return builder
}

// if enableFlag is "1" indicates that AcceptHttp_10 is enabled.
func enableHTTP10(enableFlag string) bool {
	return enableFlag == "1"
}

type listenerBinding struct {
	// binds contains a list of all addresses this listener should bind to. The first one in the list is considered the primary
	binds []string
	// bindToPort determines whether this binds to a real port. If so, it becomes a real linux-level listener. Otherwise,
	// it is just a synthetic listener for matching with original_dst
	bindToPort bool
}

// Primary returns the primary bind, or empty if there is none
func (l listenerBinding) Primary() string {
	if len(l.binds) == 0 {
		return ""
	}
	return l.binds[0]
}

// Extra returns any additional bindings. This is always empty if dual stack is disabled
func (l listenerBinding) Extra() []string {
	if len(l.binds) > 1 {
		return l.binds[1:]
	}
	return nil
}

type outboundListenerEntry struct {
	servicePort *model.Port

	bind listenerBinding

	locked   bool
	chains   []*filterChainOpts
	protocol protocol.Instance
}

func protocolName(p protocol.Instance) string {
	switch istionetworking.ModelProtocolToListenerProtocol(p) {
	case istionetworking.ListenerProtocolHTTP:
		return "HTTP"
	case istionetworking.ListenerProtocolTCP:
		return "TCP"
	default:
		return "UNKNOWN"
	}
}

type outboundListenerConflict struct {
	metric          monitoring.Metric
	node            *model.Proxy
	listenerName    string
	currentProtocol protocol.Instance
	newHostname     host.Name
	newProtocol     protocol.Instance
}

func (c outboundListenerConflict) addMetric(metrics model.Metrics) {
	metrics.AddMetric(c.metric,
		c.listenerName,
		c.node.ID,
		fmt.Sprintf("Listener=%s Accepted=%s Rejected=%s (%s)",
			c.listenerName,
			protocolName(c.currentProtocol),
			protocolName(c.newProtocol),
			c.newHostname))
}

// buildSidecarOutboundListeners generates http and tcp listeners for
// outbound connections from the proxy based on the sidecar scope associated with the proxy.
func (lb *ListenerBuilder) buildSidecarOutboundListeners(node *model.Proxy,
	push *model.PushContext,
) []*listener.Listener {
	proxyNoneMode := node.GetInterceptionMode() == model.InterceptionNone

	actualWildcards, actualLocalHosts := getWildcardsAndLocalHost(node.GetIPMode())

	// For conflict resolution
	listenerMap := make(map[listenerKey]*outboundListenerEntry)

	// The sidecarConfig if provided could filter the list of
	// services/virtual services that we need to process. It could also
	// define one or more listeners with specific ports. Once we generate
	// listeners for these user specified ports, we will auto generate
	// configs for other ports if and only if the sidecarConfig has an
	// egressListener on wildcard port.
	//
	// Validation will ensure that we have utmost one wildcard egress listener
	// occurring in the end

	// Add listeners based on the config in the sidecar.EgressListeners.
	// If no Sidecar CRD is provided for this config namespace,
	// push.SidecarScope will generate a default catch all egress listener.
	for _, egressListener := range node.SidecarScope.EgressListeners {

		services := egressListener.Services()
		virtualServices := egressListener.VirtualServices()

		bind := listenerBinding{}
		// determine the bindToPort setting for listeners
		if proxyNoneMode {
			// do not care what the listener's capture mode setting is. The proxy does not use iptables
			bind.bindToPort = true
		} else if egressListener.IstioListener != nil {
			if egressListener.IstioListener.CaptureMode == networking.CaptureMode_NONE {
				// proxy uses iptables redirect or tproxy. If the
				// listener's capture mode specifies NONE, then the proxy wants
				// this listener alone to be on a physical port. If the
				// listener's capture mode is default, then its same as
				// iptables i.e. BindToPort is false.
				bind.bindToPort = true
			} else if strings.HasPrefix(egressListener.IstioListener.Bind, model.UnixAddressPrefix) {
				// If the bind is a Unix domain socket, set bindtoPort to true as it makes no
				// sense to have ORIG_DST listener for unix domain socket listeners.
				bind.bindToPort = true
			}
		}

		// If capture mode is NONE i.e., bindToPort is true, and
		// Bind IP + Port is specified, we will bind to the specified IP and Port.
		// This specified IP is ideally expected to be a loopback IP.
		//
		// If capture mode is NONE i.e., bindToPort is true, and
		// only Port is specified, we will bind to the default loopback IP
		// 127.0.0.1 and the specified Port.
		//
		// If capture mode is NONE, i.e., bindToPort is true, and
		// only Bind IP is specified, we will bind to the specified IP
		// for each port as defined in the service registry.
		//
		// If captureMode is not NONE, i.e., bindToPort is false, then
		// we will bind to user specified IP (if any) or to the VIPs of services in
		// this egress listener.
		if egressListener.IstioListener != nil && egressListener.IstioListener.Bind != "" {
			bind.binds = []string{egressListener.IstioListener.Bind}
		} else if bind.bindToPort {
			bind.binds = actualLocalHosts
		}

		if egressListener.IstioListener != nil &&
			egressListener.IstioListener.Port != nil {
			// We have a non catch all listener on some user specified port
			// The user specified port may or may not match a service port.
			// If it does not match any service port and the service has only
			// one port, then we pick a default service port. If service has
			// multiple ports, we expect the user to provide a virtualService
			// that will route to a proper Service.

			// Skip ports we cannot bind to
			wildcard := wildCards[node.GetIPMode()][0]
			listenPort := &model.Port{
				Port:     int(egressListener.IstioListener.Port.Number),
				Protocol: protocol.Parse(egressListener.IstioListener.Port.Protocol),
				Name:     egressListener.IstioListener.Port.Name,
			}
			if canbind, knownlistener := lb.node.CanBindToPort(bind.bindToPort, node, push, bind.Primary(),
				listenPort.Port, listenPort.Protocol, wildcard); !canbind {
				if knownlistener {
					log.Warnf("buildSidecarOutboundListeners: skipping sidecar port %d for node %s as it conflicts with static listener",
						egressListener.IstioListener.Port.Number, lb.node.ID)
				} else {
					log.Warnf("buildSidecarOutboundListeners: skipping privileged service port %d for node %s as it is an unprivileged proxy",
						egressListener.IstioListener.Port.Number, lb.node.ID)
				}
				continue
			}

			// TODO: dualstack wildcards
			for _, service := range services {
				listenerOpts := outboundListenerOpts{
					push:    push,
					proxy:   node,
					bind:    bind,
					port:    listenPort,
					service: service,
				}
				// Set service specific attributes here.
				lb.buildSidecarOutboundListener(listenerOpts, listenerMap, virtualServices, actualWildcards)
			}
		} else {
			// This is a catch all egress listener with no port. This
			// should be the last egress listener in the sidecar
			// Scope. Construct a listener for each service and service
			// port, if and only if this port was not specified in any of
			// the preceding listeners from the sidecarScope. This allows
			// users to specify a trimmed set of services for one or more
			// listeners and then add a catch all egress listener for all
			// other ports. Doing so allows people to restrict the set of
			// services exposed on one or more listeners, and avoid hard
			// port conflicts like tcp taking over http or http taking over
			// tcp, or simply specify that of all the listeners that Istio
			// generates, the user would like to have only specific sets of
			// services exposed on a particular listener.
			//
			// To ensure that we do not add anything to listeners we have
			// already generated, run through the outboundListenerEntry map and set
			// the locked bit to true.
			// buildSidecarOutboundListener will not add/merge
			// any HTTP/TCP listener if there is already a outboundListenerEntry
			// with locked bit set to true
			for _, e := range listenerMap {
				e.locked = true
			}

			for _, service := range services {
				saddress := service.GetAddressForProxy(node)
				for _, servicePort := range service.Ports {
					// Skip ports we cannot bind to
					wildcard := wildCards[node.GetIPMode()][0]
					if canbind, knownlistener := lb.node.CanBindToPort(bind.bindToPort, node, push, bind.Primary(),
						servicePort.Port, servicePort.Protocol, wildcard); !canbind {
						if knownlistener {
							log.Warnf("buildSidecarOutboundListeners: skipping sidecar port %d for node %s as it conflicts with static listener",
								servicePort.Port, lb.node.ID)
						} else {
							log.Warnf("buildSidecarOutboundListeners: skipping privileged service port %d for node %s as it is an unprivileged proxy",
								servicePort.Port, lb.node.ID)
						}
						continue
					}
					listenerOpts := outboundListenerOpts{
						push:    push,
						proxy:   node,
						bind:    bind,
						port:    servicePort,
						service: service,
					}

					// Support statefulsets/headless services with TCP ports, and empty service address field.
					// Instead of generating a single 0.0.0.0:Port listener, generate a listener
					// for each instance. HTTP services can happily reside on 0.0.0.0:PORT and use the
					// wildcard route match to get to the appropriate IP through original dst clusters.
					if bind.Primary() == "" && service.Resolution == model.Passthrough &&
						saddress == constants.UnspecifiedIP && (servicePort.Protocol.IsTCP() || servicePort.Protocol.IsUnsupported()) {
						instances := push.ServiceEndpointsByPort(service, servicePort.Port, nil)
						if service.Attributes.ServiceRegistry != provider.Kubernetes && len(instances) == 0 && service.Attributes.LabelSelectors == nil {
							// A Kubernetes service with no endpoints means there are no endpoints at
							// all, so don't bother sending, as traffic will never work. If we did
							// send a wildcard listener, we may get into a situation where a scale
							// down leads to a listener conflict. Similarly, if we have a
							// labelSelector on the Service, then this may have endpoints not yet
							// selected or scaled down, so we skip these as well. This leaves us with
							// only a plain ServiceEntry with resolution NONE. In this case, we will
							// fallback to a wildcard listener.
							lb.buildSidecarOutboundListener(listenerOpts, listenerMap, virtualServices, actualWildcards)
							continue
						}
						for _, instance := range instances {
							// Make sure each endpoint address is a valid address
							// as service entries could have NONE resolution with label selectors for workload
							// entries (which could technically have hostnames).
							isValidInstance := true
							for _, addr := range instance.Addresses {
								if !netutil.IsValidIPAddress(addr) {
									isValidInstance = false
									break
								}
							}
							if !isValidInstance {
								continue
							}
							// Skip build outbound listener to the node itself,
							// as when app access itself by pod ip will not flow through this listener.
							// Simultaneously, it will be duplicate with inbound listener.
							// should continue if current IstioEndpoint instance has the same ip with the
							// first ip of node IPaddresses.
							// The comparison works because both IstioEndpoint and Proxy always use the first PodIP (as provided by Kubernetes)
							// as the first entry of their respective lists.
							if instance.FirstAddressOrNil() == node.IPAddresses[0] {
								continue
							}
							listenerOpts.bind.binds = instance.Addresses
							lb.buildSidecarOutboundListener(listenerOpts, listenerMap, virtualServices, actualWildcards)
						}
					} else {
						// Standard logic for headless and non headless services
						lb.buildSidecarOutboundListener(listenerOpts, listenerMap, virtualServices, actualWildcards)
					}
				}
			}
		}
	}

	// Now validate all the listeners. Collate the tcp listeners first and then the HTTP listeners
	// TODO: This is going to be bad for caching as the order of listeners in tcpListeners or httpListeners is not
	// guaranteed.
	return finalizeOutboundListeners(lb, listenerMap)
}

func finalizeOutboundListeners(lb *ListenerBuilder, listenerMap map[listenerKey]*outboundListenerEntry) []*listener.Listener {
	listeners := make([]*listener.Listener, 0, len(listenerMap))
	for _, le := range listenerMap {
		// TODO: this could be outside the loop, but we would get object sharing in EnvoyFilter patches.
		fallthroughNetworkFilters := buildOutboundCatchAllNetworkFiltersOnly(lb.push, lb.node)
		l := buildListenerFromEntry(lb, le, fallthroughNetworkFilters)
		listeners = append(listeners, l)
	}
	return listeners
}

func buildListenerFromEntry(builder *ListenerBuilder, le *outboundListenerEntry, fallthroughNetworkFilters []*listener.Filter) *listener.Listener {
	l := &listener.Listener{
		// TODO: need to sanitize the opts.bind if its a UDS socket, as it could have colons, that envoy doesn't like
		Name:                             getListenerName(le.bind.Primary(), le.servicePort.Port, istionetworking.TransportProtocolTCP),
		Address:                          util.BuildAddress(le.bind.Primary(), uint32(le.servicePort.Port)),
		AdditionalAddresses:              util.BuildAdditionalAddresses(le.bind.Extra(), uint32(le.servicePort.Port)),
		TrafficDirection:                 core.TrafficDirection_OUTBOUND,
		ContinueOnListenerFiltersTimeout: true,
	}
	if builder.node.Metadata.OutboundListenerExactBalance {
		l.ConnectionBalanceConfig = &listener.Listener_ConnectionBalanceConfig{
			BalanceType: &listener.Listener_ConnectionBalanceConfig_ExactBalance_{
				ExactBalance: &listener.Listener_ConnectionBalanceConfig_ExactBalance{},
			},
		}
	}
	if !le.bind.bindToPort {
		l.BindToPort = proto.BoolFalse
	}

	// add a TLS inspector if we need to detect ServerName or ALPN
	// (this is not applicable for QUIC listeners)
	needTLSInspector := false
	needHTTPInspector := false
	for _, chain := range le.chains {
		needsALPN := chain.tlsContext != nil && chain.tlsContext.CommonTlsContext != nil && len(chain.tlsContext.CommonTlsContext.AlpnProtocols) > 0
		if len(chain.sniHosts) > 0 || needsALPN {
			needTLSInspector = true
		}
		needHTTP := len(chain.applicationProtocols) > 0
		if needHTTP {
			needHTTPInspector = true
		}
	}

	if needTLSInspector {
		l.ListenerFilters = append(l.ListenerFilters, xdsfilters.TLSInspector)
	}

	if needHTTPInspector {
		l.ListenerFilters = append(l.ListenerFilters, xdsfilters.HTTPInspector)
		// Enable timeout only if they configure it and we have an HTTP inspector.
		// This is really unsafe, so hopefully not used...
		l.ListenerFiltersTimeout = builder.push.Mesh.ProtocolDetectionTimeout
	} else {
		// Otherwise, do not have a timeout at all
		l.ListenerFiltersTimeout = durationpb.New(0)
	}
	wasm := builder.push.WasmPluginsByListenerInfo(builder.node, model.WasmPluginListenerInfo{
		Port:  le.servicePort.Port,
		Class: istionetworking.ListenerClassSidecarOutbound,
	}, model.WasmPluginTypeNetwork)
	for _, opt := range le.chains {
		chain := &listener.FilterChain{
			Metadata:        opt.metadata,
			TransportSocket: buildDownstreamTLSTransportSocket(opt.tlsContext),
			// Setting this timeout enables the proxy to enhance its resistance against memory exhaustion attacks,
			// such as slow TLS Handshake attacks.
			TransportSocketConnectTimeout: durationpb.New(defaultGatewayTransportSocketConnectTimeout),
		}
		if opt.httpOpts == nil {
			// we are building a network filter chain (no http connection manager) for this filter chain
			chain.Filters = opt.networkFilters
		} else {
			statsPrefix := strings.ToLower(l.TrafficDirection.String()) + "_" + l.Name
			opt.httpOpts.statPrefix = util.DelimitedStatsPrefix(statsPrefix)
			opt.httpOpts.port = le.servicePort.Port
			hcm := builder.buildHTTPConnectionManager(opt.httpOpts)
			filter := &listener.Filter{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(hcm)},
			}
			opt.networkFilters = extension.PopAppendNetwork(opt.networkFilters, wasm, extensions.PluginPhase_AUTHN)
			opt.networkFilters = extension.PopAppendNetwork(opt.networkFilters, wasm, extensions.PluginPhase_AUTHZ)
			opt.networkFilters = extension.PopAppendNetwork(opt.networkFilters, wasm, extensions.PluginPhase_STATS)
			opt.networkFilters = extension.PopAppendNetwork(opt.networkFilters, wasm, extensions.PluginPhase_UNSPECIFIED_PHASE)
			chain.Filters = append(chain.Filters, opt.networkFilters...)
			chain.Filters = append(chain.Filters, filter)
		}

		// Set a default filter chain. This allows us to avoid issues where
		// traffic starts to match a filter chain but then doesn't match latter criteria, leading to
		// dropped requests. See https://github.com/istio/istio/issues/26079 for details.
		// If there are multiple filter chains and a match all chain, move it to DefaultFilterChain
		// This ensures it will always be used as the fallback.
		if opt.isMatchAll() {
			l.DefaultFilterChain = chain
		} else {
			chain.FilterChainMatch = opt.toFilterChainMatch()
			l.FilterChains = append(l.FilterChains, chain)
		}
	}
	// If there is only one filter chain, no need to use DefaultFilterChain
	// This is probably not necessary, but for consistency with older code we keep the same logic.
	if l.DefaultFilterChain != nil && len(l.FilterChains) == 0 {
		l.FilterChains = []*listener.FilterChain{l.DefaultFilterChain}
		l.DefaultFilterChain = nil
	} else if l.DefaultFilterChain == nil {
		l.DefaultFilterChain = &listener.FilterChain{
			FilterChainMatch: &listener.FilterChainMatch{},
			Name:             util.PassthroughFilterChain,
			Filters:          fallthroughNetworkFilters,
		}
	}
	return l
}

func (lb *ListenerBuilder) buildHTTPProxy(node *model.Proxy,
	push *model.PushContext,
) *listener.Listener {
	httpProxyPort := push.Mesh.ProxyHttpPort // global
	if node.Metadata.HTTPProxyPort != "" {
		port, err := strconv.Atoi(node.Metadata.HTTPProxyPort)
		if err == nil {
			httpProxyPort = int32(port)
		}
	}
	if httpProxyPort == 0 {
		return nil
	}
	ph := GetProxyHeaders(node, push, istionetworking.ListenerClassSidecarOutbound)

	// enable HTTP PROXY port if necessary; this will add an RDS route for this port
	_, actualLocalHosts := getWildcardsAndLocalHost(node.GetIPMode())

	httpOpts := &core.Http1ProtocolOptions{
		AllowAbsoluteUrl: proto.BoolTrue,
	}
	if features.HTTP10 || enableHTTP10(node.Metadata.HTTP10) {
		httpOpts.AcceptHttp_10 = true
	}

	fcs := []*filterChainOpts{{
		httpOpts: &httpListenerOpts{
			rds:              model.RDSHttpProxy,
			useRemoteAddress: false,
			connectionManager: &hcm.HttpConnectionManager{
				HttpProtocolOptions:        httpOpts,
				ServerName:                 ph.ServerName,
				ServerHeaderTransformation: ph.ServerHeaderTransformation,
				GenerateRequestId:          ph.GenerateRequestID,
			},
			suppressEnvoyDebugHeaders: ph.SuppressDebugHeaders,
			skipIstioMXHeaders:        false,
			protocol:                  protocol.HTTP_PROXY,
			class:                     istionetworking.ListenerClassSidecarOutbound,
		},
	}}

	return buildListenerFromEntry(lb, &outboundListenerEntry{
		chains: fcs,
		bind: listenerBinding{
			binds:      actualLocalHosts,
			bindToPort: true,
		},
		servicePort: &model.Port{Port: int(httpProxyPort)},
	}, nil)
}

func buildSidecarOutboundHTTPListenerOpts(
	opts outboundListenerOpts,
	actualWildcard string,
	listenerProtocol istionetworking.ListenerProtocol,
) []*filterChainOpts {
	var rdsName string
	if opts.port.Port == 0 {
		rdsName = opts.bind.Primary() // use the UDS as a rds name
	} else {
		if listenerProtocol == istionetworking.ListenerProtocolAuto && opts.bind.Primary() != actualWildcard && opts.service != nil {
			// For sniffed services, we have a unique listener and route just for that service
			rdsName = string(opts.service.Hostname) + ":" + strconv.Itoa(opts.port.Port)
		} else {
			// Otherwise we have a shared one per-port
			rdsName = strconv.Itoa(opts.port.Port)
		}
	}
	ph := GetProxyHeaders(opts.proxy, opts.push, istionetworking.ListenerClassSidecarOutbound)
	httpOpts := &httpListenerOpts{
		// Set useRemoteAddress to true for sidecar outbound listeners so that it picks up the localhost address of the sender,
		// which is an internal address, so that trusted headers are not sanitized. This helps to retain the timeout headers
		// such as "x-envoy-upstream-rq-timeout-ms" set by the calling application.
		useRemoteAddress: features.UseRemoteAddress,
		rds:              rdsName,

		connectionManager: &hcm.HttpConnectionManager{
			ServerName:                 ph.ServerName,
			ServerHeaderTransformation: ph.ServerHeaderTransformation,
			GenerateRequestId:          ph.GenerateRequestID,
			Proxy_100Continue:          features.Enable100ContinueHeaders,
		},
		suppressEnvoyDebugHeaders: ph.SuppressDebugHeaders,
		skipIstioMXHeaders:        ph.SkipIstioMXHeaders,
		protocol:                  opts.port.Protocol,
		class:                     istionetworking.ListenerClassSidecarOutbound,
	}

	// Configure X-Forwarded-Port and X-Forwarded-Host headers
	if ph.XForwardedPort {
		httpOpts.connectionManager.XForwardedPort = proto.BoolTrue
	}
	if ph.XForwardedHost {
		httpOpts.connectionManager.XForwardedHost = proto.BoolTrue
	}

	if features.HTTP10 || enableHTTP10(opts.proxy.Metadata.HTTP10) {
		httpOpts.connectionManager.HttpProtocolOptions = &core.Http1ProtocolOptions{
			AcceptHttp_10: true,
		}
	}

	return []*filterChainOpts{{
		httpOpts: httpOpts,
	}}
}

func buildSidecarOutboundTCPListenerOpts(opts outboundListenerOpts, virtualServices []config.Config) []*filterChainOpts {
	meshGateway := sets.New(constants.IstioMeshGateway)
	out := make([]*filterChainOpts, 0)
	var svcConfigs []config.Config
	if opts.service != nil {
		// Do not filter namespace for now.
		// TODO(https://github.com/istio/istio/issues/46146) we may need to, or something more sophisticated
		svcConfigs = getConfigsForHost("", opts.service.Hostname, virtualServices)
	} else {
		svcConfigs = virtualServices
	}

	out = append(out, buildSidecarOutboundTLSFilterChainOpts(opts.proxy, opts.push, opts.cidr, opts.service,
		opts.bind.Primary(), opts.port, meshGateway, svcConfigs)...)
	out = append(out, buildSidecarOutboundTCPFilterChainOpts(opts.proxy, opts.push, opts.cidr, opts.service,
		opts.port, meshGateway, svcConfigs)...)
	return out
}

// buildSidecarOutboundListener builds a single listener and
// adds it to the listenerMap provided by the caller.  Listeners are added
// if one doesn't already exist. HTTP listeners on same port are ignored
// (as vhosts are shipped through RDS).  TCP listeners on same port are
// allowed only if they have different CIDR matches.
func (lb *ListenerBuilder) buildSidecarOutboundListener(listenerOpts outboundListenerOpts,
	listenerMap map[listenerKey]*outboundListenerEntry, virtualServices []config.Config, actualWildcards []string,
) {
	// Alias services do not get listeners generated
	if listenerOpts.service.Resolution == model.Alias {
		return
	}
	// TODO: remove actualWildcard
	var currentListenerEntry *outboundListenerEntry

	conflictType := NoConflict

	listenerPortProtocol := listenerOpts.port.Protocol
	listenerProtocol := istionetworking.ModelProtocolToListenerProtocol(listenerOpts.port.Protocol)

	var listenerMapKey listenerKey
	switch listenerProtocol {
	case istionetworking.ListenerProtocolTCP, istionetworking.ListenerProtocolAuto:
		// Determine the listener address if bind is empty
		// we listen on the service VIP if and only
		// if the address is an IP address. If its a CIDR, we listen on
		// 0.0.0.0, and setup a filter chain match for the CIDR range.
		// As a small optimization, CIDRs with /32 prefix will be converted
		// into listener address so that there is a dedicated listener for this
		// ip:port. This will reduce the impact of a listener reload
		if listenerOpts.bind.Primary() == "" { // TODO: make this better
			svcListenAddress := listenerOpts.service.GetAddressForProxy(listenerOpts.proxy)
			svcExtraListenAddresses := listenerOpts.service.GetExtraAddressesForProxy(listenerOpts.proxy)
			// Override the svcListenAddress, using the proxy ipFamily, for cases where the ipFamily cannot be detected easily.
			// For example: due to the possibility of using hostnames instead of ips in ServiceEntry,
			// it is hard to detect ipFamily for such services.
			if listenerOpts.service.Attributes.ServiceRegistry == provider.External && listenerOpts.proxy.IsIPv6() &&
				svcListenAddress == constants.UnspecifiedIP {
				svcListenAddress = constants.UnspecifiedIPv6
			}

			// For dualstack proxies we need to add the unspecifed ipv6 address to the list of extra listen addresses
			if listenerOpts.service.Attributes.ServiceRegistry == provider.External && listenerOpts.proxy.IsDualStack() &&
				svcListenAddress == constants.UnspecifiedIP {
				svcExtraListenAddresses = append(svcExtraListenAddresses, constants.UnspecifiedIPv6)
			}
			// We should never get an empty address.
			// This is a safety guard, in case some platform adapter isn't doing things
			// properly
			if len(svcListenAddress) > 0 {
				if !strings.Contains(svcListenAddress, "/") {
					listenerOpts.bind.binds = append([]string{svcListenAddress}, svcExtraListenAddresses...)
				} else {
					// Address is a CIDR. Fall back to 0.0.0.0 and
					// filter chain match
					listenerOpts.bind.binds = actualWildcards
					listenerOpts.cidr = append([]string{svcListenAddress}, svcExtraListenAddresses...)
				}
			}
		}
		listenerMapKey = listenerKey{listenerOpts.bind.Primary(), listenerOpts.port.Port}

	case istionetworking.ListenerProtocolHTTP:
		// first identify the bind if its not set. Then construct the key
		// used to lookup the listener in the conflict map.
		if len(listenerOpts.bind.Primary()) == 0 { // no user specified bind. Use 0.0.0.0:Port or [::]:Port
			listenerOpts.bind.binds = actualWildcards
		}
		listenerMapKey = listenerKey{listenerOpts.bind.Primary(), listenerOpts.port.Port}
	}

	// Have we already generated a listener for this Port based on user
	// specified listener ports? if so, we should not add any more HTTP
	// services to the port. The user could have specified a sidecar
	// resource with one or more explicit ports and then added a catch
	// all listener, implying add all other ports as usual. When we are
	// iterating through the services for a catchAll egress listener,
	// the caller would have set the locked bit for each listener Entry
	// in the map.
	//
	// Check if this HTTP listener conflicts with an existing TCP
	// listener. We could have listener conflicts occur on unix domain
	// sockets, or on IP binds. Specifically, its common to see
	// conflicts on binds for wildcard address when a service has NONE
	// resolution type, since we collapse all HTTP listeners into a
	// single 0.0.0.0:port listener and use vhosts to distinguish
	// individual http services in that port
	if cur, exists := listenerMap[listenerMapKey]; exists {
		currentListenerEntry = cur
		// NOTE: This is not a conflict. This is simply filtering the
		// services for a given listener explicitly.
		// When the user declares their own ports in Sidecar.egress
		// with some specific services on those ports, we should not
		// generate any more listeners on that port as the user does
		// not want those listeners. Protocol sniffing is not needed.
		if cur.locked {
			return
		}
	}

	var opts []*filterChainOpts
	// For HTTP_PROXY protocol defined by sidecars, just create the HTTP listener right away.
	if listenerPortProtocol == protocol.HTTP_PROXY {
		opts = buildSidecarOutboundHTTPListenerOpts(listenerOpts, actualWildcards[0], listenerProtocol)
	} else {
		switch listenerProtocol {
		case istionetworking.ListenerProtocolHTTP:
			// Check if conflict happens
			if currentListenerEntry != nil {
				// Build HTTP listener. If current listener entry is using HTTP or protocol sniffing,
				// append the service. Otherwise (TCP), change current listener to use protocol sniffing.
				if currentListenerEntry.protocol.IsTCP() {
					conflictType = HTTPOverTCP
				} else {
					// Exit early, listener already exists
					return
				}
			}
			opts = buildSidecarOutboundHTTPListenerOpts(listenerOpts, actualWildcards[0], listenerProtocol)

			// Add application protocol filter chain match to the http filter chain. The application protocol will be set by http inspector
			// Since application protocol filter chain match has been added to the http filter chain, a fall through filter chain will be
			// appended to the listener later to allow arbitrary egress TCP traffic pass through when its port is conflicted with existing
			// HTTP services, which can happen when a pod accesses a non registry service.
			if listenerOpts.bind.Primary() == actualWildcards[0] {
				for _, opt := range opts {
					// Support HTTP/1.0, HTTP/1.1 and HTTP/2
					opt.applicationProtocols = append(opt.applicationProtocols, plaintextHTTPALPNs...)
					opt.transportProtocol = xdsfilters.RawBufferTransportProtocol
				}

				// if we have a tcp fallthrough filter chain, this is no longer an HTTP listener - it
				// is instead "unsupported" (auto detected), as we have a TCP and HTTP filter chain with
				// inspection to route between them
				listenerPortProtocol = protocol.Unsupported
			}

		case istionetworking.ListenerProtocolTCP:
			opts = buildSidecarOutboundTCPListenerOpts(listenerOpts, virtualServices)

			// Check if conflict happens
			if currentListenerEntry != nil {
				// Build TCP listener. If current listener entry is using HTTP, add a new TCP filter chain
				// If current listener is using protocol sniffing, merge the TCP filter chains.
				if currentListenerEntry.protocol.IsHTTP() {
					conflictType = TCPOverHTTP
				} else if currentListenerEntry.protocol.IsTCP() {
					conflictType = TCPOverTCP
				} else {
					conflictType = TCPOverAuto
				}
			}

		case istionetworking.ListenerProtocolAuto:
			if currentListenerEntry != nil {
				if currentListenerEntry.protocol.IsHTTP() {
					conflictType = AutoOverHTTP
				} else if currentListenerEntry.protocol.IsTCP() {
					conflictType = AutoOverTCP
				} else {
					// Exit early, listener already exists
					return
				}
			}
			// Add tcp filter chain, build TCP filter chain first.
			tcpOpts := buildSidecarOutboundTCPListenerOpts(listenerOpts, virtualServices)

			// Add http filter chain and tcp filter chain to the listener opts
			httpOpts := buildSidecarOutboundHTTPListenerOpts(listenerOpts, actualWildcards[0], listenerProtocol)
			// Add application protocol filter chain match to the http filter chain. The application protocol will be set by http inspector
			for _, opt := range httpOpts {
				// Support HTTP/1.0, HTTP/1.1 and HTTP/2
				opt.applicationProtocols = append(opt.applicationProtocols, plaintextHTTPALPNs...)
				opt.transportProtocol = xdsfilters.RawBufferTransportProtocol
			}

			opts = append(tcpOpts, httpOpts...)

		default:
			// UDP or other protocols: no need to log, it's too noisy
			return
		}
	}

	// If there is a TCP listener on well known port, cannot add any http filter chain
	// with the inspector as it will break for server-first protocols. Similarly,
	// if there was a HTTP listener on well known port, cannot add a tcp listener
	// with the inspector as inspector breaks all server-first protocols.
	if currentListenerEntry != nil &&
		!isConflictWithWellKnownPort(listenerOpts.port.Protocol, currentListenerEntry.protocol, conflictType) {
		log.Warnf("conflict happens on a well known port %d, incoming protocol %v, existing protocol %v, conflict type %v",
			listenerOpts.port.Port, listenerOpts.port.Protocol, currentListenerEntry.protocol, conflictType)
		return
	}

	// In general, for handling conflicts we:
	// * Turn on sniffing if its HTTP and TCP mixed
	// * Merge filter chains
	switch conflictType {
	case NoConflict, AutoOverHTTP:
		// This is a new entry (NoConflict), or completely overriding (AutoOverHTTP); add it to the map
		listenerMap[listenerMapKey] = &outboundListenerEntry{
			servicePort: listenerOpts.port,
			bind:        listenerOpts.bind,
			chains:      opts,
			protocol:    listenerPortProtocol,
		}

	case HTTPOverTCP, TCPOverHTTP, AutoOverTCP:
		// Merge the two and "upgrade" to sniffed
		mergeTCPFilterChains(currentListenerEntry, opts, listenerOpts)
		currentListenerEntry.protocol = protocol.Unsupported

	case TCPOverTCP, TCPOverAuto:
		// Merge two TCP filter chains. HTTP filter chain will not conflict with TCP filter chain because HTTP filter chain match for
		// HTTP filter chain is different from TCP filter chain's.
		mergeTCPFilterChains(currentListenerEntry, opts, listenerOpts)

	default:
		// This should never happen
		log.Errorf("Got unexpected conflict type %v. This should never happen", conflictType)
	}
}

// httpListenerOpts are options for an HTTP listener
type httpListenerOpts struct {
	routeConfig *route.RouteConfiguration
	rds         string
	// If set, use this as a basis
	connectionManager *hcm.HttpConnectionManager
	// stat prefix for the http connection manager
	// DO not set this field. Will be overridden by buildCompleteFilterChain
	statPrefix       string
	protocol         protocol.Instance
	useRemoteAddress bool

	suppressEnvoyDebugHeaders bool
	skipIstioMXHeaders        bool

	// http3Only indicates that the HTTP codec used
	// is HTTP/3 over QUIC transport (uses UDP)
	http3Only bool

	class istionetworking.ListenerClass
	port  int
	hbone bool

	// Waypoint-specific modifications in HCM
	isWaypoint bool

	// allow service attached policy for to-service chains
	// currently only used for waypoints
	policySvc *model.Service
}

// filterChainOpts describes a filter chain: a set of filters with the same TLS context
type filterChainOpts struct {
	// Matching criteria. Will eventually turn into FilterChainMatch
	sniHosts             []string
	destinationCIDRs     []string
	applicationProtocols []string
	transportProtocol    string

	// Arbitrary metadata to attach to the filter
	metadata *core.Metadata

	// TLS configuration for the filter
	tlsContext *auth.DownstreamTlsContext

	// Set if this is for HTTP.
	httpOpts *httpListenerOpts
	// Set if this is for TCP chain.
	networkFilters []*listener.Filter
}

// gatewayListenerOpts are the options required to build a gateway Listener
type gatewayListenerOpts struct {
	push  *model.PushContext
	proxy *model.Proxy

	bindToPort bool
	bind       string
	extraBind  []string

	port              int
	filterChainOpts   []*filterChainOpts
	needPROXYProtocol bool
}

// outboundListenerOpts are the options to build an outbound listener
type outboundListenerOpts struct {
	push  *model.PushContext
	proxy *model.Proxy

	bind listenerBinding
	cidr []string

	port    *model.Port
	service *model.Service
}

// buildGatewayListener builds and initializes a Listener proto based on the provided opts. It does not set any filters.
// Optionally for HTTP filters with TLS enabled, HTTP/3 can be supported by generating QUIC Mirror filters for the
// same port (it is fine as QUIC uses UDP)
func buildGatewayListener(opts gatewayListenerOpts, transport istionetworking.TransportProtocol) *listener.Listener {
	filterChains := make([]*listener.FilterChain, 0, len(opts.filterChainOpts))
	var listenerFilters []*listener.ListenerFilter

	// Strip PROXY header first for non-QUIC traffic if requested.
	if opts.needPROXYProtocol {
		listenerFilters = append(listenerFilters, xdsfilters.ProxyProtocol)
	}

	// add a TLS inspector if we need to detect ServerName or ALPN
	// (this is not applicable for QUIC listeners)
	if transport == istionetworking.TransportProtocolTCP {
		for _, chain := range opts.filterChainOpts {
			needsALPN := chain.tlsContext != nil && chain.tlsContext.CommonTlsContext != nil && len(chain.tlsContext.CommonTlsContext.AlpnProtocols) > 0
			if len(chain.sniHosts) > 0 || needsALPN {
				listenerFilters = append(listenerFilters, xdsfilters.TLSInspector)
				break
			}
		}
	}

	for _, chain := range opts.filterChainOpts {
		match := chain.toFilterChainMatch()
		var transportSocket *core.TransportSocket
		switch transport {
		case istionetworking.TransportProtocolTCP:
			transportSocket = buildDownstreamTLSTransportSocket(chain.tlsContext)
		case istionetworking.TransportProtocolQUIC:
			transportSocket = buildDownstreamQUICTransportSocket(chain.tlsContext)
		}
		filterChains = append(filterChains, &listener.FilterChain{
			FilterChainMatch: match,
			TransportSocket:  transportSocket,
			// Setting this timeout enables the proxy to enhance its resistance against memory exhaustion attacks,
			// such as slow TLS Handshake attacks.
			TransportSocketConnectTimeout: durationpb.New(defaultGatewayTransportSocketConnectTimeout),
		})
	}

	res := &listener.Listener{
		TrafficDirection: core.TrafficDirection_OUTBOUND,
		ListenerFilters:  listenerFilters,
		FilterChains:     filterChains,
		// No listener filter timeout is set for the gateway here; it will default to 15 seconds in Envoy.
		// This timeout setting helps prevent memory leaks in Envoy when a TLS inspector filter is present,
		// by avoiding slow requests that could otherwise lead to such issues.
		// Note that this timer only takes effect when a listener filter is present.

		MaxConnectionsToAcceptPerSocketEvent: maxConnectionsToAcceptPerSocketEvent(),
	}
	switch transport {
	case istionetworking.TransportProtocolTCP:
		// TODO: need to sanitize the opts.bind if its a UDS socket, as it could have colons, that envoy doesn't like
		res.Name = getListenerName(opts.bind, opts.port, istionetworking.TransportProtocolTCP)
		log.Debugf("buildGatewayListener: building TCP listener %s", res.Name)
		// TODO: need to sanitize the opts.bind if its a UDS socket, as it could have colons, that envoy doesn't like
		res.Address = util.BuildAddress(opts.bind, uint32(opts.port))
		// only use to exact_balance for tcp outbound listeners; virtualOutbound listener should
		// not have this set per Envoy docs for redirected listeners
		if opts.proxy.Metadata.OutboundListenerExactBalance {
			res.ConnectionBalanceConfig = &listener.Listener_ConnectionBalanceConfig{
				BalanceType: &listener.Listener_ConnectionBalanceConfig_ExactBalance_{
					ExactBalance: &listener.Listener_ConnectionBalanceConfig_ExactBalance{},
				},
			}
		}
	case istionetworking.TransportProtocolQUIC:
		// TODO: switch on TransportProtocolQUIC is in too many places now. Once this is a bit
		//       mature, refactor some of these to an interface so that they kick off the process
		//       of building listener, filter chains, serializing etc based on transport protocol
		res.Name = getListenerName(opts.bind, opts.port, istionetworking.TransportProtocolQUIC)
		log.Debugf("buildGatewayListener: building UDP/QUIC listener %s", res.Name)
		res.Address = util.BuildNetworkAddress(opts.bind, uint32(opts.port), istionetworking.TransportProtocolQUIC)
		res.UdpListenerConfig = &listener.UdpListenerConfig{
			// TODO: Maybe we should add options in MeshConfig to
			//       configure QUIC options - it should look similar
			//       to the H2 protocol options.
			QuicOptions:            &listener.QuicProtocolOptions{},
			DownstreamSocketConfig: &core.UdpSocketConfig{},
		}

	}
	// add extra addresses for the listener
	if features.EnableDualStack && len(opts.extraBind) > 0 {
		res.AdditionalAddresses = util.BuildAdditionalAddresses(opts.extraBind, uint32(opts.port))
		// Ensure consistent transport protocol with main address
		for _, additionalAddress := range res.AdditionalAddresses {
			additionalAddress.GetAddress().GetSocketAddress().Protocol = transport.ToEnvoySocketProtocol()
		}
	}
	accessLogBuilder.setListenerAccessLog(opts.push, opts.proxy, res, istionetworking.ListenerClassGateway)

	return res
}

// isMatchAll returns true if this chain will match everything
// This closely matches toFilterChainMatch
func (chain *filterChainOpts) isMatchAll() bool {
	return (len(chain.sniHosts) == 0 || slices.Contains(chain.sniHosts, "*")) &&
		len(chain.applicationProtocols) == 0 &&
		len(chain.transportProtocol) == 0 &&
		len(chain.destinationCIDRs) == 0
}

func (chain *filterChainOpts) conflictsWith(other *filterChainOpts) bool {
	a, b := chain, other
	if a == nil || b == nil {
		return a == b
	}
	if a.transportProtocol != b.transportProtocol {
		return false
	}
	if !slices.Equal(a.applicationProtocols, b.applicationProtocols) {
		return false
	}
	// SNI order does not matter, and we ignore * entries
	sniSet := func(sni []string) sets.String {
		if len(sni) == 0 {
			return nil
		}
		res := sets.NewWithLength[string](len(sni))
		for _, s := range sni {
			if s == "*" {
				continue
			}
			res.Insert(s)
		}
		return res
	}
	if !sniSet(a.sniHosts).Equals(sniSet(b.sniHosts)) {
		return false
	}

	// Order doesn't matter. Make sure we properly handle overlapping prefixes though
	// eg: 1.2.3.4/8 is the same as 1.5.6.7/8.
	cidrSet := func(cidrs []string) sets.String {
		if len(cidrs) == 0 {
			return nil
		}
		res := sets.NewWithLength[string](len(cidrs))
		for _, s := range cidrs {
			prefix, err := util.AddrStrToPrefix(s)
			if err != nil {
				continue
			}
			if prefix.Addr().String() == constants.UnspecifiedIP {
				continue
			}

			if s == "*" {
				continue
			}
			res.Insert(prefix.Masked().String())
		}
		return res
	}
	return cidrSet(a.destinationCIDRs).Equals(cidrSet(b.destinationCIDRs))
}

func (chain *filterChainOpts) toFilterChainMatch() *listener.FilterChainMatch {
	if chain.isMatchAll() {
		return nil
	}
	match := &listener.FilterChainMatch{
		ApplicationProtocols: chain.applicationProtocols,
		TransportProtocol:    chain.transportProtocol,
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
			chain.sniHosts = append([]string{}, chain.sniHosts...)
			sort.Stable(sort.StringSlice(chain.sniHosts))
			match.ServerNames = chain.sniHosts
		}
	}
	if len(chain.destinationCIDRs) > 0 {
		chain.destinationCIDRs = append([]string{}, chain.destinationCIDRs...)
		sort.Stable(sort.StringSlice(chain.destinationCIDRs))
		for _, d := range chain.destinationCIDRs {
			cidr := util.ConvertAddressToCidr(d)
			if cidr != nil && cidr.AddressPrefix != constants.UnspecifiedIP {
				match.PrefixRanges = append(match.PrefixRanges, cidr)
			}
		}
	}

	return match
}

func mergeTCPFilterChains(current *outboundListenerEntry, incoming []*filterChainOpts, opts outboundListenerOpts) {
	// TODO(rshriram) merge multiple identical filter chains with just a single destination CIDR based
	// filter chain match, into a single filter chain and array of destinationcidr matches

	// The code below checks for TCP over TCP conflicts and merges listeners

	// Merge the newly built listener with the existing listener, if and only if the filter chains have distinct conditions.
	// Extract the current filter chain matches, for every new filter chain match being added, check if there is a matching
	// one in previous filter chains, if so, skip adding this filter chain with a warning.

	merged := make([]*filterChainOpts, 0, len(current.chains)+len(incoming))
	// Start with the current listener's filter chains.
	merged = append(merged, current.chains...)

	for _, incoming := range incoming {
		conflict := false

		for _, existing := range merged {
			conflict = existing.conflictsWith(incoming)

			if conflict {
				// NOTE: While pluginParams.Service can be nil,
				// this code cannot be reached if Service is nil because a pluginParams.Service can be nil only
				// for user defined Egress listeners with ports. And these should occur in the API before
				// the wildcard egress listener. the check for the "locked" bit will eliminate the collision.
				// User is also not allowed to add duplicate ports in the egress listener
				var newHostname host.Name
				if opts.service != nil {
					newHostname = opts.service.Hostname
				} else {
					// user defined outbound listener via sidecar API
					newHostname = "sidecar-config-egress-tcp-listener"
				}

				outboundListenerConflict{
					metric:          model.ProxyStatusConflictOutboundListenerTCPOverTCP,
					node:            opts.proxy,
					listenerName:    getListenerName(opts.bind.Primary(), opts.port.Port, istionetworking.TransportProtocolTCP),
					currentProtocol: current.servicePort.Protocol,
					newHostname:     newHostname,
					newProtocol:     opts.port.Protocol,
				}.addMetric(opts.push)
				break
			}
		}

		if !conflict {
			// There is no conflict with any filter chain in the existing listener.
			// So append the new filter chains to the existing listener's filter chains
			merged = append(merged, incoming)
		}
	}
	current.chains = merged
}

// isConflictWithWellKnownPort checks conflicts between incoming protocol and existing protocol.
// Mongo and MySQL are not allowed to co-exist with other protocols in one port.
func isConflictWithWellKnownPort(incoming, existing protocol.Instance, conflict int) bool {
	if conflict == NoConflict {
		return true
	}

	if (incoming == protocol.Mongo ||
		incoming == protocol.MySQL ||
		existing == protocol.Mongo ||
		existing == protocol.MySQL) && incoming != existing {
		return false
	}

	return true
}

// nolint: interfacer
func buildDownstreamTLSTransportSocket(tlsContext *auth.DownstreamTlsContext) *core.TransportSocket {
	if tlsContext == nil {
		return nil
	}
	return &core.TransportSocket{
		Name:       wellknown.TransportSocketTLS,
		ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(tlsContext)},
	}
}

func buildDownstreamQUICTransportSocket(tlsContext *auth.DownstreamTlsContext) *core.TransportSocket {
	if tlsContext == nil {
		return nil
	}
	return &core.TransportSocket{
		Name: wellknown.TransportSocketQuic,
		ConfigType: &core.TransportSocket_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&envoyquicv3.QuicDownstreamTransport{
				DownstreamTlsContext: tlsContext,
			}),
		},
	}
}

type listenerKey struct {
	bind string
	port int
}
