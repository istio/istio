// Copyright 2018 Istio Authors
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

package v1

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"

	meshconfig "istio.io/api/mesh/v1alpha1"
	routing "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

func buildGatewayHTTPListeners(mesh *meshconfig.MeshConfig,
	configStore model.IstioConfigStore, node model.Proxy) (Listeners, error) {

	gateways, err := configStore.List(model.Gateway.Type, model.NamespaceAll)
	if err != nil {
		return nil, fmt.Errorf("listing gateways: %s", err)
	}

	if len(gateways) == 0 {
		log.Debug("no gateways for router", zap.String("node", node.ID))
		return Listeners{}, nil
	}

	gateway := &routing.Gateway{}
	for _, spec := range gateways {
		err := model.MergeGateways(gateway, spec.Spec.(*routing.Gateway))
		if err != nil {
			return nil, fmt.Errorf("merge gateways: %s", err)
		}
	}

	listeners := make(Listeners, 0, len(gateway.Servers))
	for _, server := range gateway.Servers {
		// TODO: TCP

		// build physical listener
		physicalListener := buildPhysicalGatewayListener(mesh, node, configStore, server)
		if physicalListener == nil {
			continue // TODO: add support for all protocols
		}

		listeners = append(listeners, physicalListener)
	}

	return listeners.normalize(), nil
}

func buildPhysicalGatewayListener(
	mesh *meshconfig.MeshConfig,
	node model.Proxy,
	config model.IstioConfigStore,
	server *routing.Server,
) *Listener {

	opts := buildHTTPListenerOpts{
		mesh:             mesh,
		proxy:            node,
		proxyInstances:   nil, // only required to support deprecated mixerclient behavior
		routeConfig:      nil,
		ip:               WildcardAddress,
		port:             int(server.Port.Number),
		rds:              strconv.Itoa(int(server.Port.Number)),
		useRemoteAddress: true,
		direction:        IngressTraceOperation,
		outboundListener: false,
		store:            config,
	}

	switch strings.ToUpper(server.Port.Protocol) {
	case "HTTPS":
		listener := buildHTTPListener(opts)
		listener.SSLContext = tlsToSSLContext(server.Tls, server.Port.Protocol)
		return listener
	case "HTTP", "GRPC", "HTTP2":
		listener := buildHTTPListener(opts)
		if server.Tls != nil {
			listener.SSLContext = tlsToSSLContext(server.Tls, server.Port.Protocol)
		}
		return listener
	case "TCP":
		log.Warnf("TCP protocol support for Gateways is not yet implemented")
		return nil
	case "MONGO":
		log.Warnf("Mongo protocol support for Gateways is not yet implemented")
		return nil
	default:
		log.Warnf("Gateway with invalid protocol: %q; %v", server.Port.Protocol, server)
		return nil
	}
}

// TODO: this isn't really correct: we need xDS v2 APIs to really configure this correctly.
// Our TLS options align with SDSv2 DownstreamTlsContext, but the v1 API's SSLContext is split
// into three pieces; we need at least two of the pieces here.
func tlsToSSLContext(tls *routing.Server_TLSOptions, protocol string) *SSLContext {
	return &SSLContext{
		CertChainFile:            tls.ServerCertificate,
		PrivateKeyFile:           tls.PrivateKey,
		CaCertFile:               tls.CaCertificates,
		RequireClientCertificate: tls.Mode == routing.Server_TLSOptions_MUTUAL,
		ALPNProtocols:            strings.Join(ListenersALPNProtocols, ","),
	}
}

// buildGatewayHTTPRoutes creates HTTP route configs for a single external port on a gateway
func buildGatewayVirtualHosts(configStore model.IstioConfigStore, node model.Proxy, listenerPort int) (*HTTPRouteConfig, error) {
	gateways, err := configStore.List(model.Gateway.Type, model.NamespaceAll)
	if err != nil {
		return nil, err
	}

	allRules, err := configStore.List(model.V1alpha2RouteRule.Type, model.NamespaceAll)
	if err != nil {
		return nil, fmt.Errorf("getting all rules: %s", err)
	}

	virtualHosts := []*VirtualHost{}
	for _, gwConfig := range gateways {
		gatewayName := gwConfig.Name
		gateway := gwConfig.Spec.(*routing.Gateway)
		for _, server := range gateway.Servers {
			if listenerPort != int(server.Port.Number) {
				continue
			}
			for _, externalHostname := range server.Hosts {
				routesForThisVirtualHost, err := buildDestinationHTTPRoutesForGatewayVirtualHost(allRules,
					node, configStore, listenerPort, buildOutboundCluster,
					gatewayName, externalHostname)
				if err != nil {
					return nil, err
				}
				if len(routesForThisVirtualHost) == 0 {
					log.Warn("gateway-virtual-hosts", zap.String("host", externalHostname))
					continue
				}

				virtualHosts = append(virtualHosts, &VirtualHost{
					Name:    fmt.Sprintf("%s|%d|%s", gatewayName, listenerPort, externalHostname),
					Domains: []string{fmt.Sprintf("%s:%d", externalHostname, listenerPort), externalHostname},
					Routes:  routesForThisVirtualHost,
				})
			}
		}
	}

	configs := (&HTTPRouteConfig{VirtualHosts: virtualHosts}).normalize()
	return configs, nil
}

func buildDestinationHTTPRoutesForGatewayVirtualHost(
	allRules []model.Config,
	node model.Proxy, config model.IstioConfigStore,
	externalPort int,
	buildCluster buildClusterFunc,
	gatewayName string, externalHostname string) ([]*HTTPRoute, error) {

	rule, found := filterRulesToGatewayAndExternalHostname(allRules, gatewayName, externalHostname)
	if !found {
		return nil, nil
	}

	pseudoServicePort := &model.Port{
		Protocol: model.ProtocolHTTP, // TODO: support others
		Port:     externalPort,
		Name:     "http", // TODO: support other names?
	}
	pseudoService := &model.Service{
		Hostname: externalHostname,
		Ports: []*model.Port{
			pseudoServicePort,
		},
	}
	return buildHTTPRoutes(config, rule, pseudoService, pseudoServicePort, nil, node.Domain, buildCluster), nil
}

func filterRulesToGatewayAndExternalHostname(allRules []model.Config, gatewayName string, externalHostname string) (model.Config, bool) {
	for _, untypedRule := range allRules {
		rule := untypedRule.Spec.(*routing.RouteRule)
		if stringSliceContains(rule.Hosts, externalHostname) && stringSliceContains(rule.Gateways, gatewayName) {
			return untypedRule, true
		}
	}
	return model.Config{}, false
}

func stringSliceContains(things []string, match string) bool {
	for _, thing := range things {
		if thing == match {
			return true
		}
	}
	return false
}
