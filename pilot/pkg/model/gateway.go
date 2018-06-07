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
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/log"
)

// MergedGateway describes a set of gateways for a workload merged into a single logical gateway.
//
// TODO: do we need a `func (m *MergedGateway) MergeInto(gateway *networking.Gateway)`?
type MergedGateway struct {
	Names map[string]bool

	// maps from physical port to virtual servers
	// Typical value length is always 1 (one server per port)
	// For value length is >1 for HTTPS ports only
	Servers map[uint32][]*networking.Server

	// maps from port names to virtual servers
	// Used for RDS. No two port names share same port except for HTTPS
	ServersWithPortNames map[string]*networking.Server
}

// MergeGateways combines multiple gateways targeting the same workload into a single logical Gateway.
// Note that today any Servers in the combined gateways listening on the same port must have the same protocol.
// If servers with different protocols attempt to listen on the same port, one of the protocols will be chosen at random.
func MergeGateways(gateways ...Config) *MergedGateway {
	names := make(map[string]bool, len(gateways))
	servers := make(map[uint32][]*networking.Server, len(gateways))
	serversWithPortNames := make(map[string]*networking.Server, len(gateways))

	log.Debugf("MergeGateways: merging %d gateways", len(gateways))
	for _, spec := range gateways {
		name := ResolveShortnameToFQDN(spec.Name, spec.ConfigMeta)
		names[name.String()] = true

		gateway := spec.Spec.(*networking.Gateway)
		log.Debugf("MergeGateways: merging gateway %q into %v:\n%v", name, names, gateway)
		for _, s := range gateway.Servers {
			log.Debugf("MergeGateways: gateway %q processing server %v", name, s.Hosts)
			if ss, ok := servers[s.Port.Number]; ok {
				// ss must have at least one element because the key exists in the map, otherwise we'd be in the else case below.
				if ss[0].Port.Protocol != s.Port.Protocol {
					log.Debugf("skipping server: attempting to merge servers for gateway %q into %v but servers have non-HTTPS protocols: want %v have %v",
						spec.Name, names, ss[0].Port.Protocol, s.Port.Protocol)
					continue
				} else if ParseProtocol(s.Port.Protocol) != ProtocolHTTPS {
					// Merge only HTTPS servers with different port names
					log.Debugf("skipping server: attempting to merge non-HTTPS servers (%v protocol) for gateway %q into %v",
						s.Port.Protocol, spec.Name, names)
					continue
				} else {
					// both servers are HTTPS servers. Make sure the port names are different so that RDS can pick out individual servers
					if _, exists := serversWithPortNames[s.Port.Name]; exists {
						log.Debugf("skipping server: attempting to merge HTTPS servers for gateway %q into %v - port name %v already taken",
							spec.Name, names, s.Port.Name)
						continue
					}
				}
				servers[s.Port.Number] = append(ss, s)
				serversWithPortNames[s.Port.Name] = s
			} else {
				if _, exists := serversWithPortNames[s.Port.Name]; exists {
					log.Debugf("skipping server: attempting to merge servers for gateway %q into %v - port name %v already taken",
						spec.Name, names, s.Port.Name)
					continue
				}

				servers[s.Port.Number] = []*networking.Server{s}
				serversWithPortNames[s.Port.Name] = s
			}
			log.Debugf("MergeGateways: gateway %q merged server %v", name, s.Hosts)
		}
	}

	return &MergedGateway{
		Names:                names,
		Servers:              servers,
		ServersWithPortNames: serversWithPortNames,
	}
}
