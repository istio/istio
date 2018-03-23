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
	"sort"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	google_protobuf "github.com/gogo/protobuf/types"
	// for logging
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	_ "github.com/golang/glog"
	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
)

const (
	// TODO: move to go-control-plane
	fileAccessLog = "envoy.file_access_log"
)

const (
	// RDSName is the name of route-discovery-service (RDS) cluster
	RDSName = "rds"

	// RDSHttpProxy is the special name for HTTP PROXY route
	RDSHttpProxy = "http_proxy"

	// VirtualListenerName is the name for traffic capture listener
	VirtualListenerName = "virtual"

	// WildcardAddress binds to all IP addresses
	WildcardAddress = "0.0.0.0"

	// LocalhostAddress for local binding
	LocalhostAddress = "127.0.0.1"
)

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func BuildListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {
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
		listeners := buildSidecarListeners(env.Mesh, proxyInstances,
			services, env.ManagementPorts(node.IPAddress), node, env.IstioConfigStore)
		return listeners, nil
	case model.Router:
		// TODO: add listeners for other protocols too
		return buildGatewayHTTPListeners(env.Mesh, env.IstioConfigStore, node)
	case model.Ingress:
		// TODO : Need v1alpha3 equivalent of buildIngressGateway
	}
	return nil, nil
}

// buildSidecarListeners produces a list of listeners for sidecar proxies
func buildSidecarListeners(
	mesh *meshconfig.MeshConfig,
	proxyInstances []*model.ServiceInstance,
	services []*model.Service,
	managementPorts model.PortList,
	node model.Proxy,
	config model.IstioConfigStore) []*xdsapi.Listener {

	// ensure services are ordered to simplify generation logic
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })

	listeners := make([]*xdsapi.Listener, 0)

	//if node.Type == model.Router {
	//	outbound := buildOutboundListeners(mesh, node, proxyInstances, services, config)
	//	listeners = append(listeners, outbound...)
	//} else
	if mesh.ProxyListenPort > 0 {
		inbound := buildInboundListeners(mesh, node, proxyInstances, config)
		outbound := buildOutboundListeners(mesh, node, proxyInstances, services, config)
		// TODO: health check ports
		//mgmtListeners := buildMgmtPortListeners(mesh, managementPorts, node.IPAddress)

		listeners = append(listeners, inbound...)
		listeners = append(listeners, outbound...)

		// If management listener port and service port are same, bad things happen
		// when running in kubernetes, as the probes stop responding. So, append
		// non overlapping listeners only.
		//for i := range mgmtListeners {
		//	m := mgmtListeners[i]
		//	l := getByAddress(listeners, m.Address.String())
		//	if l != nil {
		//		log.Warnf("Omitting listener for management address %s (%s) due to collision with service listener %s (%s)",
		//			m.Name, m.Address, l.Name, l.Address)
		//		continue
		//	}
		//	listeners = append(listeners, m)
		//}

		// We need a dummy filter to fill in the filter stack for orig_dst listener
		dummyTcpProxy := &tcp_proxy.TcpProxy{
			StatPrefix: "Dummy",
			Cluster:    "Dummy",
		}

		// add an extra listener that binds to the port that is the recipient of the iptables redirect
		listeners = append(listeners, &xdsapi.Listener{
			Name:           VirtualListenerName,
			Address:        buildAddress(WildcardAddress, uint32(mesh.ProxyListenPort)),
			UseOriginalDst: &google_protobuf.BoolValue{true},
			FilterChains: []listener.FilterChain{
				{
					Filters: []listener.Filter{
						{
							Name:   util.TCPProxy,
							Config: messageToStruct(dummyTcpProxy),
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

		listeners = append(listeners, buildHTTPListener(buildHTTPListenerOpts{
			mesh:             mesh,
			proxy:            node,
			proxyInstances:   proxyInstances,
			routeConfig:      nil,
			ip:               listenAddress,
			port:             int(mesh.ProxyHttpPort),
			rds:              RDSHttpProxy,
			useRemoteAddress: useRemoteAddress,
			direction:        traceOperation,
			store:            config,
			authnPolicy:      nil, /* authN policy is not needed for outbound listener */
		}))
		// TODO: need inbound listeners in HTTP_PROXY case, with dedicated ingress listener.
	}

	return normalizeListeners(listeners)
}

// buildInboundListeners creates listeners for the server-side (inbound)
// configuration for co-located service proxyInstances.
func buildInboundListeners(mesh *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, config model.IstioConfigStore) []*xdsapi.Listener {
	listeners := make([]*xdsapi.Listener, 0, len(proxyInstances))

	// inbound connections/requests are redirected to the endpoint address but appear to be sent
	// to the service address.
	for _, instance := range proxyInstances {
		endpoint := instance.Endpoint
		protocol := endpoint.ServicePort.Protocol

		var l *xdsapi.Listener
		authenticationPolicy := model.GetConsolidateAuthenticationPolicy(mesh,
			config, instance.Service.Hostname, instance.Endpoint.ServicePort)

		// Local service instances can be accessed through one of three
		// addresses: localhost, endpoint IP, and service
		// VIP. Localhost bypasses the proxy and doesn't need any TCP
		// route config. Endpoint IP is handled below and Service IP is handled
		// by outbound routes.
		// Traffic sent to our service VIP is redirected by remote
		// services' kubeproxy to our specific endpoint IP.
		switch protocol {
		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
			l = buildHTTPListener(buildHTTPListenerOpts{
				mesh:             mesh,
				proxy:            node,
				proxyInstances:   proxyInstances,
				routeConfig:      buildInboundHTTPRouteConfig(instance),
				ip:               endpoint.Address,
				port:             endpoint.Port,
				rds:              "",
				useRemoteAddress: false,
				direction:        http_conn.INGRESS,
				store:            config,
				authnPolicy:      authenticationPolicy,
			})

		case model.ProtocolTCP, model.ProtocolHTTPS, model.ProtocolMongo, model.ProtocolRedis:
			l = buildTCPListener(buildInboundNetworkFilters(instance), endpoint.Address, uint32(endpoint.Port), protocol)

			// TODO: set server-side mixer filter config
			//if mesh.MixerCheckServer != "" || mesh.MixerReportServer != "" {
			//	// config := v1.BuildTCPMixerFilterConfig(mesh, node, instance)
			//	l.FilterChains = append(l.FilterChains, listener.FilterChain{
			//		Filters: []listener.Filter{
			//			{
			//				// TODO(mostrowski): need proto version of mixer config.
			//				// Config: messageToStruct(&config),
			//			},
			//		},
			//	})
			//}

		default:
			log.Debugf("Unsupported inbound protocol %v for port %#v", protocol, instance.Endpoint.ServicePort)
		}

		if l != nil {
			// TODO: move to plugin
			applyInboundAuth(authenticationPolicy, l)
			listeners = append(listeners, l)
		}
	}

	return listeners
}

// buildOutboundListeners generates http and tcp listeners for outbound connections from the service instance
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
func buildOutboundListeners(mesh *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, services []*model.Service,
	config model.IstioConfigStore) []*xdsapi.Listener {

	tcpListeners := make([]*xdsapi.Listener, 0)
	httpListeners := make([]*xdsapi.Listener, 0)

	wildcardListenerPorts := make(map[int]bool)
	for _, service := range services {
		for _, servicePort := range service.Ports {
			clusterName := model.BuildSubsetKey(model.TrafficDirectionOutbound, "",
				service.Hostname, servicePort)

			var addresses []string
			var listenAddress string

			switch servicePort.Protocol {
			case model.ProtocolTCP, model.ProtocolHTTPS, model.ProtocolMongo, model.ProtocolRedis:
				if service.Resolution == model.Passthrough || node.Type == model.Router {
					// ensure only one wildcard listener is created per port if its headless service
					// or if its for a Router (where there is one wildcard TCP listener per port)
					// or if this is in environment where services don't get a dummy load balancer IP.
					if wildcardListenerPorts[servicePort.Port] {
						log.Debugf("Multiple definitions for port %d", servicePort.Port)
						continue
					}
					wildcardListenerPorts[servicePort.Port] = true
					listenAddress = WildcardAddress
					addresses = nil
				} else {
					listenAddress = service.Address
					addresses = []string{service.Address}
				}
				listener := buildTCPListener(buildOutboundNetworkFilters(clusterName, addresses, servicePort),
					listenAddress, uint32(servicePort.Port), servicePort.Protocol)
				tcpListeners = append(tcpListeners, listener)
				// TODO: Set SNI for HTTPS
			case model.ProtocolHTTP2, model.ProtocolHTTP, model.ProtocolGRPC:
				operation := http_conn.EGRESS
				useRemoteAddress := false

				if node.Type == model.Router {
					// if this is in Router mode, then use ingress style trace operation, and remote address settings
					useRemoteAddress = true
					operation = http_conn.INGRESS
				}

				httpListeners = append(httpListeners, buildHTTPListener(buildHTTPListenerOpts{
					mesh:             mesh,
					proxy:            node,
					proxyInstances:   proxyInstances,
					ip:               WildcardAddress,
					port:             servicePort.Port,
					rds:              fmt.Sprintf("%d", servicePort.Port),
					useRemoteAddress: useRemoteAddress,
					direction:        operation,
					store:            config,
					authnPolicy:      nil, /* authn policy is not needed for outbound listener */
				}))

			}
		}
	}

	return append(tcpListeners, httpListeners...)
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
//func buildMgmtPortListeners(mesh *meshconfig.MeshConfig, managementPorts model.PortList,
//	managementIP string) []*xdsapi.Listener {
//	listeners := make([]*xdsapi.Listener, 0, len(managementPorts))
//
//	// assumes that inbound connections/requests are sent to the endpoint address
//	for _, mPort := range managementPorts {
//		switch mPort.Protocol {
//		case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC, model.ProtocolTCP,
//			model.ProtocolHTTPS, model.ProtocolMongo, model.ProtocolRedis:
//			cluster := v1.BuildInboundCluster(mPort.Port, mPort.Protocol, mesh.ConnectTimeout)
//			listener := buildTCPListener(&v1.TCPRouteConfig{
//				Routes: []*v1.TCPRoute{v1.BuildTCPRoute(cluster, []string{managementIP})},
//			}, managementIP, uint32(mPort.Port), model.ProtocolTCP)
//
//			listeners = append(listeners, listener)
//		default:
//			log.Warnf("Unsupported inbound protocol %v for management port %#v",
//				mPort.Protocol, mPort)
//		}
//	}
//
//	return listeners
//}

// TODO: move to plugins
// applyInboundAuth adds ssl_context to the listener if the policy requires one.
func applyInboundAuth(authenticationPolicy *authn.Policy, listener *xdsapi.Listener) {

	if model.RequireTLS(authenticationPolicy) {
		// TODO(mostrowski): figure out SSL
		log.Debugf("TODO Apply authN policy %#v for %#v\n", authenticationPolicy, listener)
	}
}

// options required to build an HTTPListener
type buildHTTPListenerOpts struct { // nolint: maligned
	// config           model.Config
	// env              model.Environment
	mesh             *meshconfig.MeshConfig
	proxy            model.Proxy
	proxyInstances   []*model.ServiceInstance
	routeConfig      *xdsapi.RouteConfiguration
	rdsConfig        *http_conn.HttpConnectionManager_Rds
	ip               string
	port             int
	rds              string
	useRemoteAddress bool
	direction        http_conn.HttpConnectionManager_Tracing_OperationName
	store            model.IstioConfigStore
	authnPolicy      *authn.Policy
}

func buildHTTPListener(opts buildHTTPListenerOpts) *xdsapi.Listener {
	filters := []*http_conn.HttpFilter{}
	filters = append(filters, &http_conn.HttpFilter{
		Name: util.CORS,
	})
	// TODO: need alphav3 fault filters.
	// filters = append(filters, buildFaultFilters(opts.config, opts.env, opts.proxy)...)
	filters = append(filters, &http_conn.HttpFilter{
		Name: util.Router,
	})

	/*	TODO(mostrowski): need to port internal build functions for mixer.
		if opts.mesh.MixerCheckServer != "" || opts.mesh.MixerReportServer != "" {
			mixerConfig := v1.BuildHTTPMixerFilterConfig(opts.mesh, opts.proxy, opts.proxyInstances, opts.outboundListener, opts.store)
		filter := &http_conn.HttpFilter{
			Name: v1.MixerFilter,
			Config:messageToStruct(mixerConfig),
		}
			filters = append([]*http_conn.HttpFilter{filter}, filters...)
		}
	*/
	//refresh := time.Duration(opts.mesh.RdsRefreshDelay.Seconds) * time.Second

	if filter := buildJwtFilter(opts.authnPolicy); filter != nil {
		filters = append([]*http_conn.HttpFilter{filter}, filters...)
	}

	var rds *http_conn.HttpConnectionManager_Rds
	if opts.rds != "" {
		rds = &http_conn.HttpConnectionManager_Rds{
			Rds: &http_conn.Rds{
				RouteConfigName: opts.rds,
				ConfigSource: core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
						ApiConfigSource: &core.ApiConfigSource{
							ApiType:      core.ApiConfigSource_GRPC,
							ClusterNames: []string{RDSName},
							//RefreshDelay: &refresh,
						},
					},
				},
			},
		}
	} else {
		rds = opts.rdsConfig
	}

	connectionManager := &http_conn.HttpConnectionManager{
		CodecType: http_conn.AUTO,
		AccessLog: []*accesslog.AccessLog{
			{
				Config: nil,
			},
		},
		HttpFilters:      filters,
		StatPrefix:       "http",
		RouteSpecifier:   rds,
		UseRemoteAddress: &google_protobuf.BoolValue{opts.useRemoteAddress},
	}

	if opts.mesh.AccessLogFile != "" {
		fl := &accesslog.FileAccessLog{
			Path: opts.mesh.AccessLogFile,
		}

		connectionManager.AccessLog = []*accesslog.AccessLog{
			{
				Config: messageToStruct(fl),
				Name:   fileAccessLog,
			},
		}
	}

	if opts.mesh.EnableTracing {
		connectionManager.Tracing = &http_conn.HttpConnectionManager_Tracing{
			OperationName: opts.direction,
		}
		connectionManager.GenerateRequestId = &google_protobuf.BoolValue{true}
	}

	connectionManagerJSON, _ := json.MarshalIndent(connectionManager, "  ", "  ")
	log.Infof("LDS: %s \n", string(connectionManagerJSON))

	return &xdsapi.Listener{
		Name:    fmt.Sprintf("http_%s_%d", opts.ip, opts.port),
		Address: buildAddress(opts.ip, uint32(opts.port)),
		FilterChains: []listener.FilterChain{
			{
				Filters: []listener.Filter{
					{
						Name:   util.HTTPConnectionManager,
						Config: messageToStruct(connectionManager),
					},
				},
			},
		},
		UseOriginalDst: &google_protobuf.BoolValue{
			Value: false,
		},
		PerConnectionBufferLimitBytes: &google_protobuf.UInt32Value{
			Value: 0,
		},
		Metadata: &core.Metadata{
			FilterMetadata: nil,
		},
		DeprecatedV1: &xdsapi.Listener_DeprecatedV1{
			BindToPort: &google_protobuf.BoolValue{
				Value: false,
			},
		},
		DrainType:       0,
		ListenerFilters: nil,
		Transparent:     false,
		Freebind:        false,
	}
}

// buildTCPListener constructs a listener for the TCP proxy
func buildTCPListener(filters []listener.Filter, ip string, port uint32, protocol model.Protocol) *xdsapi.Listener {
	filterChain := listener.FilterChain{
		// This is not implemented in Envoy yet, except for SNI domains
		FilterChainMatch: &listener.FilterChainMatch{
			SniDomains:    nil,
			PrefixRanges:  nil,
			AddressSuffix: "",
			SuffixLen: &google_protobuf.UInt32Value{
				Value: 0,
			},
			SourcePrefixRanges: nil,
			SourcePorts:        nil,
			DestinationPort: &google_protobuf.UInt32Value{
				Value: 0,
			},
		},
		TlsContext: &auth.DownstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				TlsParams: &auth.TlsParameters{
					TlsMinimumProtocolVersion: 0,
					TlsMaximumProtocolVersion: 0,
					CipherSuites:              nil,
					EcdhCurves:                nil,
				},
				TlsCertificates:                nil,
				TlsCertificateSdsSecretConfigs: nil,
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: nil,
					},
					VerifyCertificateHash: nil,
					VerifySpkiSha256:      nil,
					VerifySubjectAltName:  nil,
					RequireOcspStaple: &google_protobuf.BoolValue{
						Value: false,
					},
					RequireSignedCertificateTimestamp: &google_protobuf.BoolValue{
						Value: false,
					},
					Crl: &core.DataSource{
						Specifier: nil,
					},
				},
				AlpnProtocols: nil,
				DeprecatedV1: &auth.CommonTlsContext_DeprecatedV1{
					AltAlpnProtocols: "",
				},
			},
			RequireClientCertificate: &google_protobuf.BoolValue{
				Value: false,
			},
			RequireSni: &google_protobuf.BoolValue{
				Value: false,
			},
			SessionTicketKeysType: nil,
		},
		Filters: filters,
		UseProxyProto: &google_protobuf.BoolValue{
			Value: false,
		},
		Metadata: &core.Metadata{
			FilterMetadata: nil,
		},
		TransportSocket: &core.TransportSocket{
			Name: "",
			Config: &google_protobuf.Struct{
				Fields: nil,
			},
		},
	}
	return &xdsapi.Listener{
		Name:    fmt.Sprintf("%s_%s_%d", protocol, ip, port),
		Address: buildAddress(ip, port),
		FilterChains: []listener.FilterChain{
			filterChain,
		},
		UseOriginalDst: &google_protobuf.BoolValue{
			Value: false,
		},
		PerConnectionBufferLimitBytes: &google_protobuf.UInt32Value{
			Value: 0,
		},
		Metadata: &core.Metadata{
			FilterMetadata: nil,
		},
		DeprecatedV1: &xdsapi.Listener_DeprecatedV1{
			BindToPort: &google_protobuf.BoolValue{
				Value: false,
			},
		},
		DrainType:       0,
		ListenerFilters: nil,
		Transparent:     false,
		Freebind:        false,
	}
}
