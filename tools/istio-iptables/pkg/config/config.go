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

package config

import (
	"encoding/json"
	"time"

	"istio.io/pkg/log"
)

// Command line options
// nolint: maligned
type Config struct {
	ProxyPort               string        `json:"PROXY_PORT"`
	InboundCapturePort      string        `json:"INBOUND_CAPTURE_PORT"`
	InboundTunnelPort       string        `json:"INBOUND_TUNNEL_PORT"`
	ProxyUID                string        `json:"PROXY_UID"`
	ProxyGID                string        `json:"PROXY_GID"`
	InboundInterceptionMode string        `json:"INBOUND_INTERCEPTION_MODE"`
	InboundTProxyMark       string        `json:"INBOUND_TPROXY_MARK"`
	InboundTProxyRouteTable string        `json:"INBOUND_TPROXY_ROUTE_TABLE"`
	InboundPortsInclude     string        `json:"INBOUND_PORTS_INCLUDE"`
	InboundPortsExclude     string        `json:"INBOUND_PORTS_EXCLUDE"`
	OutboundPortsInclude    string        `json:"OUTBOUND_PORTS_INCLUDE"`
	OutboundPortsExclude    string        `json:"OUTBOUND_PORTS_EXCLUDE"`
	OutboundIPRangesInclude string        `json:"OUTBOUND_IPRANGES_INCLUDE"`
	OutboundIPRangesExclude string        `json:"OUTBOUND_IPRANGES_EXCLUDE"`
	KubevirtInterfaces      string        `json:"KUBEVIRT_INTERFACES"`
	IptablesProbePort       uint16        `json:"IPTABLES_PROBE_PORT"`
	ProbeTimeout            time.Duration `json:"PROBE_TIMEOUT"`
	DryRun                  bool          `json:"DRY_RUN"`
	RestoreFormat           bool          `json:"RESTORE_FORMAT"`
	SkipRuleApply           bool          `json:"SKIP_RULE_APPLY"`
	RunValidation           bool          `json:"RUN_VALIDATION"`
	RedirectDNS             bool          `json:"REDIRECT_DNS"`
	CaptureAllDNS           bool          `json:"CAPTURE_ALL_DNS"`
	EnableInboundIPv6       bool          `json:"ENABLE_INBOUND_IPV6"`
	DNSServersV4            []string      `json:"DNS_SERVERS_V4"`
	DNSServersV6            []string      `json:"DNS_SERVERS_V6"`
}

func (c *Config) String() string {
	output, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		log.Fatalf("Unable to marshal config object: %v", err)
	}
	return string(output)
}

func (c *Config) Print() {
	log.Info("Variables:")
	log.Info("----------")
	log.Infof("PROXY_PORT=%s", c.ProxyPort)
	log.Infof("PROXY_INBOUND_CAPTURE_PORT=%s", c.InboundCapturePort)
	log.Infof("PROXY_TUNNEL_PORT=%s", c.InboundTunnelPort)
	log.Infof("PROXY_UID=%s", c.ProxyUID)
	log.Infof("PROXY_GID=%s", c.ProxyGID)
	log.Infof("INBOUND_INTERCEPTION_MODE=%s", c.InboundInterceptionMode)
	log.Infof("INBOUND_TPROXY_MARK=%s", c.InboundTProxyMark)
	log.Infof("INBOUND_TPROXY_ROUTE_TABLE=%s", c.InboundTProxyRouteTable)
	log.Infof("INBOUND_PORTS_INCLUDE=%s", c.InboundPortsInclude)
	log.Infof("INBOUND_PORTS_EXCLUDE=%s", c.InboundPortsExclude)
	log.Infof("OUTBOUND_IP_RANGES_INCLUDE=%s", c.OutboundIPRangesInclude)
	log.Infof("OUTBOUND_IP_RANGES_EXCLUDE=%s", c.OutboundIPRangesExclude)
	log.Infof("OUTBOUND_PORTS_INCLUDE=%s", c.OutboundPortsInclude)
	log.Infof("OUTBOUND_PORTS_EXCLUDE=%s", c.OutboundPortsExclude)
	log.Infof("KUBEVIRT_INTERFACES=%s", c.KubevirtInterfaces)
	log.Infof("ENABLE_INBOUND_IPV6=%t", c.EnableInboundIPv6)
	log.Infof("DNS_CAPTURE=%t", c.RedirectDNS)
	log.Infof("CAPTURE_ALL_DNS=%t", c.CaptureAllDNS)
	log.Infof("DNS_SERVERS=%s,%s", c.DNSServersV4, c.DNSServersV6)
	log.Info("")
}
