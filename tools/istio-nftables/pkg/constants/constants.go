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

package constants

import "istio.io/istio/pkg/env"

const (
	// Table names used in sideCar mode when applying native nftables rules
	IstioProxyNatTable    = "istio-proxy-nat"
	IstioProxyMangleTable = "istio-proxy-mangle"
	IstioProxyRawTable    = "istio-proxy-raw"

	// Table names used in Ambient mode when applying native nftables rules
	IstioAmbientNatTable    = "istio-ambient-nat"
	IstioAmbientMangleTable = "istio-ambient-mangle"
	IstioAmbientRawTable    = "istio-ambient-raw"

	// Base chains.
	PreroutingChain = "prerouting"
	OutputChain     = "output"

	// Regular chains prefixed with "istio" to distinguish them from base chains
	IstioInboundChain    = "istio-inbound"
	IstioOutputChain     = "istio-output"
	IstioOutputDNSChain  = "istio-output-dns"
	IstioRedirectChain   = "istio-redirect"
	IstioInRedirectChain = "istio-in-redirect"
	IstioDivertChain     = "istio-divert"
	IstioTproxyChain     = "istio-tproxy"
	IstioPreroutingChain = "istio-prerouting"
	IstioDropChain       = "istio-drop"
)

// In TPROXY mode, mark the packet from envoy outbound to app by podIP,
// this is to prevent it being intercepted to envoy inbound listener.
const OutboundMark = "1338"

// DNS ports
const (
	IstioAgentDNSListenerPort = "15053"
)

// Environment variables that deliberately have no equivalent command-line flags.
//
// The variables are defined as env.Var for documentation purposes.
//
// Use viper to resolve the value of the environment variable.
var (
	HostIPv4LoopbackCidr = env.Register("ISTIO_OUTBOUND_IPV4_LOOPBACK_CIDR", "127.0.0.1/32",
		`IPv4 CIDR range used to identify outbound traffic on loopback interface intended for application container`)

	OwnerGroupsInclude = env.Register("ISTIO_OUTBOUND_OWNER_GROUPS", "*",
		`Comma separated list of groups whose outgoing traffic is to be redirected to Envoy.
A group can be specified either by name or by a numeric GID.
The wildcard character "*" can be used to configure redirection of traffic from all groups.`)

	OwnerGroupsExclude = env.Register("ISTIO_OUTBOUND_OWNER_GROUPS_EXCLUDE", "",
		`Comma separated list of groups whose outgoing traffic is to be excluded from redirection to Envoy.
A group can be specified either by name or by a numeric GID.
Only applies when traffic from all groups (i.e. "*") is being redirected to Envoy.`)

	IstioInboundInterceptionMode = env.Register("INBOUND_INTERCEPTION_MODE", "",
		`The mode used to redirect inbound connections to Envoy, either "REDIRECT" or "TPROXY"`)

	IstioInboundTproxyMark = env.Register("INBOUND_TPROXY_MARK", "",
		``)
)

const (
	DefaultProxyUID = "1337"
)
