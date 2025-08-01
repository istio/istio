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

	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/cni/pkg/util"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	iptablesconfig "istio.io/istio/tools/common/config"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
	iptablescapture "istio.io/istio/tools/istio-iptables/pkg/capture"
	iptablesconstants "istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

var log = scopes.CNIAgent

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

	DNSCapturePort              = 15053
	ZtunnelInboundPort          = 15008
	ZtunnelOutboundPort         = 15001
	ZtunnelInboundPlaintextPort = 15006
	ProbeIPSet                  = "istio-inpod-probes"
)

// "global"/per-instance IptablesConfig
type IptablesConfig struct {
	TraceLogging           bool       `json:"IPTABLES_TRACE_LOGGING"`
	EnableIPv6             bool       `json:"ENABLE_INBOUND_IPV6"`
	RedirectDNS            bool       `json:"REDIRECT_DNS"`
	HostProbeSNATAddress   netip.Addr `json:"HOST_PROBE_SNAT_ADDRESS"`
	HostProbeV6SNATAddress netip.Addr `json:"HOST_PROBE_V6_SNAT_ADDRESS"`
	Reconcile              bool       `json:"RECONCILE"`
	CleanupOnly            bool       `json:"CLEANUP_ONLY"`
	ForceApply             bool       `json:"FORCE_APPLY"`
}

// For inpod rules, any runtime/dynamic pod-level
// config overrides that may need to be taken into account
// when injecting pod rules
type PodLevelOverrides struct {
	VirtualInterfaces []string
	IngressMode       bool
	DNSProxy          PodDNSOverride
}

type PodDNSOverride int

const (
	PodDNSUnset PodDNSOverride = iota
	PodDNSEnabled
	PodDNSDisabled
)

type IptablesConfigurator struct {
	ext    dep.Dependencies
	nlDeps NetlinkDependencies
	cfg    *IptablesConfig
	iptV   dep.IptablesVersion
	ipt6V  dep.IptablesVersion
}

func ipbuildConfig(c *IptablesConfig) *iptablesconfig.Config {
	return &iptablesconfig.Config{
		EnableIPv6:  c.EnableIPv6,
		RedirectDNS: c.RedirectDNS,
		Reconcile:   c.Reconcile,
		ForceApply:  c.ForceApply,
	}
}

func NewIptablesConfigurator(
	hostCfg *IptablesConfig,
	podCfg *IptablesConfig,
	hostDeps dep.Dependencies,
	podDeps dep.Dependencies,
	nlDeps NetlinkDependencies,
) (*IptablesConfigurator, *IptablesConfigurator, error) {
	if hostCfg == nil {
		hostCfg = &IptablesConfig{}
	}
	if podCfg == nil {
		podCfg = &IptablesConfig{}
	}

	configurator := &IptablesConfigurator{
		ext:    hostDeps,
		nlDeps: nlDeps,
		cfg:    hostCfg,
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
	err := util.RunAsHost(func() error {
		iptVer, err := hostDeps.DetectIptablesVersion(false)
		if err != nil {
			return err
		}

		log.Debugf("found iptables binary: %+v", iptVer)

		configurator.iptV = iptVer

		ipt6Ver, err := hostDeps.DetectIptablesVersion(true)
		if err != nil {
			if hostCfg.EnableIPv6 {
				return err
			}
			log.Warnf("Failed to detect a working ip6tables binary; continuing because IPv6 support is disabled (ENABLE_INBOUND_IPV6=false): %v", err)
			ipt6Ver = dep.IptablesVersion{}
		} else {
			log.Debugf("found iptables v6 binary: %+v", ipt6Ver)
		}

		configurator.ipt6V = ipt6Ver
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Setup another configurator with inpod configuration. Basically this will just change how locking is done.
	inPodConfigurator := ptr.Of(*configurator)
	inPodConfigurator.ext = podDeps
	inPodConfigurator.cfg = podCfg
	return configurator, inPodConfigurator, nil
}

func (cfg *IptablesConfigurator) DeleteInpodRules(log *istiolog.Scope) error {
	var inpodErrs []error

	log.Debug("deleting iptables rules")
	cfg.executeDeleteCommands(log)
	inpodErrs = append(inpodErrs, cfg.delInpodMarkIPRule(), cfg.delLoopbackRoute())
	return errors.Join(inpodErrs...)
}

func (cfg *IptablesConfigurator) executeDeleteCommands(log *istiolog.Scope) {
	deleteCmds := [][]string{
		{"-t", "mangle", "-D", "PREROUTING", "-j", ChainInpodPrerouting},
		{"-t", "mangle", "-D", "OUTPUT", "-j", ChainInpodOutput},
		{"-t", "nat", "-D", "OUTPUT", "-j", ChainInpodOutput},
		{"-t", "raw", "-D", "PREROUTING", "-j", ChainInpodPrerouting},
		{"-t", "raw", "-D", "OUTPUT", "-j", ChainInpodOutput},
		{"-t", "nat", "-D", "PREROUTING", "-j", ChainInpodPrerouting},
		// flush-then-delete our created chains
		// these sometimes fail due to "Device or resource busy" or because they are optional given the iptables cfg
		{"-t", "mangle", "-F", ChainInpodPrerouting},
		{"-t", "mangle", "-F", ChainInpodOutput},
		{"-t", "nat", "-F", ChainInpodPrerouting},
		{"-t", "nat", "-F", ChainInpodOutput},
		{"-t", "raw", "-F", ChainInpodPrerouting},
		{"-t", "raw", "-F", ChainInpodOutput},
		{"-t", "mangle", "-X", ChainInpodPrerouting},
		{"-t", "mangle", "-X", ChainInpodOutput},
		{"-t", "nat", "-X", ChainInpodPrerouting},
		{"-t", "nat", "-X", ChainInpodOutput},
		{"-t", "raw", "-X", ChainInpodPrerouting},
		{"-t", "raw", "-X", ChainInpodOutput},
	}

	iptablesVariant := []dep.IptablesVersion{}
	iptablesVariant = append(iptablesVariant, cfg.iptV)

	if cfg.cfg.EnableIPv6 {
		iptablesVariant = append(iptablesVariant, cfg.ipt6V)
	}

	for _, iptVer := range iptablesVariant {
		for _, cmd := range deleteCmds {
			_, _ = cfg.ext.Run(log, true, iptablesconstants.IPTables, &iptVer, nil, cmd...)
		}
	}
}

// Setup iptables rules for in-pod mode. Ideally this should be an idempotent function.
// NOTE that this expects to be run from within the pod network namespace!
func (cfg *IptablesConfigurator) CreateInpodRules(log *istiolog.Scope, podOverrides PodLevelOverrides) error {
	// Append our rules here
	builder := cfg.AppendInpodRules(podOverrides)

	if err := cfg.addLoopbackRoute(); err != nil {
		return err
	}

	if err := cfg.addInpodMarkIPRule(); err != nil {
		return err
	}

	log.Debug("Adding iptables rules")
	if err := cfg.executeCommands(log, builder); err != nil {
		log.Errorf("failed to restore iptables rules: %v", err)
		return err
	}

	return nil
}

func (cfg *IptablesConfigurator) AppendInpodRules(podOverrides PodLevelOverrides) *builder.IptablesRuleBuilder {
	var redirectDNS bool

	switch podOverrides.DNSProxy {
	case PodDNSUnset:
		redirectDNS = cfg.cfg.RedirectDNS
	case PodDNSEnabled:
		redirectDNS = true
	case PodDNSDisabled:
		redirectDNS = false
	}

	inpodMark := fmt.Sprintf("0x%x", InpodMark) + "/" + fmt.Sprintf("0x%x", InpodMask)
	inpodTproxyMark := fmt.Sprintf("0x%x", InpodTProxyMark) + "/" + fmt.Sprintf("0x%x", InpodTProxyMask)

	iptablesBuilder := builder.NewIptablesRuleBuilder(ipbuildConfig(cfg.cfg))

	// Insert jumps to our custom chains
	// This is mostly just for visual tidiness and cleanup, as we can delete the secondary chains and jumps
	// without polluting the main table too much.

	// -t mangle -A PREROUTING -j ISTIO_PRERT
	iptablesBuilder.AppendRule(
		"PREROUTING", "mangle",
		"-j", ChainInpodPrerouting,
	)

	// -t mangle -A OUTPUT -p tcp -j ISTIO_OUTPUT
	iptablesBuilder.AppendRule(
		"OUTPUT", "mangle",
		"-j", ChainInpodOutput,
	)

	// -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT
	iptablesBuilder.AppendRule(
		"OUTPUT", "nat",
		"-j", ChainInpodOutput,
	)

	if redirectDNS {
		iptablesBuilder.AppendRule(
			"PREROUTING", "raw",
			"-j", ChainInpodPrerouting,
		)
		iptablesBuilder.AppendRule(
			"OUTPUT", "raw",
			"-j", ChainInpodOutput,
		)
	}

	// -t nat -A PREROUTING -p tcp -j ISTIO_PRERT
	iptablesBuilder.AppendRule(
		"PREROUTING", "nat",
		"-j", ChainInpodPrerouting,
	)

	// From here on, we should be only inserting rules into our custom chains.

	// To keep things manageable, the first rules in the ISTIO_PRERT chain should be short-circuits, like
	// virtual interface exclusions/redirects:
	if len(podOverrides.VirtualInterfaces) != 0 {
		for _, virtInterface := range podOverrides.VirtualInterfaces {
			// CLI: -t nat -A ISTIO_PRERT -i virt0 -p tcp -j REDIRECT --to-ports 15001
			//
			// DESC: For any configured virtual interfaces, treat inbound as outbound traffic (useful for kubeVirt, VMs, DinD, etc)
			// and just shunt it directly to the outbound port of the proxy.
			// Note that for now this explicitly excludes UDP traffic, as we can't proxy arbitrary UDP stuff,
			// and this is a difference from the old sidecar `traffic.sidecar.istio.io/kubevirtInterfaces` annotation.
			iptablesBuilder.AppendRule(ChainInpodPrerouting, "nat",
				"-i", fmt.Sprint(virtInterface),
				"-p", "tcp",
				"-j", "REDIRECT",
				"--to-ports", fmt.Sprint(ZtunnelOutboundPort),
			)
			// CLI: -t nat -A ISTIO_PRERT -i virt0 -p tcp -j RETURN
			//
			// DESC: Now that the virtual interface packet has been redirected, just stop processing and jump out of the istio PRERT chain.
			// Another difference from the sidecar kubevirt rules is that this one RETURNs from the istio chain,
			// and not the top-level PREROUTING table like the kubevirt rule does.
			// Returning from the top-level PREROUTING table would skip other people's PRERT rules unconditionally,
			// which is unsafe (and should not be needed anyway) - if we really find ourselves needing to do that, we should ACCEPT inside our chain instead.
			iptablesBuilder.AppendRule(ChainInpodPrerouting, "nat",
				"-i", fmt.Sprint(virtInterface),
				"-p", "tcp",
				"-j", "RETURN",
			)
		}
	}

	if !podOverrides.IngressMode {
		// CLI: -A ISTIO_PRERT -m mark --mark 0x539/0xfff -j CONNMARK --set-xmark 0x111/0xfff
		//
		// DESC: If we have a packet mark, set a connmark.
		iptablesBuilder.AppendRule(ChainInpodPrerouting, "mangle", "-m", "mark",
			"--mark", inpodMark,
			"-j", "CONNMARK",
			"--set-xmark", inpodTproxyMark)

		// Handle healthcheck probes from the host node. In the host netns, before the packet enters the pod, we SNAT
		// the healthcheck packet to a fixed IP if the packet is coming from a node-local process with a socket.
		//
		// We do this so we can exempt this traffic from ztunnel capture/proxy - otherwise both kube-proxy (legit)
		// and kubelet (skippable) traffic would have the same srcip once they got to the pod, and would be indistinguishable.

		// CLI: -t mangle -A ISTIO_PRERT -s 169.254.7.127 -p tcp -m tcp --dport <PROBEPORT> -j ACCEPT
		// CLI: -t mangle -A ISTIO_PRERT -s fd16:9254:7127:1337:ffff:ffff:ffff:ffff -p tcp -m tcp --dport <PROBEPORT> -j ACCEPT
		//
		// DESC: If this is one of our node-probe ports and is from our SNAT-ed/"special" hostside IP, short-circuit out here
		iptablesBuilder.AppendVersionedRule(cfg.cfg.HostProbeSNATAddress.String(), cfg.cfg.HostProbeV6SNATAddress.String(),
			ChainInpodPrerouting, "nat",
			"-s", iptablesconstants.IPVersionSpecific,
			"-p", "tcp",
			"-m", "tcp",
			"-j", "ACCEPT",
		)
	}

	// CLI: -t NAT -A ISTIO_OUTPUT -d 169.254.7.127 -p tcp -m tcp -j ACCEPT
	// CLI: -t NAT -A ISTIO_OUTPUT -d fd16:9254:7127:1337:ffff:ffff:ffff:ffff -p tcp -m tcp -j ACCEPT
	//
	// DESC: Anything coming BACK from the pod healthcheck port with a dest of our SNAT-ed hostside IP
	// we also short-circuit.
	iptablesBuilder.AppendVersionedRule(cfg.cfg.HostProbeSNATAddress.String(), cfg.cfg.HostProbeV6SNATAddress.String(),
		ChainInpodOutput, "nat",
		"-d", iptablesconstants.IPVersionSpecific,
		"-p", "tcp",
		"-m", "tcp",
		"-j", "ACCEPT",
	)

	if !podOverrides.IngressMode {
		// CLI: -A ISTIO_PRERT ! -d 127.0.0.1/32 -p tcp ! --dport 15008 -m mark ! --mark 0x539/0xfff -j REDIRECT --to-ports <INPLAINPORT>
		//
		// DESC: Anything that is not bound for localhost and does not have the mark, REDIRECT to ztunnel inbound plaintext port <INPLAINPORT>
		// Skip 15008, which will go direct without redirect needed.
		iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
			ChainInpodPrerouting, "nat",
			"!", "-d", iptablesconstants.IPVersionSpecific,
			"-p", "tcp",
			"!", "--dport", fmt.Sprint(ZtunnelInboundPort),
			"-m", "mark", "!",
			"--mark", inpodMark,
			"-j", "REDIRECT",
			"--to-ports", fmt.Sprint(ZtunnelInboundPlaintextPort),
		)
	}

	// CLI: -A ISTIO_OUTPUT -m connmark --mark 0x111/0xfff -j CONNMARK --restore-mark --nfmask 0xffffffff --ctmask 0xffffffff
	//
	// DESC: Propagate/restore connmark (if we had one) for outbound
	iptablesBuilder.AppendRule(
		ChainInpodOutput, "mangle",
		"-m", "connmark",
		"--mark", inpodTproxyMark,
		"-j", "CONNMARK",
		"--restore-mark",
		"--nfmask", fmt.Sprintf("0x%x", InpodRestoreMask),
		"--ctmask", fmt.Sprintf("0x%x", InpodRestoreMask),
	)

	if redirectDNS {
		// CLI: -A ISTIO_OUTPUT ! -o lo -p udp -m udp --dport 53 -j REDIRECT --to-port 15053
		//
		// DESC: If this is a UDP DNS request to a non-localhost resolver, send it to ztunnel DNS proxy port
		iptablesBuilder.AppendRule(
			ChainInpodOutput, "nat",
			"!", "-o", "lo",
			"-p", "udp",
			"-m", "mark", "!",
			"--mark", inpodMark,
			"-m", "udp",
			"--dport", "53",
			"-j", "REDIRECT",
			"--to-port", fmt.Sprintf("%d", DNSCapturePort),
		)
		// Same as above for TCP
		iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
			ChainInpodOutput, "nat",
			"!", "-d", iptablesconstants.IPVersionSpecific,
			"-p", "tcp",
			"--dport", "53",
			"-m", "mark", "!",
			"--mark", inpodMark,
			"-j", "REDIRECT",
			"--to-ports", fmt.Sprintf("%d", DNSCapturePort),
		)

		// Assign packets between the proxy and upstream DNS servers to their own conntrack zones to avoid issues in port collision
		// See https://github.com/istio/istio/issues/33469
		// Proxy --> Upstream
		iptablesBuilder.AppendRule(
			ChainInpodOutput, "raw",
			"-p", "udp",
			// Proxy will mark outgoing packets
			"-m", "mark",
			"--mark", inpodMark,
			"-m", "udp",
			"--dport", "53",
			"-j", "CT",
			"--zone", "1",
		)
		// Upstream --> Proxy return packets
		iptablesBuilder.AppendRule(
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

	// CLI: -A ISTIO_OUTPUT -p tcp -m mark --mark 0x111/0xfff -j ACCEPT
	//
	// DESC: If this is outbound and has our mark, let it go.
	iptablesBuilder.AppendRule(
		ChainInpodOutput, "nat",
		"-p", "tcp",
		"-m", "mark",
		"--mark", inpodTproxyMark,
		"-j", "ACCEPT",
	)

	// Do not redirect app calls to back itself via Ztunnel when using the endpoint address
	// e.g. appN => appN by lo
	iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
		ChainInpodOutput, "nat",
		"!", "-d", iptablesconstants.IPVersionSpecific,
		"-o", "lo",
		"-j", "ACCEPT",
	)
	// CLI: -A ISTIO_OUTPUT ! -d 127.0.0.1/32 -p tcp -m mark ! --mark 0x539/0xfff -j REDIRECT --to-ports <OUTPORT>
	//
	// DESC: If this is outbound, not bound for localhost, and does not have our packet mark, redirect to ztunnel proxy <OUTPORT>
	iptablesBuilder.AppendVersionedRule("127.0.0.1/32", "::1/128",
		ChainInpodOutput, "nat",
		"!", "-d", iptablesconstants.IPVersionSpecific,
		"-p", "tcp",
		"-m", "mark", "!",
		"--mark", inpodMark,
		"-j", "REDIRECT",
		"--to-ports", fmt.Sprintf("%d", ZtunnelOutboundPort),
	)
	return iptablesBuilder
}

func (cfg *IptablesConfigurator) executeCommands(log *istiolog.Scope, iptablesBuilder *builder.IptablesRuleBuilder) error {
	var execErrs []error
	guardrails := false
	defer func() {
		if guardrails {
			log.Info("Removing guardrails")
			guardrailsCleanup := iptablesBuilder.BuildCleanupGuardrails()
			_ = cfg.executeIptablesCommands(log, &cfg.iptV, guardrailsCleanup)
			if cfg.cfg.EnableIPv6 {
				_ = cfg.executeIptablesCommands(log, &cfg.ipt6V, guardrailsCleanup)
			}
		}
	}()
	residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, cfg.ext, iptablesBuilder, &cfg.iptV, &cfg.ipt6V)
	if residueExists && deltaExists && !cfg.cfg.Reconcile {
		log.Warn("reconcile is needed but no-reconcile flag is set. Unexpected behavior may occur due to preexisting iptables rules")
	}
	// Cleanup Step
	if (residueExists && deltaExists && cfg.cfg.Reconcile) || cfg.cfg.CleanupOnly {
		// Apply safety guardrails
		if !cfg.cfg.CleanupOnly {
			log.Info("Setting up guardrails")
			guardrailsCleanup := iptablesBuilder.BuildCleanupGuardrails()
			guardrailsRules := iptablesBuilder.BuildGuardrails()
			iptVersions := []dep.IptablesVersion{cfg.iptV}
			if cfg.cfg.EnableIPv6 {
				iptVersions = append(iptVersions, cfg.ipt6V)
			}
			for _, ver := range iptVersions {
				cfg.tryExecuteIptablesCommands(log, &ver, guardrailsCleanup)
				if err := cfg.executeIptablesCommands(log, &ver, guardrailsRules); err != nil {
					return err
				}
				guardrails = true
			}
		}
		// Remove old iptables
		log.Info("Performing cleanup of existing iptables")
		cfg.tryExecuteIptablesCommands(log, &cfg.iptV, iptablesBuilder.BuildCleanupV4())
		if cfg.cfg.EnableIPv6 {
			cfg.tryExecuteIptablesCommands(log, &cfg.ipt6V, iptablesBuilder.BuildCleanupV6())
		}

		// Remove leftovers from non-matching istio iptables cfg
		if cfg.cfg.Reconcile {
			log.Info("Performing cleanup of any unexpected leftovers from previous iptables executions")
			cfg.cleanupIstioLeftovers(log, cfg.ext, iptablesBuilder, &cfg.iptV, &cfg.ipt6V)
		}
	}

	// Apply Step
	if (deltaExists || cfg.cfg.ForceApply) && !cfg.cfg.CleanupOnly {
		log.Info("Applying iptables chains and rules")
		// Execute iptables-restore
		execErrs = append(execErrs, cfg.executeIptablesRestoreCommand(log, iptablesBuilder.BuildV4Restore(), &cfg.iptV))
		// Execute ip6tables-restore
		if cfg.cfg.EnableIPv6 {
			execErrs = append(execErrs, cfg.executeIptablesRestoreCommand(log, iptablesBuilder.BuildV6Restore(), &cfg.ipt6V))
		}
	}
	return errors.Join(execErrs...)
}

func (cfg *IptablesConfigurator) cleanupIstioLeftovers(log *istiolog.Scope, ext dep.Dependencies, ruleBuilder *builder.IptablesRuleBuilder,
	iptV *dep.IptablesVersion, ipt6V *dep.IptablesVersion,
) {
	for _, ipVer := range []*dep.IptablesVersion{iptV, ipt6V} {
		if ipVer.DetectedBinary == "" {
			continue
		}
		output, err := ext.Run(log, true, iptablesconstants.IPTablesSave, ipVer, nil)
		if err == nil {
			currentState := ruleBuilder.GetStateFromSave(output.String())
			leftovers := iptablescapture.HasIstioLeftovers(currentState)
			if len(leftovers) > 0 {
				log.Infof("Detected Istio iptables artifacts from a previous execution; initiating a second cleanup pass.")
				log.Debugf("Istio iptables artifacts identified for cleanup: %+v", leftovers)
				cfg.tryExecuteIptablesCommands(log, ipVer, builder.BuildCleanupFromState(leftovers))
			}
		}
	}
}

func (cfg *IptablesConfigurator) executeIptablesRestoreCommand(
	log *istiolog.Scope,
	data string,
	iptVer *dep.IptablesVersion,
) error {
	cmd := iptablesconstants.IPTablesRestore
	log.Infof("Running %s with the following input:\n%v", iptVer.CmdToString(cmd), strings.TrimSpace(data))
	// --noflush to prevent flushing/deleting previous contents from table
	_, err := cfg.ext.Run(log, false, cmd, iptVer, strings.NewReader(data), "--noflush", "-v")
	return err
}

func (cfg *IptablesConfigurator) executeIptablesCommands(log *istiolog.Scope, iptVer *dep.IptablesVersion, args [][]string) error {
	var iptErrs []error
	// TODO: pass log all the way through
	for _, argSet := range args {
		if _, err := cfg.ext.Run(log, false, iptablesconstants.IPTables, iptVer, nil, argSet...); err != nil {
			iptErrs = append(iptErrs, err)
		}
	}
	return errors.Join(iptErrs...)
}

func (cfg *IptablesConfigurator) tryExecuteIptablesCommands(log *istiolog.Scope, iptVer *dep.IptablesVersion, commands [][]string) {
	for _, cmd := range commands {
		_, _ = cfg.ext.Run(log, true, iptablesconstants.IPTables, iptVer, nil, cmd...)
	}
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
func (cfg *IptablesConfigurator) CreateHostRulesForHealthChecks() error {
	log.Info("configuring host-level iptables rules (healthchecks, etc)")
	// Append our rules here
	builder := cfg.AppendHostRules()

	log.Info("Adding host netnamespace iptables rules")

	return util.RunAsHost(func() error {
		if err := cfg.executeCommands(log.WithLabels("component", "host"), builder); err != nil {
			log.Errorf("failed to add host netnamespace iptables rules: %v", err)
			return err
		}
		return nil
	})
}

func (cfg *IptablesConfigurator) DeleteHostRules() {
	log.Debug("Attempting to delete hostside iptables rules (if they exist)")
	builder := cfg.AppendHostRules()
	runCommands := func(cmds [][]string, version *dep.IptablesVersion) {
		for _, cmd := range cmds {
			// Ignore errors, as it is expected to fail in cases where the node is already cleaned up.
			_, _ = cfg.ext.Run(log.WithLabels("component", "host"), true, iptablesconstants.IPTables, version, nil, cmd...)
		}
	}

	err := util.RunAsHost(func() error {
		runCommands(builder.BuildCleanupV4(), &cfg.iptV)

		if cfg.cfg.EnableIPv6 {
			runCommands(builder.BuildCleanupV6(), &cfg.ipt6V)
		}
		return nil
	})
	if err != nil {
		log.Errorf("Can't switch to host namespace: %v", err)
	}
}

func (cfg *IptablesConfigurator) AppendHostRules() *builder.IptablesRuleBuilder {
	iptablesBuilder := builder.NewIptablesRuleBuilder(ipbuildConfig(cfg.cfg))

	// For easier cleanup, insert a jump into an owned chain
	// -I POSTROUTING 1 -p tcp -j ISTIO_POSTRT
	iptablesBuilder.InsertRule(
		"POSTROUTING", "nat", 1,
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
	iptablesBuilder.AppendRuleV4(
		ChainHostPostrouting, "nat",
		"-m", "owner",
		"--socket-exists",
		"-p", "tcp",
		"-m", "set",
		"--match-set", fmt.Sprintf(ipset.V4Name, ProbeIPSet),
		"dst",
		"-j", "SNAT",
		"--to-source", cfg.cfg.HostProbeSNATAddress.String(),
	)

	// For V6 we have to use a different set and a different SNAT IP
	if cfg.cfg.EnableIPv6 {
		iptablesBuilder.AppendRuleV6(
			ChainHostPostrouting, "nat",
			"-m", "owner",
			"--socket-exists",
			"-p", "tcp",
			"-m", "set",
			"--match-set", fmt.Sprintf(ipset.V6Name, ProbeIPSet),
			"dst",
			"-j", "SNAT",
			"--to-source", cfg.cfg.HostProbeV6SNATAddress.String(),
		)
	}

	return iptablesBuilder
}

// ReconcileModeEnable returns true if this particular iptables configurator
// supports/has idempotent execution enabled - i.e. reconcile mode.
func (cfg *IptablesConfigurator) ReconcileModeEnabled() bool {
	return cfg.cfg.Reconcile
}
