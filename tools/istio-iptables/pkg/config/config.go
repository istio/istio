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
	"fmt"
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
	EnableInboundIPv6       bool          `json:"ENABLE_INBOUND_IPV6"`
}

func (c *Config) String() string {
	output, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		log.Fatalf("Unable to marshal config object: %v", err)
	}
	return string(output)
}

func (c *Config) Print() {
	fmt.Println("Variables:")
	fmt.Println("----------")
	fmt.Printf("PROXY_PORT=%s\n", c.ProxyPort)
	fmt.Printf("PROXY_INBOUND_CAPTURE_PORT=%s\n", c.InboundCapturePort)
	fmt.Printf("PROXY_TUNNEL_PORT=%s\n", c.InboundTunnelPort)
	fmt.Printf("PROXY_UID=%s\n", c.ProxyUID)
	fmt.Printf("PROXY_GID=%s\n", c.ProxyGID)
	fmt.Printf("INBOUND_INTERCEPTION_MODE=%s\n", c.InboundInterceptionMode)
	fmt.Printf("INBOUND_TPROXY_MARK=%s\n", c.InboundTProxyMark)
	fmt.Printf("INBOUND_TPROXY_ROUTE_TABLE=%s\n", c.InboundTProxyRouteTable)
	fmt.Printf("INBOUND_PORTS_INCLUDE=%s\n", c.InboundPortsInclude)
	fmt.Printf("INBOUND_PORTS_EXCLUDE=%s\n", c.InboundPortsExclude)
	fmt.Printf("OUTBOUND_IP_RANGES_INCLUDE=%s\n", c.OutboundIPRangesInclude)
	fmt.Printf("OUTBOUND_IP_RANGES_EXCLUDE=%s\n", c.OutboundIPRangesExclude)
	fmt.Printf("OUTBOUND_PORTS_INCLUDE=%s\n", c.OutboundPortsInclude)
	fmt.Printf("OUTBOUND_PORTS_EXCLUDE=%s\n", c.OutboundPortsExclude)
	fmt.Printf("KUBEVIRT_INTERFACES=%s\n", c.KubevirtInterfaces)
	fmt.Printf("ENABLE_INBOUND_IPV6=%t\n", c.EnableInboundIPv6)
	fmt.Println("")
}
