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

package iptables

import (
	"net/netip"

	"istio.io/istio/cni/pkg/scopes"
	istiolog "istio.io/istio/pkg/log"
)

// For inpod rules, any runtime/dynamic pod-level
// config overrides that may need to be taken into account
// when injecting pod rules
type PodLevelOverrides struct {
	VirtualInterfaces []string
	IngressMode       bool
	DNSProxy          PodDNSOverride
}

type PodDNSOverride int

const (
	PodDNSUnset PodDNSOverride = iota
	PodDNSEnabled
	PodDNSDisabled
)

const (
	DNSCapturePort              = 15053
	ZtunnelInboundPort          = 15008
	ZtunnelOutboundPort         = 15001
	ZtunnelInboundPlaintextPort = 15006
	Localhost                   = "127.0.0.1"
)

type IptablesConfig struct {
	TraceLogging           bool       `json:"IPTABLES_TRACE_LOGGING"`
	EnableIPv6             bool       `json:"ENABLE_INBOUND_IPV6"`
	RedirectDNS            bool       `json:"REDIRECT_DNS"`
	HostProbeSNATAddress   netip.Addr `json:"HOST_PROBE_SNAT_ADDRESS"`
	HostProbeV6SNATAddress netip.Addr `json:"HOST_PROBE_V6_SNAT_ADDRESS"`
	Reconcile              bool       `json:"RECONCILE"`
	CleanupOnly            bool       `json:"CLEANUP_ONLY"`
	ForceApply             bool       `json:"FORCE_APPLY"`
}

type InPodRouter interface {
	// by convention use 1st Addr for ipv4 and 2nd for ipv6 SNAT host
	CreateInpodRules(*istiolog.Scope, PodLevelOverrides) error
	DeleteInpodRules(*istiolog.Scope) error
	ReconcileModeEnabled() bool
}

type HostRuler interface {
	// by convention use 1st Addr for ipv4 and 2nd for ipv6 SNAT host
	CreateHostRulesForHealthChecks() error
	DeleteHostRules()
}

var log = scopes.CNIAgent
