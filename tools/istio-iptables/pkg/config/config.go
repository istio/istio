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
	"strconv"
	"strings"
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
	OwnerGroupsInclude      string        `json:"OUTBOUND_OWNER_GROUPS_INCLUDE"`
	OwnerGroupsExclude      string        `json:"OUTBOUND_OWNER_GROUPS_EXCLUDE"`
	OutboundPortsInclude    string        `json:"OUTBOUND_PORTS_INCLUDE"`
	OutboundPortsExclude    string        `json:"OUTBOUND_PORTS_EXCLUDE"`
	OutboundIPRangesInclude string        `json:"OUTBOUND_IPRANGES_INCLUDE"`
	OutboundIPRangesExclude string        `json:"OUTBOUND_IPRANGES_EXCLUDE"`
	KubeVirtInterfaces      string        `json:"KUBE_VIRT_INTERFACES"`
	ExcludeInterfaces       string        `json:"EXCLUDE_INTERFACES"`
	IptablesProbePort       uint16        `json:"IPTABLES_PROBE_PORT"`
	ProbeTimeout            time.Duration `json:"PROBE_TIMEOUT"`
	DryRun                  bool          `json:"DRY_RUN"`
	RestoreFormat           bool          `json:"RESTORE_FORMAT"`
	SkipRuleApply           bool          `json:"SKIP_RULE_APPLY"`
	RunValidation           bool          `json:"RUN_VALIDATION"`
	RedirectDNS             bool          `json:"REDIRECT_DNS"`
	DropInvalid             bool          `json:"DROP_INVALID"`
	CaptureAllDNS           bool          `json:"CAPTURE_ALL_DNS"`
	EnableInboundIPv6       bool          `json:"ENABLE_INBOUND_IPV6"`
	DNSServersV4            []string      `json:"DNS_SERVERS_V4"`
	DNSServersV6            []string      `json:"DNS_SERVERS_V6"`
	OutputPath              string        `json:"OUTPUT_PATH"`
	NetworkNamespace        string        `json:"NETWORK_NAMESPACE"`
	CNIMode                 bool          `json:"CNI_MODE"`
	HostNSEnterExec         bool          `json:"HOST_NSENTER_EXEC"`
	TraceLogging            bool          `json:"IPTABLES_TRACE_LOGGING"`
}

func (c *Config) String() string {
	output, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		log.Fatalf("Unable to marshal config object: %v", err)
	}
	return string(output)
}

func (c *Config) Print() {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("PROXY_PORT=%s\n", c.ProxyPort))
	b.WriteString(fmt.Sprintf("PROXY_INBOUND_CAPTURE_PORT=%s\n", c.InboundCapturePort))
	b.WriteString(fmt.Sprintf("PROXY_TUNNEL_PORT=%s\n", c.InboundTunnelPort))
	b.WriteString(fmt.Sprintf("PROXY_UID=%s\n", c.ProxyUID))
	b.WriteString(fmt.Sprintf("PROXY_GID=%s\n", c.ProxyGID))
	b.WriteString(fmt.Sprintf("INBOUND_INTERCEPTION_MODE=%s\n", c.InboundInterceptionMode))
	b.WriteString(fmt.Sprintf("INBOUND_TPROXY_MARK=%s\n", c.InboundTProxyMark))
	b.WriteString(fmt.Sprintf("INBOUND_TPROXY_ROUTE_TABLE=%s\n", c.InboundTProxyRouteTable))
	b.WriteString(fmt.Sprintf("INBOUND_PORTS_INCLUDE=%s\n", c.InboundPortsInclude))
	b.WriteString(fmt.Sprintf("INBOUND_PORTS_EXCLUDE=%s\n", c.InboundPortsExclude))
	b.WriteString(fmt.Sprintf("OUTBOUND_OWNER_GROUPS_INCLUDE=%s\n", c.OwnerGroupsInclude))
	b.WriteString(fmt.Sprintf("OUTBOUND_OWNER_GROUPS_EXCLUDE=%s\n", c.OwnerGroupsExclude))
	b.WriteString(fmt.Sprintf("OUTBOUND_IP_RANGES_INCLUDE=%s\n", c.OutboundIPRangesInclude))
	b.WriteString(fmt.Sprintf("OUTBOUND_IP_RANGES_EXCLUDE=%s\n", c.OutboundIPRangesExclude))
	b.WriteString(fmt.Sprintf("OUTBOUND_PORTS_INCLUDE=%s\n", c.OutboundPortsInclude))
	b.WriteString(fmt.Sprintf("OUTBOUND_PORTS_EXCLUDE=%s\n", c.OutboundPortsExclude))
	b.WriteString(fmt.Sprintf("KUBE_VIRT_INTERFACES=%s\n", c.KubeVirtInterfaces))
	b.WriteString(fmt.Sprintf("ENABLE_INBOUND_IPV6=%t\n", c.EnableInboundIPv6))
	b.WriteString(fmt.Sprintf("DNS_CAPTURE=%t\n", c.RedirectDNS))
	b.WriteString(fmt.Sprintf("DROP_INVALID=%t\n", c.DropInvalid))
	b.WriteString(fmt.Sprintf("CAPTURE_ALL_DNS=%t\n", c.CaptureAllDNS))
	b.WriteString(fmt.Sprintf("DNS_SERVERS=%s,%s\n", c.DNSServersV4, c.DNSServersV6))
	b.WriteString(fmt.Sprintf("OUTPUT_PATH=%s\n", c.OutputPath))
	b.WriteString(fmt.Sprintf("NETWORK_NAMESPACE=%s\n", c.NetworkNamespace))
	b.WriteString(fmt.Sprintf("CNI_MODE=%s\n", strconv.FormatBool(c.CNIMode)))
	b.WriteString(fmt.Sprintf("HOST_NSENTER_EXEC=%s\n", strconv.FormatBool(c.HostNSEnterExec)))
	b.WriteString(fmt.Sprintf("EXCLUDE_INTERFACES=%s\n", c.ExcludeInterfaces))
	log.Infof("Istio iptables variables:\n%s", b.String())
}

func (c *Config) Validate() error {
	return ValidateOwnerGroups(c.OwnerGroupsInclude, c.OwnerGroupsExclude)
}
