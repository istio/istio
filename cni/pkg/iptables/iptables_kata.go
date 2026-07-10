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

// Kata-mode variant of the in-pod iptables setup.
//
// This file is entered when a pod's PodLevelOverrides.Kata is non-nil (see
// EXPERIMENTAL_KATA_RUNTIMECLASS_NAMES in cni/pkg/nodeagent/options.go). The
// standard runc path in iptables.go is NOT executed for such pods -- the
// dispatch happens at the top of CreateInpodRules.
//
// Why a separate function instead of if/else sprinkling:
//   - For kata pods the workload runs inside a guest VM, connected to the
//     pod netns over a tap interface (e.g. tap0_kata). The pod IP is NOT
//     locally bound; there is no in-netns app process. This flips several
//     assumptions of the runc rule set (the CONNMARK save/restore pair
//     never fires, OUTPUT-side outbound capture has no netns-local
//     workload to capture, the ISTIO_OUTPUT nat chain is left empty).
//   - Rather than gating each such rule with `if Kata == nil` in the runc
//     builder, we emit the kata rule set from scratch here. A reviewer of
//     iptables.go can trust that no runc-path rule is affected by kata
//     mode: the only kata reference in iptables.go is the top-level
//     dispatch.
//
// nftables has no kata support today. NftablesConfigurator.CreateInpodRules
// fails fast when it sees Kata != nil.
package iptables

import (
	"fmt"

	"istio.io/istio/cni/pkg/config"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
	iptablesconstants "istio.io/istio/tools/istio-iptables/pkg/constants"
)

// createInpodRulesKata is the kata-mode counterpart to
// (*IptablesConfigurator).CreateInpodRules. It runs inside the pod netns.
func (cfg *IptablesConfigurator) createInpodRulesKata(log *istiolog.Scope, podOverrides config.PodLevelOverrides) error {
	// Discover the pod-netns default gateway BEFORE kata replaces eth0's
	// setup with the tap interface. Used later to SNAT kubelet probe
	// traffic so the guest VM has a route back.
	if !podOverrides.Kata.PodGatewayV4.IsValid() && !podOverrides.Kata.PodGatewayV6.IsValid() {
		gw4, gw6 := discoverDefaultGateway()
		podOverrides.Kata.PodGatewayV4 = gw4
		podOverrides.Kata.PodGatewayV6 = gw6
		log.Debugf("kata mode: discovered pod-netns default gateway v4=%v v6=%v", gw4, gw6)
	}
	// Snapshot the pod-netns addresses of eth0 (before kata takes it over).
	// Used to install the self-hairpin OUTPUT mangle rule below.
	if len(podOverrides.Kata.PodIPv4) == 0 && len(podOverrides.Kata.PodIPv6) == 0 {
		ipv4, ipv6 := discoverPodIPs()
		podOverrides.Kata.PodIPv4 = ipv4
		podOverrides.Kata.PodIPv6 = ipv6
		log.Debugf("kata mode: discovered pod-netns addresses v4=%v v6=%v", ipv4, ipv6)
	}

	// Kata DNS interception below DNATs VM-originated DNS queries to
	// 127.0.0.1:15053 (ztunnel's DNS proxy). The packet arrives on
	// tap0_kata with dst=kube-dns; route_localnet must be enabled for the
	// kernel to route loopback addresses on a non-loopback ingress iface.
	if err := writeSysctl("/proc/sys/net/ipv4/conf/all/route_localnet", "1"); err != nil {
		log.Warnf("kata mode: failed to enable route_localnet (DNS interception may fail): %v", err)
	}

	b := cfg.appendInpodRulesKata(podOverrides)

	if err := cfg.addLoopbackRoute(); err != nil {
		return err
	}
	if err := cfg.addInpodMarkIPRule(); err != nil {
		return err
	}

	log.Debug("Adding iptables rules (kata mode)")
	if err := cfg.executeCommands(log, b); err != nil {
		log.Errorf("failed to restore iptables rules: %v", err)
		return err
	}
	return nil
}

// appendInpodRulesKata builds the complete in-pod iptables rule set for a
// kata pod. This is a from-scratch replacement for AppendInpodRules -- the
// runc builder is never entered for kata pods.
func (cfg *IptablesConfigurator) appendInpodRulesKata(podOverrides config.PodLevelOverrides) *builder.IptablesRuleBuilder {
	var redirectDNS bool
	switch podOverrides.DNSProxy {
	case config.PodDNSUnset:
		redirectDNS = cfg.cfg.RedirectDNS
	case config.PodDNSEnabled:
		redirectDNS = true
	case config.PodDNSDisabled:
		redirectDNS = false
	}

	inpodTproxyMark := fmt.Sprintf("0x%x", config.InpodTProxyMark) + "/" + fmt.Sprintf("0x%x", config.InpodTProxyMask)
	inpodMark := fmt.Sprintf("0x%x", config.InpodMark) + "/" + fmt.Sprintf("0x%x", config.InpodMask)

	b := builder.NewIptablesRuleBuilder(config.GetConfig(cfg.cfg))

	// --- Top-level jumps into our custom chains (same as runc). ---

	b.AppendRule("PREROUTING", "mangle", "-j", ChainInpodPrerouting)
	b.AppendRule("OUTPUT", "mangle", "-j", ChainInpodOutput)
	b.AppendRule("OUTPUT", "nat", "-j", ChainInpodOutput)
	if redirectDNS {
		b.AppendRule("PREROUTING", "raw", "-j", ChainInpodPrerouting)
		b.AppendRule("OUTPUT", "raw", "-j", ChainInpodOutput)
	}
	b.AppendRule("PREROUTING", "nat", "-j", ChainInpodPrerouting)

	// --- VirtualInterfaces short-circuit (kubevirt-style, same as runc). ---
	// Note: kata implicitly adds tap0_kata to the treat-as-VM set below via
	// its own rule; VirtualInterfaces here is the user-annotated list.
	for _, virtInterface := range podOverrides.VirtualInterfaces {
		b.AppendRule(ChainInpodPrerouting, "nat",
			"-i", virtInterface,
			"-p", "tcp",
			"-j", "REDIRECT",
			"--to-ports", fmt.Sprint(config.ZtunnelOutboundPort),
		)
		b.AppendRule(ChainInpodPrerouting, "nat",
			"-i", virtInterface,
			"-p", "tcp",
			"-j", "RETURN",
		)
	}

	// --- Kata-specific inbound steering. ---
	//
	// The pod IP is not in the netns's local table -- it's forwarded to the
	// guest VM over tap0_kata -- so ordinary routing would deliver inbound
	// traffic to the VM instead of ztunnel's wildcard listeners on lo.
	//
	// Solution: mark every TCP packet that enters on eth0 with 0x111. The
	// pod netns already has `ip rule fwmark 0x111 lookup 100` + table 100 =
	// `local default dev lo`, so any marked packet is delivered locally to
	// ztunnel (*:15006 plaintext, *:15008 HBONE). Ztunnel-originated egress
	// carries mark 0x539 (SO_MARK) but only appears on tap0_kata, never on
	// eth0, so no exclusion is needed on the eth0 MARK.
	//
	// Note: tap0_kata may not exist in the netns yet -- kata creates it
	// when the sandbox VM boots. iptables rules referencing interfaces by
	// name are evaluated per-packet, so installing them up-front is fine.

	// Kubelet HTTP/TCP probes arrive at eth0 with src HostProbeSNATAddress
	// (a fixed link-local set by host-side istio-cni). For runc these are
	// routed via lo to the in-netns app. For kata the app is in the VM,
	// so we must forward via tap0_kata instead -- which means skipping the
	// eth0 MARK below (otherwise fwmark 0x111 sends them to table 100/lo).
	// The matching POSTROUTING SNAT further down rewrites the source to
	// the original CNI gateway so the VM can route the reply back.
	b.AppendVersionedRule(cfg.cfg.HostProbeSNATAddress.String(), cfg.cfg.HostProbeV6SNATAddress.String(),
		ChainInpodPrerouting, "mangle",
		"-s", iptablesconstants.IPVersionSpecific,
		"-p", "tcp",
		"-j", "ACCEPT",
	)
	b.AppendRule(ChainInpodPrerouting, "mangle",
		"-i", "eth0",
		"-p", "tcp",
		"-j", "MARK",
		"--set-xmark", inpodTproxyMark,
	)
	// Reply path for ztunnel-originated outbound: replies come back on
	// tap0_kata (not eth0), so match by owning socket.
	b.AppendRule(ChainInpodPrerouting, "mangle",
		"-p", "tcp",
		"-m", "socket",
		"-j", "MARK",
		"--set-xmark", inpodTproxyMark,
	)
	// ACCEPT so the fwmark rule fires (skips downstream mangle rules).
	b.AppendRule(ChainInpodPrerouting, "mangle",
		"-p", "tcp",
		"-m", "mark",
		"--mark", inpodTproxyMark,
		"-j", "ACCEPT",
	)

	if redirectDNS {
		// VM-originated UDP DNS: DNAT to 127.0.0.1:15053. We can't use
		// REDIRECT here because ztunnel's DNS proxy only binds on
		// loopback and PREROUTING REDIRECT would rewrite dst to the
		// tap iface IP (169.254.0.1) where nothing is listening.
		// route_localnet=1 (set in createInpodRulesKata) is required.
		b.AppendRuleV4(ChainInpodPrerouting, "nat",
			"-i", "tap0_kata",
			"-p", "udp",
			"-m", "udp",
			"--dport", "53",
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("127.0.0.1:%d", config.DNSCapturePort),
		)
		// TCP DNS: same, and MUST come BEFORE the tap0_kata catch-all
		// REDIRECT to 15001 below (which would otherwise swallow dport 53).
		b.AppendRuleV4(ChainInpodPrerouting, "nat",
			"-i", "tap0_kata",
			"-p", "tcp",
			"--dport", "53",
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("127.0.0.1:%d", config.DNSCapturePort),
		)
		// Mark ztunnel<-upstream DNS replies (sport 53 on eth0) so they
		// route via lo back to ztunnel's DNS proxy socket rather than
		// out tap0_kata to the VM.
		b.AppendRuleV4(ChainInpodPrerouting, "mangle",
			"-i", "eth0",
			"-p", "udp",
			"--sport", "53",
			"-j", "MARK",
			"--set-xmark", inpodTproxyMark,
		)
	}

	// VM-originated outbound: everything arriving on tap0_kata (that's not
	// already DNAT-ed above) gets shunted to ztunnel's outbound port.
	b.AppendRule(ChainInpodPrerouting, "nat",
		"-i", "tap0_kata",
		"-p", "tcp",
		"-j", "REDIRECT",
		"--to-ports", fmt.Sprint(config.ZtunnelOutboundPort),
	)

	// SNAT kubelet-probe traffic (entering with src HostProbeSNATAddress)
	// to the original CNI gateway when it leaves over tap0_kata, so the
	// guest VM has a route back for the reply.
	if podOverrides.Kata.PodGatewayV4.IsValid() {
		b.AppendRuleV4(
			"POSTROUTING", "nat",
			"-s", cfg.cfg.HostProbeSNATAddress.String(),
			"-o", "tap0_kata",
			"-j", "SNAT",
			"--to-source", podOverrides.Kata.PodGatewayV4.String(),
		)
	}
	if podOverrides.Kata.PodGatewayV6.IsValid() {
		b.AppendRuleV6(
			"POSTROUTING", "nat",
			"-s", cfg.cfg.HostProbeV6SNATAddress.String(),
			"-o", "tap0_kata",
			"-j", "SNAT",
			"--to-source", podOverrides.Kata.PodGatewayV6.String(),
		)
	}

	// Ambient self-hairpin: when the local workload (in the VM) hits a
	// Service whose endpoints include this pod, ztunnel may LB-select self
	// and open an outbound HBONE to <own_pod_ip>:15008. For runc this
	// loops via the kernel `local <ip> dev lo` route; in kata-l3fwd we
	// installed `<pod_ip> dev tap0_kata`, which now sends the self-HBONE
	// out to the VM (which has no :15008 listener → RST). Marking with
	// the TProxy mark routes via table 100 (`local default dev lo`) to
	// ztunnel's *:15008 listener, restoring runc behavior.
	for _, ip := range podOverrides.Kata.PodIPv4 {
		b.AppendRuleV4(
			"OUTPUT", "mangle",
			"-d", ip.String()+"/32",
			"-p", "tcp",
			"--dport", fmt.Sprint(config.ZtunnelInboundPort),
			"-j", "MARK",
			"--set-xmark", inpodTproxyMark,
		)
	}
	for _, ip := range podOverrides.Kata.PodIPv6 {
		b.AppendRuleV6(
			"OUTPUT", "mangle",
			"-d", ip.String()+"/128",
			"-p", "tcp",
			"--dport", fmt.Sprint(config.ZtunnelInboundPort),
			"-j", "MARK",
			"--set-xmark", inpodTproxyMark,
		)
	}

	// --- Shared with runc: hostprobe ACCEPT in nat PRERT + inbound
	// plaintext REDIRECT. Both are needed in kata (kata's mangle MARK just
	// steers packets to lo; they still traverse nat PRERT afterward). ---

	if !podOverrides.IngressMode {
		// Kubelet-probe ACCEPT in nat PRERT (mirror of the mangle ACCEPT
		// above, needed so the nat REDIRECT below doesn't swallow probes).
		b.AppendVersionedRule(cfg.cfg.HostProbeSNATAddress.String(), cfg.cfg.HostProbeV6SNATAddress.String(),
			ChainInpodPrerouting, "nat",
			"-s", iptablesconstants.IPVersionSpecific,
			"-p", "tcp",
			"-m", "tcp",
			"-j", "ACCEPT",
		)
		// Wildcard REDIRECT to ztunnel plaintext port (15006). Skip 15008
		// which reaches ztunnel's HBONE listener via lo without REDIRECT.
		b.AppendVersionedRule("127.0.0.1/32", "::1/128",
			ChainInpodPrerouting, "nat",
			"!", "-d", iptablesconstants.IPVersionSpecific,
			"-p", "tcp",
			"!", "--dport", fmt.Sprint(config.ZtunnelInboundPort),
			"-m", "mark", "!",
			"--mark", inpodMark,
			"-j", "REDIRECT",
			"--to-ports", fmt.Sprint(config.ZtunnelInboundPlaintextPort),
		)
	}

	if redirectDNS {
		// DNS conntrack-zone separation for ztunnel's proxy<->upstream
		// DNS forwarding. Needed regardless of runtime; the OUTPUT-side
		// REDIRECT-to-15053 is NOT needed in kata (only netns-local
		// process is ztunnel, whose traffic carries mark 0x539 and would
		// be excluded anyway).
		// See https://github.com/istio/istio/issues/33469
		b.AppendRule(
			ChainInpodOutput, "raw",
			"-p", "udp",
			"-m", "mark",
			"--mark", inpodMark,
			"-m", "udp",
			"--dport", "53",
			"-j", "CT",
			"--zone", "1",
		)
		b.AppendRule(
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

	// Deliberately NOT emitted for kata (compared to runc AppendInpodRules):
	//   * mangle ISTIO_PRERT CONNMARK save (mark→connmark): ztunnel-
	//     originated traffic in kata targets pod_ip and routes out
	//     tap0_kata, so no packet with mark 0x539 ever traverses PRERT.
	//   * mangle ISTIO_OUTPUT CONNMARK restore: paired with the save above.
	//   * nat ISTIO_OUTPUT hostprobe ACCEPT: exemption from the outbound
	//     capture REDIRECT, which is itself omitted here.
	//   * nat ISTIO_OUTPUT outbound-capture block (ACCEPT + lo ACCEPT +
	//     REDIRECT to 15001): the workload is in the guest VM; its
	//     outbound enters via tap0_kata into PREROUTING (handled above),
	//     never OUTPUT. The only netns-local process is ztunnel (mark
	//     0x539). Leaving nat ISTIO_OUTPUT empty.
	//   * nat ISTIO_OUTPUT DNS REDIRECT: same reason -- no netns-local
	//     app to intercept DNS from.

	return b
}
