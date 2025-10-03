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

package model

import (
	"strconv"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/gateway"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/util/sets"
)

// ServerPort defines port for the gateway server.
type ServerPort struct {
	// A valid non-negative integer port number.
	Number uint32
	// The protocol exposed on the port.
	Protocol string
	// The bind server specified on this port.
	Bind string
}

// MergedServers describes set of servers defined in all gateways per port.
type MergedServers struct {
	Servers   []*networking.Server
	RouteName string // RouteName for http servers. For HTTPS, TLSServerInfo will hold the route name.
}

// TLSServerInfo contains additional information for TLS Servers.
type TLSServerInfo struct {
	RouteName string
	SNIHosts  []string
}

// MergedGateway describes a set of gateways for a workload merged into a single logical gateway.
type MergedGateway struct {
	// ServerPorts maintains a list of unique server ports, used for stable ordering.
	ServerPorts []ServerPort

	// MergedServers map from physical port to virtual servers
	// using TCP protocols (like HTTP1.1, H2, mysql, redis etc)
	MergedServers map[ServerPort]*MergedServers

	// MergedQUICTransportServers map from physical port to servers listening
	// on QUIC (like HTTP3). Currently the support is experimental and
	// is limited to HTTP3 only
	MergedQUICTransportServers map[ServerPort]*MergedServers

	// HTTP3AdvertisingRoutes represents the set of HTTP routes which advertise HTTP/3.
	// This mapping is used to generate alt-svc header that is needed for HTTP/3 server discovery.
	HTTP3AdvertisingRoutes sets.String

	// GatewayNameForServer maps from server to the owning gateway name.
	// Used for select the set of virtual services that apply to a port.
	GatewayNameForServer map[*networking.Server]string

	// ServersByRouteName maps from port names to virtual hosts
	// Used for RDS. No two port names share same port except for HTTPS
	// The typical length of the value is always 1, except for HTTP (not HTTPS),
	ServersByRouteName map[string][]*networking.Server

	// TLSServerInfo maps from server to a corresponding TLS information like TLS Routename and SNIHosts.
	TLSServerInfo map[*networking.Server]*TLSServerInfo

	// ContainsAutoPassthroughGateways determines if there are any type AUTO_PASSTHROUGH Gateways, requiring additional
	// clusters to be sent to the workload
	ContainsAutoPassthroughGateways bool

	// AutoPassthroughSNIHosts
	AutoPassthroughSNIHosts sets.Set[string]

	// PortMap defines a mapping of targetPorts to the set of Service ports that reference them
	PortMap GatewayPortMap

	// VerifiedCertificateReferences contains a set of all credentialNames referenced by gateways *in the same namespace as the proxy*.
	// These are considered "verified", since there is mutually agreement from the pod, Secret, and Gateway, as all
	// reside in the same namespace and trust boundary.
	// Note: Secrets that are not referenced by any Gateway, but are in the same namespace as the pod, are explicitly *not*
	// included. This ensures we don't give permission to unexpected secrets, such as the citadel root key/cert.
	VerifiedCertificateReferences sets.String
}

func (g *MergedGateway) HasAutoPassthroughGateways() bool {
	if g != nil {
		return g.ContainsAutoPassthroughGateways
	}
	return false
}

// PrevMergedGateway describes previous state of the gateway.
// Currently, it only contains information relevant for auto passthrough gateways used by CDS.
type PrevMergedGateway struct {
	ContainsAutoPassthroughGateways bool
	AutoPassthroughSNIHosts         sets.Set[string]
}

func (g *PrevMergedGateway) HasAutoPassthroughGateway() bool {
	if g != nil {
		return g.ContainsAutoPassthroughGateways
	}
	return false
}

func (g *PrevMergedGateway) GetAutoPassthroughSNIHosts() sets.Set[string] {
	if g != nil {
		return g.AutoPassthroughSNIHosts
	}
	return sets.Set[string]{}
}

var (
	typeTag = monitoring.CreateLabel("type")
	nameTag = monitoring.CreateLabel("name")

	totalRejectedConfigs = monitoring.NewSum(
		"pilot_total_rejected_configs",
		"Total number of configs that Pilot had to reject or ignore.",
	)
)

func RecordRejectedConfig(gatewayName string) {
	totalRejectedConfigs.With(typeTag.Value("gateway"), nameTag.Value(gatewayName)).Increment()
}

// DisableGatewayPortTranslationLabel is a label on Service that declares that, for that particular
// service, we should not translate Gateway ports to target ports. For example, if I have a Service
// on port 80 with target port 8080, with the label. Gateways on port 80 would *not* match. Instead,
// only Gateways on port 8080 would be used. This prevents ambiguities when there are multiple
// Services on port 80 referring to different target ports. Long term, this will be replaced by
// Gateways directly referencing a Service, rather than label selectors. Warning: this label is
// intended solely for as a workaround for Knative's Istio integration, and not intended for any
// other usage. It can, and will, be removed immediately after the new direct reference is ready for
// use.
const DisableGatewayPortTranslationLabel = "experimental.istio.io/disable-gateway-port-translation"

// mergeGateways combines multiple gateways targeting the same workload into a single logical Gateway.
// Note that today any Servers in the combined gateways listening on the same port must have the same protocol.
// If servers with different protocols attempt to listen on the same port, one of the protocols will be chosen at random.
func mergeGateways(gateways []gatewayWithInstances, proxy *Proxy, ps *PushContext) *MergedGateway {
	gatewayPorts := sets.New[uint32]()
	nonPlainTextGatewayPortsBindMap := map[uint32]sets.String{}
	mergedServers := make(map[ServerPort]*MergedServers)
	mergedQUICServers := make(map[ServerPort]*MergedServers)
	serverPorts := make([]ServerPort, 0)
	plainTextServers := make(map[uint32]ServerPort)
	serversByRouteName := make(map[string][]*networking.Server)
	tlsServerInfo := make(map[*networking.Server]*TLSServerInfo)
	gatewayNameForServer := make(map[*networking.Server]string)
	verifiedCertificateReferences := sets.New[string]()
	http3AdvertisingRoutes := sets.New[string]()
	tlsHostsByPort := map[uint32]map[string]string{} // port -> host/bind map
	autoPassthrough := false

	log.Debugf("mergeGateways: merging %d gateways", len(gateways))
	for _, gwAndInstance := range gateways {
		gatewayConfig := gwAndInstance.gateway
		gatewayName := gatewayConfig.Namespace + "/" + gatewayConfig.Name // Format: %s/%s
		gatewayCfg := gatewayConfig.Spec.(*networking.Gateway)
		log.Debugf("mergeGateways: merging gateway %q :\n%v", gatewayName, gatewayCfg)
		snames := sets.String{}
		for _, s := range gatewayCfg.Servers {
			if len(s.Name) > 0 {
				if snames.InsertContains(s.Name) {
					log.Warnf("Server name %s is not unique in gateway %s and may create possible issues like stat prefix collision ",
						s.Name, gatewayName)
				}
			}
			if s.Port == nil {
				// Should be rejected in validation, this is an extra check
				log.Debugf("invalid server without port: %q", gatewayName)
				RecordRejectedConfig(gatewayName)
				continue
			}
			sanitizeServerHostNamespace(s, gatewayConfig.Namespace)
			gatewayNameForServer[s] = gatewayName
			log.Debugf("mergeGateways: gateway %q processing server %s :%v", gatewayName, s.Name, s.Hosts)

			expectedSA := gatewayConfig.Annotations[constants.InternalServiceAccount]
			identityVerified := proxy.VerifiedIdentity != nil &&
				proxy.VerifiedIdentity.Namespace == gatewayConfig.Namespace &&
				(proxy.VerifiedIdentity.ServiceAccount == expectedSA || expectedSA == "")
			cn := s.GetTls().GetCredentialName()
			if cn != "" && identityVerified {
				// Ignore BuiltinGatewaySecretTypeURI, as it is not referencing a Secret at all
				if !strings.HasPrefix(cn, credentials.BuiltinGatewaySecretTypeURI) {
					rn := credentials.ToResourceName(cn)
					parse, err := credentials.ParseResourceName(rn, proxy.VerifiedIdentity.Namespace, "", "")
					if err == nil && gatewayConfig.Namespace == proxy.VerifiedIdentity.Namespace && parse.Namespace == proxy.VerifiedIdentity.Namespace {
						// Same namespace is always allowed
						verifiedCertificateReferences.Insert(rn)
						if s.GetTls().GetMode() == networking.ServerTLSSettings_MUTUAL {
							verifiedCertificateReferences.Insert(rn + credentials.SdsCaSuffix)
						}
					} else if ps.ReferenceAllowed(gvk.Secret, rn, proxy.VerifiedIdentity.Namespace) {
						// Explicitly allowed by some policy
						verifiedCertificateReferences.Insert(rn)
						if s.GetTls().GetMode() == networking.ServerTLSSettings_MUTUAL {
							verifiedCertificateReferences.Insert(rn + credentials.SdsCaSuffix)
						}
					}
				}
			}
			for _, resolvedPort := range resolvePorts(s.Port.Number, gwAndInstance.instances, gwAndInstance.legacyGatewaySelector) {
				routeName := gatewayRDSRouteName(s, resolvedPort, gatewayConfig)
				if s.Tls != nil {
					// Envoy will reject config that has multiple filter chain matches with the same matching rules.
					// To avoid this, we need to make sure we don't have duplicated hosts, which will become
					// SNI filter chain matches.

					// When there is Bind specified in the Gateway, the listener is built per IP instead of
					// sharing one wildcard listener. So different Gateways can
					// have same host as long as they have different Bind.
					if tlsHostsByPort[resolvedPort] == nil {
						tlsHostsByPort[resolvedPort] = map[string]string{}
					}
					if duplicateHosts := CheckDuplicates(s.Hosts, s.Bind, tlsHostsByPort[resolvedPort]); len(duplicateHosts) != 0 {
						log.Warnf("skipping server on gateway %s, duplicate host names: %v", gatewayName, duplicateHosts)
						RecordRejectedConfig(gatewayName)
						continue
					}
					tlsServerInfo[s] = &TLSServerInfo{SNIHosts: GetSNIHostsForServer(s), RouteName: routeName}
					if s.Tls.Mode == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
						autoPassthrough = true
					}
				}
				serverPort := ServerPort{resolvedPort, s.Port.Protocol, s.Bind}
				serverProtocol := protocol.Parse(serverPort.Protocol)
				if gatewayPorts.Contains(resolvedPort) {
					// We have two servers on the same port. Should we merge?
					// 1. Yes if both servers are plain text and HTTP
					// 2. Yes if both servers are using TLS
					//    if using HTTPS ensure that port name is distinct so that we can setup separate RDS
					//    for each server (as each server ends up as a separate http connection manager due to filter chain match)
					// 3. No for everything else.
					if current, exists := plainTextServers[resolvedPort]; exists {
						if !canMergeProtocols(serverProtocol, protocol.Parse(current.Protocol)) && current.Bind == serverPort.Bind {
							log.Infof("skipping server on gateway %s port %s.%d.%s: conflict with existing server %d.%s",
								gatewayConfig.Name, s.Port.Name, resolvedPort, s.Port.Protocol, serverPort.Number, serverPort.Protocol)
							RecordRejectedConfig(gatewayName)
							continue
						}
						// For TCP gateway/route the route name is empty but if they are different binds, should continue to generate the listener
						// i.e gateway 10.0.0.1:8000:TCP should not conflict with 10.0.0.2:8000:TCP
						if routeName == "" && current.Bind == serverPort.Bind {
							log.Debugf("skipping server on gateway %s port %s.%d.%s: could not build RDS name from server",
								gatewayConfig.Name, s.Port.Name, resolvedPort, s.Port.Protocol)
							RecordRejectedConfig(gatewayName)
							continue
						}
						if current.Bind != serverPort.Bind {
							// Merge it to servers with the same port and bind.
							if mergedServers[serverPort] == nil {
								mergedServers[serverPort] = &MergedServers{Servers: []*networking.Server{}}
								serverPorts = append(serverPorts, serverPort)
							}
							ms := mergedServers[serverPort]
							ms.RouteName = routeName
							ms.Servers = append(ms.Servers, s)
						} else {
							// Merge this to current known port with same bind.
							ms := mergedServers[current]
							ms.Servers = append(ms.Servers, s)
						}
						serversByRouteName[routeName] = append(serversByRouteName[routeName], s)
					} else {
						// We have duplicate port. Its not in plaintext servers. So, this has to be a TLS server.
						// Check if this is also a HTTP server and if so, ensure uniqueness of port name.
						if gateway.IsHTTPServer(s) {
							if routeName == "" {
								log.Debugf("skipping server on gateway %s port %s.%d.%s: could not build RDS name from server",
									gatewayConfig.Name, s.Port.Name, resolvedPort, s.Port.Protocol)
								RecordRejectedConfig(gatewayName)
								continue
							}

							// Both servers are HTTPS servers. Make sure the port names are different so that RDS can pick out individual servers.
							// We cannot have two servers with same port name because we need the port name to distinguish one HTTPS server from another.
							// We cannot merge two HTTPS servers even if their TLS settings have same path to the keys, because we don't know if the contents
							// of the keys are same. So we treat them as effectively different TLS settings.
							// This check is largely redundant now since we create rds names for https using gateway name, namespace
							// and validation ensures that all port names within a single gateway config are unique.
							if _, exists := serversByRouteName[routeName]; exists {
								log.Infof("skipping server on gateway %s port %s.%d.%s: non unique port name for HTTPS port",
									gatewayConfig.Name, s.Port.Name, resolvedPort, s.Port.Protocol)
								RecordRejectedConfig(gatewayName)
								continue
							}
							serversByRouteName[routeName] = []*networking.Server{s}
						}
						// build the port bind map for none plain text protocol, thus can avoid protocol conflict if it's different bind
						var newBind bool
						if bindsPortMap, ok := nonPlainTextGatewayPortsBindMap[resolvedPort]; ok {
							newBind = !bindsPortMap.InsertContains(serverPort.Bind)
						} else {
							nonPlainTextGatewayPortsBindMap[resolvedPort] = sets.New(serverPort.Bind)
							newBind = true
						}
						// If the bind/port combination is not being used as non-plaintext, they are different
						// listeners and won't get conflicted even with same port different protocol
						// i.e 0.0.0.0:443:GRPC/1.0.0.1:443:GRPC/1.0.0.2:443:HTTPS they are not conflicted, otherwise
						// We have another TLS server on the same port. Can differentiate servers using SNI
						if s.Tls == nil && !newBind {
							log.Warnf("TLS server without TLS options %s %s", gatewayName, s.String())
							RecordRejectedConfig(gatewayName)
							continue
						}
						if mergedServers[serverPort] == nil {
							mergedServers[serverPort] = &MergedServers{Servers: []*networking.Server{s}}
							serverPorts = append(serverPorts, serverPort)
						} else {
							mergedServers[serverPort].Servers = append(mergedServers[serverPort].Servers, s)
						}

						// We have TLS settings defined and we have already taken care of unique route names
						// if it is HTTPS. So we can construct a QUIC server on the same port. It is okay as
						// QUIC listens on UDP port, not TCP
						if features.EnableQUICListeners && gateway.IsEligibleForHTTP3Upgrade(s) &&
							udpSupportedPort(s.GetPort().GetNumber(), gwAndInstance.instances) {
							log.Debugf("Server at port %d eligible for HTTP3 upgrade. Add UDP listener for QUIC", serverPort.Number)
							if mergedQUICServers[serverPort] == nil {
								mergedQUICServers[serverPort] = &MergedServers{Servers: []*networking.Server{}}
							}
							mergedQUICServers[serverPort].Servers = append(mergedQUICServers[serverPort].Servers, s)
							http3AdvertisingRoutes.Insert(routeName)
						}
					}
				} else {
					// This is a new gateway on this port. Create MergedServers for it.
					gatewayPorts.Insert(resolvedPort)
					if !gateway.IsTLSServer(s) {
						plainTextServers[serverPort.Number] = serverPort
					}
					if gateway.IsHTTPServer(s) {
						serversByRouteName[routeName] = []*networking.Server{s}

						if features.EnableQUICListeners && gateway.IsEligibleForHTTP3Upgrade(s) &&
							udpSupportedPort(s.GetPort().GetNumber(), gwAndInstance.instances) {
							log.Debugf("Server at port %d eligible for HTTP3 upgrade. So QUIC listener will be added", serverPort.Number)
							http3AdvertisingRoutes.Insert(routeName)

							if mergedQUICServers[serverPort] == nil {
								// This should be treated like non-passthrough HTTPS case. There will be multiple filter
								// chains, multiple routes per server port. So just like in TLS server case we do not
								// track route name here. Instead, TLS server info is used (it is fine for now because
								// this would be a mirror of an existing non-passthrough HTTPS server)
								mergedQUICServers[serverPort] = &MergedServers{Servers: []*networking.Server{s}}
							}
						}
					}
					mergedServers[serverPort] = &MergedServers{Servers: []*networking.Server{s}, RouteName: routeName}
					serverPorts = append(serverPorts, serverPort)
				}
				log.Debugf("mergeGateways: gateway %q merged server %v", gatewayName, s.Hosts)
			}
		}
	}
	autoPassthroughSNIHosts := sets.Set[string]{}
	if autoPassthrough {
		for _, tls := range mergedServers {
			for _, s := range tls.Servers {
				if s.GetTls().GetMode() == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
					autoPassthroughSNIHosts.InsertAll(s.Hosts...)
				}
			}
		}
	}
	return &MergedGateway{
		MergedServers:                   mergedServers,
		MergedQUICTransportServers:      mergedQUICServers,
		ServerPorts:                     serverPorts,
		GatewayNameForServer:            gatewayNameForServer,
		TLSServerInfo:                   tlsServerInfo,
		ServersByRouteName:              serversByRouteName,
		HTTP3AdvertisingRoutes:          http3AdvertisingRoutes,
		ContainsAutoPassthroughGateways: autoPassthrough,
		AutoPassthroughSNIHosts:         autoPassthroughSNIHosts,
		PortMap:                         getTargetPortMap(serversByRouteName),
		VerifiedCertificateReferences:   verifiedCertificateReferences,
	}
}

func (g *MergedGateway) GetAutoPassthroughGatewaySNIHosts() sets.Set[string] {
	if g != nil {
		return g.AutoPassthroughSNIHosts
	}
	return sets.Set[string]{}
}

func udpSupportedPort(number uint32, instances []ServiceTarget) bool {
	for _, w := range instances {
		if int(number) == w.Port.Port && w.Port.Protocol == protocol.UDP {
			return true
		}
	}
	return false
}

// resolvePorts takes a Gateway port, and resolves it to the port that will actually be listened on.
// When legacyGatewaySelector=false, then the gateway is directly referencing a Service. In this
// case, the translation is un-ambiguous - we just find the matching port and return the targetPort
// When legacyGatewaySelector=true things are a bit more complex, as we support referencing a Service
// port and translating to the targetPort in addition to just directly referencing a port. In this
// case, we just make a best effort guess by picking the first match.
func resolvePorts(number uint32, instances []ServiceTarget, legacyGatewaySelector bool) []uint32 {
	ports := sets.New[uint32]()
	for _, w := range instances {
		if _, disablePortTranslation := w.Service.Attributes.Labels[DisableGatewayPortTranslationLabel]; disablePortTranslation && legacyGatewaySelector {
			// Skip this Service, they opted out of port translation
			// This is only done for legacyGatewaySelector, as the new gateway selection mechanism *only* allows
			// referencing the Service port, and references are un-ambiguous.
			continue
		}
		if w.Port.Port == int(number) {
			if legacyGatewaySelector {
				// When we are using legacy gateway label selection, we only resolve to a single port
				// This has pros and cons; we don't allow merging of routes when it would be desirable, but
				// we also avoid accidentally merging routes when we didn't intend to. While neither option is great,
				// picking the first one here preserves backwards compatibility.
				return []uint32{w.Port.TargetPort}
			}
			ports.Insert(w.Port.TargetPort)
		}
	}
	ret := ports.UnsortedList()
	if len(ret) == 0 && legacyGatewaySelector {
		// When we are using legacy gateway label selection, we should bind to the port as-is if there is
		// no matching ServiceInstance.
		return []uint32{number}
	}
	// For cases where we are directly referencing a Service, we know that they port *must* be in the Service,
	// so we have no fallback. If there was no match, the Gateway is a no-op.
	return ret
}

func canMergeProtocols(current protocol.Instance, p protocol.Instance) bool {
	return (current.IsHTTP() || current == p) && p.IsHTTP()
}

func GetSNIHostsForServer(server *networking.Server) []string {
	if server.Tls == nil {
		return nil
	}
	// sanitize the server hosts as it could contain hosts of form ns/host
	sniHosts := sets.String{}
	for _, h := range server.Hosts {
		if strings.Contains(h, "/") {
			parts := strings.Split(h, "/")
			h = parts[1]
		}
		// do not add hosts, that have already been added
		sniHosts.Insert(h)
	}
	return sets.SortedList(sniHosts)
}

// CheckDuplicates returns all of the hosts provided that are already known
// If there were no duplicates, all hosts are added to the known hosts.
func CheckDuplicates(hosts []string, bind string, knownHosts map[string]string) []string {
	var duplicates []string
	for _, h := range hosts {
		if existingBind, ok := knownHosts[h]; ok && bind == existingBind {
			duplicates = append(duplicates, h)
		}
	}
	// No duplicates found, so we can mark all of these hosts as known
	if len(duplicates) == 0 {
		for _, h := range hosts {
			knownHosts[h] = bind
		}
	}
	return duplicates
}

// gatewayRDSRouteName generates the RDS route config name for gateway's servers.
// Unlike sidecars where the RDS route name is the listener port number, gateways have a different
// structure for RDS.
// HTTP servers have route name set to http.<portNumber>.
//
//	Multiple HTTP servers can exist on the same port and the code will combine all of them into
//	one single RDS payload for http.<portNumber>
//
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
func gatewayRDSRouteName(server *networking.Server, portNumber uint32, cfg config.Config) string {
	p := protocol.Parse(server.Port.Protocol)
	bind := ""
	if server.Bind != "" {
		bind = "." + server.Bind
	}
	if p.IsHTTP() {
		return "http" + "." + strconv.Itoa(int(portNumber)) + bind // Format: http.%d.%s
	}

	if p == protocol.HTTPS && !gateway.IsPassThroughServer(server) {
		return "https" + "." + strconv.Itoa(int(server.Port.Number)) + "." +
			server.Port.Name + "." + cfg.Name + "." + cfg.Namespace + bind // Format: https.%d.%s.%s.%s.%s
	}

	return ""
}

// ParseGatewayRDSRouteName is used by the EnvoyFilter patching logic to match
// a specific route configuration to patch.
func ParseGatewayRDSRouteName(name string) (portNumber int, portName, gatewayName string) {
	if strings.HasPrefix(name, "http.") {
		// this is a http gateway. Parse port number and return empty string for rest
		port := name[len("http."):]
		portNumber, _ = strconv.Atoi(port)
		return
	} else if strings.HasPrefix(name, "https.") && strings.Count(name, ".") == 4 {
		name = name[len("https."):]
		// format: https.<port>.<port_name>.<gw name>.<gw namespace>
		portNums, rest, ok := strings.Cut(name, ".")
		if !ok {
			return
		}
		portNumber, _ = strconv.Atoi(portNums)
		portName, rest, ok = strings.Cut(rest, ".")
		if !ok {
			return
		}
		gwName, gwNs, ok := strings.Cut(rest, ".")
		if !ok {
			return
		}
		gatewayName = gwNs + "/" + gwName
	}
	return
}

// convert ./host to currentNamespace/Host
// */host to just host
// */* to just *
func sanitizeServerHostNamespace(server *networking.Server, namespace string) {
	for i, h := range server.Hosts {
		if strings.Contains(h, "/") {
			parts := strings.Split(h, "/")
			if parts[0] == "." {
				server.Hosts[i] = namespace + "/" + parts[1] // format: %s/%s
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

type GatewayPortMap map[int]sets.Set[int]

func getTargetPortMap(serversByRouteName map[string][]*networking.Server) GatewayPortMap {
	pm := GatewayPortMap{}
	for r, s := range serversByRouteName {
		portNumber, _, _ := ParseGatewayRDSRouteName(r)
		if _, f := pm[portNumber]; !f {
			pm[portNumber] = sets.New[int]()
		}
		for _, se := range s {
			if se.Port == nil {
				continue
			}
			pm[portNumber].Insert(int(se.Port.Number))
		}
	}
	return pm
}
