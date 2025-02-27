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
)

type Ops int

const (
	// AppendOps performs append operations of rules
	AppendOps Ops = iota
	// DeleteOps performs delete operations of rules
	DeleteOps
)

type IptablesConfigurator struct {
	ruleBuilder *builder.IptablesRuleBuilder
	// TODO(abhide): Fix dep.Dependencies with better interface
	ext   dep.Dependencies
	cfg   *config.Config
	iptV  dep.IptablesVersion
	ipt6V dep.IptablesVersion
}

func NewIptablesConfigurator(cfg *config.Config, ext dep.Dependencies) (*IptablesConfigurator, error) {
	iptVer, err := ext.DetectIptablesVersion(false)
	if err != nil {
		return nil, err
	}

	ipt6Ver, err := ext.DetectIptablesVersion(true)
	if err != nil {
		return nil, err
	}

	return &IptablesConfigurator{
		ruleBuilder: builder.NewIptablesRuleBuilder(cfg),
		ext:         ext,
		cfg:         cfg,
		iptV:        iptVer,
		ipt6V:       ipt6Ver,
	}, nil
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
		if cfg.cfg.InboundInterceptionMode == "TPROXY" {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the ISTIOINBOUND chain, '-j ISTIODIVERT' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			cfg.ruleBuilder.AppendRule(constants.ISTIODIVERT, "mangle", "-j", "MARK", "--set-mark",
				cfg.cfg.InboundTProxyMark)
			cfg.ruleBuilder.AppendRule(constants.ISTIODIVERT, "mangle", "-j", "ACCEPT")

			// Create a new chain for redirecting inbound traffic to the common Envoy
			// port.
			// In the ISTIOINBOUND chain, '-j RETURN' bypasses Envoy and
			// '-j ISTIOTPROXY' redirects to Envoy.
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128",
				constants.ISTIOTPROXY, "mangle", "!", "-d", constants.IPVersionSpecific,
				"-p", "tcp", "-j", "TPROXY",
				"--tproxy-mark", cfg.cfg.InboundTProxyMark+"/0xffffffff", "--on-port", cfg.cfg.InboundCapturePort)
			table = "mangle"
		} else {
			table = "nat"
		}
		cfg.ruleBuilder.AppendRule("PREROUTING", table, "-p", "tcp",
			"-j", constants.ISTIOINBOUND)

		if cfg.cfg.InboundPortsInclude == "*" {
			// Apply any user-specified port exclusions.
			if cfg.cfg.InboundPortsExclude != "" {
				for _, port := range split(cfg.cfg.InboundPortsExclude) {
					cfg.ruleBuilder.AppendRule(constants.ISTIOINBOUND, table, "-p", "tcp",
						"--dport", port, "-j", "RETURN")
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if cfg.cfg.InboundInterceptionMode == "TPROXY" {
				// If an inbound packet belongs to an established socket, route it to the
				// loopback interface.
				cfg.ruleBuilder.AppendRule(constants.ISTIOINBOUND, "mangle", "-p", "tcp",
					"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", constants.ISTIODIVERT)
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				cfg.ruleBuilder.AppendRule(constants.ISTIOINBOUND, "mangle", "-p", "tcp",
					"-j", constants.ISTIOTPROXY)
			} else {
				cfg.ruleBuilder.AppendRule(constants.ISTIOINBOUND, "nat", "-p", "tcp",
					"-j", constants.ISTIOINREDIRECT)
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(cfg.cfg.InboundPortsInclude) {
				if cfg.cfg.InboundInterceptionMode == "TPROXY" {
					cfg.ruleBuilder.AppendRule(constants.ISTIOINBOUND, "mangle", "-p", "tcp",
						"--dport", port, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", constants.ISTIODIVERT)
					cfg.ruleBuilder.AppendRule(
						constants.ISTIOINBOUND, "mangle", "-p", "tcp", "--dport", port, "-j", constants.ISTIOTPROXY)
				} else {
					cfg.ruleBuilder.AppendRule(
						constants.ISTIOINBOUND, "nat", "-p", "tcp", "--dport", port, "-j", constants.ISTIOINREDIRECT)
				}
			}
		}
	}
}

func (cfg *IptablesConfigurator) handleOutboundIncludeRules(
	rangeInclude NetworkRange,
	appendRule func(chain string, table string, params ...string) *builder.IptablesRuleBuilder,
	insert func(chain string, table string, position int, params ...string) *builder.IptablesRuleBuilder,
) {
	// Apply outbound IP inclusions.
	if rangeInclude.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		appendRule(constants.ISTIOOUTPUT, "nat", "-j", constants.ISTIOREDIRECT)
		for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
			insert(
				"PREROUTING", "nat", 1, "-i", internalInterface, "-j", constants.ISTIOREDIRECT)
		}
	} else if len(rangeInclude.CIDRs) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range rangeInclude.CIDRs {
			for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
				insert("PREROUTING", "nat", 1, "-i", internalInterface,
					"-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
			}
			appendRule(
				constants.ISTIOOUTPUT, "nat", "-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
		}
	}
}

func (cfg *IptablesConfigurator) shortCircuitKubeInternalInterface() {
	for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
		cfg.ruleBuilder.InsertRule("PREROUTING", "nat", 1, "-i", internalInterface, "-j", "RETURN")
	}
}

func (cfg *IptablesConfigurator) shortCircuitExcludeInterfaces() {
	for _, excludeInterface := range split(cfg.cfg.ExcludeInterfaces) {
		cfg.ruleBuilder.AppendRule(
			"PREROUTING", "nat", "-i", excludeInterface, "-j", "RETURN")
		cfg.ruleBuilder.AppendRule("OUTPUT", "nat", "-o", excludeInterface, "-j", "RETURN")
	}
	if cfg.cfg.InboundInterceptionMode == "TPROXY" {
		for _, excludeInterface := range split(cfg.cfg.ExcludeInterfaces) {

			cfg.ruleBuilder.AppendRule(
				"PREROUTING", "mangle", "-i", excludeInterface, "-j", "RETURN")
			cfg.ruleBuilder.AppendRule("OUTPUT", "mangle", "-o", excludeInterface, "-j", "RETURN")
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
	defer func() {
		// Best effort since we don't know if the commands exist
		if state, err := cfg.ext.Run(log.WithLabels(), true, constants.IPTablesSave, &cfg.iptV, nil); err == nil {
			log.Infof("Final iptables state (IPv4):\n%s", state)
		}
		if cfg.cfg.EnableIPv6 {
			if state, err := cfg.ext.Run(log.WithLabels(), true, constants.IPTablesSave, &cfg.ipt6V, nil); err == nil {
				log.Infof("Final iptables state (IPv6):\n%s", state)
			}
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
	// How many DNS flags do we have? Three DNS flags! AH AH AH AH
	if redirectDNS && !cfg.cfg.CaptureAllDNS && len(cfg.cfg.DNSServersV4) == 0 && len(cfg.cfg.DNSServersV6) == 0 {
		log.Warn("REDIRECT_DNS is set, but CAPTURE_ALL_DNS is false, and no DNS servers provided. DNS capture disabled.")
		redirectDNS = false
	}

	cfg.logConfig()

	cfg.shortCircuitExcludeInterfaces()

	// Do not capture internal interface.
	cfg.shortCircuitKubeInternalInterface()

	// Create a rule for invalid drop in PREROUTING chain in mangle table, so the iptables will drop the out of window packets instead of reset connection .
	dropInvalid := cfg.cfg.DropInvalid
	if dropInvalid {
		cfg.ruleBuilder.AppendRule("PREROUTING", "mangle", "-m", "conntrack", "--ctstate",
			"INVALID", "-j", constants.ISTIODROP)
		cfg.ruleBuilder.AppendRule(constants.ISTIODROP, "mangle", "-j", "DROP")
	}

	// Create a new chain for to hit tunnel port directly. Envoy will be listening on port acting as VPN tunnel.
	cfg.ruleBuilder.AppendRule(constants.ISTIOINBOUND, "nat", "-p", "tcp", "--dport",
		cfg.cfg.InboundTunnelPort, "-j", "RETURN")

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIOREDIRECT'
	// redirects to Envoy.
	cfg.ruleBuilder.AppendRule(
		constants.ISTIOREDIRECT, "nat", "-p", "tcp", "-j", "REDIRECT", "--to-ports", cfg.cfg.ProxyPort)

	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.

	cfg.ruleBuilder.AppendRule(constants.ISTIOINREDIRECT, "nat", "-p", "tcp", "-j", "REDIRECT",
		"--to-ports", cfg.cfg.InboundCapturePort)

	cfg.handleInboundPortsInclude()

	// TODO: change the default behavior to not intercept any output - user may use http_proxy or another
	// iptablesOrFail wrapper (like ufw). Current default is similar with 0.1
	// Jump to the ISTIOOUTPUT chain from OUTPUT chain for all traffic
	// NOTE: udp traffic will be optionally shunted (or no-op'd) within the ISTIOOUTPUT chain, we don't need a conditional jump here.
	cfg.ruleBuilder.AppendRule("OUTPUT", "nat", "-j", constants.ISTIOOUTPUT)

	// Apply port based exclusions. Must be applied before connections back to self are redirected.
	if cfg.cfg.OutboundPortsExclude != "" {
		for _, port := range split(cfg.cfg.OutboundPortsExclude) {
			cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat", "-p", "tcp", "--dport", port, "-j", "RETURN")
			cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat", "-p", "udp", "--dport", port, "-j", "RETURN")
		}
	}

	// 127.0.0.6/::7 is bind connect from inbound passthrough cluster
	cfg.ruleBuilder.AppendVersionedRule("127.0.0.6/32", "::6/128", constants.ISTIOOUTPUT, "nat",
		"-o", "lo", "-s", constants.IPVersionSpecific, "-j", "RETURN")

	for _, uid := range split(cfg.cfg.ProxyUID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		// nolint: lll
		if redirectDNS {
			// When DNS is enabled, we skip this for port 53. This ensures we do not have:
			// app => istio-agent => Envoy inbound => dns server
			// Instead, we just have:
			// app => istio-agent => dns server
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", constants.ISTIOOUTPUT, "nat",
				"-o", "lo",
				"!", "-d", constants.IPVersionSpecific,
				"-p", "tcp",
				"-m", "multiport",
				"!", "--dports", "53,"+cfg.cfg.InboundTunnelPort,
				"-m", "owner", "--uid-owner", uid, "-j", constants.ISTIOINREDIRECT)
		} else {
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", constants.ISTIOOUTPUT, "nat",
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
				cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat", "-o", "lo", "-p", "tcp",
					"!", "--dport", "53",
					"-m", "owner", "!", "--uid-owner", uid, "-j", "RETURN")
			} else {
				cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat",
					"-o", "lo", "-m", "owner", "!", "--uid-owner", uid, "-j", "RETURN")
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		// Note that this rule is, unlike the others, protocol-independent - we want to unconditionally skip
		// all UDP/TCP packets from Envoy, regardless of dest.
		cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat",
			"-m", "owner", "--uid-owner", uid, "-j", "RETURN")
	}

	for _, gid := range split(cfg.cfg.ProxyGID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", constants.ISTIOOUTPUT, "nat",
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
				cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat",
					"-o", "lo", "-p", "tcp",
					"!", "--dport", "53",
					"-m", "owner", "!", "--gid-owner", gid, "-j", "RETURN")
			} else {
				cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat",
					"-o", "lo", "-m", "owner", "!", "--gid-owner", gid, "-j", "RETURN")
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		// Note that this rule is, unlike the others, protocol-independent - we want to unconditionally skip
		// all UDP/TCP packets from Envoy, regardless of dest.
		cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat", "-m", "owner", "--gid-owner", gid, "-j", "RETURN")
	}

	ownerGroupsFilter := config.ParseInterceptFilter(cfg.cfg.OwnerGroupsInclude, cfg.cfg.OwnerGroupsExclude)

	cfg.handleCaptureByOwnerGroup(ownerGroupsFilter)

	if redirectDNS {
		SetupDNSRedir(
			cfg.ruleBuilder, cfg.cfg.ProxyUID, cfg.cfg.ProxyGID,
			cfg.cfg.DNSServersV4, cfg.cfg.DNSServersV6, cfg.cfg.CaptureAllDNS,
			ownerGroupsFilter)
	}

	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", constants.ISTIOOUTPUT, "nat",
		"-d", constants.IPVersionSpecific, "-j", "RETURN")
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.CIDRs {
		cfg.ruleBuilder.AppendRuleV4(constants.ISTIOOUTPUT, "nat", "-d", cidr.String(), "-j", "RETURN")
	}
	for _, cidr := range ipv6RangesExclude.CIDRs {
		cfg.ruleBuilder.AppendRuleV6(constants.ISTIOOUTPUT, "nat", "-d", cidr.String(), "-j", "RETURN")
	}

	cfg.handleOutboundPortsInclude()

	cfg.handleOutboundIncludeRules(ipv4RangesInclude, cfg.ruleBuilder.AppendRuleV4, cfg.ruleBuilder.InsertRuleV4)
	cfg.handleOutboundIncludeRules(ipv6RangesInclude, cfg.ruleBuilder.AppendRuleV6, cfg.ruleBuilder.InsertRuleV6)

	if cfg.cfg.InboundInterceptionMode == "TPROXY" {
		// save packet mark set by envoy.filters.listener.original_src as connection mark
		cfg.ruleBuilder.AppendRule("PREROUTING", "mangle",
			"-p", "tcp", "-m", "mark", "--mark", cfg.cfg.InboundTProxyMark, "-j", "CONNMARK", "--save-mark")
		// If the packet is already marked with 1337, then return. This is to prevent mark envoy --> app traffic again.
		cfg.ruleBuilder.AppendRule("OUTPUT", "mangle",
			"-p", "tcp", "-o", "lo", "-m", "mark", "--mark", cfg.cfg.InboundTProxyMark, "-j", "RETURN")
		for _, uid := range split(cfg.cfg.ProxyUID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", "OUTPUT", "mangle",
				"!", "-d", constants.IPVersionSpecific, "-p", "tcp", "-o", "lo",
				"-m", "owner", "--uid-owner", uid, "-j", "MARK", "--set-mark", constants.OutboundMark)
		}
		for _, gid := range split(cfg.cfg.ProxyGID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			cfg.ruleBuilder.AppendVersionedRule(cfg.cfg.HostIPv4LoopbackCidr, "::1/128", "OUTPUT", "mangle",
				"!", "-d", constants.IPVersionSpecific, "-p", "tcp", "-o", "lo",
				"-m", "owner", "--gid-owner", gid, "-j", "MARK", "--set-mark", constants.OutboundMark)
		}
		// mark outgoing packets from workload, match it to policy routing entry setup for TPROXY mode
		cfg.ruleBuilder.AppendRule("OUTPUT", "mangle",
			"-p", "tcp", "-m", "connmark", "--mark", cfg.cfg.InboundTProxyMark, "-j", "CONNMARK", "--restore-mark")
		// prevent infinite redirect
		cfg.ruleBuilder.InsertRule(constants.ISTIOINBOUND, "mangle", 1,
			"-p", "tcp", "-m", "mark", "--mark", cfg.cfg.InboundTProxyMark, "-j", "RETURN")
		// prevent intercept traffic from envoy/pilot-agent ==> app by 127.0.0.6 --> podip
		cfg.ruleBuilder.InsertRuleV4(constants.ISTIOINBOUND, "mangle", 2,
			"-p", "tcp", "-s", "127.0.0.6/32", "-i", "lo", "-j", "RETURN")
		cfg.ruleBuilder.InsertRuleV6(constants.ISTIOINBOUND, "mangle", 2,
			"-p", "tcp", "-s", "::6/128", "-i", "lo", "-j", "RETURN")
		// prevent intercept traffic from app ==> app by pod ip
		cfg.ruleBuilder.InsertRule(constants.ISTIOINBOUND, "mangle", 3,
			"-p", "tcp", "-i", "lo", "-m", "mark", "!", "--mark", constants.OutboundMark, "-j", "RETURN")
	}
	return cfg.executeCommands(&cfg.iptV, &cfg.ipt6V)
}

// SetupDNSRedir is a helper function to tackle with DNS UDP specific operations.
// This helps the creation logic of DNS UDP rules in sync with the deletion.
func SetupDNSRedir(iptables *builder.IptablesRuleBuilder, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool,
	ownerGroupsFilter config.InterceptFilter,
) {
	// Uniquely for DNS (at this time) we need a jump in "raw:OUTPUT", so this jump is conditional on that setting.
	// And, unlike nat/OUTPUT, we have no shared rules, so no need to do a 2-level jump at this time
	iptables.AppendRule("OUTPUT", "raw", "-j", constants.ISTIOOUTPUTDNS)

	// Conditionally insert jumps for V6 and V4 - we may have DNS capture enabled for V4 servers but not V6, or vice versa.
	// This avoids creating no-op jumps in v6 if we only need them in v4.
	//
	// TODO we should probably *conditionally* create jumps if and only if rules exist in the jumped-to table,
	// in a more automatic fashion.
	if captureAllDNS || len(dnsServersV4) > 0 {
		iptables.AppendRuleV4(constants.ISTIOOUTPUT, "nat", "-j", constants.ISTIOOUTPUTDNS)
	}

	if captureAllDNS || len(dnsServersV6) > 0 {
		iptables.AppendRuleV6(constants.ISTIOOUTPUT, "nat", "-j", constants.ISTIOOUTPUTDNS)
	}

	if captureAllDNS {
		// Redirect all TCP dns traffic on port 53 to the agent on port 15053
		// This will be useful for the CNI case where pod DNS server address cannot be decided.
		iptables.AppendRule(
			constants.ISTIOOUTPUTDNS, "nat",
			"-p", "tcp",
			"--dport", "53",
			"-j", "REDIRECT",
			"--to-ports", constants.IstioAgentDNSListenerPort)
	} else {
		for _, s := range dnsServersV4 {
			// redirect all TCP dns traffic on port 53 to the agent on port 15053 for all servers
			// in etc/resolv.conf
			// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
			// such as: app -> istio dns server -> dnsmasq -> upstream
			// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
			// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
			// pointed to server X, this would not work. However, the assumption is that is not a common case.
			iptables.AppendRuleV4(
				constants.ISTIOOUTPUTDNS, "nat",
				"-p", "tcp",
				"--dport", "53",
				"-d", s+"/32",
				"-j", "REDIRECT",
				"--to-ports", constants.IstioAgentDNSListenerPort)
		}
		for _, s := range dnsServersV6 {
			iptables.AppendRuleV6(
				constants.ISTIOOUTPUTDNS, "nat",
				"-p", "tcp",
				"--dport", "53",
				"-d", s+"/128",
				"-j", "REDIRECT",
				"--to-ports", constants.IstioAgentDNSListenerPort)
		}
	}

	if captureAllDNS {
		// Redirect all UDP dns traffic on port 53 to the agent on port 15053
		// This will be useful for the CNI case where pod DNS server address cannot be decided.
		iptables.AppendRule(constants.ISTIOOUTPUTDNS, "nat", "-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-port", constants.IstioAgentDNSListenerPort)
	} else {
		// redirect all UDP dns traffic on port 53 to the agent on port 15053 for all servers
		// in etc/resolv.conf
		// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
		// such as: app -> istio dns server -> dnsmasq -> upstream
		// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
		// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
		// pointed to server X, this would not work. However, the assumption is that is not a common case.
		for _, s := range dnsServersV4 {
			iptables.AppendRuleV4(constants.ISTIOOUTPUTDNS, "nat", "-p", "udp", "--dport", "53", "-d", s+"/32",
				"-j", "REDIRECT", "--to-port", constants.IstioAgentDNSListenerPort)
		}
		for _, s := range dnsServersV6 {
			iptables.AppendRuleV6(constants.ISTIOOUTPUTDNS, "nat", "-p", "udp", "--dport", "53", "-d", s+"/128",
				"-j", "REDIRECT", "--to-port", constants.IstioAgentDNSListenerPort)
		}
	}
	// Split UDP DNS traffic to separate conntrack zones
	addDNSConntrackZones(iptables, proxyUID, proxyGID, dnsServersV4, dnsServersV6, captureAllDNS)
}

// addDNSConntrackZones is a helper function to add iptables rules to split DNS traffic
// in two separate conntrack zones to avoid issues with UDP conntrack race conditions.
// Traffic that goes from istio to DNS servers and vice versa are zone 1 and traffic from
// DNS client to istio and vice versa goes to zone 2
func addDNSConntrackZones(
	iptables *builder.IptablesRuleBuilder, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool,
) {
	// TODO: add ip6 as well
	for _, uid := range split(proxyUID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		iptables.AppendRule(constants.ISTIOOUTPUTDNS, "raw", "-p", "udp", "--dport", "53", "-m", "owner", "--uid-owner", uid, "-j", "CT", "--zone", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		iptables.AppendRule(constants.ISTIOOUTPUTDNS, "raw", "-p", "udp", "--sport", "15053", "-m", "owner", "--uid-owner", uid, "-j", "CT", "--zone", "2")
	}
	for _, gid := range split(proxyGID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		iptables.AppendRule(constants.ISTIOOUTPUTDNS, "raw", "-p", "udp", "--dport", "53", "-m", "owner", "--gid-owner", gid, "-j", "CT", "--zone", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		iptables.AppendRule(constants.ISTIOOUTPUTDNS, "raw", "-p", "udp", "--sport", "15053", "-m", "owner", "--gid-owner", gid, "-j", "CT", "--zone", "2")
	}

	// For DNS conntrack, we need (at least one) inbound rule in raw/PREROUTING, so make a chain
	// and jump to it. NOTE that we are conditionally creating the jump from the nat/PREROUTING chain
	// to the ISTIO_INBOUND chain here, because otherwise it is possible to create a jump to an empty chain,
	// which the reconciliation logic currently ignores/won't clean up.
	//
	// TODO in practice this is harmless - a jump to an empty chain is a no-op - but it borks tests.
	if captureAllDNS {
		iptables.AppendRule("PREROUTING", "raw", "-j", constants.ISTIOINBOUND)
		// Not specifying destination address is useful for the CNI case where pod DNS server address cannot be decided.

		// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
		iptables.AppendRule(constants.ISTIOOUTPUTDNS, "raw", "-p", "udp", "--dport", "53",
			"-j", "CT", "--zone", "2")
		// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
		iptables.AppendRule(constants.ISTIOINBOUND, "raw", "-p", "udp", "--sport", "53", "-j", "CT", "--zone", "1")

	} else {

		if len(dnsServersV4) != 0 {
			iptables.AppendRuleV4("PREROUTING", "raw", "-j", constants.ISTIOINBOUND)
		}
		// Go through all DNS servers in etc/resolv.conf and mark the packets based on these destination addresses.
		for _, s := range dnsServersV4 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			iptables.AppendRuleV4(constants.ISTIOOUTPUTDNS, "raw", "-p", "udp", "--dport", "53", "-d", s+"/32",
				"-j", "CT", "--zone", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			iptables.AppendRuleV4(constants.ISTIOINBOUND, "raw", "-p", "udp", "--sport", "53", "-s", s+"/32",
				"-j", "CT", "--zone", "1")
		}

		if len(dnsServersV6) != 0 {
			iptables.AppendRuleV6("PREROUTING", "raw", "-j", constants.ISTIOINBOUND)
		}
		for _, s := range dnsServersV6 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			iptables.AppendRuleV6(constants.ISTIOOUTPUTDNS, "raw", "-p", "udp", "--dport", "53", "-d", s+"/128",
				"-j", "CT", "--zone", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			iptables.AppendRuleV6(constants.ISTIOINBOUND, "raw", "-p", "udp", "--sport", "53", "-s", s+"/128",
				"-j", "CT", "--zone", "1")
		}
	}
}

func (cfg *IptablesConfigurator) handleOutboundPortsInclude() {
	if cfg.cfg.OutboundPortsInclude != "" {
		for _, port := range split(cfg.cfg.OutboundPortsInclude) {
			cfg.ruleBuilder.AppendRule(
				constants.ISTIOOUTPUT, "nat", "-p", "tcp", "--dport", port, "-j", constants.ISTIOREDIRECT)
		}
	}
}

func (cfg *IptablesConfigurator) handleCaptureByOwnerGroup(filter config.InterceptFilter) {
	if filter.Except {
		for _, group := range filter.Values {
			cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat",
				"-m", "owner", "--gid-owner", group, "-j", "RETURN")
		}
	} else {
		groupIsNoneOf := CombineMatchers(filter.Values, func(group string) []string {
			return []string{"-m", "owner", "!", "--gid-owner", group}
		})
		cfg.ruleBuilder.AppendRule(constants.ISTIOOUTPUT, "nat",
			append(groupIsNoneOf, "-j", "RETURN")...)
	}
}

func (cfg *IptablesConfigurator) executeIptablesCommands(iptVer *dep.IptablesVersion, commands [][]string) error {
	for _, cmd := range commands {
		if _, err := cfg.ext.Run(log.WithLabels(), false, constants.IPTables, iptVer, nil, cmd...); err != nil {
			return err
		}
	}
	return nil
}

func (cfg *IptablesConfigurator) tryExecuteIptablesCommands(iptVer *dep.IptablesVersion, commands [][]string) {
	for _, cmd := range commands {
		_, _ = cfg.ext.Run(log.WithLabels(), true, constants.IPTables, iptVer, nil, cmd...)
	}
}

func (cfg *IptablesConfigurator) executeIptablesRestoreCommand(iptVer *dep.IptablesVersion, data string) error {
	log.Infof("Running iptables restore with: %s and the following input:\n%v", iptVer.CmdToString(constants.IPTablesRestore), strings.TrimSpace(data))
	// --noflush to prevent flushing/deleting previous contents from table
	_, err := cfg.ext.Run(log.WithLabels(), false, constants.IPTablesRestore, iptVer, strings.NewReader(data), "--noflush")
	return err
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

	residueExists, deltaExists := VerifyIptablesState(log.WithLabels(), cfg.ext, cfg.ruleBuilder, iptVer, ipt6Ver)
	if residueExists && deltaExists && !cfg.cfg.Reconcile {
		log.Info("reconcile is recommended but no-reconcile flag is set. Unexpected behavior may occur due to preexisting iptables rules")
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
