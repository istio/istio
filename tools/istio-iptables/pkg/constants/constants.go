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

import (
	"time"

	"istio.io/pkg/env"
)

// iptables tables
const (
	MANGLE = "mangle"
	NAT    = "nat"
	FILTER = "filter"
	RAW    = "raw"
)

// Built-in iptables chains
const (
	INPUT       = "INPUT"
	OUTPUT      = "OUTPUT"
	FORWARD     = "FORWARD"
	PREROUTING  = "PREROUTING"
	POSTROUTING = "POSTROUTING"
)

var BuiltInChainsMap = map[string]struct{}{
	INPUT:       {},
	OUTPUT:      {},
	FORWARD:     {},
	PREROUTING:  {},
	POSTROUTING: {},
}

// Constants used for generating iptables commands
const (
	TCP = "tcp"
	UDP = "udp"

	TPROXY   = "TPROXY"
	RETURN   = "RETURN"
	ACCEPT   = "ACCEPT"
	REJECT   = "REJECT"
	REDIRECT = "REDIRECT"
	MARK     = "MARK"
	CT       = "CT"
	DROP     = "DROP"
)

const (
	// IPVersionSpecific is used as an input to rules that will be replaced with an ip version (v4/v6)
	// specific value
	IPVersionSpecific = "PLACEHOLDER_IP_VERSION_SPECIFIC"
)

// iptables chains
const (
	ISTIOOUTPUT     = "ISTIO_OUTPUT"
	ISTIOINBOUND    = "ISTIO_INBOUND"
	ISTIODIVERT     = "ISTIO_DIVERT"
	ISTIOTPROXY     = "ISTIO_TPROXY"
	ISTIOREDIRECT   = "ISTIO_REDIRECT"
	ISTIOINREDIRECT = "ISTIO_IN_REDIRECT"
)

// Constants used in cobra/viper CLI
const (
	InboundInterceptionMode   = "istio-inbound-interception-mode"
	InboundTProxyMark         = "istio-inbound-tproxy-mark"
	InboundTProxyRouteTable   = "istio-inbound-tproxy-route-table"
	InboundPorts              = "istio-inbound-ports"
	LocalExcludePorts         = "istio-local-exclude-ports"
	ExcludeInterfaces         = "istio-exclude-interfaces"
	ServiceCidr               = "istio-service-cidr"
	ServiceExcludeCidr        = "istio-service-exclude-cidr"
	OutboundPorts             = "istio-outbound-ports"
	LocalOutboundPortsExclude = "istio-local-outbound-ports-exclude"
	EnvoyPort                 = "envoy-port"
	InboundCapturePort        = "inbound-capture-port"
	InboundTunnelPort         = "inbound-tunnel-port"
	ProxyUID                  = "proxy-uid"
	ProxyGID                  = "proxy-gid"
	KubeVirtInterfaces        = "kube-virt-interfaces"
	DryRun                    = "dry-run"
	TraceLogging              = "iptables-trace-logging"
	Clean                     = "clean"
	RestoreFormat             = "restore-format"
	SkipRuleApply             = "skip-rule-apply"
	RunValidation             = "run-validation"
	IptablesProbePort         = "iptables-probe-port"
	ProbeTimeout              = "probe-timeout"
	RedirectDNS               = "redirect-dns"
	DropInvalid               = "drop-invalid"
	CaptureAllDNS             = "capture-all-dns"
	OutputPath                = "output-paths"
	NetworkNamespace          = "network-namespace"
	CNIMode                   = "cni-mode"
)

// Environment variables that deliberately have no equivalent command-line flags.
//
// The variables are defined as env.Var for documentation purposes.
//
// Use viper to resolve the value of the environment variable.
var (
	OwnerGroupsInclude = env.RegisterStringVar("ISTIO_OUTBOUND_OWNER_GROUPS", "*",
		`Comma separated list of groups whose outgoing traffic is to be redirected to Envoy.
A group can be specified either by name or by a numeric GID.
The wildcard character "*" can be used to configure redirection of traffic from all groups.`)

	OwnerGroupsExclude = env.RegisterStringVar("ISTIO_OUTBOUND_OWNER_GROUPS_EXCLUDE", "",
		`Comma separated list of groups whose outgoing traffic is to be excluded from redirection to Envoy.
A group can be specified either by name or by a numeric GID.
Only applies when traffic from all groups (i.e. "*") is being redirected to Envoy.`)
)

const (
	DefaultProxyUID = "1337"
)

// Constants used in environment variables
const (
	DisableRedirectionOnLocalLoopback = "DISABLE_REDIRECTION_ON_LOCAL_LOOPBACK"
	EnvoyUser                         = "ENVOY_USER"
)

// Constants for iptables commands
const (
	IPTABLES         = "iptables"
	IPTABLESRESTORE  = "iptables-restore"
	IPTABLESSAVE     = "iptables-save"
	IP6TABLES        = "ip6tables"
	IP6TABLESRESTORE = "ip6tables-restore"
	IP6TABLESSAVE    = "ip6tables-save"
	NSENTER          = "nsenter"
)

// Constants for syscall
const (
	// sys/socket.h
	SoOriginalDst = 80
)

const (
	DefaultIptablesProbePort = "15002"
	DefaultProbeTimeout      = 5 * time.Second
)

const (
	ValidationContainerName = "istio-validation"
	ValidationErrorCode     = 126
)

// DNS ports
const (
	IstioAgentDNSListenerPort = "15053"
)

const (
	CommandConfigureRoutes = "configure-routes"
)
