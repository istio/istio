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

package model

import (
	"fmt"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
)

// MergedGateway describes a set of gateways for a workload merged into a single logical gateway.
//
// TODO: do we need a `func (m *MergedGateway) MergeInto(gateway *networking.Gateway)`?
type MergedGateway struct {
	// maps from physical port to virtual servers
	Servers map[uint32][]*networking.Server

	// maps from server to the owning gateway name
	// Needed to select the set of virtual services that apply to a port
	GatewayNameForServer map[*networking.Server]string

	// maps from port names to virtual hosts
	// Used for RDS. No two port names share same port except for HTTPS
	// The typical length of the value is always 1, except for HTTP (not HTTPS),
	ServersByRouteName map[string][]*networking.Server

	// maps from server to a corresponding RDS route name
	// Inverse of ServersByRouteName. Returning this as part of merge result allows to keep route name generation logic
	// encapsulated within the model and, as a side effect, to avoid generating route names twice.
	RouteNamesByServer map[*networking.Server]string
}

// MergeGateways combines multiple gateways targeting the same workload into a single logical Gateway.
// Note that today any Servers in the combined gateways listening on the same port must have the same protocol.
// If servers with different protocols attempt to listen on the same port, one of the protocols will be chosen at random.
func MergeGateways(gateways ...Config) *MergedGateway {
	names := make(map[string]bool, len(gateways))
	gatewayPorts := make(map[uint32]bool)
	servers := make(map[uint32][]*networking.Server)
	tlsServers := make(map[uint32][]*networking.Server)
	plaintextServers := make(map[uint32][]*networking.Server)
	serversByRouteName := make(map[string][]*networking.Server)
	routeNamesByServer := make(map[*networking.Server]string)
	gatewayNameForServer := make(map[*networking.Server]string)

	log.Debugf("MergeGateways: merging %d gateways", len(gateways))
	for _, config := range gateways {
		gatewayName := fmt.Sprintf("%s/%s", config.Namespace, config.Name)
		names[gatewayName] = true

		gateway := config.Spec.(*networking.Gateway)
		log.Debugf("MergeGateways: merging gateway %q into %v:\n%v", gatewayName, names, gateway)
		for _, s := range gateway.Servers {
			sanitizeServerHostNamespace(s, config.Namespace)
			gatewayNameForServer[s] = gatewayName
			log.Debugf("MergeGateways: gateway %q processing server %v", gatewayName, s.Hosts)
			protocol := ParseProtocol(s.Port.Protocol)
			if gatewayPorts[s.Port.Number] {
				// We have two servers on the same port. Should we merge?
				// 1. Yes if both servers are plain text and HTTP
				// 2. Yes if both servers are using TLS
				//    if using HTTPS ensure that port name is distinct so that we can setup separate RDS
				//    for each server (as each server ends up as a separate http connection manager due to filter chain match
				// 3. No for everything else.

				if p, exists := plaintextServers[s.Port.Number]; exists {
					currentProto := ParseProtocol(p[0].Port.Protocol)
					if currentProto != protocol || !protocol.IsHTTP() {
						log.Debugf("skipping server on gateway %s port %s.%d.%s: conflict with existing server %s.%d.%s",
							config.Name, s.Port.Name, s.Port.Number, s.Port.Protocol, p[0].Port.Name, p[0].Port.Number, p[0].Port.Protocol)
						continue
					}
					routeName := gatewayRDSRouteName(s, config)
					if routeName == "" {
						log.Debugf("skipping server on gateway %s port %s.%d.%s: could not build RDS name from server",
							config.Name, s.Port.Name, s.Port.Number, s.Port.Protocol)
						continue
					}
					serversByRouteName[routeName] = append(serversByRouteName[routeName], s)
					routeNamesByServer[s] = routeName
				} else {
					// We have duplicate port. Its not in plaintext servers. So, this has to be in TLS servers
					// Check if this is also a HTTP server and if so, ensure uniqueness of port name
					if IsHTTPServer(s) {
						routeName := gatewayRDSRouteName(s, config)
						if routeName == "" {
							log.Debugf("skipping server on gateway %s port %s.%d.%s: could not build RDS name from server",
								config.Name, s.Port.Name, s.Port.Number, s.Port.Protocol)
							continue
						}

						// both servers are HTTPS servers. Make sure the port names are different so that RDS can pick out individual servers
						// WE cannot have two servers with same port name because we need the port name to distinguish one HTTPS server from another
						// WE cannot merge two HTTPS servers even if their TLS settings have same path to the keys, because we don't know if the contents
						// of the keys are same. So we treat them as effectively different TLS settings.
						// This check is largely redundant now since we create rds names for https using gateway name, namespace
						// and validation ensures that all port names within a single gateway config are unique
						if _, exists := serversByRouteName[routeName]; exists {
							log.Infof("skipping server on gateway %s port %s.%d.%s: non unique port name for HTTPS port",
								config.Name, s.Port.Name, s.Port.Number, s.Port.Protocol)
							continue
						}
						serversByRouteName[routeName] = []*networking.Server{s}
						routeNamesByServer[s] = routeName
					}

					// We have another TLS server on the same port. Can differentiate servers using SNI
					if s.Tls == nil {
						log.Warnf("TLS server without TLS options %s %s", gatewayName, s.String())
						continue
					}
					tlsServers[s.Port.Number] = append(tlsServers[s.Port.Number], s)
				}
			} else {
				gatewayPorts[s.Port.Number] = true
				if IsTLSServer(s) {
					tlsServers[s.Port.Number] = []*networking.Server{s}
				} else {
					plaintextServers[s.Port.Number] = []*networking.Server{s}
				}

				if IsHTTPServer(s) {
					routeName := gatewayRDSRouteName(s, config)
					serversByRouteName[routeName] = []*networking.Server{s}
					routeNamesByServer[s] = routeName
				}
			}
			log.Debugf("MergeGateways: gateway %q merged server %v", gatewayName, s.Hosts)
		}
	}

	// Concatenate both sets of servers
	for p, v := range tlsServers {
		servers[p] = v
	}
	for p, v := range plaintextServers {
		servers[p] = v
	}

	return &MergedGateway{
		Servers:              servers,
		GatewayNameForServer: gatewayNameForServer,
		ServersByRouteName:   serversByRouteName,
		RouteNamesByServer:   routeNamesByServer,
	}
}

// IsTLSServer returns true if this server is non HTTP, with some TLS settings for termination/passthrough
func IsTLSServer(server *networking.Server) bool {
	if server.Tls != nil && !ParseProtocol(server.Port.Protocol).IsHTTP() {
		return true
	}
	return false
}

// IsHTTPServer returns true if this server is using HTTP or HTTPS with termination
func IsHTTPServer(server *networking.Server) bool {
	protocol := ParseProtocol(server.Port.Protocol)
	if protocol.IsHTTP() {
		return true
	}

	if protocol == ProtocolHTTPS && server.Tls != nil && !IsPassThroughServer(server) {
		return true
	}

	return false
}

// IsPassThroughServer returns true if this server does TLS passthrough (auto or manual)
func IsPassThroughServer(server *networking.Server) bool {
	if server.Tls == nil {
		return false
	}

	if server.Tls.Mode == networking.Server_TLSOptions_PASSTHROUGH ||
		server.Tls.Mode == networking.Server_TLSOptions_AUTO_PASSTHROUGH {
		return true
	}

	return false
}

// gatewayRDSRouteName generates the RDS route config name for gateway's servers.
// Unlike sidecars where the RDS route name is the listener port number, gateways have a different
// structure for RDS.
// HTTP servers have route name set to http.<portNumber>.
//   Multiple HTTP servers can exist on the same port and the code will combine all of them into
//   one single RDS payload for http.<portNumber>
// HTTPS servers with TLS termination (i.e. envoy decoding the content, and making outbound http calls to backends)
// will use route name https.<portNumber>.<portName>.<gatewayName>.<namespace>. HTTPS servers using SNI passthrough or
// non-HTTPS servers (e.g., TCP+TLS) with SNI passthrough will be setup as opaque TCP proxies without terminating
// the SSL connection. They would inspect the SNI header and forward to the appropriate upstream as opaque TCP.
//
// Within HTTPS servers terminating TLS, user could setup multiple servers in the gateway. each server could have
// one or more hosts but have different TLS certificates. In this case, we end up having separate filter chain
// for each server, with the filter chain match matching on the server specific TLS certs and SNI headers.
// We have two options here: either have all filter chains use the same RDS route name (e.g. "443") and expose
// all virtual hosts on that port to every filter chain uniformly or expose only the set of virtual hosts
// configured under the server for those certificates. We adopt the latter approach. In other words, each
// filter chain in the multi-filter-chain listener will have a distinct RDS route name
// (https.<portNumber>.<portName>.<gatewayName>.<namespace>) so that when a RDS request comes in, we serve the virtual
// hosts and associated routes for that server.
//
// Note that the common case is one where multiple servers are exposed under a single multi-SAN cert on a single port.
// In this case, we have a single https.<portNumber>.<portName>.<gatewayName>.<namespace> RDS for the HTTPS server.
// While we can use the same RDS route name for two servers (say HTTP and HTTPS) exposing the same set of hosts on
// different ports, the optimization (one RDS instead of two) could quickly become useless the moment the set of
// hosts on the two servers start differing -- necessitating the need for two different RDS routes.
func gatewayRDSRouteName(server *networking.Server, config Config) string {
	protocol := ParseProtocol(server.Port.Protocol)
	if protocol.IsHTTP() {
		return fmt.Sprintf("http.%d", server.Port.Number)
	}

	if protocol == ProtocolHTTPS && server.Tls != nil && !IsPassThroughServer(server) {
		return fmt.Sprintf("https.%d.%s.%s.%s",
			server.Port.Number, server.Port.Name, config.Name, config.Namespace)
	}

	return ""
}

// convert ./host to currentNamespace/Host
// */host to just host
// */* to just *
func sanitizeServerHostNamespace(server *networking.Server, namespace string) {
	for i, h := range server.Hosts {
		if strings.Contains(h, "/") {
			parts := strings.Split(h, "/")
			if parts[0] == "." {
				server.Hosts[i] = fmt.Sprintf("%s/%s", namespace, parts[1])
			} else if parts[0] == "*" {
				if parts[1] == "*" {
					server.Hosts = []string{"*"}
					return
				}
				server.Hosts[i] = parts[1]
			}
		}
	}
}
