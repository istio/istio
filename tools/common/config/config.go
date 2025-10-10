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
	"net"
	"net/netip"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

func DefaultConfig() *Config {
	return &Config{
		ProxyPort:               "15001",
		InboundCapturePort:      "15006",
		InboundTunnelPort:       "15008",
		InboundTProxyMark:       "1337",
		InboundTProxyRouteTable: "133",
		IptablesProbePort:       constants.DefaultIptablesProbePortUint,
		ProbeTimeout:            constants.DefaultProbeTimeout,
		OwnerGroupsInclude:      constants.OwnerGroupsInclude.DefaultValue,
		OwnerGroupsExclude:      constants.OwnerGroupsExclude.DefaultValue,
		HostIPv4LoopbackCidr:    constants.HostIPv4LoopbackCidr.DefaultValue,
	}
}

// Command line options
// nolint: maligned
type Config struct {
	ProxyPort                string        `json:"PROXY_PORT"`
	InboundCapturePort       string        `json:"INBOUND_CAPTURE_PORT"`
	InboundTunnelPort        string        `json:"INBOUND_TUNNEL_PORT"`
	ProxyUID                 string        `json:"PROXY_UID"`
	ProxyGID                 string        `json:"PROXY_GID"`
	InboundInterceptionMode  string        `json:"INBOUND_INTERCEPTION_MODE"`
	InboundTProxyMark        string        `json:"INBOUND_TPROXY_MARK"`
	InboundTProxyRouteTable  string        `json:"INBOUND_TPROXY_ROUTE_TABLE"`
	InboundPortsInclude      string        `json:"INBOUND_PORTS_INCLUDE"`
	InboundPortsExclude      string        `json:"INBOUND_PORTS_EXCLUDE"`
	OwnerGroupsInclude       string        `json:"OUTBOUND_OWNER_GROUPS_INCLUDE"`
	OwnerGroupsExclude       string        `json:"OUTBOUND_OWNER_GROUPS_EXCLUDE"`
	OutboundPortsInclude     string        `json:"OUTBOUND_PORTS_INCLUDE"`
	OutboundPortsExclude     string        `json:"OUTBOUND_PORTS_EXCLUDE"`
	OutboundIPRangesInclude  string        `json:"OUTBOUND_IPRANGES_INCLUDE"`
	OutboundIPRangesExclude  string        `json:"OUTBOUND_IPRANGES_EXCLUDE"`
	RerouteVirtualInterfaces string        `json:"KUBE_VIRT_INTERFACES"`
	ExcludeInterfaces        string        `json:"EXCLUDE_INTERFACES"`
	IptablesProbePort        uint16        `json:"IPTABLES_PROBE_PORT"`
	ProbeTimeout             time.Duration `json:"PROBE_TIMEOUT"`
	DryRun                   bool          `json:"DRY_RUN"`
	SkipRuleApply            bool          `json:"SKIP_RULE_APPLY"`
	RunValidation            bool          `json:"RUN_VALIDATION"`
	RedirectDNS              bool          `json:"REDIRECT_DNS"`
	DropInvalid              bool          `json:"DROP_INVALID"`
	CaptureAllDNS            bool          `json:"CAPTURE_ALL_DNS"`
	EnableIPv6               bool          `json:"ENABLE_INBOUND_IPV6"`
	DNSServersV4             []string      `json:"DNS_SERVERS_V4"`
	DNSServersV6             []string      `json:"DNS_SERVERS_V6"`
	NetworkNamespace         string        `json:"NETWORK_NAMESPACE"`
	// When running in host filesystem, we have different semantics around the environment.
	// For instance, we would have a node-shared IPTables lock, despite not needing it.
	// HostFilesystemPodNetwork indicates we are in this mode, typically from the CNI.
	HostFilesystemPodNetwork bool       `json:"CNI_MODE"`
	DualStack                bool       `json:"DUAL_STACK"`
	HostIP                   netip.Addr `json:"HOST_IP"`
	HostIPv4LoopbackCidr     string     `json:"HOST_IPV4_LOOPBACK_CIDR"`
	Reconcile                bool       `json:"RECONCILE"`
	CleanupOnly              bool       `json:"CLEANUP_ONLY"`
	ForceApply               bool       `json:"FORCE_APPLY"`
	NativeNftables           bool       `json:"NATIVE_NFTABLES"`
	ForceLegacyIPTables      bool       `json:"FORCE_LEGACY_IPTABLES"`
}

type NetworkRange struct {
	IsWildcard    bool
	CIDRs         []netip.Prefix
	HasLoopBackIP bool
}

func (c *Config) String() string {
	output, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		log.Errorf("Unable to marshal config object: %v", err)
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
	b.WriteString(fmt.Sprintf("KUBE_VIRT_INTERFACES=%s\n", c.RerouteVirtualInterfaces))
	// TODO consider renaming this env var to ENABLE_IPV6 - nothing about it is specific to "INBOUND"
	b.WriteString(fmt.Sprintf("ENABLE_INBOUND_IPV6=%t\n", c.EnableIPv6))
	// TODO remove this flag - "dual stack" should just mean
	// - supports IPv6
	// - supports pods with more than one podIP
	// The former already has a flag, the latter is something we should do by default and is a bug where we do not
	b.WriteString(fmt.Sprintf("DUAL_STACK=%t\n", c.DualStack))
	b.WriteString(fmt.Sprintf("DNS_CAPTURE=%t\n", c.RedirectDNS))
	b.WriteString(fmt.Sprintf("DROP_INVALID=%t\n", c.DropInvalid))
	b.WriteString(fmt.Sprintf("CAPTURE_ALL_DNS=%t\n", c.CaptureAllDNS))
	b.WriteString(fmt.Sprintf("DNS_SERVERS=%s,%s\n", c.DNSServersV4, c.DNSServersV6))
	b.WriteString(fmt.Sprintf("NETWORK_NAMESPACE=%s\n", c.NetworkNamespace))
	b.WriteString(fmt.Sprintf("CNI_MODE=%s\n", strconv.FormatBool(c.HostFilesystemPodNetwork)))
	b.WriteString(fmt.Sprintf("EXCLUDE_INTERFACES=%s\n", c.ExcludeInterfaces))
	b.WriteString(fmt.Sprintf("RECONCILE=%t\n", c.Reconcile))
	b.WriteString(fmt.Sprintf("CLEANUP_ONLY=%t\n", c.CleanupOnly))
	b.WriteString(fmt.Sprintf("FORCE_APPLY=%t\n", c.ForceApply))
	b.WriteString(fmt.Sprintf("NATIVE_NFTABLES=%t\n", c.NativeNftables))
	b.WriteString(fmt.Sprintf("FORCE_LEGACY_IPTABLES=%t\n", c.ForceLegacyIPTables))
	log.Infof("Istio config:\n%s", b.String())
}

func (c *Config) Validate() error {
	if err := ValidateOwnerGroups(c.OwnerGroupsInclude, c.OwnerGroupsExclude); err != nil {
		return err
	}

	if err := ValidateNftLegacyFlags(c.NativeNftables, c.ForceLegacyIPTables); err != nil {
		return err
	}

	return ValidateIPv4LoopbackCidr(c.HostIPv4LoopbackCidr)
}

var envoyUserVar = env.Register(constants.EnvoyUser, "istio-proxy", "Envoy proxy username")

func (c *Config) FillConfigFromEnvironment() error {
	// Fill in env-var only options
	c.OwnerGroupsInclude = constants.OwnerGroupsInclude.Get()
	c.OwnerGroupsExclude = constants.OwnerGroupsExclude.Get()

	c.HostIPv4LoopbackCidr = constants.HostIPv4LoopbackCidr.Get()

	// TODO: Make this more configurable, maybe with an allowlist of users to be captured for output instead of a denylist.
	if c.ProxyUID == "" {
		usr, err := user.Lookup(envoyUserVar.Get())
		var userID string
		// Default to the UID of ENVOY_USER
		if err != nil {
			userID = constants.DefaultProxyUID
		} else {
			userID = usr.Uid
		}
		c.ProxyUID = userID
	}

	// For TPROXY as its uid and gid are same.
	if c.ProxyGID == "" {
		c.ProxyGID = c.ProxyUID
	}
	// Detect whether IPv6 is enabled by checking if the pod's IP address is IPv4 or IPv6.
	// TODO remove this check, it will break with more than one pod IP
	hostIP, isIPv6, err := getLocalIP(c.DualStack)
	if err != nil {
		return err
	}

	c.HostIP = hostIP
	c.EnableIPv6 = isIPv6

	// Lookup DNS nameservers. We only do this if DNS is enabled in case of some obscure theoretical
	// case where reading /etc/resolv.conf could fail.
	// If capture all DNS option is enabled, we don't need to read from the dns resolve conf. All
	// traffic to port 53 will be captured.
	if c.RedirectDNS && !c.CaptureAllDNS {
		dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			return fmt.Errorf("failed to load /etc/resolv.conf: %v", err)
		}
		c.DNSServersV4, c.DNSServersV6 = netutil.IPsSplitV4V6(dnsConfig.Servers)
	}
	return nil
}

// mock net.InterfaceAddrs to make its unit test become available
var (
	LocalIPAddrs = net.InterfaceAddrs
)

// getLocalIP returns one of the local IP address and it should support IPv6 or not
func getLocalIP(dualStack bool) (netip.Addr, bool, error) {
	var isIPv6 bool
	var ipAddrs []netip.Addr
	addrs, err := LocalIPAddrs()
	if err != nil {
		return netip.Addr{}, isIPv6, err
	}

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ip := ipnet.IP
			ipAddr, ok := netip.AddrFromSlice(ip)
			if !ok {
				continue
			}
			// unwrap the IPv4-mapped IPv6 address
			unwrapAddr := ipAddr.Unmap()
			if !unwrapAddr.IsLoopback() && !unwrapAddr.IsLinkLocalUnicast() && !unwrapAddr.IsLinkLocalMulticast() {
				isIPv6 = unwrapAddr.Is6()
				ipAddrs = append(ipAddrs, unwrapAddr)
				if !dualStack {
					return unwrapAddr, isIPv6, nil
				}
				if isIPv6 {
					break
				}
			}
		}
	}

	if len(ipAddrs) > 0 {
		return ipAddrs[0], isIPv6, nil
	}

	return netip.Addr{}, isIPv6, fmt.Errorf("no valid local IP address found")
}

func SeparateV4V6(cidrList string) (NetworkRange, NetworkRange, error) {
	if cidrList == "*" {
		return NetworkRange{IsWildcard: true}, NetworkRange{IsWildcard: true}, nil
	}
	ipv6Ranges := NetworkRange{}
	ipv4Ranges := NetworkRange{}
	for _, ipRange := range Split(cidrList) {
		ipp, err := netip.ParsePrefix(ipRange)
		if err != nil {
			return ipv4Ranges, ipv6Ranges, err
		}
		if ipp.Addr().Is4() {
			ipv4Ranges.CIDRs = append(ipv4Ranges.CIDRs, ipp)
			if ipp.Addr().IsLoopback() {
				ipv4Ranges.HasLoopBackIP = true
			}
		} else {
			ipv6Ranges.CIDRs = append(ipv6Ranges.CIDRs, ipp)
			if ipp.Addr().IsLoopback() {
				ipv6Ranges.HasLoopBackIP = true
			}
		}
	}
	return ipv4Ranges, ipv6Ranges, nil
}
