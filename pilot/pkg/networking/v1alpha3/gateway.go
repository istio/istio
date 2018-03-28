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

package v1alpha3

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"go.uber.org/zap"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	deprecated "istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pkg/log"
)

func buildGatewayHTTPListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {

	config := env.IstioConfigStore

	gateways, err := config.List(model.Gateway.Type, model.NamespaceAll)
	if err != nil {
		return nil, fmt.Errorf("listing gateways: %s", err)
	}

	if len(gateways) == 0 {
		log.Debug("no gateways for router", zap.String("node", node.ID))
		return []*xdsapi.Listener{}, nil
	}

	gateway := &networking.Gateway{}
	for _, spec := range gateways {
		err := model.MergeGateways(gateway, spec.Spec.(*networking.Gateway))
		if err != nil {
			return nil, fmt.Errorf("merge gateways: %s", err)
		}
	}

	listeners := make([]*xdsapi.Listener, 0, len(gateway.Servers))
	for _, server := range gateway.Servers {
		// TODO: TCP

		// build physical listener
		physicalListener := buildGatewayListener(env, node, server)
		if physicalListener == nil {
			continue // TODO: add support for all protocols
		}

		listeners = append(listeners, physicalListener)
	}

	return normalizeListeners(listeners), nil
}

func buildGatewayListener(
	env model.Environment,
	node model.Proxy,
	server *networking.Server,
) *xdsapi.Listener {

	opts := buildHTTPListenerOpts{
		env:              env,
		proxy:            node,
		proxyInstances:   nil, // only required to support deprecated mixerclient behavior
		routeConfig:      nil,
		ip:               WildcardAddress,
		port:             int(server.Port.Number),
		rds:              strconv.Itoa(int(server.Port.Number)),
		useRemoteAddress: true,
		direction:        http_conn.INGRESS,
	}

	switch strings.ToUpper(server.Port.Protocol) {
	case "HTTPS":
		listener := buildHTTPListener(opts)
		// TODO(mostrowski) listener.SSLContext = tlsToSSLContext(server.Tls, server.Port.Protocol)
		return listener
	case "HTTP", "GRPC", "HTTP2":
		listener := buildHTTPListener(opts)
		//if server.Tls != nil {
		// TODO(mostrowski)	listener.SSLContext = tlsToSSLContext(server.Tls, server.Port.Protocol)
		//}
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

func buildLegacyIngressListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {

	proxyInstances, err := env.GetProxyServiceInstances(node)
	if err != nil {
		return nil, err
	}

	mesh := env.Mesh
	config := env.IstioConfigStore

	opts := buildHTTPListenerOpts{
		env:              env,
		proxy:            node,
		proxyInstances:   proxyInstances,
		routeConfig:      nil,
		ip:               WildcardAddress,
		port:             80,
		rds:              ":80",
		useRemoteAddress: true,
		direction:        http_conn.EGRESS,
		bindToPort:       true,
	}

	listeners := []*xdsapi.Listener{buildHTTPListener(opts)}

	// TODO: SNI
	// TODO: Get rid of this v1 reference
	// lack of SNI in Envoy implies that TLS secrets are attached to listeners
	// therefore, we should first check that TLS endpoint is needed before shipping TLS listener
	_, secret := deprecated.BuildIngressRoutes(mesh, node, proxyInstances, env.ServiceDiscovery, config)
	if secret != "" {
		opts.port = 443
		opts.rds = ":443"

		l := buildHTTPListener(opts)
		// there is going to be only one filter chain
		// Get the reference, append TLS context
		filterChain := l.FilterChains[0]

		filterChain.TlsContext = &auth.DownstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				AlpnProtocols: ListenersALPNProtocols,
				TlsCertificates: []*auth.TlsCertificate{
					{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: path.Join(model.IngressCertsPath, model.IngressCertFilename),
							},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: path.Join(model.IngressCertsPath, model.IngressKeyFilename),
							},
						},
					},
				},
			},
		}

		listeners = append(listeners, l)
	}

	return listeners, nil
}
