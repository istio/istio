// Copyright 2019 Istio Authors
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

package native

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdsapiCore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	xdsapiListener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoyFilterHttp "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	envoyFilterTcp "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	envoyUtil "github.com/envoyproxy/go-control-plane/pkg/util"
	googleProtobuf6 "github.com/gogo/protobuf/types"

	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/envoy/discovery"
	"istio.io/istio/pkg/test/util/reserveport"
)

const (
	maxStreams   = 100000
	listenerType = "type.googleapis.com/envoy.api.v2.Listener"
	routeType    = "type.googleapis.com/envoy.api.v2.RouteConfiguration"
	clusterType  = "type.googleapis.com/envoy.api.v2.Cluster"
)

var (
	outboundHTTPListenerNamePattern = regexp.MustCompile("0.0.0.0_[0-9]+")
)

// discoveryFilter is an XDS filter sitting between Pilot and Envoy that injects changes into the
// Envoy config to support running applications natively (i.e. no iptables).
type discoveryFilter interface {
	// Stop the discoveryFilter
	Stop()

	// GetDiscoveryPort returns the port for the discovery filter server.
	GetDiscoveryAddress() *net.TCPAddr

	// GetBoundOutboundListenerPort returns a port bound to the agent's Envoy listener. Traffic
	// sent to this port will automatically be sent to the outbound cluster. Pilot assumes that
	// outbound traffic will automatically be redirected to Envoy, so it just sends the
	// port for the remote service. To make outbound requests go through Envoy, however, the
	// native agent needs to bind a real outbound port for the listener and all outbound requests
	// must pass through that port.
	GetBoundOutboundListenerPort(portFromPilot int) (int, error)
}

func newDiscoverFilter(discoveryAddress string, portManager reserveport.PortManager) (f discoveryFilter, err error) {
	out := &discoveryFilterImpl{
		discoveryAddress: discoveryAddress,
		portManager:      portManager,
		boundPortMap:     make(map[int]int),
	}

	defer func() {
		if err != nil {
			out.Stop()
		}
	}()

	out.discoveryFilter = &discovery.Filter{
		DiscoveryAddr: discoveryAddress,
		FilterFunc:    out.filterDiscoveryResponse,
	}

	// Start a GRPC server and register the handlers.
	out.discoveryFilterGrpcServer = grpc.NewServer(grpc.MaxConcurrentStreams(uint32(maxStreams)))
	// get the grpc server wired up
	grpc.EnableTracing = true
	out.discoveryFilter.Register(out.discoveryFilterGrpcServer)

	// Dynamically assign a port for the GRPC server.
	var listener net.Listener
	listener, err = net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}

	// Start the gRPC server for the discovery filter.
	out.discoveryFilterAddr = listener.Addr().(*net.TCPAddr)
	go func() {
		if err := out.discoveryFilterGrpcServer.Serve(listener); err != nil {
			log.Warna(err)
		}
	}()
	return out, nil
}

type discoveryFilterImpl struct {
	portManager               reserveport.PortManager
	discoveryFilterGrpcServer *grpc.Server
	discoveryFilter           *discovery.Filter
	discoveryFilterAddr       *net.TCPAddr
	boundPortMap              map[int]int
	discoveryAddress          string
}

func (a *discoveryFilterImpl) GetBoundOutboundListenerPort(portFromPilot int) (int, error) {
	p, ok := a.boundPortMap[portFromPilot]
	if !ok {
		return 0, fmt.Errorf("failed to find bound outbound port for servicePort %d", portFromPilot)
	}
	return p, nil
}

func (a *discoveryFilterImpl) Stop() {
	if a.discoveryFilterGrpcServer != nil {
		a.discoveryFilterGrpcServer.Stop()
	}
}

func (a *discoveryFilterImpl) GetDiscoveryAddress() *net.TCPAddr {
	return a.discoveryFilterAddr
}

func (a *discoveryFilterImpl) filterDiscoveryResponse(resp *xdsapi.DiscoveryResponse) (*xdsapi.DiscoveryResponse, error) {
	newResponse := xdsapi.DiscoveryResponse{
		TypeUrl:     resp.TypeUrl,
		Canary:      resp.Canary,
		VersionInfo: resp.VersionInfo,
		Nonce:       resp.Nonce,
	}

	for _, any := range resp.Resources {
		switch any.TypeUrl {
		case listenerType:
			l := &xdsapi.Listener{}
			if err := l.Unmarshal(any.Value); err != nil {
				return nil, err
			}

			if isVirtualListener(l) {
				// Exclude the iptables-mapped listener from the Envoy configuration. It's hard-coded to port 15001,
				// which will likely fail to be bound.
				continue
			}

			inbound, err := isInboundListener(l)
			if err != nil {
				return nil, err
			}
			if inbound {
				// This is a dynamic listener generated by Pilot for an inbound port. All inbound ports for the local
				// proxy are built into the static config, so we can safely ignore this listener.
				//
				// In addition, since we're using 127.0.0.1 as the IP address for all services/instances, the external
				// service registry's GetProxyServiceInstances() will mistakenly return instances for all services.
				// This is due to the fact that it uses IP address alone to map the instances. This results in Pilot
				// incorrectly generating inbound listeners for other services. These listeners shouldn't cause any
				// problems, but filtering them out here for correctness and clarity of the Envoy config.
				continue
			}

			outbound, err := isOutboundListener(l)
			if err != nil {
				return nil, err
			}
			if outbound {
				// Bind a real outbound port for this listener.
				if err := a.bindOutboundPort(&any, l); err != nil {
					return nil, err
				}
			}

		case clusterType:
			// Remove any management clusters.
			c := &xdsapi.Cluster{}
			if err := c.Unmarshal(any.Value); err != nil {
				return nil, err
			}
			switch {
			case strings.Contains(c.Name, "mgmtCluster"):
				continue
			}
		}
		newResponse.Resources = append(newResponse.Resources, any)
	}

	// Take a second pass to update routes to use updated listener ports.
	for i, any := range newResponse.Resources {
		switch any.TypeUrl {
		case routeType:
			r := &xdsapi.RouteConfiguration{}
			if err := r.Unmarshal(any.Value); err != nil {
				return nil, err
			}

			// Dynamic routes for outbound ports are named with their port.
			port, err := strconv.Atoi(r.Name)
			if err != nil {
				continue
			}

			// Look up the port to see if we have a custom bound port
			boundPort, ok := a.boundPortMap[port]
			if !ok {
				continue
			}

			modified := false
			for i, vh := range r.VirtualHosts {
				for domainIndex, domain := range vh.Domains {
					parts := strings.Split(domain, ":")
					if len(parts) == 2 {
						modified = true
						r.VirtualHosts[i].Domains[domainIndex] = fmt.Sprintf("%s:%d", parts[0], boundPort)
					}
				}
			}

			if modified {
				// Update the resource.
				b, err := r.Marshal()
				if err != nil {
					return nil, err
				}
				newResponse.Resources[i].Value = b
			}
		}
	}

	return &newResponse, nil
}

func (a *discoveryFilterImpl) bindOutboundPort(any *googleProtobuf6.Any, l *xdsapi.Listener) error {
	portFromPilot := int(l.Address.GetSocketAddress().GetPortValue())
	boundPort, ok := a.boundPortMap[portFromPilot]

	// Bind a real port for the outbound listener if we haven't already.
	if !ok {
		var err error
		boundPort, err = findFreePort(a.portManager)
		if err != nil {
			return err
		}
		a.boundPortMap[portFromPilot] = boundPort
	}

	// Store the bound port in the listener.
	l.Address.GetSocketAddress().PortSpecifier.(*xdsapiCore.SocketAddress_PortValue).PortValue = uint32(boundPort)
	l.DeprecatedV1.BindToPort.Value = true

	// Output this content of the any.
	b, err := l.Marshal()
	if err != nil {
		return err
	}
	any.Value = b
	return nil
}

func isVirtualListener(l *xdsapi.Listener) bool {
	return l.Name == "virtual"
}

// nolint: staticcheck
func getTCPProxyClusterName(filter *xdsapiListener.Filter) (string, error) {
	// First, check if it's using the deprecated v1 format.
	config := filter.GetConfig()
	if config == nil {
		config, _ = envoyUtil.MessageToStruct(filter.GetTypedConfig())
	}
	fields := config.Fields
	deprecatedV1, ok := fields["deprecated_v1"]
	if ok && deprecatedV1.GetBoolValue() {
		v, ok := fields["value"]
		if !ok {
			return "", errors.New("value field missing")
		}
		v, ok = v.GetStructValue().Fields["route_config"]
		if !ok {
			return "", errors.New("route_config field missing")
		}
		v, ok = v.GetStructValue().Fields["routes"]
		if !ok {
			return "", errors.New("routes field missing")
		}
		vs := v.GetListValue().Values
		for _, v = range vs {
			v, ok = v.GetStructValue().Fields["cluster"]
			if ok {
				return v.GetStringValue(), nil
			}
		}
		return "", errors.New("cluster field missing")
	}

	cfg := &envoyFilterTcp.TcpProxy{}
	var err error
	if filter.GetConfig() != nil {
		err = envoyUtil.StructToMessage(filter.GetConfig(), cfg)
	} else {
		err = cfg.Unmarshal(filter.GetTypedConfig().GetValue())
	}
	if err != nil {
		return "", err
	}
	clusterSpec := cfg.ClusterSpecifier.(*envoyFilterTcp.TcpProxy_Cluster)
	if clusterSpec == nil {
		return "", fmt.Errorf("expected TCPProxy cluster")
	}
	return clusterSpec.Cluster, nil
}

// nolint: staticcheck
func isInboundListener(l *xdsapi.Listener) (bool, error) {
	for _, filter := range l.FilterChains[0].Filters {
		switch filter.Name {
		case envoyUtil.HTTPConnectionManager:
			cfg := &envoyFilterHttp.HttpConnectionManager{}
			var err error
			if filter.GetConfig() != nil {
				err = envoyUtil.StructToMessage(filter.GetConfig(), cfg)
			} else {
				err = cfg.Unmarshal(filter.GetTypedConfig().GetValue())
			}
			if err != nil {
				return false, err
			}
			rcfg := cfg.GetRouteConfig()
			if rcfg != nil {
				if strings.HasPrefix(rcfg.Name, "inbound") {
					return true, nil
				}
			}
			return false, nil
		case envoyUtil.TCPProxy:
			clusterName, err := getTCPProxyClusterName(&filter)
			if err != nil {
				return false, err
			}
			return strings.HasPrefix(clusterName, "inbound"), nil
		}
	}

	return false, fmt.Errorf("unable to determine whether the listener is inbound: %s", pb2Json(l))
}

func isOutboundListener(l *xdsapi.Listener) (bool, error) {
	for _, filter := range l.FilterChains[0].Filters {
		switch filter.Name {
		case envoyUtil.HTTPConnectionManager:
			return outboundHTTPListenerNamePattern.MatchString(l.Name), nil
		case envoyUtil.TCPProxy:
			clusterName, err := getTCPProxyClusterName(&filter)
			if err != nil {
				return false, err
			}
			return strings.HasPrefix(clusterName, "outbound"), nil
		}
	}

	return false, fmt.Errorf("unable to determine whether the listener is outbound: %s", pb2Json(l))
}
