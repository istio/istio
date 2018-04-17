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
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"go.uber.org/zap"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pkg/log"
)

func buildGatewayHTTPListeners(mesh *meshconfig.MeshConfig,
	configStore model.IstioConfigStore, node model.Proxy) ([]*xdsapi.Listener, error) {

	gateways, err := configStore.List(model.Gateway.Type, model.NamespaceAll)
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
		physicalListener := buildPhysicalGatewayListener(mesh, node, configStore, server)
		if physicalListener == nil {
			continue // TODO: add support for all protocols
		}

		listeners = append(listeners, physicalListener)
	}

	return normalizeListeners(listeners), nil
}

func buildPhysicalGatewayListener(
	mesh *meshconfig.MeshConfig,
	node model.Proxy,
	config model.IstioConfigStore,
	server *networking.Server,
) *xdsapi.Listener {

	opts := buildHTTPListenerOpts{
		mesh:             mesh,
		proxy:            node,
		proxyInstances:   nil, // only required to support deprecated mixerclient behavior
		routeConfig:      nil,
		ip:               v1.WildcardAddress,
		port:             int(server.Port.Number),
		rds:              strconv.Itoa(int(server.Port.Number)),
		useRemoteAddress: true,
		direction:        http_conn.INGRESS,
		outboundListener: false,
		store:            config,
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
