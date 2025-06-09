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
	"context"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"sigs.k8s.io/knftables"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/common/config"
	"istio.io/istio/tools/istio-nftables/pkg/builder"
	"istio.io/istio/tools/istio-nftables/pkg/constants"
)

// NftablesConfigurator is the main struct used to create nftables rules based on Istio configuration.
// It builds the necessary rules and applies them using a provided nftProvider function.
type NftablesConfigurator struct {
	cfg              *config.Config
	NetworkNamespace string
	ruleBuilder      *builder.NftablesRuleBuilder // Helper to construct nftables rules
	nftProvider      func(table string) (NftablesAPI, error)
}

// NetworkRange is used to hold IPv4 or IPv6 ranges.
type NetworkRange struct {
	IsWildcard    bool
	CIDRs         []netip.Prefix
	HasLoopBackIP bool
}

func split(s string) []string {
	return config.Split(s)
}

// NewNftablesConfigurator initializes a new configurator instance.
// If nftProvider is not supplied, it uses the real system provider with the default inet family.
func NewNftablesConfigurator(cfg *config.Config, nftProvider func(table string) (NftablesAPI, error)) (*NftablesConfigurator, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}

	if nftProvider == nil {
		nftProvider = func(table string) (NftablesAPI, error) {
			return NewRealNftables(knftables.InetFamily, table)
		}
	}

	return &NftablesConfigurator{
		cfg:              cfg,
		NetworkNamespace: cfg.NetworkNamespace,
		ruleBuilder:      builder.NewNftablesRuleBuilder(cfg),
		nftProvider:      nftProvider,
	}, nil
}

// separateV4V6 takes a comma-separated CIDR string and splits it into separate IPv4 and IPv6 ranges.
// It also marks whether loopback IPs are included and handles wildcard ("*") case.
func (cfg *NftablesConfigurator) separateV4V6(cidrList string) (NetworkRange, NetworkRange, error) {
	if cidrList == "*" {
		return NetworkRange{IsWildcard: true}, NetworkRange{IsWildcard: true}, nil
	}
	ipv6Ranges := NetworkRange{}
	ipv4Ranges := NetworkRange{}
	for _, ipRange := range split(cidrList) {
		ipp, err := netip.ParsePrefix(ipRange)
		if err != nil {
			_, err = fmt.Fprintf(os.Stderr, "Ignoring error for bug compatibility with istio-nftables: %s\n", err.Error())
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

// logConfig prints out the current environment variable values and the loaded config.
// This is useful for debugging to verify which settings are being used.
func (cfg *NftablesConfigurator) logConfig() {
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
	log.Infof("Istio nftables environment:\n%s", b.String())
	cfg.cfg.Print()
}

// handleInboundPortsInclude sets up rules to redirect inbound TCP traffic to Envoy.
// If TPROXY is enabled, it adds rules to mark packets and route them through mangle tables.
// It also handles excluded ports and loopback exceptions, based on the config.
// The goal here is to make sure incoming traffic reaches Envoy before going to the actual app.
func (cfg *NftablesConfigurator) handleInboundPortsInclude() {
	// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
	// to the local service. If not set, no inbound port will be intercepted by istio nftablesOrFail.
	var table string
	if cfg.cfg.InboundPortsInclude != "" {
		if cfg.cfg.InboundInterceptionMode == "TPROXY" {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the IstioInboundChain chain, 'counter jump IstioDivertChain' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			cfg.ruleBuilder.AppendRule(constants.IstioDivertChain, constants.IstioProxyMangleTable,
				"meta mark set", cfg.cfg.InboundTProxyMark)
			cfg.ruleBuilder.AppendRule(constants.IstioDivertChain, constants.IstioProxyMangleTable, "accept")

			// Create a new chain for redirecting inbound traffic to the common Envoy port.
			// In the IstioInboundChain chain, 'return' bypasses Envoy and
			// 'jump IstioTproxyChain' redirects to Envoy.
			cfg.ruleBuilder.AppendRule(constants.IstioTproxyChain, constants.IstioProxyMangleTable,
				"meta l4proto tcp",
				"ip daddr", "!=", cfg.cfg.HostIPv4LoopbackCidr,
				"tproxy ip to", ":"+cfg.cfg.InboundCapturePort,
				"meta mark set", cfg.cfg.InboundTProxyMark,
				"accept")
			cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioTproxyChain, constants.IstioProxyMangleTable,
				"meta l4proto tcp",
				"ip6 daddr", "!=", "::1/128",
				"tproxy ip6 to", ":"+cfg.cfg.InboundCapturePort,
				"meta mark set", cfg.cfg.InboundTProxyMark,
				"accept")

			table = constants.IstioProxyMangleTable
		} else {
			table = constants.IstioProxyNatTable
		}
		cfg.ruleBuilder.AppendRule(constants.PreroutingChain, table,
			"meta l4proto tcp",
			"jump", constants.IstioInboundChain)

		if cfg.cfg.InboundPortsInclude == "*" {
			// Apply any user-specified port exclusions.
			if cfg.cfg.InboundPortsExclude != "" {
				for _, port := range split(cfg.cfg.InboundPortsExclude) {
					cfg.ruleBuilder.AppendRule(constants.IstioInboundChain, table,
						"meta l4proto tcp",
						"tcp dport", port, "return")
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if cfg.cfg.InboundInterceptionMode == "TPROXY" {
				// If an inbound packet belongs to an established socket, route it to the loopback interface.
				cfg.ruleBuilder.AppendRule(constants.IstioInboundChain, constants.IstioProxyMangleTable,
					"meta l4proto tcp",
					"ct state", "related,established",
					"jump", constants.IstioDivertChain)
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				cfg.ruleBuilder.AppendRule(constants.IstioInboundChain, constants.IstioProxyMangleTable,
					"meta l4proto tcp",
					"jump", constants.IstioTproxyChain)
			} else {
				cfg.ruleBuilder.AppendRule(constants.IstioInboundChain, constants.IstioProxyNatTable,
					"meta l4proto tcp",
					"jump", constants.IstioInRedirectChain)
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(cfg.cfg.InboundPortsInclude) {
				if cfg.cfg.InboundInterceptionMode == "TPROXY" {
					cfg.ruleBuilder.AppendRule(constants.IstioInboundChain, constants.IstioProxyMangleTable,
						"meta l4proto tcp",
						"ct state", "related,established",
						"tcp dport", port,
						"jump", constants.IstioDivertChain)
					cfg.ruleBuilder.AppendRule(
						constants.IstioInboundChain, constants.IstioProxyMangleTable,
						"meta l4proto tcp", "tcp dport", port, "jump", constants.IstioTproxyChain)
				} else {
					cfg.ruleBuilder.AppendRule(
						constants.IstioInboundChain, constants.IstioProxyNatTable,
						"meta l4proto tcp", "tcp dport", port, "jump", constants.IstioInRedirectChain)
				}
			}
		}
	}
}

// handleOutboundIncludeRules sets up redirection for outbound traffic to Envoy based on the IP ranges included in the config.
// If the config contains "*", it means all outbound traffic should be captured.
// Otherwise, only the specified IPv4 and IPv6 CIDRs will be captured and redirected.
func (cfg *NftablesConfigurator) handleOutboundIncludeRules(ipv4NwRange NetworkRange, ipv6NwRange NetworkRange) {
	// Apply outbound IP inclusions.
	if ipv4NwRange.IsWildcard || ipv6NwRange.IsWildcard {
		cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable, "jump", constants.IstioRedirectChain)
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
			cfg.ruleBuilder.InsertRule(
				constants.PreroutingChain, constants.IstioProxyNatTable, 0, "iifname", internalInterface, "jump", constants.IstioRedirectChain)
		}
	} else if len(ipv4NwRange.CIDRs) > 0 || len(ipv6NwRange.CIDRs) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range ipv4NwRange.CIDRs {
			for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
				cfg.ruleBuilder.InsertRule(constants.PreroutingChain, constants.IstioProxyNatTable, 0, "iifname", internalInterface,
					"ip daddr", cidr.String(), "jump", constants.IstioRedirectChain)
			}
			cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable, "ip daddr", cidr.String(), "jump", constants.IstioRedirectChain)
		}

		for _, cidr := range ipv6NwRange.CIDRs {
			for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
				cfg.ruleBuilder.InsertV6RuleIfSupported(constants.PreroutingChain, constants.IstioProxyNatTable, 0, "iifname", internalInterface,
					"ip6 daddr", cidr.String(), "jump", constants.IstioRedirectChain)
			}
			cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioOutputChain, constants.IstioProxyNatTable, "ip6 daddr",
				cidr.String(), "jump", constants.IstioRedirectChain)
		}
	}
}

// shortCircuitKubeInternalInterface adds a rule to skip traffic redirection for configured interfaces.
func (cfg *NftablesConfigurator) shortCircuitKubeInternalInterface() {
	for _, internalInterface := range split(cfg.cfg.RerouteVirtualInterfaces) {
		cfg.ruleBuilder.InsertRule(constants.PreroutingChain, constants.IstioProxyNatTable, 0, "iifname", internalInterface, "return")
	}
}

// shortCircuitExcludeInterfaces adds return rules to skip both NAT and mangle table processing
// for the interfaces listed in ExcludeInterfaces. This is useful when you want to avoid capturing
// traffic from specific network interfaces.
func (cfg *NftablesConfigurator) shortCircuitExcludeInterfaces() {
	for _, excludeInterface := range split(cfg.cfg.ExcludeInterfaces) {
		cfg.ruleBuilder.AppendRule(
			constants.PreroutingChain, constants.IstioProxyNatTable, "iifname", excludeInterface, "return")
		cfg.ruleBuilder.AppendRule(constants.OutputChain, constants.IstioProxyNatTable, "oifname", excludeInterface, "return")
	}
	if cfg.cfg.InboundInterceptionMode == "TPROXY" {
		for _, excludeInterface := range split(cfg.cfg.ExcludeInterfaces) {

			cfg.ruleBuilder.AppendRule(
				constants.PreroutingChain, constants.IstioProxyMangleTable, "iifname", excludeInterface, "return")
			cfg.ruleBuilder.AppendRule(constants.OutputChain, constants.IstioProxyMangleTable, "oifname", excludeInterface, "return")
		}
	}
}

// Run is the main function that builds and applies nftables rules based on the provided configuration.
// It handles exclusion and inclusion logic for inbound and outbound traffic, DNS redirection,
// owner-based filtering, TPROXY mark handling, and finally applies all the rules.
func (cfg *NftablesConfigurator) Run() (map[string]*knftables.Transaction, error) {
	// Since OUTBOUND_IP_RANGES_EXCLUDE could carry ipv4 and ipv6 ranges
	// need to split them in different arrays one for ipv4 and one for ipv6
	// in order to not to fail
	ipv4RangesExclude, ipv6RangesExclude, err := cfg.separateV4V6(cfg.cfg.OutboundIPRangesExclude)
	if err != nil {
		return nil, err
	}
	if ipv4RangesExclude.IsWildcard {
		return nil, fmt.Errorf("invalid value for OUTBOUND_IP_RANGES_EXCLUDE")
	}

	ipv4RangesInclude, ipv6RangesInclude, err := cfg.separateV4V6(cfg.cfg.OutboundIPRangesInclude)
	if err != nil {
		return nil, err
	}

	redirectDNS := cfg.cfg.RedirectDNS
	// How many DNS flags do we have? Three DNS flags! AH AH AH AH
	if redirectDNS && !cfg.cfg.CaptureAllDNS && len(cfg.cfg.DNSServersV4) == 0 && len(cfg.cfg.DNSServersV6) == 0 {
		log.Warn("REDIRECT_DNS is set, but CAPTURE_ALL_DNS is false, and no DNS servers provided. DNS capture disabled.")
		redirectDNS = false
	}

	cfg.logConfig()

	// Add rules to skip specific interfaces from redirection.
	cfg.shortCircuitExcludeInterfaces()
	cfg.shortCircuitKubeInternalInterface()

	// Create a rule for invalid drop in PREROUTING chain in mangle table, so the nftables will drop the out of window packets instead of reset connection .
	dropInvalid := cfg.cfg.DropInvalid
	if dropInvalid {
		cfg.ruleBuilder.AppendRule(constants.PreroutingChain, constants.IstioProxyMangleTable,
			"meta l4proto tcp",
			"ct state", "INVALID",
			"jump", constants.IstioDropChain)
		cfg.ruleBuilder.AppendRule(constants.IstioDropChain, constants.IstioProxyMangleTable, "drop")
	}

	// Create a new chain for to hit tunnel port directly. Envoy will be listening on port acting as VPN tunnel.
	cfg.ruleBuilder.AppendRule(constants.IstioInboundChain, constants.IstioProxyNatTable,
		"meta l4proto tcp",
		"tcp dport", cfg.cfg.InboundTunnelPort,
		"return")

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, 'counter RETURN' bypasses Envoy and 'counter jump IstioRedirectChain'
	// redirects to Envoy.
	cfg.ruleBuilder.AppendRule(
		constants.IstioRedirectChain, constants.IstioProxyNatTable,
		"meta l4proto tcp",
		"redirect to", ":"+cfg.cfg.ProxyPort)

	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.

	cfg.ruleBuilder.AppendRule(constants.IstioInRedirectChain, constants.IstioProxyNatTable,
		"meta l4proto tcp",
		"redirect to", ":"+cfg.cfg.InboundCapturePort)

	// Setup rules to intercept inbound traffic.
	cfg.handleInboundPortsInclude()

	// Send all output traffic to the output chain
	cfg.ruleBuilder.AppendRule(constants.OutputChain, constants.IstioProxyNatTable, "jump", constants.IstioOutputChain)

	// Apply port based exclusions. Must be applied before connections back to self are redirected.
	if cfg.cfg.OutboundPortsExclude != "" {
		for _, port := range split(cfg.cfg.OutboundPortsExclude) {
			cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable, "tcp dport", port, "return")
			cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable, "udp dport", port, "return")
		}
	}

	// 127.0.0.6 and ::6 is bind connect from inbound passthrough cluster
	cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable, "oifname", "lo", "ip saddr", "127.0.0.6/32", "return")
	cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioOutputChain, constants.IstioProxyNatTable, "oifname", "lo", "ip6 saddr", "::6/128", "return")

	for _, uid := range split(cfg.cfg.ProxyUID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		// nolint: lll
		if redirectDNS {
			// When DNS is enabled, we skip this for port 53. This ensures we do not have:
			// app => istio-agent => Envoy inbound => dns server
			// Instead, we just have:
			// app => istio-agent => dns server

			cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip daddr", "!=", cfg.cfg.HostIPv4LoopbackCidr,
				"tcp dport", "!=", "{ "+"53, "+cfg.cfg.InboundTunnelPort+" }",
				"skuid", uid,
				"jump", constants.IstioInRedirectChain)

			cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioOutputChain, constants.IstioProxyNatTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip6 daddr", "!=", "::1/128",
				"tcp dport", "!=", "{ "+"53, "+cfg.cfg.InboundTunnelPort+" }",
				"skuid", uid,
				"jump", constants.IstioInRedirectChain)

		} else {
			cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip daddr", "!=", cfg.cfg.HostIPv4LoopbackCidr,
				"tcp dport", "!=", cfg.cfg.InboundTunnelPort,
				"skuid", uid,
				"jump", constants.IstioInRedirectChain)

			cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioOutputChain, constants.IstioProxyNatTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip6 daddr", "!=", "::1/128",
				"tcp dport", "!=", cfg.cfg.InboundTunnelPort,
				"skuid", uid,
				"jump", constants.IstioInRedirectChain)
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
				cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
					"oifname", "lo",
					"meta l4proto tcp",
					"tcp dport", "!=", "53",
					"skuid", uid,
					"return")
			} else {
				cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
					"oifname", "lo",
					"skuid", "!=", uid,
					"return")
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		// Note that this rule is, unlike the others, protocol-independent - we want to unconditionally skip
		// all UDP/TCP packets from Envoy, regardless of dest.
		cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
			"skuid", uid,
			"return")
	}

	for _, gid := range split(cfg.cfg.ProxyGID) {
		// Redirect app calls back to itself via Envoy when using the service VIP
		// e.g. appN => Envoy (client) => Envoy (server) => appN.
		cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
			"oifname", "lo",
			"meta l4proto tcp",
			"ip daddr", "!=", cfg.cfg.HostIPv4LoopbackCidr,
			"tcp dport", "!=", cfg.cfg.InboundTunnelPort,
			"skgid", gid,
			"jump", constants.IstioInRedirectChain)

		cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioOutputChain, constants.IstioProxyNatTable,
			"oifname", "lo",
			"meta l4proto tcp",
			"ip6 daddr", "!=", "::1/128",
			"tcp dport", "!=", cfg.cfg.InboundTunnelPort,
			"skgid", gid,
			"jump", constants.IstioInRedirectChain)

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
				cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
					"oifname", "lo",
					"meta l4proto tcp",
					"tcp dport", "!=", "53",
					"skgid", "!=", gid,
					"return")
			} else {
				cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
					"oifname", "lo",
					"skgid", "!=", gid,
					"return")
			}
		}

		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		// Note that this rule is, unlike the others, protocol-independent - we want to unconditionally skip
		// all UDP/TCP packets from Envoy, regardless of dest.
		cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
			"skgid", gid,
			"return")
	}

	ownerGroupsFilter := config.ParseInterceptFilter(cfg.cfg.OwnerGroupsInclude, cfg.cfg.OwnerGroupsExclude)

	cfg.handleCaptureByOwnerGroup(ownerGroupsFilter)

	if redirectDNS {
		cfg.SetupDNSRedir(
			cfg.ruleBuilder, cfg.cfg.ProxyUID, cfg.cfg.ProxyGID,
			cfg.cfg.DNSServersV4, cfg.cfg.DNSServersV6, cfg.cfg.CaptureAllDNS,
			ownerGroupsFilter)
	}

	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
		"ip daddr", cfg.cfg.HostIPv4LoopbackCidr, "return")

	cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioOutputChain, constants.IstioProxyNatTable,
		"ip6 daddr", "::1/128", "return")

	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.CIDRs {
		cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
			"ip daddr", cidr.String(),
			"return")
	}
	for _, cidr := range ipv6RangesExclude.CIDRs {
		cfg.ruleBuilder.AppendV6RuleIfSupported(constants.IstioOutputChain, constants.IstioProxyNatTable,
			"ip6 daddr", cidr.String(),
			"return")
	}

	cfg.handleOutboundPortsInclude()

	cfg.handleOutboundIncludeRules(ipv4RangesInclude, ipv6RangesInclude)

	if cfg.cfg.InboundInterceptionMode == "TPROXY" {
		// save packet mark set by envoy.filters.listener.original_src as connection mark
		cfg.ruleBuilder.AppendRule(constants.PreroutingChain, constants.IstioProxyMangleTable,
			"meta l4proto tcp",
			"mark", cfg.cfg.InboundTProxyMark,
			"ct mark set mark")
		// If the packet is already marked with 1337, then return. This is to prevent mark envoy --> app traffic again.
		cfg.ruleBuilder.AppendRule(constants.OutputChain, constants.IstioProxyMangleTable,
			"oifname", "lo",
			"meta l4proto tcp",
			"mark", cfg.cfg.InboundTProxyMark,
			"return")
		for _, uid := range split(cfg.cfg.ProxyUID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			cfg.ruleBuilder.AppendRule(constants.OutputChain, constants.IstioProxyMangleTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip daddr", "!=", cfg.cfg.HostIPv4LoopbackCidr,
				"skuid", uid,
				"meta mark set", constants.OutboundMark)
			cfg.ruleBuilder.AppendV6RuleIfSupported(constants.OutputChain, constants.IstioProxyMangleTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip6 daddr", "!=", "::1/128",
				"skuid", uid,
				"meta mark set", constants.OutboundMark)
		}
		for _, gid := range split(cfg.cfg.ProxyGID) {
			// mark outgoing packets from envoy to workload by pod ip
			// app call VIP --> envoy outbound -(mark 1338)-> envoy inbound --> app
			cfg.ruleBuilder.AppendRule(constants.OutputChain, constants.IstioProxyMangleTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip daddr", "!=", cfg.cfg.HostIPv4LoopbackCidr,
				"skgid", gid,
				"meta mark set", constants.OutboundMark)
			cfg.ruleBuilder.AppendV6RuleIfSupported(constants.OutputChain, constants.IstioProxyMangleTable,
				"oifname", "lo",
				"meta l4proto tcp",
				"ip6 daddr", "!=", "::1/128",
				"skgid", gid,
				"meta mark set", constants.OutboundMark)
		}
		// mark outgoing packets from workload, match it to policy routing entry setup for TPROXY mode
		cfg.ruleBuilder.AppendRule(constants.OutputChain, constants.IstioProxyMangleTable,
			"meta l4proto tcp",
			"ct", "mark", cfg.cfg.InboundTProxyMark,
			"meta mark set", "ct", "mark")
		// prevent infinite redirect
		cfg.ruleBuilder.InsertRule(constants.IstioInboundChain, constants.IstioProxyMangleTable, 0,
			"meta l4proto tcp",
			"mark", cfg.cfg.InboundTProxyMark,
			"return")
		// prevent intercept traffic from envoy/pilot-agent ==> app by 127.0.0.6 --> podip
		cfg.ruleBuilder.InsertRule(constants.IstioInboundChain, constants.IstioProxyMangleTable, 1,
			"iifname", "lo",
			"meta l4proto tcp",
			"ip saddr", "127.0.0.6/32",
			"return")
		cfg.ruleBuilder.InsertV6RuleIfSupported(constants.IstioInboundChain, constants.IstioProxyMangleTable, 1,
			"iifname", "lo",
			"meta l4proto tcp",
			"ip6 saddr", "::6/128",
			"return")
		// prevent intercept traffic from app ==> app by pod ip
		cfg.ruleBuilder.InsertRule(constants.IstioInboundChain, constants.IstioProxyMangleTable, 2,
			"iifname", "lo",
			"meta l4proto tcp",
			"mark", "!=", constants.OutboundMark,
			"return")
	}

	return cfg.executeCommands()
}

// SetupDNSRedir is a helper function for supporting DNS redirection use-cases.
func (cfg *NftablesConfigurator) SetupDNSRedir(nft *builder.NftablesRuleBuilder, proxyUID, proxyGID string,
	dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool, ownerGroupsFilter config.InterceptFilter,
) {
	// Uniquely for DNS (at this time) we need a jump in "raw:OUTPUT", so this jump is conditional on that setting.
	// And, unlike nat/OUTPUT, we have no shared rules, so no need to do a 2-level jump at this time
	nft.AppendRule(constants.OutputChain, constants.IstioProxyRawTable, "jump", constants.IstioOutputDNSChain)

	// Conditionally insert jumps for V6 and V4 - we may have DNS capture enabled for V4 servers but not V6, or vice versa.
	// This avoids creating no-op jumps in v6 if we only need them in v4.
	//
	// TODO we should probably *conditionally* create jumps if and only if rules exist in the jumped-to table,
	// in a more automatic fashion.
	if captureAllDNS || len(dnsServersV4) > 0 || len(dnsServersV6) > 0 {
		nft.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable, "jump", constants.IstioOutputDNSChain)
	}

	if captureAllDNS {
		// Redirect all TCP dns traffic on port 53 to the agent on port 15053
		// This will be useful for the CNI case where pod DNS server address cannot be decided.
		nft.AppendRule(
			constants.IstioOutputDNSChain, constants.IstioProxyNatTable,
			"meta l4proto tcp",
			"tcp dport", "53",
			"redirect",
			"to", ":"+constants.IstioAgentDNSListenerPort)
	} else {
		for _, s := range dnsServersV4 {
			// redirect all TCP dns traffic on port 53 to the agent on port 15053 for all servers
			// in etc/resolv.conf
			// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
			// such as: app -> istio dns server -> dnsmasq -> upstream
			// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
			// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
			// pointed to server X, this would not work. However, the assumption is that is not a common case.
			nft.AppendRule(
				constants.IstioOutputDNSChain, constants.IstioProxyNatTable,
				"ip daddr", s+"/32",
				"tcp dport", "53",
				"redirect",
				"to", ":"+constants.IstioAgentDNSListenerPort)
		}
		for _, s := range dnsServersV6 {
			nft.AppendV6RuleIfSupported(
				constants.IstioOutputDNSChain, constants.IstioProxyNatTable,
				"ip6 daddr", s+"/128",
				"tcp dport", "53",
				"redirect",
				"to", ":"+constants.IstioAgentDNSListenerPort)
		}
	}

	if captureAllDNS {
		// Redirect all UDP dns traffic on port 53 to the agent on port 15053
		// This will be useful for the CNI case where pod DNS server address cannot be decided.
		nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyNatTable,
			"udp dport", "53",
			"redirect",
			"to", ":"+constants.IstioAgentDNSListenerPort)
	} else {
		// redirect all UDP dns traffic on port 53 to the agent on port 15053 for all servers
		// in etc/resolv.conf
		// We avoid redirecting all IP ranges to avoid infinite loops when there are local DNS proxies
		// such as: app -> istio dns server -> dnsmasq -> upstream
		// This ensures that we do not get requests from dnsmasq sent back to the agent dns server in a loop.
		// Note: If a user somehow configured etc/resolv.conf to point to dnsmasq and server X, and dnsmasq also
		// pointed to server X, this would not work. However, the assumption is that is not a common case.
		for _, s := range dnsServersV4 {
			nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyNatTable,
				"ip daddr", s+"/32",
				"udp dport", "53",
				"redirect",
				"to", ":"+constants.IstioAgentDNSListenerPort)
		}
		for _, s := range dnsServersV6 {
			nft.AppendV6RuleIfSupported(constants.IstioOutputDNSChain, constants.IstioProxyNatTable,
				"ip6 daddr", s+"/128",
				"udp dport", "53",
				"redirect",
				"to", ":"+constants.IstioAgentDNSListenerPort)
		}
	}
	// Split UDP DNS traffic to separate conntrack zones
	cfg.addDNSConntrackZones(nft, proxyUID, proxyGID, dnsServersV4, dnsServersV6, captureAllDNS)
}

// addDNSConntrackZones is a helper function to add nftables rules to split DNS traffic
// in two separate conntrack zones to avoid issues with UDP conntrack race conditions.
// Traffic that goes from istio to DNS servers and vice versa are zone 1 and traffic from
// DNS client to istio and vice versa goes to zone 2
func (cfg *NftablesConfigurator) addDNSConntrackZones(
	nft *builder.NftablesRuleBuilder, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool,
) {
	for _, uid := range split(proxyUID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyRawTable,
			"udp dport", "53",
			"meta",
			"skuid", uid,
			"ct", "zone", "set", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyRawTable,
			"udp sport", "15053",
			"meta",
			"skuid", uid,
			"ct", "zone", "set", "2")
	}
	for _, gid := range split(proxyGID) {
		// Packets with dst port 53 from istio to zone 1. These are Istio calls to upstream resolvers
		nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyRawTable,
			"udp dport", "53",
			"meta",
			"skgid", gid,
			"ct", "zone", "set", "1")
		// Packets with src port 15053 from istio to zone 2. These are Istio response packets to application clients
		nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyRawTable,
			"udp sport", "15053",
			"meta",
			"skgid", gid,
			"ct", "zone", "set", "2")
	}

	// For DNS conntrack, we need (at least one) inbound rule in raw/PREROUTING, so make a chain
	// and jump to it. NOTE that we are conditionally creating the jump from the nat/PREROUTING chain
	// to the ISTIO_INBOUND chain here, because otherwise it is possible to create a jump to an empty chain,
	// which the reconciliation logic currently ignores/won't clean up.
	//
	if captureAllDNS {
		nft.AppendRule(constants.PreroutingChain, constants.IstioProxyRawTable, "jump", constants.IstioInboundChain)
		// Not specifying destination address is useful for the CNI case where pod DNS server address cannot be decided.

		// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
		nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyRawTable,
			"udp dport", "53",
			"ct", "zone", "set", "2")
		// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
		nft.AppendRule(constants.IstioInboundChain, constants.IstioProxyRawTable,
			"udp sport", "53",
			"ct", "zone", "set", "1")
	} else {

		if len(dnsServersV4) != 0 || len(dnsServersV6) != 0 {
			nft.AppendRule(constants.PreroutingChain, constants.IstioProxyRawTable, "jump", constants.IstioInboundChain)
		}
		// Go through all DNS servers in etc/resolv.conf and mark the packets based on these destination addresses.
		for _, s := range dnsServersV4 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			nft.AppendRule(constants.IstioOutputDNSChain, constants.IstioProxyRawTable,
				"udp dport", "53",
				"ip daddr", s+"/32",
				"ct", "zone", "set", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			nft.AppendRule(constants.IstioInboundChain, constants.IstioProxyRawTable,
				"udp sport", "53",
				"ip daddr", s+"/32",
				"ct", "zone", "set", "1")
		}

		for _, s := range dnsServersV6 {
			// Mark all UDP dns traffic with dst port 53 as zone 2. These are application client packets towards DNS resolvers.
			nft.AppendV6RuleIfSupported(constants.IstioOutputDNSChain, constants.IstioProxyRawTable,
				"udp dport", "53",
				"ip6 daddr", s+"/128",
				"ct", "zone", "set", "2")
			// Mark all UDP dns traffic with src port 53 as zone 1. These are response packets from the DNS resolvers.
			nft.AppendV6RuleIfSupported(constants.IstioInboundChain, constants.IstioProxyRawTable,
				"udp sport", "53",
				"ip6 daddr", s+"/128",
				"ct", "zone", "set", "1")
		}
	}
}

// handleOutboundPortsInclude checks if there are any outbound ports to include for redirection.
// If yes, it splits the list of ports and adds a rule for each port to redirect TCP traffic.
// This makes sure traffic to these ports gets redirected properly in the IstioProxyNatTable table.
func (cfg *NftablesConfigurator) handleOutboundPortsInclude() {
	if cfg.cfg.OutboundPortsInclude != "" {
		for _, port := range split(cfg.cfg.OutboundPortsInclude) {
			// For each port, add a rule to redirect TCP traffic on that port from the OUTPUT chain in the NAT table to the redirect chain
			cfg.ruleBuilder.AppendRule(
				constants.IstioOutputChain, constants.IstioProxyNatTable, "tcp dport", port, "jump", constants.IstioRedirectChain)
		}
	}
}

// handleCaptureByOwnerGroup adds rules based on the socket owner group ID (skgid).
func (cfg *NftablesConfigurator) handleCaptureByOwnerGroup(filter config.InterceptFilter) {
	if filter.Except {
		for _, group := range filter.Values {
			cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
				"skgid", group,
				"return")
		}
	} else {
		groupIsNoneOf := CombineMatchers(filter.Values, func(group string) []string {
			return []string{"skgid", "!=", group}
		})
		cfg.ruleBuilder.AppendRule(constants.IstioOutputChain, constants.IstioProxyNatTable,
			append(groupIsNoneOf, "return")...)
	}
}

func (cfg *NftablesConfigurator) addIstioTableRules(
	tableName string,
	chains []knftables.Chain,
	rules []knftables.Rule,
) (*knftables.Transaction, error) {
	// Get the nftables provider for the specified table
	nft, err := cfg.nftProvider(tableName)
	if err != nil {
		return nil, err
	}

	// Create a new transaction
	tx := nft.NewTransaction()

	// Ensure that the table exists
	tx.Add(&knftables.Table{})

	// Add the specified chains
	for _, chain := range chains {
		tx.Add(&chain)
	}

	// Track how many rules have been added to each chain
	chainRuleCount := make(map[string]int)

	// Add the rules to the transaction
	for _, rule := range rules {
		chain := rule.Chain

		// In IPtables, inserting a rule at position 1 means it gets placed at the head of the chain. In contrast,
		// nftables starts rule indexing at 0. However, nftables doesn't allow inserting a rule at index 0 if the
		// chain is empty. So to handle this case, we check if the chain is empty, and if it is, we use appendRule instead.
		if rule.Index != nil && chainRuleCount[chain] == 0 {
			rule.Index = nil
		}

		// When a rule includes the Index, its considered as an Insert request.
		if rule.Index != nil {
			tx.Insert(&rule)
		} else {
			tx.Add(&rule)
		}
		chainRuleCount[chain]++
	}

	// Apply changes in this transaction
	return tx, nft.Run(context.TODO(), tx)
}

// addIstioNatTableRules sets up nftables rules in the IstioProxyNatTable table for Istio proxy.
// It makes sure the table and the necessary chains exist, then adds rules from the rule builder.
func (cfg *NftablesConfigurator) addIstioNatTableRules() (*knftables.Transaction, error) {
	chains := []knftables.Chain{
		{
			Name:     constants.PreroutingChain,
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.DNATPriority),
		},
		{
			Name:     constants.OutputChain,
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.DNATPriority),
		},
		{Name: constants.IstioInboundChain},
		{Name: constants.IstioRedirectChain},
		{Name: constants.IstioInRedirectChain},
		{Name: constants.IstioOutputChain},
		{Name: constants.IstioOutputDNSChain},
	}

	rules := cfg.ruleBuilder.Rules[constants.IstioProxyNatTable]

	return cfg.addIstioTableRules(constants.IstioProxyNatTable, chains, rules)
}

// addIstioMangleTableRules adds nftables rules to the IstioProxyMangleTable table for Istio proxy.
// It makes sure the table and the necessary chains exist, then adds rules from the rule builder.
func (cfg *NftablesConfigurator) addIstioMangleTableRules() (*knftables.Transaction, error) {
	if len(cfg.ruleBuilder.Rules[constants.IstioProxyMangleTable]) == 0 {
		return nil, nil
	}

	chains := []knftables.Chain{
		{
			Name:     constants.PreroutingChain,
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     constants.OutputChain,
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{Name: constants.IstioDivertChain},
		{Name: constants.IstioTproxyChain},
		{Name: constants.IstioInboundChain},
		{Name: constants.IstioDropChain},
	}

	rules := cfg.ruleBuilder.Rules[constants.IstioProxyMangleTable]

	return cfg.addIstioTableRules(constants.IstioProxyMangleTable, chains, rules)
}

// addIstioRawTableRules adds nftables rules to the IstioProxyRawTable table for Istio proxy.
// It makes sure the table and the necessary chains exist, then adds rules from the rule builder.
func (cfg *NftablesConfigurator) addIstioRawTableRules() (*knftables.Transaction, error) {
	if len(cfg.ruleBuilder.Rules[constants.IstioProxyRawTable]) == 0 {
		return nil, nil
	}

	chains := []knftables.Chain{
		{
			Name:     constants.PreroutingChain,
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.RawPriority),
		},
		{
			Name:     constants.OutputChain,
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.RawPriority),
		},
		{Name: constants.IstioInboundChain},
		{Name: constants.IstioOutputDNSChain},
	}

	rules := cfg.ruleBuilder.Rules[constants.IstioProxyRawTable]

	return cfg.addIstioTableRules(constants.IstioProxyRawTable, chains, rules)
}

// executeCommands runs all the steps to add nftables rules for Istio.
// It calls separate functions to add rules for NAT, MANGLE, and RAW tables one by one.
// If any step fails, it stops and returns the error right away.
func (cfg *NftablesConfigurator) executeCommands() (map[string]*knftables.Transaction, error) {
	tableTx := make(map[string]*knftables.Transaction)

	tx, addErr := cfg.addIstioNatTableRules()
	if addErr != nil {
		return nil, addErr
	}
	tableTx[constants.IstioProxyNatTable] = tx

	tx, addErr = cfg.addIstioMangleTableRules()
	if addErr != nil {
		return nil, addErr
	}
	tableTx[constants.IstioProxyMangleTable] = tx

	tx, addErr = cfg.addIstioRawTableRules()
	if addErr != nil {
		return nil, addErr
	}
	tableTx[constants.IstioProxyRawTable] = tx

	return tableTx, nil
}

// CombineMatchers takes a list of values and a matcher function.
// For each value, it calls the matcher function to get a list of conditions,
// then it combines all those lists into one big list using Flatten.
func CombineMatchers(values []string, matcher func(value string) []string) []string {
	matchers := make([][]string, 0, len(values))
	for _, value := range values {
		matchers = append(matchers, matcher(value))
	}
	return Flatten(matchers...)
}

// Flatten takes many lists of strings and joins them into a single list and appends all the elements
// from each list one after another.
func Flatten(lists ...[]string) []string {
	var result []string
	for _, list := range lists {
		result = append(result, list...)
	}
	return result
}
