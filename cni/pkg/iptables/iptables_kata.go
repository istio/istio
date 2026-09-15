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
package iptables

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"istio.io/istio/cni/pkg/config"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
)

const kataTapIface = "tap0_kata"

type kataInpodConfig struct {
	podGatewayV4 netip.Addr
	podIPv4      []netip.Addr
}

// createInpodRulesKata is the kata-mode counterpart to
// (*IptablesConfigurator).CreateInpodRules. It runs inside the pod netns.
func (cfg *IptablesConfigurator) createInpodRulesKata(log *istiolog.Scope, podOverrides config.PodLevelOverrides) error {
	if len(podOverrides.VirtualInterfaces) > 0 {
		log.Warnf("kata mode ignores virtual interfaces: %v", podOverrides.VirtualInterfaces)
	}

	// Capture the CNI-provided default gateway to SNAT kubelet probe traffic so replies
	// from the guest return through the pod network namespace.
	gatewayV4, _ := discoverDefaultGateway()
	podIPv4, podIPv6 := discoverPodIPs()
	if len(podIPv6) > 0 {
		return fmt.Errorf("kata mode does not support IPv6 pod addresses: %v", podIPv6)
	}
	kataCfg := kataInpodConfig{
		podGatewayV4: gatewayV4,
		podIPv4:      podIPv4,
	}

	ipv4Configurator := *cfg
	ipv4Config := *cfg.cfg
	ipv4Config.EnableIPv6 = false
	ipv4Configurator.cfg = &ipv4Config

	// DNS interception requires DNATing VM-originated DNS queries to
	// 127.0.0.1:15053 (ztunnel's DNS proxy). The packet arrives on
	// tap0_kata with dst=kube-dns; route_localnet must be enabled for the
	// kernel to route loopback addresses on a non-loopback ingress iface.
	if err := writeSysctl("/proc/sys/net/ipv4/conf/all/route_localnet", "1"); err != nil {
		log.Warnf("kata mode: failed to enable route_localnet (DNS interception may fail): %v", err)
	}

	b := ipv4Configurator.appendInpodRulesKata(podOverrides, kataCfg)

	if err := ipv4Configurator.addLoopbackRoute(); err != nil {
		return err
	}
	if err := ipv4Configurator.addInpodMarkIPRule(); err != nil {
		return err
	}

	log.Debug("Adding iptables rules (kata mode)")
	if err := ipv4Configurator.executeCommands(log, b); err != nil {
		log.Errorf("failed to restore iptables rules: %v", err)
		return err
	}
	return nil
}

// appendInpodRulesKata builds the complete in-pod iptables rule set for a
// kata pod. This is a from-scratch replacement for AppendInpodRules.
func (cfg *IptablesConfigurator) appendInpodRulesKata(
	podOverrides config.PodLevelOverrides,
	kataCfg kataInpodConfig,
) *builder.IptablesRuleBuilder {
	var redirectDNS bool
	switch podOverrides.DNSProxy {
	case config.PodDNSUnset:
		redirectDNS = cfg.cfg.RedirectDNS
	case config.PodDNSEnabled:
		redirectDNS = true
	case config.PodDNSDisabled:
		redirectDNS = false
	}

	// The policy-routing rule for this mark sends traffic to the local
	// route in table 100.
	inpodTproxyMark := fmt.Sprintf("0x%x", config.InpodTProxyMark) + "/" + fmt.Sprintf("0x%x", config.InpodTProxyMask)

	inpodMark := fmt.Sprintf("0x%x", config.InpodMark) + "/" + fmt.Sprintf("0x%x", config.InpodMask)

	b := builder.NewIptablesRuleBuilder(config.GetConfig(cfg.cfg))

	// Top-level jumps into our custom chains.

	b.AppendRuleV4("PREROUTING", "mangle", "-j", ChainInpodPrerouting)
	b.AppendRuleV4("OUTPUT", "mangle", "-j", ChainInpodOutput)
	b.AppendRuleV4("OUTPUT", "nat", "-j", ChainInpodOutput)
	if redirectDNS {
		b.AppendRuleV4("PREROUTING", "raw", "-j", ChainInpodPrerouting)
		b.AppendRuleV4("OUTPUT", "raw", "-j", ChainInpodOutput)
	}
	b.AppendRuleV4("PREROUTING", "nat", "-j", ChainInpodPrerouting)

	// Kubelet HTTP/TCP probes arrive at eth0 with src HostProbeSNATAddress
	// (a fixed link-local address set by the host-side Istio CNI rules).
	// They must continue to the guest through tap0_kata, so exclude them
	// from the local-routing mark applied below.
	// The matching POSTROUTING SNAT further down rewrites the source to
	// the CNI-provided gateway so the guest can route the reply back.
	b.AppendRuleV4(
		ChainInpodPrerouting, "mangle",
		"-s", cfg.cfg.HostProbeSNATAddress.String(),
		"-p", "tcp",
		"-j", "ACCEPT",
	)
	// Mark TCP traffic entering on eth0 for local delivery to ztunnel.
	// The policy rule for 0x111 uses table 100, whose local default route
	// points to lo.
	b.AppendRuleV4(ChainInpodPrerouting, "mangle",
		"-i", "eth0",
		"-p", "tcp",
		"-j", "MARK",
		"--set-xmark", inpodTproxyMark,
	)

	// Replies for ztunnel-originated connections return from the guest on
	// tap0_kata. Match packets with a local socket so policy routing sends
	// them back to ztunnel rather than through normal forwarding.
	b.AppendRuleV4(ChainInpodPrerouting, "mangle",
		"-p", "tcp",
		"-m", "socket",
		"-j", "MARK",
		"--set-xmark", inpodTproxyMark,
	)

	if redirectDNS {
		// VM-originated TCP and UDP DNS: DNAT to 127.0.0.1:15053.
		// route_localnet=1 (set in createInpodRulesKata) is required.
		b.AppendRuleV4(ChainInpodPrerouting, "nat",
			"-i", kataTapIface,
			"-p", "udp",
			"-m", "udp",
			"--dport", "53",
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("127.0.0.1:%d", config.DNSCapturePort),
		)
		b.AppendRuleV4(ChainInpodPrerouting, "nat",
			"-i", kataTapIface,
			"-p", "tcp",
			"--dport", "53",
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("127.0.0.1:%d", config.DNSCapturePort),
		)
		// Mark UDP replies from the upstream DNS server so policy routing
		// delivers them to ztunnel's DNS proxy socket rather than forwarding
		// them through tap0_kata to the guest.
		b.AppendRuleV4(ChainInpodPrerouting, "mangle",
			"-i", "eth0",
			"-p", "udp",
			"--sport", "53",
			"-j", "MARK",
			"--set-xmark", inpodTproxyMark,
		)
	}

	// Redirect VM-originated TCP traffic arriving on tap0_kata to ztunnel's
	// outbound port. Port 53 traffic is handled by the earlier DNS rules.
	b.AppendRuleV4(ChainInpodPrerouting, "nat",
		"-i", kataTapIface,
		"-p", "tcp",
		"-j", "REDIRECT",
		"--to-ports", fmt.Sprint(config.ZtunnelOutboundPort),
	)

	// Rewrite the source of kubelet probes to the CNI-provided gateway as
	// they leave through tap0_kata. The guest already uses this address as
	// its default gateway, so probe replies return to the pod netns.
	if kataCfg.podGatewayV4.IsValid() {
		b.AppendRuleV4(
			"POSTROUTING", "nat",
			"-j", ChainHostPostrouting,
		)
		b.AppendRuleV4(
			ChainHostPostrouting, "nat",
			"-s", cfg.cfg.HostProbeSNATAddress.String(),
			"-o", kataTapIface,
			"-j", "SNAT",
			"--to-source", kataCfg.podGatewayV4.String(),
		)
	}
	// ztunnel may connect to this pod's own HBONE listener through the pod
	// IP. Mark those connections for local delivery instead of routing them
	// through tap0_kata into the guest.
	for _, ip := range kataCfg.podIPv4 {
		b.AppendRuleV4(
			ChainInpodOutput, "mangle",
			"-d", ip.String()+"/32",
			"-p", "tcp",
			"--dport", fmt.Sprint(config.ZtunnelInboundPort),
			"-j", "MARK",
			"--set-xmark", inpodTproxyMark,
		)
	}

	if !podOverrides.IngressMode {
		// Kubelet-probe ACCEPT in nat PRERT (mirror of the mangle ACCEPT
		// above, needed so the nat REDIRECT below doesn't swallow probes).
		b.AppendRuleV4(
			ChainInpodPrerouting, "nat",
			"-s", cfg.cfg.HostProbeSNATAddress.String(),
			"-p", "tcp",
			"-m", "tcp",
			"-j", "ACCEPT",
		)
		// Wildcard REDIRECT to ztunnel plaintext port (15006). Skip 15008
		// which reaches ztunnel's HBONE listener via lo without REDIRECT.
		b.AppendRuleV4(
			ChainInpodPrerouting, "nat",
			"!", "-d", "127.0.0.1/32",
			"-p", "tcp",
			"!", "--dport", fmt.Sprint(config.ZtunnelInboundPort),
			"-m", "mark", "!",
			"--mark", inpodMark,
			"-j", "REDIRECT",
			"--to-ports", fmt.Sprint(config.ZtunnelInboundPlaintextPort),
		)
	}

	if redirectDNS {
		// DNS conntrack-zone separation for ztunnel's proxy-to-upstream
		// DNS forwarding. The normal OUTPUT redirect is not needed because
		// workload DNS originates in the guest and enters through tap0_kata;
		// ztunnel's own DNS traffic carries mark 0x539.
		// See https://github.com/istio/istio/issues/33469
		b.AppendRuleV4(
			ChainInpodOutput, "raw",
			"-p", "udp",
			"-m", "mark",
			"--mark", inpodMark,
			"-m", "udp",
			"--dport", "53",
			"-j", "CT",
			"--zone", "1",
		)
		b.AppendRuleV4(
			ChainInpodPrerouting, "raw",
			"-p", "udp",
			"-m", "mark", "!",
			"--mark", inpodMark,
			"-m", "udp",
			"--sport", "53",
			"-j", "CT",
			"--zone", "1",
		)
	}

	return b
}

// discoverDefaultGateway returns the IPv4 and IPv6 default-route gateways in
// the current netns, if any. Used for kata-mode probe SNAT, where we need a
// gateway address the guest VM is able to route back to.
func discoverDefaultGateway() (netip.Addr, netip.Addr) {
	var v4, v6 netip.Addr
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		routes, err := netlink.RouteList(nil, family)
		if err != nil {
			log.Debugf("failed to list routes for family %d: %v", family, err)
			continue
		}
		for _, r := range routes {
			// Default route: no Dst (or zero-length network).
			if r.Dst != nil && r.Dst.IP != nil && !r.Dst.IP.IsUnspecified() {
				continue
			}
			if r.Gw == nil {
				continue
			}
			addr, ok := netip.AddrFromSlice(r.Gw)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if family == unix.AF_INET && !v4.IsValid() {
				v4 = addr
			} else if family == unix.AF_INET6 && !v6.IsValid() {
				v6 = addr
			}
		}
	}
	return v4, v6
}

// discoverPodIPs returns non-loopback, non-link-local IPv4 and IPv6 addresses
// from non-tap interfaces in the current netns. It is used for Kata self-
// hairpin handling, where ztunnel-originated HBONE traffic to the pod's own IP
// must be delivered locally instead of routed through tap0_kata to the guest.
func discoverPodIPs() ([]netip.Addr, []netip.Addr) {
	var v4, v6 []netip.Addr
	links, err := netlink.LinkList()
	if err != nil {
		log.Debugf("failed to list links: %v", err)
		return v4, v6
	}

	for _, link := range links {
		name := link.Attrs().Name
		if name == "lo" || strings.HasPrefix(name, "tap") {
			continue
		}
		for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
			addrs, err := netlink.AddrList(link, family)
			if err != nil {
				log.Debugf("failed to list addresses for %s family %d: %v", name, family, err)
				continue
			}
			for _, a := range addrs {
				if a.IP == nil {
					continue
				}
				addr, ok := netip.AddrFromSlice(a.IP)
				if !ok {
					continue
				}
				addr = addr.Unmap()
				if !addr.IsValid() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
					continue
				}
				if family == unix.AF_INET {
					v4 = append(v4, addr)
				} else {
					v6 = append(v6, addr)
				}
			}
		}
	}
	return v4, v6
}
