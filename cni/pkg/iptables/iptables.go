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

package iptables

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"istio.io/istio/cni/pkg/nodeagent/constants"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
	iptablesconfig "istio.io/istio/tools/istio-iptables/pkg/config"
	iptablesconstants "istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	iptableslog "istio.io/istio/tools/istio-iptables/pkg/log"
)

const (
	// INPOD marks/masks
	InpodTProxyMark      = 0x111
	InpodTProxyMask      = 0xfff
	InpodMark            = 1337 // this needs to match the inpod config mark in ztunnel.
	InpodMask            = 0xfff
	InpodRestoreMask     = 0xffffffff
	ChainInpodOutput     = "ISTIO_OUTPUT"
	ChainInpodPrerouting = "ISTIO_PRERT"
	ChainHostPostrouting = "ISTIO_POSTRT"
	RouteTableInbound    = 100
)

var log = istiolog.RegisterScope("iptables", "iptables helper")

type Config struct {
	RestoreFormat     bool `json:"RESTORE_FORMAT"`
	TraceLogging      bool `json:"IPTABLES_TRACE_LOGGING"`
	EnableInboundIPv6 bool `json:"ENABLE_INBOUND_IPV6"`
	RedirectDNS       bool `json:"REDIRECT_DNS"`
}

type IptablesConfigurator struct {
	ext    dep.Dependencies
	nlDeps NetlinkDependencies
	cfg    *Config
	iptV   dep.IptablesVersion
	ipt6V  dep.IptablesVersion
}

func ipbuildConfig(c *Config) *iptablesconfig.Config {
	return &iptablesconfig.Config{
		RestoreFormat:     c.RestoreFormat,
		TraceLogging:      c.TraceLogging,
		EnableInboundIPv6: c.EnableInboundIPv6,
		RedirectDNS:       c.RedirectDNS,
	}
}

func NewIptablesConfigurator(cfg *Config, ext dep.Dependencies, nlDeps NetlinkDependencies) (*IptablesConfigurator, error) {
	if cfg == nil {
		cfg = &Config{
			RestoreFormat: true,
		}
	}

	configurator := &IptablesConfigurator{
		ext:    ext,
		nlDeps: nlDeps,
		cfg:    cfg,
	}

	// By detecting iptables versions *here* once-for-all we are
	// committing to using the same binary/variant (legacy or nft)
	// within all pods as we do on the host.
	//
	// This should be fine, as the host binaries are all we have to work with here anyway,
	// as we are running within a privileged container - and we don't want to take the time to
	// redetect for each pod anyway.
	//
	// Extreme corner case:
	// If for some reason your host had both binaries, and you were injecting out-of-band
	// iptables rules within a pod context into `legacy` tables, but your host context preferred
	// `nft`, we would still inject our rules in-pod into nft tables, which is a bit wonky.
	//
	// But that's stunningly unlikely (and would still work either way)
	iptVer, err := ext.DetectIptablesVersion(false)
	if err != nil {
		return nil, err
	}
	configurator.iptV = iptVer

	ipt6Ver, err := ext.DetectIptablesVersion(true)
	if err != nil {
		return nil, err
	}
	configurator.ipt6V = ipt6Ver

	return configurator, nil
}

func (cfg *IptablesConfigurator) DeleteInpodRules() error {
	var inpodErrs []error

	log.Debug("Deleting iptables rules")

	inpodErrs = append(inpodErrs, cfg.executeDeleteCommands(), cfg.delInpodMarkIPRule(), cfg.delLoopbackRoute())
	return errors.Join(inpodErrs...)
}

func (cfg *IptablesConfigurator) executeDeleteCommands() error {
	deleteCmds := [][]string{
		{"-t", iptablesconstants.MANGLE, "-D", iptablesconstants.PREROUTING, "-j", ChainInpodPrerouting},
		{"-t", iptablesconstants.MANGLE, "-D", iptablesconstants.OUTPUT, "-j", ChainInpodOutput},
		{"-t", iptablesconstants.NAT, "-D", iptablesconstants.OUTPUT, "-j", ChainInpodOutput},
	}

	// these sometimes fail due to "Device or resource busy"
	optionalDeleteCmds := [][]string{
		// flush-then-delete our created chains
		{"-t", iptablesconstants.MANGLE, "-F", ChainInpodPrerouting},
		{"-t", iptablesconstants.MANGLE, "-F", ChainInpodOutput},
		{"-t", iptablesconstants.NAT, "-F", ChainInpodOutput},
		{"-t", iptablesconstants.MANGLE, "-X", ChainInpodPrerouting},
		{"-t", iptablesconstants.MANGLE, "-X", ChainInpodOutput},
		{"-t", iptablesconstants.NAT, "-X", ChainInpodOutput},
	}

	var delErrs []error

	iptablesVariant := []dep.IptablesVersion{}
	iptablesVariant = append(iptablesVariant, cfg.iptV)

	if cfg.cfg.EnableInboundIPv6 {
		iptablesVariant = append(iptablesVariant, cfg.ipt6V)
	}

	for _, iptVer := range iptablesVariant {
		for _, cmd := range deleteCmds {
			delErrs = append(delErrs, cfg.ext.Run(iptablesconstants.IPTables, &iptVer, nil, cmd...))
		}

		for _, cmd := range optionalDeleteCmds {
			err := cfg.ext.Run(iptablesconstants.IPTables, &iptVer, nil, cmd...)
			if err != nil {
				log.Debugf("ignoring error deleting optional iptables rule: %v", err)
			}
		}
	}
	return errors.Join(delErrs...)
}

// Setup iptables rules for in-pod mode. Ideally this should be an idempotent function.
// NOTE that this expects to be run from within the pod network namespace!
func (cfg *IptablesConfigurator) CreateInpodRules(hostProbeSNAT *netip.Addr) error {
	// Append our rules here
	builder := cfg.appendInpodRules(hostProbeSNAT)

	if err := cfg.addLoopbackRoute(); err != nil {
		return err
	}

	if err := cfg.addInpodMarkIPRule(); err != nil {
		return err
	}

	log.Debug("Adding iptables rules")
	if err := cfg.executeCommands(builder); err != nil {
		log.Errorf("failed to restore iptables rules: %v", err)
		return err
	}

	return nil
}

func (cfg *IptablesConfigurator) appendInpodRules(hostProbeSNAT *netip.Addr) *builder.IptablesRuleBuilder {
	redirectDNS := cfg.cfg.RedirectDNS

	inpodMark := fmt.Sprintf("0x%x", InpodMark) + "/" + fmt.Sprintf("0x%x", InpodMask)
	inpodTproxyMark := fmt.Sprintf("0x%x", InpodTProxyMark) + "/" + fmt.Sprintf("0x%x", InpodTProxyMask)

	iptablesBuilder := builder.NewIptablesRuleBuilder(ipbuildConfig(cfg.cfg))

	// Insert jumps to our custom chains
	// This is mostly just for visual tidiness and cleanup, as we can delete the secondary chains and jumps
	// without polluting the main table too much.

	// -t mangle -A PREROUTING -j ISTIO_PRERT
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, iptablesconstants.PREROUTING, iptablesconstants.MANGLE,
		"-j", ChainInpodPrerouting,
	)

	// -t mangle -A OUTPUT -p tcp -j ISTIO_OUTPUT
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, iptablesconstants.OUTPUT, iptablesconstants.MANGLE,
		"-j", ChainInpodOutput,
	)

	// -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, iptablesconstants.OUTPUT, iptablesconstants.NAT,
		"-j", ChainInpodOutput,
	)

	// From here on, we should be only inserting rules into our custom chains.

	// CLI: -A ISTIO_PRERT -m mark --mark 0x539/0xfff -j CONNMARK --set-xmark 0x111/0xfff
	//
	// DESC: If we have a packet mark, set a connmark.
	iptablesBuilder.AppendRule(iptableslog.UndefinedCommand, ChainInpodPrerouting, iptablesconstants.MANGLE, "-m", "mark",
		"--mark", inpodMark,
		"-j", "CONNMARK",
		"--set-xmark", inpodTproxyMark)

	// Handle healthcheck probes from the host node. In the host netns, before the packet enters the pod, we SNAT
	// the healthcheck packet to a fixed IP if the packet is coming from a node-local process with a socket.
	//
	// We do this so we can exempt this traffic from ztunnel capture/proxy - otherwise both kube-proxy (legit)
	// and kubelet (skippable) traffic would have the same srcip once they got to the pod, and would be indistinguishable.
	//
	// Note that SortedList is used here because the istio sets class has no order guarantees,
	// and our unit tests will flake if rules have a nondeterministic ordering.
	// CLI: -t mangle -A ISTIO_PRERT -s 169.254.7.127 -p tcp -m tcp --dport <PROBEPORT> -j ACCEPT
	//
	// DESC: If this is one of our node-probe ports and is from our SNAT-ed/"special" hostside IP, short-circuit out here
	iptablesBuilder.AppendRule(iptableslog.UndefinedCommand, ChainInpodPrerouting, iptablesconstants.MANGLE,
		"-s", hostProbeSNAT.String(),
		"-p", "tcp",
		"-m", "tcp",
		"-j", "ACCEPT",
	)

	// CLI: -t NAT -A ISTIO_OUTPUT -d 169.254.7.127 -p tcp -m tcp -j ACCEPT
	//
	// DESC: Anything coming BACK from the pod healthcheck port with a dest of our SNAT-ed hostside IP
	// we also short-circuit.
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, ChainInpodOutput, iptablesconstants.NAT,
		"-d", hostProbeSNAT.String(),
		"-p", "tcp",
		"-m", "tcp",
		"-j", "ACCEPT",
	)

	// prevent intercept traffic from app ==> app by pod ip
	iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
		iptableslog.UndefinedCommand, ChainInpodPrerouting, iptablesconstants.MANGLE,
		"!", "-d", iptablesconstants.IPVersionSpecific, // ignore traffic to localhost ip, as this rule means to catch traffic to pod ip.
		"-p", iptablesconstants.TCP,
		"-i", "lo",
		"-j", "ACCEPT")

	// CLI: -A ISTIO_PRERT -p tcp -m tcp --dport <INPORT> -m mark ! --mark 0x539/0xfff -j TPROXY --on-port <INPORT> --on-ip 127.0.0.1 --tproxy-mark 0x111/0xfff
	//
	// DESC: Anything heading to <INPORT> that does not have the mark, TPROXY to ztunnel inbound port <INPORT>
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, ChainInpodPrerouting, iptablesconstants.MANGLE,
		"-p", "tcp",
		"-m", "tcp",
		"--dport", fmt.Sprintf("%d", constants.ZtunnelInboundPort),
		"-m", "mark", "!",
		"--mark", inpodMark,
		"-j", "TPROXY",
		"--on-port", fmt.Sprintf("%d", constants.ZtunnelInboundPort),
		// "--on-ip", "127.0.0.1",
		"--tproxy-mark", inpodTproxyMark,
	)

	// CLI: -A ISTIO_PRERT -p tcp -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
	//
	// DESC: Anything that's already in conntrack as an established connection, accept
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, ChainInpodPrerouting, iptablesconstants.MANGLE,
		"-p", "tcp",
		"-m", "conntrack",
		"--ctstate", "RELATED,ESTABLISHED",
		"-j", "ACCEPT",
	)

	// CLI: -A ISTIO_PRERT ! -d 127.0.0.1/32 -p tcp -m mark ! --mark 0x539/0xfff -j TPROXY --on-port <INPLAINPORT> --on-ip 127.0.0.1 --tproxy-mark 0x111/0xfff
	//
	// DESC: Anything that is not bound for localhost and does not have the mark, TPROXY to ztunnel inbound plaintext port <INPLAINPORT>
	iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
		iptableslog.UndefinedCommand, ChainInpodPrerouting, iptablesconstants.MANGLE,
		"!", "-d", iptablesconstants.IPVersionSpecific,
		"-p", "tcp",
		"-m", "mark", "!",
		"--mark", inpodMark,
		"-j", "TPROXY",
		"--on-port", fmt.Sprintf("%d", constants.ZtunnelInboundPlaintextPort),
		// "--on-ip", "127.0.0.1",
		"--tproxy-mark", inpodTproxyMark,
	)

	// CLI: -A ISTIO_OUTPUT -m connmark --mark 0x111/0xfff -j CONNMARK --restore-mark --nfmask 0xffffffff --ctmask 0xffffffff
	//
	// DESC: Propagate/restore connmark (if we had one) for outbound
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, ChainInpodOutput, iptablesconstants.MANGLE,
		"-m", "connmark",
		"--mark", inpodTproxyMark,
		"-j", "CONNMARK",
		"--restore-mark",
		"--nfmask", fmt.Sprintf("0x%x", InpodRestoreMask),
		"--ctmask", fmt.Sprintf("0x%x", InpodRestoreMask),
	)

	// CLI: -A ISTIO_OUTPUT ! -o lo -p udp -m udp --dport 53 -j REDIRECT --to-port 15053
	//
	// DESC: If this is a UDP DNS request to a non-localhost resolver, send it to ztunnel DNS proxy port
	if redirectDNS {
		iptablesBuilder.AppendRule(
			iptableslog.UndefinedCommand, ChainInpodOutput, iptablesconstants.NAT,
			"!", "-o", "lo",
			"-p", "udp",
			"-m", "udp",
			"--dport", "53",
			"-j", "REDIRECT",
			"--to-port", fmt.Sprintf("%d", constants.DNSCapturePort),
		)
	}

	// CLI: -A ISTIO_OUTPUT -p tcp -m mark --mark 0x111/0xfff -j ACCEPT
	//
	// DESC: If this is outbound and has our mark, let it go.
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, ChainInpodOutput, iptablesconstants.NAT,
		"-p", "tcp",
		"-m", "mark",
		"--mark", inpodTproxyMark,
		"-j", "ACCEPT",
	)

	// Do not redirect app calls to back itself via Ztunnel when using the endpoint address
	// e.g. appN => appN by lo
	iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
		iptableslog.UndefinedCommand, ChainInpodOutput, iptablesconstants.NAT,
		"!", "-d", iptablesconstants.IPVersionSpecific,
		"-o", "lo",
		"-j", "ACCEPT",
	)

	// CLI: -A ISTIO_OUTPUT ! -d 127.0.0.1/32 -p tcp -m mark ! --mark 0x539/0xfff -j REDIRECT --to-ports <OUTPORT>
	//
	// DESC: If this is outbound, not bound for localhost, and does not have our packet mark, redirect to ztunnel proxy <OUTPORT>
	iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
		iptableslog.UndefinedCommand, ChainInpodOutput, iptablesconstants.NAT,
		"!", "-d", iptablesconstants.IPVersionSpecific,
		"-p", "tcp",
		"-m", "mark", "!",
		"--mark", inpodMark,
		"-j", "REDIRECT",
		"--to-ports", fmt.Sprintf("%d", constants.ZtunnelOutboundPort),
	)
	return iptablesBuilder
}

func (cfg *IptablesConfigurator) executeCommands(iptablesBuilder *builder.IptablesRuleBuilder) error {
	var execErrs []error

	if cfg.cfg.RestoreFormat {
		// Execute iptables-restore
		execErrs = append(execErrs, cfg.executeIptablesRestoreCommand(iptablesBuilder, &cfg.iptV, true))
		// Execute ip6tables-restore
		if cfg.cfg.EnableInboundIPv6 {
			execErrs = append(execErrs, cfg.executeIptablesRestoreCommand(iptablesBuilder, &cfg.ipt6V, false))
		}
	} else {
		// Execute iptables commands
		execErrs = append(execErrs,
			cfg.executeIptablesCommands(&cfg.iptV, iptablesBuilder.BuildV4()))
		// Execute ip6tables commands
		if cfg.cfg.EnableInboundIPv6 {
			execErrs = append(execErrs,
				cfg.executeIptablesCommands(&cfg.ipt6V, iptablesBuilder.BuildV6()))
		}
	}
	return errors.Join(execErrs...)
}

func (cfg *IptablesConfigurator) executeIptablesCommands(iptVer *dep.IptablesVersion, args [][]string) error {
	var iptErrs []error
	for _, argSet := range args {
		iptErrs = append(iptErrs, cfg.ext.Run(iptablesconstants.IPTables, iptVer, nil, argSet...))
	}
	return errors.Join(iptErrs...)
}

func (cfg *IptablesConfigurator) executeIptablesRestoreCommand(iptablesBuilder *builder.IptablesRuleBuilder, iptVer *dep.IptablesVersion, isIpv4 bool) error {
	cmd := iptablesconstants.IPTablesRestore
	var data string

	if isIpv4 {
		data = iptablesBuilder.BuildV4Restore()
	} else {
		data = iptablesBuilder.BuildV6Restore()
	}

	log.Infof("Running %s with the following input:\n%v", iptVer.CmdToString(cmd), strings.TrimSpace(data))
	// --noflush to prevent flushing/deleting previous contents from table
	return cfg.ext.Run(cmd, iptVer, strings.NewReader(data), "--noflush", "-v")
}

func (cfg *IptablesConfigurator) addLoopbackRoute() error {
	return cfg.nlDeps.AddLoopbackRoutes(cfg.cfg)
}

func (cfg *IptablesConfigurator) delLoopbackRoute() error {
	return cfg.nlDeps.DelLoopbackRoutes(cfg.cfg)
}

func (cfg *IptablesConfigurator) addInpodMarkIPRule() error {
	return cfg.nlDeps.AddInpodMarkIPRule(cfg.cfg)
}

func (cfg *IptablesConfigurator) delInpodMarkIPRule() error {
	return cfg.nlDeps.DelInpodMarkIPRule(cfg.cfg)
}

// Setup iptables rules for HOST netnamespace. Ideally this should be an idempotent function.
// NOTE that this expects to be run from within the HOST network namespace!
//
// We need to do this specifically to be able to distinguish between traffic coming from different node-level processes
// via the nodeIP
// - kubelet (node-local healthchecks, which we do not capture)
// - kube-proxy (fowarded/proxied traffic from LoadBalancer-backed services, potentially with public IPs, which we must capture)
func (cfg *IptablesConfigurator) CreateHostRulesForHealthChecks(hostSNATIP *netip.Addr) error {
	// Append our rules here
	builder := cfg.appendHostRules(hostSNATIP)

	log.Info("Adding host netnamespace iptables rules")
	if err := cfg.executeCommands(builder); err != nil {
		log.Errorf("failed to add host netnamespace iptables rules: %v", err)
		return err
	}

	return nil
}

func (cfg *IptablesConfigurator) DeleteHostRules() {
	log.Debug("Attempting to delete hostside iptables rules (if they exist)")

	cfg.executeHostDeleteCommands()
}

func (cfg *IptablesConfigurator) executeHostDeleteCommands() {
	optionalDeleteCmds := [][]string{
		// delete our main jump in the host ruleset. If it's not there, NBD.
		{"-t", iptablesconstants.NAT, "-D", iptablesconstants.POSTROUTING, "-j", ChainHostPostrouting},
		// flush-then-delete our created chains
		// these sometimes fail due to "Device or resource busy" - again NBD.
		{"-t", iptablesconstants.NAT, "-F", ChainHostPostrouting},
		{"-t", iptablesconstants.NAT, "-X", ChainHostPostrouting},
	}

	// iptablei seems like a reasonable pluralization of iptables
	iptablesVariant := []dep.IptablesVersion{}
	iptablesVariant = append(iptablesVariant, cfg.iptV)

	if cfg.cfg.EnableInboundIPv6 {
		iptablesVariant = append(iptablesVariant, cfg.ipt6V)
	}
	for _, iptVer := range iptablesVariant {
		for _, cmd := range optionalDeleteCmds {
			err := cfg.ext.Run(iptablesconstants.IPTables, &iptVer, nil, cmd...)
			if err != nil {
				log.Debugf("ignoring error deleting optional iptables rule: %v", err)
			}
		}
	}
}

func (cfg *IptablesConfigurator) appendHostRules(hostSNATIP *netip.Addr) *builder.IptablesRuleBuilder {
	log.Info("configuring host-level iptables rules (healthchecks, etc)")

	iptablesBuilder := builder.NewIptablesRuleBuilder(ipbuildConfig(cfg.cfg))

	// For easier cleanup, insert a jump into an owned chain
	// -A POSTROUTING -p tcp -j ISTIO_POSTRT
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, iptablesconstants.POSTROUTING, iptablesconstants.NAT,
		"-j", ChainHostPostrouting,
	)

	// TODO BML I don't think we need UDP? TCP healthcheck redir should catch everything.

	// This is effectively an analog for Istio's old-style podSpec-based health check rewrites.
	// Before Istio would update the pod manifest to rewrite healthchecks to go to sidecar Envoy port 15021,
	// so that it could distinguish things that can be unauthenticated (healthchecks) from other kinds of node traffic
	// (e.g. LoadBalanced Service packets, etc) that need to be authenticated/captured/proxied.
	//
	// We want to do the same thing in ambient but can't rely on podSpec injection. So, do effectively the same thing,
	// but with iptables rules - use `--socket-exists` as a proxy for "is this a forwarded packet" vs "is this originating from
	// a local node socket". If the latter, outside the pod in the host netns, redirect that traffic to a hardcoded/custom proxy
	// healthcheck port, just like we used to. Otherwise, we can't assume it's local-node privileged traffic, and will capture and process it normally.
	//
	// All this is necessary because quite often apps use the same port for healthchecks as they do for reg. traffic, and
	// we cannot make assumptions there.

	// -A OUTPUT -m owner --socket-exists -p tcp -m set --match-set istio-inpod-probes dst,dst -j SNAT --to-source 169.254.7.127
	iptablesBuilder.AppendRule(
		iptableslog.UndefinedCommand, ChainHostPostrouting, iptablesconstants.NAT,
		"-m", "owner",
		"--socket-exists",
		"-p", "tcp",
		"-m", "set",
		"--match-set", constants.ProbeIPSet,
		"dst",
		"-j", "SNAT",
		"--to-source", hostSNATIP.String(),
	)

	return iptablesBuilder
}
