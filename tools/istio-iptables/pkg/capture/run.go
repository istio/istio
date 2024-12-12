// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package capture

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/vishvananda/netlink"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	iptableslog "istio.io/istio/tools/istio-iptables/pkg/log"
)

type Ops int

const (
	// AppendOps performs append operations of rules
	AppendOps Ops = iota
	// DeleteOps performs delete operations of rules
	DeleteOps

	// In TPROXY mode, mark the packet from envoy outbound to app by podIP,
	// this is to prevent it being intercepted to envoy inbound listener.
	outboundMark = "1338"
)

var opsToString = map[Ops]string{
	AppendOps: "-A",
	DeleteOps: "-D",
}

type IptablesConfigurator struct {
	ruleBuilder *builder.IptablesRuleBuilder
	// TODO(abhide): Fix dep.Dependencies with better interface
	ext dep.Dependencies
	cfg *config.Config
}

func NewIptablesConfigurator(cfg *config.Config, ext dep.Dependencies) *IptablesConfigurator {
	return &IptablesConfigurator{
		ruleBuilder: builder.NewIptablesRuleBuilder(cfg),
		ext:         ext,
		cfg:         cfg,
	}
}

type NetworkRange struct {
	IsWildcard    bool
	CIDRs         []netip.Prefix
	HasLoopBackIP bool
}

func split(s string) []string {
	return config.Split(s)
}

func (cfg *IptablesConfigurator) separateV4V6(cidrList string) (NetworkRange, NetworkRange, error) {
	if cidrList == "*" {
		return NetworkRange{IsWildcard: true}, NetworkRange{IsWildcard: true}, nil
	}
	ipv6Ranges := NetworkRange{}
	ipv4Ranges := NetworkRange{}
	for _, ipRange := range split(cidrList) {
		ipp, err := netip.ParsePrefix(ipRange)
		if err != nil {
			_, err = fmt.Fprintf(os.Stderr, "Ignoring error for bug compatibility with istio-iptables: %s\n", err.Error())
			if err != nil {
				return ipv4Ranges, ipv6Ranges, err
			}
			continue
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

func (cfg *IptablesConfigurator) logConfig() {
	// Dump out our environment for debugging purposes.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("ENVOY_PORT=%s\n", os.Getenv("ENVOY_PORT")))
	b.WriteString(fmt.Sprintf("INBOUND_CAPTURE_PORT=%s\n", os.Getenv("INBOUND_CAPTURE_PORT")))
	b.WriteString(fmt.Sprintf("ISTIO_INBOUND_INTERCEPTION_MODE=%s\n", os.Getenv("ISTIO_INBOUND_INTERCEPTION_MODE")))
	b.WriteString(fmt.Sprintf("ISTIO_INBOUND_TPROXY_ROUTE_TABLE=%s\n", os.Getenv("ISTIO_INBOUND_TPROXY_ROUTE_TABLE")))
	b.WriteString(fmt.Sprintf("ISTIO_INBOUND_PORTS=%s\n", os.Getenv("ISTIO_INBOUND_PORTS")))
	b.WriteString(fmt.Sprintf("ISTIO_OUTBOUND_PORTS=%s\n", os.Getenv("ISTIO_OUTBOUND_PORTS")))
	b.WriteString(fmt.Sprintf("ISTIO_LOCAL_EXCLUDE_PORTS=%s\n", os.Getenv("ISTIO_LOCAL_EXCLUDE_PORTS")))
	b.WriteString(fmt.Sprintf("ISTIO_EXCLUDE_INTERFACES=%s\n", os.Getenv("ISTIO_EXCLUDE_INTERFACES")))
	b.WriteString(fmt.Sprintf("ISTIO_SERVICE_CIDR=%s\n", os.Getenv("ISTIO_SERVICE_CIDR")))
	b.WriteString(fmt.Sprintf("ISTIO_SERVICE_EXCLUDE_CIDR=%s\n", os.Getenv("ISTIO_SERVICE_EXCLUDE_CIDR")))
	b.WriteString(fmt.Sprintf("ISTIO_META_DNS_CAPTURE=%s\n", os.Getenv("ISTIO_META_DNS_CAPTURE")))
	b.WriteString(fmt.Sprintf("INVALID_DROP=%s\n", os.Getenv("INVALID_DROP")))
	log.Infof("Istio iptables environment:\n%s", b.String())
	cfg.cfg.Print()
}

func (cfg *IptablesConfigurator) handleInboundPortsInclude() {
	// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
	// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
	var table string
	if cfg.cfg.InboundPortsInclude != "" {
		if cfg.cfg.InboundInterceptionMode == constants.TPROXY {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the ISTIOINBOUND chain, '-j ISTIODIVERT' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIODIVERT, constants.MANGLE, "-j", constants.MARK, "--set-mark",
				cfg.cfg.InboundTProxyMark)
			cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIODIVERT, constants.MANGLE, "-j", constants.ACCEPT)

			// Create a new chain for redirecting inbound traffic to the common Envoy
			// port.
			// In the ISTIOINBOUND chain, '-j RETURN' bypasses Envoy and
			// '-j ISTIOTPROXY' redirects to Envoy.
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", iptableslog.UndefinedCommand,
				constants.ISTIOTPROXY, constants.MANGLE, "!", "-d", constants.IPVersionSpecific,
				"-p", constants.TCP, "-j", constants.TPROXY,
				"--tproxy-mark", cfg.cfg.InboundTProxyMark+"/0xffffffff", "--on-port", cfg.cfg.InboundCapturePort)
			table = constants.MANGLE
		} else {
			table = constants.NAT
		}
		cfg.ruleBuilder.AppendRule(iptableslog.JumpInbound, constants.PREROUTING, table, "-p", constants.TCP,
			"-j", constants.ISTIOINBOUND)

		if cfg.cfg.InboundPortsInclude == "*" {
			// Apply any user-specified port exclusions.
			if cfg.cfg.InboundPortsExclude != "" {
				for _, port := range split(cfg.cfg.InboundPortsExclude) {
					cfg.ruleBuilder.AppendRule(iptableslog.ExcludeInboundPort, constants.ISTIOINBOUND, table, "-p", constants.TCP,
						"--dport", port, "-j", constants.RETURN)
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if cfg.cfg.InboundInterceptionMode == constants.TPROXY {
				// If an inbound packet belongs to an established socket, route it to the
				// loopback interface.
				cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP,
					"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", constants.ISTIODIVERT)
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP,
					"-j", constants.ISTIOTPROXY)
			} else {
				cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.NAT, "-p", constants.TCP,
					"-j", constants.ISTIOINREDIRECT)
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(cfg.cfg.InboundPortsInclude) {
				if cfg.cfg.InboundInterceptionMode == constants.TPROXY {
					cfg.ruleBuilder.AppendRule(iptableslog.IncludeInboundPort, constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP,
						"--dport", port, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", constants.ISTIODIVERT)
					cfg.ruleBuilder.AppendRule(iptableslog.IncludeInboundPort,
						constants.ISTIOINBOUND, constants.MANGLE, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOTPROXY)
				} else {
					cfg.ruleBuilder.AppendRule(iptableslog.IncludeInboundPort,
						constants.ISTIOINBOUND, constants.NAT, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOINREDIRECT)
				}
			}
		}
	}
}

func (cfg *IptablesConfigurator) handleOutboundIncludeRules(
	rangeInclude NetworkRange,
	appendRule func(command iptableslog.Command, chain string, table string, params ...string) *builder.IptablesRuleBuilder,
	insert func(command iptableslog.Command, chain string, table string, position int, params ...string) *builder.IptablesRuleBuilder,
) {
	// Apply outbound IP inclusions.
	if rangeInclude.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		appendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT, "-j", constants.ISTIOREDIRECT)
		for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
			insert(iptableslog.KubevirtCommand,
				constants.PREROUTING, constants.NAT, 1, "-i", internalInterface, "-j", constants.ISTIOREDIRECT)
		}
	} else if len(rangeInclude.CIDRs) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range rangeInclude.CIDRs {
			for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
				insert(iptableslog.KubevirtCommand, constants.PREROUTING, constants.NAT, 1, "-i", internalInterface,
					"-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
			}
			appendRule(iptableslog.UndefinedCommand,
				constants.ISTIOOUTPUT, constants.NAT, "-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
		}
	}
}

func (cfg *IptablesConfigurator) shortCircuitKubeInternalInterface() {
	for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
		cfg.ruleBuilder.InsertRule(iptableslog.KubevirtCommand, constants.PREROUTING, constants.NAT, 1, "-i", internalInterface, "-j", constants.RETURN)
	}
}

func (cfg *IptablesConfigurator) shortCircuitExcludeInterfaces() {
	for _, excludeInterface := range split(cfg.cfg.ExcludeInterfaces) {
		cfg.ruleBuilder.AppendRule(
			iptableslog.ExcludeInterfaceCommand, constants.PREROUTING, constants.NAT, "-i", excludeInterface, "-j", constants.RETURN)
		cfg.ruleBuilder.AppendRule(iptableslog.ExcludeInterfaceCommand, constants.OUTPUT, constants.NAT, "-o", excludeInterface, "-j", constants.RETURN)
	}
	if cfg.cfg.InboundInterceptionMode == constants.TPROXY {
		for _, excludeInterface := range split(cfg.cfg.ExcludeInterfaces) {

			cfg.ruleBuilder.AppendRule(
				iptableslog.ExcludeInterfaceCommand, constants.PREROUTING, constants.MANGLE, "-i", excludeInterface, "-j", constants.RETURN)
			cfg.ruleBuilder.AppendRule(iptableslog.ExcludeInterfaceCommand, constants.OUTPUT, constants.MANGLE, "-o", excludeInterface, "-j", constants.RETURN)
		}
	}
}

func ignoreExists(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "file exists") {
		return nil
	}
	return err
}

// configureIPv6Addresses sets up a new IP address on local interface. This is used as the source IP
// for inbound traffic to distinguish traffic we want to capture vs traffic we do not. This is needed
// for IPv6 but not IPv4, as IPv4 defaults to `netmask 255.0.0.0`, which allows binding to addresses
// in the 127.x.y.z range, while IPv6 defaults to `prefixlen 128` which allows binding only to ::1.
// Equivalent to `ip -6 addr add "::6/128" dev lo`
func configureIPv6Addresses(cfg *config.Config) error {
	if !cfg.EnableIPv6 {
		return nil
	}
	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("failed to find 'lo' link: %v", err)
	}
	// Setup a new IP address on local interface. This is used as the source IP for inbound traffic
	// to distinguish traffic we want to capture vs traffic we do not.
	// Equivalent to `ip -6 addr add "::6/128" dev lo`
	address := &net.IPNet{IP: net.ParseIP("::6"), Mask: net.CIDRMask(128, 128)}
	addr := &netlink.Addr{IPNet: address}

	err = netlink.AddrAdd(link, addr)
	if ignoreExists(err) != nil {
		return fmt.Errorf("failed to add IPv6 inbound address: %v", err)
	}
	log.Infof("Added ::6 address")
	return nil
}

func (cfg *IptablesConfigurator) Run() error {
	iptVer, err := cfg.ext.DetectIptablesVersion(false)
	if err != nil {
		return err
	}

	ipt6Ver, err := cfg.ext.DetectIptablesVersion(true)
	if err != nil {
		return err
	}

	defer func() {
		// Best effort since we don't know if the commands exist
		_ = cfg.ext.Run(log.WithLabels(), constants.IPTablesSave, &iptVer, nil)
		if cfg.cfg.EnableIPv6 {
			_ = cfg.ext.Run(log.WithLabels(), constants.IPTablesSave, &ipt6Ver, nil)
		}
	}()

	// Since OUTBOUND_IP_RANGES_EXCLUDE could carry ipv4 and ipv6 ranges
	// need to split them in different arrays one for ipv4 and one for ipv6
	// in order to not to fail
	ipv4RangesExclude, ipv6RangesExclude, err := cfg.separateV4V6(cfg.cfg.OutboundIPRangesExclude)
	if err != nil {
		return err
	}
	if ipv4RangesExclude.IsWildcard {
		return fmt.Errorf("invalid value for OUTBOUND_IP_RANGES_EXCLUDE")
	}
	// FixMe: Do we need similar check for ipv6RangesExclude as well ??

	ipv4RangesInclude, ipv6RangesInclude, err := cfg.separateV4V6(cfg.cfg.OutboundIPRangesInclude)
	if err != nil {
		return err
	}

	redirectDNS := cfg.cfg.RedirectDNS
	cfg.logConfig()

	cfg.shortCircuitExcludeInterfaces()

	// Do not capture internal interface.
	cfg.shortCircuitKubeInternalInterface()

	// Create a rule for invalid drop in PREROUTING chain in mangle table, so the iptables will drop the out of window packets instead of reset connection .
	dropInvalid := cfg.cfg.DropInvalid
	if dropInvalid {
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.PREROUTING, constants.MANGLE, "-m", "conntrack", "--ctstate",
			"INVALID", "-j", constants.ISTIODROP)
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIODROP, constants.MANGLE, "-j", constants.DROP)
	}

	// Create a new chain for to hit tunnel port directly. Envoy will be listening on port acting as VPN tunnel.
	cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.NAT, "-p", constants.TCP, "--dport",
		cfg.cfg.InboundTunnelPort, "-j", constants.RETURN)

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIOREDIRECT'
	// redirects to Envoy.
	cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand,
		constants.ISTIOREDIRECT, constants.NAT, "-p", constants.TCP, "-j", constants.REDIRECT, "--to-ports", cfg.cfg.ProxyPort)

	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.

	cfg.ruleBuilder.AppendRule(iptableslog.InboundCapture, constants.ISTIOINREDIRECT, constants.NAT, "-p", constants.TCP, "-j", constants.REDIRECT,
		"--to-ports", cfg.cfg.InboundCapturePort)

	cfg.handleInboundPortsInclude()

	// TODO: change the default behavior to not intercept any output - user may use http_proxy or another
	// iptablesOrFail wrapper (like ufw). Current default is similar with 0.1
	// Jump to the ISTIOOUTPUT chain from OUTPUT chain for all tcp traffic
	cfg.ruleBuilder.AppendRule(iptableslog.JumpOutbound, constants.OUTPUT, constants.NAT, "-p", constants.TCP, "-j", constants.ISTIOOUTPUT)
	// Apply port based exclusions. Must be applied before connections back to self are redirected.
	if cfg.cfg.OutboundPortsExclude != "" {
		for _, port := range split(cfg.cfg.OutboundPortsExclude) {
			cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT, "-p", constants.TCP,
				"--dport", port, "-j", constants.RETURN)
		}
	}

	// 127.0.0.6/::7 is bind connect from inbound passthrough cluster
	cfg.ruleBuilder.AppendVersionedRule("127.0.0.6/32", "::6/128", iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
		"-o", "lo", "-s", constants.IPVersionSpecific, "-j", constants.RETURN)

	for _, uid := range split(cfg.cfg.ProxyUID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		// nolint: lll
		if redirectDNS {
			// When DNS is enabled, we skip this for port 53. This ensures we do not have:
			// app => istio-agent => Envoy inbound => dns server
			// Instead, we just have:
			// app => istio-agent => dns server
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
				"-o", "lo",
				"!", "-d", constants.IPVersionSpecific,
				"-p", "tcp",
				"-m", "multiport",
				"!", "--dports", "53,"+cfg.cfg.InboundTunnelPort,
				"-m", "owner", "--uid-owner", uid, "-j", constants.ISTIOINREDIRECT)
		} else {
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
				"-o", "lo",
				"!", "-d", constants.IPVersionSpecific,
				"-p", "tcp",
				"!", "--dport", cfg.cfg.InboundTunnelPort,
				"-m", "owner", "--uid-owner", uid, "-j", constants.ISTIOINREDIRECT)
		}
		// Do not redirect app calls to back itself via Envoy when using the endpoint address
		// e.g. appN => appN by lo
		// If loopback explicitly set via OutboundIPRangesInclude, then don't return.
		if !ipv4RangesInclude.HasLoopBackIP && !ipv6RangesInclude.HasLoopBackIP {
			if redirectDNS {
				// Users may have a DNS server that is on localhost. In these cases, applications may
				// send TCP traffic to the DNS server that we actually *do* want to intercept. To
				// handle this case, we exclude port 53 from this rule. Note: We cannot just move the
				// port 53 redirection rule further up the list, as we will want to avoid capturing
				// DNS requests from the proxy UID/GID
				cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT, "-o", "lo", "-p", "tcp",
					"!", "--dport", "53",
					"-m", "owner", "!", "--uid-owner", uid, "-j", constants.RETURN)
			} else {
				cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
					"-o", "lo", "-m", "owner", "!", "--uid-owner", uid, "-j", constants.RETURN)
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
			"-m", "owner", "--uid-owner", uid, "-j", constants.RETURN)
	}

	for _, gid := range split(cfg.cfg.ProxyGID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
			"-o", "lo",
			"!", "-d", constants.IPVersionSpecific,
			"-p", "tcp",
			"!", "--dport", cfg.cfg.InboundTunnelPort,
			"-m", "owner", "--gid-owner", gid, "-j", constants.ISTIOINREDIRECT)

		// Do not redirect app calls to back itself via Envoy when using the endpoint address
		// e.g. appN => appN by lo
		// If loopback explicitly set via OutboundIPRangesInclude, then don't return.
		if !ipv4RangesInclude.HasLoopBackIP && !ipv6RangesInclude.HasLoopBackIP {
			if redirectDNS {
				// Users may have a DNS server that is on localhost. In these cases, applications may
				// send TCP traffic to the DNS server that we actually *do* want to intercept. To
				// handle this case, we exclude port 53 from this rule. Note: We cannot just move the
				// port 53 redirection rule further up the list, as we will want to avoid capturing
				// DNS requests from the proxy UID/GID
				cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
					"-o", "lo", "-p", "tcp",
					"!", "--dport", "53",
					"-m", "owner", "!", "--gid-owner", gid, "-j", constants.RETURN)
			} else {
				cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
					"-o", "lo", "-m", "owner", "!", "--gid-owner", gid, "-j", constants.RETURN)
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT, "-m", "owner", "--gid-owner", gid, "-j", constants.RETURN)
	}

	ownerGroupsFilter := config.ParseInterceptFilter(cfg.cfg.OwnerGroupsInclude, cfg.cfg.OwnerGroupsExclude)

	cfg.handleCaptureByOwnerGroup(ownerGroupsFilter)

	if redirectDNS {
		if cfg.cfg.CaptureAllDNS {
			// Redirect all TCP dns traffic on port 53 to the agent on port 15053
			// This will be useful for the CNI case where pod DNS server address cannot be decided.
			cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand,
				constants.ISTIOOUTPUT, constants.NAT,
				"-p", constants.TCP,
				"--dport", "53",
				"-j", constants.REDIRECT,
				"--to-ports", constants.IstioAgentDNSListenerPort)
		} else {
			for _, s := range cfg.cfg.DNSServersV4 {
				// redirect all TCP dns traffic on port 53 to the agent on port 15053 for all servers
				// in etc/resolv.conf
				// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
				// such as: app -> istio dns server -> dnsmasq -> upstream
				// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
				// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
				// pointed to server X, this would not work. However, the assumption is that is not a common case.
				cfg.ruleBuilder.AppendRuleV4(iptableslog.UndefinedCommand,
					constants.ISTIOOUTPUT, constants.NAT,
					"-p", constants.TCP,
					"--dport", "53",
					"-d", s+"/32",
					"-j", constants.REDIRECT,
					"--to-ports", constants.IstioAgentDNSListenerPort)
			}
			for _, s := range cfg.cfg.DNSServersV6 {
				cfg.ruleBuilder.AppendRuleV6(iptableslog.UndefinedCommand,
					constants.ISTIOOUTPUT, constants.NAT,
					"-p", constants.TCP,
					"--dport", "53",
					"-d", s+"/128",
					"-j", constants.REDIRECT,
					"--to-ports", constants.IstioAgentDNSListenerPort)
			}
		}
	}

	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
		"-d", constants.IPVersionSpecific, "-j", constants.RETURN)
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.CIDRs {
		cfg.ruleBuilder.AppendRuleV4(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT, "-d", cidr.String(), "-j", constants.RETURN)
	}
	for _, cidr := range ipv6RangesExclude.CIDRs {
		cfg.ruleBuilder.AppendRuleV6(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT, "-d", cidr.String(), "-j", constants.RETURN)
	}

	cfg.handleOutboundPortsInclude()

	cfg.handleOutboundIncludeRules(ipv4RangesInclude, cfg.ruleBuilder.AppendRuleV4, cfg.ruleBuilder.InsertRuleV4)
	cfg.handleOutboundIncludeRules(ipv6RangesInclude, cfg.ruleBuilder.AppendRuleV6, cfg.ruleBuilder.InsertRuleV6)

	if redirectDNS {
		// Jump from OUTPUT chain to ISTIOOUTPUT chain for all UDP traffic
		cfg.ruleBuilder.AppendRule(iptableslog.JumpOutbound, constants.OUTPUT, constants.NAT, "-p", constants.UDP, "-j", constants.ISTIOOUTPUT)
		cfg.ruleBuilder.AppendRule(iptableslog.JumpOutbound, constants.OUTPUT, constants.RAW, "-p", constants.UDP, "-j", constants.ISTIOOUTPUT)

		HandleDNSUDP(
			AppendOps, cfg.ruleBuilder, cfg.ext, &iptVer, &ipt6Ver,
			cfg.cfg.ProxyUID, cfg.cfg.ProxyGID,
			cfg.cfg.DNSServersV4, cfg.cfg.DNSServersV6, cfg.cfg.CaptureAllDNS,
			ownerGroupsFilter)
	}

	if cfg.cfg.InboundInterceptionMode == constants.TPROXY {
		// save packet mark set by envoy.filters.listener.original_src as connection mark
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.PREROUTING, constants.MANGLE,
			"-p", constants.TCP, "-m", "mark", "--mark", cfg.cfg.InboundTProxyMark, "-j", "CONNMARK", "--save-mark")
		// If the packet is already marked with 1337, then return. This is to prevent mark envoy --> app traffic again.
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.OUTPUT, constants.MANGLE,
			"-p", constants.TCP, "-o", "lo", "-m", "mark", "--mark", cfg.cfg.InboundTProxyMark, "-j", constants.RETURN)
		for _, uid := range split(cfg.cfg.ProxyUID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", iptableslog.UndefinedCommand, constants.OUTPUT, constants.MANGLE,
				"!", "-d", constants.IPVersionSpecific, "-p", constants.TCP, "-o", "lo",
				"-m", "owner", "--uid-owner", uid, "-j", constants.MARK, "--set-mark", outboundMark)
		}
		for _, gid := range split(cfg.cfg.ProxyGID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", iptableslog.UndefinedCommand, constants.OUTPUT, constants.MANGLE,
				"!", "-d", constants.IPVersionSpecific, "-p", constants.TCP, "-o", "lo",
				"-m", "owner", "--gid-owner", gid, "-j", constants.MARK, "--set-mark", outboundMark)
		}
		// mark outgoing packets from workload, match it to policy routing entry setup for TPROXY mode
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.OUTPUT, constants.MANGLE,
			"-p", constants.TCP, "-m", "connmark", "--mark", cfg.cfg.InboundTProxyMark, "-j", "CONNMARK", "--restore-mark")
		// prevent infinite redirect
		cfg.ruleBuilder.InsertRule(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.MANGLE, 1,
			"-p", constants.TCP, "-m", "mark", "--mark", cfg.cfg.InboundTProxyMark, "-j", constants.RETURN)
		// prevent intercept traffic from envoy/pilot-agent ==> app by 127.0.0.6 --> podip
		cfg.ruleBuilder.InsertRuleV4(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.MANGLE, 2,
			"-p", constants.TCP, "-s", "127.0.0.6/32", "-i", "lo", "-j", constants.RETURN)
		cfg.ruleBuilder.InsertRuleV6(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.MANGLE, 2,
			"-p", constants.TCP, "-s", "::6/128", "-i", "lo", "-j", constants.RETURN)
		// prevent intercept traffic from app ==> app by pod ip
		cfg.ruleBuilder.InsertRule(iptableslog.UndefinedCommand, constants.ISTIOINBOUND, constants.MANGLE, 3,
			"-p", constants.TCP, "-i", "lo", "-m", "mark", "!", "--mark", outboundMark, "-j", constants.RETURN)
	}
	return cfg.executeCommands(&iptVer, &ipt6Ver)
}

type UDPRuleApplier struct {
	iptables *builder.IptablesRuleBuilder
	ext      dep.Dependencies
	ops      Ops
	table    string
	chain    string
	iptV     *dep.IptablesVersion
	ipt6V    *dep.IptablesVersion
}

func (f UDPRuleApplier) RunV4(args ...string) {
	switch f.ops {
	case AppendOps:
		f.iptables.AppendRuleV4(iptableslog.UndefinedCommand, f.chain, f.table, args...)
	case DeleteOps:
		deleteArgs := []string{"-t", f.table, opsToString[f.ops], f.chain}
		deleteArgs = append(deleteArgs, args...)
		f.ext.RunQuietlyAndIgnore(log.WithLabels(), constants.IPTables, f.iptV, nil, deleteArgs...)
	}
}

func (f UDPRuleApplier) RunV6(args ...string) {
	switch f.ops {
	case AppendOps:
		f.iptables.AppendRuleV6(iptableslog.UndefinedCommand, f.chain, f.table, args...)
	case DeleteOps:
		deleteArgs := []string{"-t", f.table, opsToString[f.ops], f.chain}
		deleteArgs = append(deleteArgs, args...)
		f.ext.RunQuietlyAndIgnore(log.WithLabels(), constants.IPTables, f.ipt6V, nil, deleteArgs...)
	}
}

func (f UDPRuleApplier) Run(args ...string) {
	f.RunV4(args...)
	f.RunV6(args...)
}

func (f UDPRuleApplier) WithChain(chain string) UDPRuleApplier {
	f.chain = chain
	return f
}

func (f UDPRuleApplier) WithTable(table string) UDPRuleApplier {
	f.table = table
	return f
}

// HandleDNSUDP is a helper function to tackle with DNS UDP specific operations.
// This helps the creation logic of DNS UDP rules in sync with the deletion.
func HandleDNSUDP(
	ops Ops, iptables *builder.IptablesRuleBuilder, ext dep.Dependencies,
	iptV, ipt6V *dep.IptablesVersion, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool,
	ownerGroupsFilter config.InterceptFilter,
) {
	// TODO BML drop "UDPRuleApplier", it is a largely useless type.
	// we do not need a unique type just to apply UDP iptables rules
	f := UDPRuleApplier{
		iptables: iptables,
		ext:      ext,
		ops:      ops,
		table:    constants.NAT,
		chain:    constants.ISTIOOUTPUT,
		iptV:     iptV,
		ipt6V:    ipt6V,
	}
	// Make sure that upstream DNS requests from agent/envoy dont get captured.
	// TODO: add ip6 as well
	for _, uid := range split(proxyUID) {
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--uid-owner", uid, "-j", constants.RETURN)
	}
	for _, gid := range split(proxyGID) {
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--gid-owner", gid, "-j", constants.RETURN)
	}

	if ownerGroupsFilter.Except {
		for _, group := range ownerGroupsFilter.Values {
			f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--gid-owner", group, "-j", constants.RETURN)
		}
	} else {
		groupIsNoneOf := CombineMatchers(ownerGroupsFilter.Values, func(group string) []string {
			return []string{"-m", "owner", "!", "--gid-owner", group}
		})
		f.Run(Flatten([]string{"-p", "udp", "--dport", "53"}, groupIsNoneOf, []string{"-j", constants.RETURN})...)
	}

	if captureAllDNS {
		// Redirect all UDP dns traffic on port 53 to the agent on port 15053
		// This will be useful for the CNI case where pod DNS server address cannot be decided.
		f.Run("-p", "udp", "--dport", "53", "-j", constants.REDIRECT, "--to-port", constants.IstioAgentDNSListenerPort)
	} else {
		// redirect all UDP dns traffic on port 53 to the agent on port 15053 for all servers
		// in etc/resolv.conf
		// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
		// such as: app -> istio dns server -> dnsmasq -> upstream
		// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
		// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
		// pointed to server X, this would not work. However, the assumption is that is not a common case.
		for _, s := range dnsServersV4 {
			f.RunV4("-p", "udp", "--dport", "53", "-d", s+"/32",
				"-j", constants.REDIRECT, "--to-port", constants.IstioAgentDNSListenerPort)
		}
		for _, s := range dnsServersV6 {
			f.RunV6("-p", "udp", "--dport", "53", "-d", s+"/128",
				"-j", constants.REDIRECT, "--to-port", constants.IstioAgentDNSListenerPort)
		}
	}
	// Split UDP DNS traffic to separate conntrack zones
	addConntrackZoneDNSUDP(f.WithTable(constants.RAW), proxyUID, proxyGID, dnsServersV4, dnsServersV6, captureAllDNS)
}

// addConntrackZoneDNSUDP is a helper function to add iptables rules to split DNS traffic
// in two separate conntrack zones to avoid issues with UDP conntrack race conditions.
// Traffic that goes from istio to DNS servers and vice versa are zone 1 and traffic from
// DNS client to istio and vice versa goes to zone 2
func addConntrackZoneDNSUDP(
	f UDPRuleApplier, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool,
) {
	// TODO: add ip6 as well
	for _, uid := range split(proxyUID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--uid-owner", uid, "-j", constants.CT, "--zone", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		f.Run("-p", "udp", "--sport", "15053", "-m", "owner", "--uid-owner", uid, "-j", constants.CT, "--zone", "2")
	}
	for _, gid := range split(proxyGID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		f.Run("-p", "udp", "--dport", "53", "-m", "owner", "--gid-owner", gid, "-j", constants.CT, "--zone", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		f.Run("-p", "udp", "--sport", "15053", "-m", "owner", "--gid-owner", gid, "-j", constants.CT, "--zone", "2")

	}

	if captureAllDNS {
		// Not specifying destination address is useful for the CNI case where pod DNS server address cannot be decided.

		// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
		f.Run("-p", "udp", "--dport", "53",
			"-j", constants.CT, "--zone", "2")
		// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
		f.WithChain(constants.PREROUTING).Run("-p", "udp", "--sport", "53",
			"-j", constants.CT, "--zone", "1")
	} else {
		// Go through all DNS servers in etc/resolv.conf and mark the packets based on these destination addresses.
		for _, s := range dnsServersV4 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			f.RunV4("-p", "udp", "--dport", "53", "-d", s+"/32",
				"-j", constants.CT, "--zone", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			f.WithChain(constants.PREROUTING).RunV4("-p", "udp", "--sport", "53", "-s", s+"/32",
				"-j", constants.CT, "--zone", "1")
		}
		for _, s := range dnsServersV6 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			f.RunV6("-p", "udp", "--dport", "53", "-d", s+"/128",
				"-j", constants.CT, "--zone", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			f.WithChain(constants.PREROUTING).RunV6("-p", "udp", "--sport", "53", "-s", s+"/128",
				"-j", constants.CT, "--zone", "1")
		}
	}
}

func (cfg *IptablesConfigurator) handleOutboundPortsInclude() {
	if cfg.cfg.OutboundPortsInclude != "" {
		for _, port := range split(cfg.cfg.OutboundPortsInclude) {
			cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand,
				constants.ISTIOOUTPUT, constants.NAT, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOREDIRECT)
		}
	}
}

func (cfg *IptablesConfigurator) handleCaptureByOwnerGroup(filter config.InterceptFilter) {
	if filter.Except {
		for _, group := range filter.Values {
			cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
				"-m", "owner", "--gid-owner", group, "-j", constants.RETURN)
		}
	} else {
		groupIsNoneOf := CombineMatchers(filter.Values, func(group string) []string {
			return []string{"-m", "owner", "!", "--gid-owner", group}
		})
		cfg.ruleBuilder.AppendRule(iptableslog.UndefinedCommand, constants.ISTIOOUTPUT, constants.NAT,
			append(groupIsNoneOf, "-j", constants.RETURN)...)
	}
}

func (cfg *IptablesConfigurator) executeIptablesCommands(iptVer *dep.IptablesVersion, commands [][]string) error {
	for _, cmd := range commands {
		if err := cfg.ext.Run(log.WithLabels(), constants.IPTables, iptVer, nil, cmd...); err != nil {
			return err
		}
	}
	return nil
}

func (cfg *IptablesConfigurator) tryExecuteIptablesCommands(iptVer *dep.IptablesVersion, commands [][]string) {
	for _, cmd := range commands {
		cfg.ext.RunQuietlyAndIgnore(log.WithLabels(), constants.IPTables, iptVer, nil, cmd...)
	}
}

func (cfg *IptablesConfigurator) executeIptablesRestoreCommand(iptVer *dep.IptablesVersion, data string) error {
	log.Infof("Running iptables restore with: %s and the following input:\n%v", iptVer.CmdToString(constants.IPTablesRestore), strings.TrimSpace(data))
	// --noflush to prevent flushing/deleting previous contents from table
	return cfg.ext.Run(log.WithLabels(), constants.IPTablesRestore, iptVer, strings.NewReader(data), "--noflush")
}

// VerifyIptablesState function verifies the current iptables state against the expected state.
// The current state is considered equal to the expected state if the following three conditions are met:
//   - Every ISTIO_* chain in the expected state must also exist in the current state.
//   - Every ISTIO_* chain must have the same number of elements in both the current and expected state.
//   - Every rule in the expected state (whether it is in an ISTIO or non-ISTIO chain) must also exist in the current state.
//     The verification is performed by using "iptables -C" on the rule produced by our iptables builder. No comparison of the parsed rules is done.
//
// Note: The order of the rules is not checked and is not used to determine the equivalence of the two states.
// The function returns two boolean values, the first one indicates whether residues exist,
// and the second one indicates whether differences were found between the current and expected state.
func (cfg *IptablesConfigurator) VerifyIptablesState(iptVer, ipt6Ver *dep.IptablesVersion) (bool, bool) {
	// These variables track the status of iptables installation
	residueExists := false // Flag to indicate if iptables residues from previous executions are found
	deltaExists := false   // Flag to indicate if a difference is found between expected and current state

check_loop:
	for _, ipCfg := range []struct {
		ver        *dep.IptablesVersion
		expected   string
		checkRules [][]string
	}{
		{iptVer, cfg.ruleBuilder.BuildV4Restore(), cfg.ruleBuilder.BuildCheckV4()},
		{ipt6Ver, cfg.ruleBuilder.BuildV6Restore(), cfg.ruleBuilder.BuildCheckV6()},
	} {
		output, err := cfg.ext.RunWithOutput(log.WithLabels(), constants.IPTablesSave, ipCfg.ver, nil)
		if err == nil {
			currentState := cfg.ruleBuilder.GetStateFromSave(output.String())
			log.Debugf("Current iptables state: %#v", currentState)
			for _, value := range currentState {
				if residueExists {
					break
				}
				residueExists = len(value) != 0
			}
			if !residueExists {
				continue
			}
			expectedState := cfg.ruleBuilder.GetStateFromSave(ipCfg.expected)
			log.Debugf("Expected iptables state: %#v", expectedState)
			for table, chains := range expectedState {
				_, ok := currentState[table]
				if !ok {
					deltaExists = true
					log.Debugf("Can't find expected table %s in current state", table)
					break check_loop
				}
				for chain, rules := range chains {
					currentRules, ok := currentState[table][chain]
					if !ok || (strings.HasPrefix(chain, "ISTIO_") && len(rules) != len(currentRules)) {
						deltaExists = true
						log.Debugf("Mismatching number of rules in chain %s between current and expected state", chain)
						break check_loop
					}
				}
			}
			err = cfg.executeIptablesCommands(ipCfg.ver, ipCfg.checkRules)
			if err != nil {
				deltaExists = true
				log.Debugf("iptables check rules failed")
				break
			}
		}

	}

	if !residueExists {
		log.Info("Clean-state detected, new iptables are needed")
		return false, true
	}

	if deltaExists {
		log.Warn("Found residues of old iptables rules/chains, reconciliation is needed")
	} else {
		log.Warn("Found compatible residues of old iptables rules/chains, reconciliation not needed")
	}

	return residueExists, deltaExists
}

func (cfg *IptablesConfigurator) executeCommands(iptVer, ipt6Ver *dep.IptablesVersion) error {
	guardrails := false
	defer func() {
		if guardrails {
			log.Info("Removing guardrails")
			guardrailsCleanup := cfg.ruleBuilder.BuildCleanupGuardrails()
			_ = cfg.executeIptablesCommands(iptVer, guardrailsCleanup)
			_ = cfg.executeIptablesCommands(ipt6Ver, guardrailsCleanup)
		}
	}()

	residueExists, deltaExists := cfg.VerifyIptablesState(iptVer, ipt6Ver)
	if residueExists && deltaExists && !cfg.cfg.Reconcile {
		log.Warn("reconcile is needed but no-reconcile flag is set. Unexpected behavior may occur due to preexisting iptables rules")
	}
	// Cleanup Step
	if (residueExists && deltaExists && cfg.cfg.Reconcile) || cfg.cfg.CleanupOnly {
		// Apply safety guardrails
		if !cfg.cfg.CleanupOnly {
			log.Info("Setting up guardrails")
			guardrailsCleanup := cfg.ruleBuilder.BuildCleanupGuardrails()
			guardrailsRules := cfg.ruleBuilder.BuildGuardrails()
			for _, ver := range []*dep.IptablesVersion{iptVer, ipt6Ver} {
				cfg.tryExecuteIptablesCommands(ver, guardrailsCleanup)
				if err := cfg.executeIptablesCommands(ver, guardrailsRules); err != nil {
					return err
				}
				guardrails = true
			}
		}
		// Remove old iptables
		log.Info("Performing cleanup of existing iptables")
		cfg.tryExecuteIptablesCommands(iptVer, cfg.ruleBuilder.BuildCleanupV4())
		cfg.tryExecuteIptablesCommands(ipt6Ver, cfg.ruleBuilder.BuildCleanupV6())
	}

	// Apply Step
	if (deltaExists || cfg.cfg.ForceApply) && !cfg.cfg.CleanupOnly {
		log.Info("Applying iptables chains and rules")
		// Execute iptables-restore
		if err := cfg.executeIptablesRestoreCommand(iptVer, cfg.ruleBuilder.BuildV4Restore()); err != nil {
			return err
		}
		// Execute ip6tables-restore
		if err := cfg.executeIptablesRestoreCommand(ipt6Ver, cfg.ruleBuilder.BuildV6Restore()); err != nil {
			return err
		}
	}

	if !deltaExists && cfg.cfg.ForceApply {
		log.Warn("The forced apply of iptables changes succeeded despite the presence of conflicting rules or chains. " +
			"If you encounter this message, please consider reporting it as an issue.")
	}

	return nil
}
