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
	ProxyUID                string        `json:"PROXY_UID"`
	ProxyGID                string        `json:"PROXY_GID"`
	InboundInterceptionMode string        `json:"INBOUND_INTERCEPTION_MODE"`
	InboundTProxyMark       string        `json:"INBOUND_TPROXY_MARK"`
	InboundTProxyRouteTable string        `json:"INBOUND_TPROXY_ROUTE_TABLE"`
	InboundPortsInclude     string        `json:"INBOUND_PORTS_INCLUDE"`
	InboundPortsExclude     string        `json:"INBOUND_PORTS_EXCLUDE"`
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
	fmt.Println(fmt.Sprintf("PROXY_PORT=%s", c.ProxyPort))
	fmt.Println(fmt.Sprintf("PROXY_INBOUND_CAPTURE_PORT=%s", c.InboundCapturePort))
	fmt.Println(fmt.Sprintf("PROXY_UID=%s", c.ProxyUID))
	fmt.Println(fmt.Sprintf("PROXY_GID=%s", c.ProxyGID))
	fmt.Println(fmt.Sprintf("INBOUND_INTERCEPTION_MODE=%s", c.InboundInterceptionMode))
	fmt.Println(fmt.Sprintf("INBOUND_TPROXY_MARK=%s", c.InboundTProxyMark))
	fmt.Println(fmt.Sprintf("INBOUND_TPROXY_ROUTE_TABLE=%s", c.InboundTProxyRouteTable))
	fmt.Println(fmt.Sprintf("INBOUND_PORTS_INCLUDE=%s", c.InboundPortsInclude))
	fmt.Println(fmt.Sprintf("INBOUND_PORTS_EXCLUDE=%s", c.InboundPortsExclude))
	fmt.Println(fmt.Sprintf("OUTBOUND_IP_RANGES_INCLUDE=%s", c.OutboundIPRangesInclude))
	fmt.Println(fmt.Sprintf("OUTBOUND_IP_RANGES_EXCLUDE=%s", c.OutboundIPRangesExclude))
	fmt.Println(fmt.Sprintf("OUTBOUND_PORTS_EXCLUDE=%s", c.OutboundPortsExclude))
	fmt.Println(fmt.Sprintf("KUBEVIRT_INTERFACES=%s", c.KubevirtInterfaces))
	fmt.Println(fmt.Sprintf("ENABLE_INBOUND_IPV6=%t", c.EnableInboundIPv6))
	fmt.Println("")
}
