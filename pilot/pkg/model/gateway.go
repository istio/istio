// Copyright 2018 Istio Authors.
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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/log"
)

// MergedGateway describes a set of gateways for a workload merged into a single logical gateway.
//
// TODO: do we need a `func (m *MergedGateway) MergeInto(gateway *networking.Gateway)`?
type MergedGateway struct {
	Names map[string]bool

	// maps from physical port to virtual servers
	Servers map[uint32][]*networking.Server

	// maps from port names to virtual hosts
	// Used for RDS. No two port names share same port except for HTTPS
	// The typical length of the value is always 1, except for HTTP (not HTTPS),
	RDSRouteConfigNames map[string][]*networking.Server
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
	rdsRouteConfigNames := make(map[string][]*networking.Server)

	log.Debugf("MergeGateways: merging %d gateways", len(gateways))
	for _, spec := range gateways {
		name := ResolveShortnameToFQDN(spec.Name, spec.ConfigMeta)
		names[name.String()] = true

		gateway := spec.Spec.(*networking.Gateway)
		log.Debugf("MergeGateways: merging gateway %q into %v:\n%v", name, names, gateway)
		for _, s := range gateway.Servers {
			log.Debugf("MergeGateways: gateway %q processing server %v", name, s.Hosts)
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
							spec.Name, s.Port.Name, s.Port.Number, s.Port.Protocol, p[0].Port.Name, p[0].Port.Number, p[0].Port.Protocol)
						continue
					}
					rdsName := getRDSRouteName(s)
					rdsRouteConfigNames[rdsName] = append(rdsRouteConfigNames[rdsName], s)
				} else {
					// This has to be a TLS server
					if isHTTPServer(s) {
						rdsName := getRDSRouteName(s)
						// both servers are HTTPS servers. Make sure the port names are different so that RDS can pick out individual servers
						// WE cannot have two servers with same port name because we need the port name to distinguish one HTTPS server from another
						// WE cannot merge two HTTPS servers even if their TLS settings have same path to the keys, because we don't know if the contents
						// of the keys are same. So we treat them as effectively different TLS settings.
						if _, exists := rdsRouteConfigNames[rdsName]; exists {
							log.Debugf("skipping server on gateway %s port %s.%d.%s: non unique port name for HTTPS port",
								spec.Name, s.Port.Name, s.Port.Number, s.Port.Protocol)
							continue
						}
						rdsRouteConfigNames[rdsName] = []*networking.Server{s}
					}
					tlsServers[s.Port.Number] = append(tlsServers[s.Port.Number], s)
				}
			} else {
				gatewayPorts[s.Port.Number] = true
				if isTLSServer(s) {
					tlsServers[s.Port.Number] = []*networking.Server{s}
				} else {
					plaintextServers[s.Port.Number] = []*networking.Server{s}
				}

				if isHTTPServer(s) {
					rdsRouteConfigNames[getRDSRouteName(s)] = []*networking.Server{s}
				}
			}
			log.Debugf("MergeGateways: gateway %q merged server %v", name, s.Hosts)
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
		Names:               names,
		Servers:             servers,
		RDSRouteConfigNames: rdsRouteConfigNames,
	}
}

func isTLSServer(server *networking.Server) bool {
	if server.Tls == nil || ParseProtocol(server.Port.Protocol).IsHTTP() {
		return false
	}
	return true
}

func isHTTPServer(server *networking.Server) bool {
	protocol := ParseProtocol(server.Port.Protocol)
	if protocol.IsHTTP() {
		return true
	}

	if protocol == ProtocolHTTPS && server.Tls != nil && server.Tls.Mode != networking.Server_TLSOptions_PASSTHROUGH {
		return true
	}

	return false
}

func getRDSRouteName(server *networking.Server) string {
	protocol := ParseProtocol(server.Port.Protocol)
	if protocol.IsHTTP() {
		return fmt.Sprintf("http.%d", server.Port.Number)
	}

	if protocol == ProtocolHTTPS && server.Tls != nil && server.Tls.Mode != networking.Server_TLSOptions_PASSTHROUGH {
		return fmt.Sprintf("https.%d.%s", server.Port.Number, server.Port.Name)
	}

	return ""
}
