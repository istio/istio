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

package nftables

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/knftables"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/cni/pkg/util"
	istiolog "istio.io/istio/pkg/log"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	"istio.io/istio/tools/istio-nftables/pkg/builder"
)

const (
	AmbientNatTable    = "istio-ambient-nat"
	AmbientMangleTable = "istio-ambient-mangle"
	AmbientRawTable    = "istio-ambient-raw"

	// Base chains.
	PreroutingChain  = "prerouting"
	PostroutingChain = "postrouting"
	OutputChain      = "output"

	// Regular chains prefixed with "istio" to distinguish them from base chains
	IstioOutputChain     = "istio-output"
	IstioPreroutingChain = "istio-prerouting"

	Counter = "counter"
)

var log = scopes.CNIAgent

type NftProviderFunc func(family knftables.Family, table string) (builder.NftablesAPI, error)

var nftProviderVar NftProviderFunc

// NftablesConfigurator handles nftables rule management for Ambient mode
type NftablesConfigurator struct {
	nlDeps      iptables.NetlinkDependencies
	cfg         *config.AmbientConfig
	ruleBuilder *builder.NftablesRuleBuilder
	nftProvider NftProviderFunc
}

// NewNftablesConfigurator creates both host and pod nftables configurators
func NewNftablesConfigurator(
	hostCfg *config.AmbientConfig,
	podCfg *config.AmbientConfig,
	hostDeps dep.Dependencies,
	podDeps dep.Dependencies,
	nlDeps iptables.NetlinkDependencies,
) (*NftablesConfigurator, *NftablesConfigurator, error) {
	if hostCfg == nil {
		hostCfg = &config.AmbientConfig{}
	}
	if podCfg == nil {
		podCfg = &config.AmbientConfig{}
	}

	// Use the real implementation when the package level variable is not set (i.e., during unit tests)
	nftProvider := nftProviderVar
	if nftProvider == nil {
		nftProvider = func(family knftables.Family, table string) (builder.NftablesAPI, error) {
			return builder.NewNftImpl(family, table)
		}
	}

	hostConfigurator := &NftablesConfigurator{
		nlDeps:      nlDeps,
		cfg:         hostCfg,
		nftProvider: nftProvider,
	}

	podConfigurator := &NftablesConfigurator{
		nlDeps:      nlDeps,
		cfg:         podCfg,
		nftProvider: nftProvider,
	}

	return hostConfigurator, podConfigurator, nil
}

// CreateInpodRules creates nftables rules within a pod's network namespace
func (cfg *NftablesConfigurator) CreateInpodRules(log *istiolog.Scope, podOverrides config.PodLevelOverrides) error {
	log.Info("native nftables enabled, using nft rules for inpod traffic redirection")

	rules, err := cfg.AppendInpodRules(podOverrides)
	if err != nil {
		return err
	}
	builder.LogNftRules(rules)

	// Add loopback routes
	if err := cfg.addLoopbackRoute(); err != nil {
		return err
	}

	// Add inpod mark IP rule
	if err := cfg.addInpodMarkIPRule(); err != nil {
		return err
	}

	log.Info("nftables inpod rules creation completed")
	return nil
}

func (cfg *NftablesConfigurator) AppendInpodRules(podOverrides config.PodLevelOverrides) (*knftables.Transaction, error) {
	cfg.ruleBuilder = builder.NewNftablesRuleBuilder(config.GetConfig(cfg.cfg))

	var redirectDNS bool

	switch podOverrides.DNSProxy {
	case config.PodDNSUnset:
		redirectDNS = cfg.cfg.RedirectDNS
	case config.PodDNSEnabled:
		redirectDNS = true
	case config.PodDNSDisabled:
		redirectDNS = false
	}

	cfg.ruleBuilder.AppendRule(
		PreroutingChain, AmbientMangleTable,
		"jump", IstioPreroutingChain,
	)

	cfg.ruleBuilder.AppendRule(
		OutputChain, AmbientMangleTable,
		"jump", IstioOutputChain,
	)

	cfg.ruleBuilder.AppendRule(
		OutputChain, AmbientNatTable,
		"jump", IstioOutputChain,
	)

	if redirectDNS {
		cfg.ruleBuilder.AppendRule(
			PreroutingChain, AmbientRawTable,
			"jump", IstioPreroutingChain,
		)
		cfg.ruleBuilder.AppendRule(
			OutputChain, AmbientRawTable,
			"jump", IstioOutputChain,
		)
	}

	cfg.ruleBuilder.AppendRule(
		PreroutingChain, AmbientNatTable,
		"jump", IstioPreroutingChain,
	)

	// To keep things manageable, the first rules in the istio-prerouting chain should be short-circuits, like
	// virtual interface exclusions/redirects:
	if len(podOverrides.VirtualInterfaces) != 0 {
		for _, virtInterface := range podOverrides.VirtualInterfaces {
			// DESC: For any configured virtual interfaces, treat inbound as outbound traffic (useful for kubeVirt, VMs, DinD, etc)
			// and just shunt it directly to the outbound port of the proxy.
			// Note that for now this explicitly excludes UDP traffic, as we can't proxy arbitrary UDP stuff,
			// and this is a difference from the old sidecar `traffic.sidecar.istio.io/kubevirtInterfaces` annotation.
			cfg.ruleBuilder.AppendRule(IstioPreroutingChain, AmbientNatTable,
				"iifname", fmt.Sprint(virtInterface),
				"meta l4proto tcp", Counter,
				"redirect to", ":"+fmt.Sprint(config.ZtunnelOutboundPort),
			)

			cfg.ruleBuilder.AppendRule(IstioPreroutingChain, AmbientNatTable,
				"iifname", fmt.Sprint(virtInterface),
				"meta l4proto tcp", Counter,
				"return",
			)
		}
	}

	if !podOverrides.IngressMode {
		// CLI: nft add rule inet istio-ambient-mangle istio-prerouting meta mark & 0xfff == 0x539 counter ct mark set ct mark & 0xfffff000 | 0x111
		// DESC: If we have a packet mark, set a connmark.
		cfg.ruleBuilder.AppendRule(IstioPreroutingChain, AmbientMangleTable,
			"meta mark & 0xfff ==",
			fmt.Sprintf("0x%x", config.InpodMark), Counter, "ct mark set ct mark & 0xfffff000 | ",
			fmt.Sprintf("0x%x", config.InpodTProxyMark))

		// Handle healthcheck probes from the host node. In the host netns, before the packet enters the pod, we SNAT
		// the healthcheck packet to a fixed IP if the packet is coming from a node-local process with a socket.
		//
		// We do this so we can exempt this traffic from ztunnel capture/proxy - otherwise both kube-proxy (legit)
		// and kubelet (skippable) traffic would have the same srcip once they got to the pod, and would be indistinguishable.

		// CLI: nft add rule inet istio-ambient-nat istio-prerouting meta l4proto tcp ip saddr 169.254.7.127 counter accept
		//
		// DESC: If this is one of our node-probe ports and is from our SNAT-ed/"special" hostside IP, short-circuit out here
		cfg.ruleBuilder.AppendRule(IstioPreroutingChain, AmbientNatTable,
			"meta l4proto tcp",
			"ip saddr", cfg.cfg.HostProbeSNATAddress.String(), Counter,
			"accept",
		)

		// CLI: nft add rule inet istio-ambient-nat istio-prerouting meta l4proto tcp ip6 saddr
		// fd16:9254:7127:1337:ffff:ffff:ffff:ffff counter accept
		cfg.ruleBuilder.AppendRule(IstioPreroutingChain, AmbientNatTable,
			"meta l4proto tcp",
			"ip6 saddr", cfg.cfg.HostProbeV6SNATAddress.String(), Counter,
			"accept",
		)
	}

	// CLI: nft add rule inet istio-ambient-nat istio-output meta l4proto tcp ip daddr 169.254.7.127 counter accept
	// DESC: Anything coming BACK from the pod healthcheck port with a dest of our SNAT-ed hostside IP we also short-circuit.
	cfg.ruleBuilder.AppendRule(IstioOutputChain, AmbientNatTable,
		"meta l4proto tcp",
		"ip daddr", cfg.cfg.HostProbeSNATAddress.String(), Counter,
		"accept",
	)

	// CLI: nft add rule inet istio-ambient-nat istio-output meta l4proto tcp ip6 daddr fd16:9254:7127:1337:ffff:ffff:ffff:ffff counter accept
	cfg.ruleBuilder.AppendRule(IstioOutputChain, AmbientNatTable,
		"meta l4proto tcp",
		"ip6 daddr", cfg.cfg.HostProbeV6SNATAddress.String(), Counter,
		"accept",
	)

	if !podOverrides.IngressMode {
		// CLI: nft add rule inet istio-ambient-nat istio-prerouting ip daddr != 127.0.0.1 tcp dport != 15008 mark and 0xfff != 0x539
		// counter redirect to :15006
		//
		// DESC: Anything that is not bound for localhost and does not have the mark, REDIRECT to ztunnel inbound plaintext port <INPLAINPORT>
		// Skip 15008, which will go direct without redirect needed.
		cfg.ruleBuilder.AppendRule(IstioPreroutingChain, AmbientNatTable,
			"ip daddr", "!=", "127.0.0.1/32",
			"tcp dport", "!=", fmt.Sprint(config.ZtunnelInboundPort),
			"mark and 0xfff ", "!=", fmt.Sprintf("0x%x", config.InpodMark), Counter,
			"redirect to", ":"+fmt.Sprint(config.ZtunnelInboundPlaintextPort),
		)

		cfg.ruleBuilder.AppendRule(IstioPreroutingChain, AmbientNatTable,
			"ip6 daddr", "!=", "::1/128",
			"tcp dport", "!=", fmt.Sprint(config.ZtunnelInboundPort),
			"mark and 0xfff", "!=", fmt.Sprintf("0x%x", config.InpodMark), Counter,
			"redirect to", ":"+fmt.Sprint(config.ZtunnelInboundPlaintextPort),
		)
	}

	// CLI: nft add rule inet istio-ambient-mangle istio-output ct mark and 0xfff == 0x111 counter meta mark set ct mark
	//
	// DESC: Propagate/restore connmark (if we had one) for outbound
	cfg.ruleBuilder.AppendRule(
		IstioOutputChain, AmbientMangleTable,
		"ct mark and", fmt.Sprintf("0x%x", config.InpodTProxyMask),
		"==", fmt.Sprintf("0x%x", config.InpodTProxyMark), Counter,
		"meta mark set ct mark",
	)

	if redirectDNS {
		// CLI: nft add rule inet istio-ambient-nat istio-output oifname != "lo" mark and 0xfff != 0x539 udp dport 53 counter redirect to :15053
		//
		// DESC: If this is a UDP DNS request to a non-localhost resolver, send it to ztunnel DNS proxy port
		cfg.ruleBuilder.AppendRule(
			IstioOutputChain, AmbientNatTable,
			"oifname", "!=", "lo",
			"mark and", fmt.Sprintf("0x%x", config.InpodMask),
			"!=", fmt.Sprintf("0x%x", config.InpodMark),
			"udp dport", "53", Counter,
			"redirect to", ":"+fmt.Sprintf("%d", config.DNSCapturePort),
		)

		// CLI: nft add rule inet istio-ambient-nat istio-output ip daddr != 127.0.0.1/32 tcp dport 53 mark and 0xfff != 0x539 counter redirect to :15053
		cfg.ruleBuilder.AppendRule(
			IstioOutputChain, AmbientNatTable,
			"ip daddr", "!=", "127.0.0.1/32",
			"tcp dport", "53",
			"mark and", fmt.Sprintf("0x%x", config.InpodMask),
			"!=", fmt.Sprintf("0x%x", config.InpodMark), Counter,
			"redirect to", ":"+fmt.Sprintf("%d", config.DNSCapturePort),
		)

		// CLI: nft add rule inet istio-ambient-nat istio-output ip6 daddr != ::1/128 tcp dport 53 mark and 0xfff != 0x539 counter redirect to :15053
		cfg.ruleBuilder.AppendRule(
			IstioOutputChain, AmbientNatTable,
			"ip6 daddr", "!=", "::1/128",
			"tcp dport", "53",
			"mark and", fmt.Sprintf("0x%x", config.InpodMask),
			"!=", fmt.Sprintf("0x%x", config.InpodMark), Counter,
			"redirect to", ":"+fmt.Sprintf("%d", config.DNSCapturePort),
		)

		// Assign packets between the proxy and upstream DNS servers to their own conntrack zones to avoid issues in port collision
		// See https://github.com/istio/istio/issues/33469
		// CLI: nft add rule inet istio-ambient-raw istio-output udp dport 53 meta mark and 0xfff == 0x539 counter ct zone set 1
		// Proxy --> Upstream
		cfg.ruleBuilder.AppendRule(
			IstioOutputChain, AmbientRawTable,
			"udp dport", "53",
			"meta mark and", fmt.Sprintf("0x%x", config.InpodMask),
			"==", fmt.Sprintf("0x%x", config.InpodMark), Counter,
			"ct zone set", "1",
		)

		// CLI: nft add rule inet istio-ambient-raw istio-prerouting udp sport 53 meta mark and 0xfff != 0x539 counter ct zone set 1
		// Upstream --> Proxy return packets
		cfg.ruleBuilder.AppendRule(
			IstioPreroutingChain, AmbientRawTable,
			"udp sport", "53",
			"meta mark and", fmt.Sprintf("0x%x", config.InpodMask),
			"!=", fmt.Sprintf("0x%x", config.InpodMark), Counter,
			"ct zone set", "1",
		)
	}

	// CLI: nft add rule inet istio-ambient-nat istio-output meta l4proto tcp mark and 0xfff == 0x111 counter accept
	//
	// DESC: If this is outbound and has our mark, let it go.
	cfg.ruleBuilder.AppendRule(
		IstioOutputChain, AmbientNatTable,
		"meta l4proto tcp",
		"mark and", fmt.Sprintf("0x%x", config.InpodTProxyMask),
		"==", fmt.Sprintf("0x%x", config.InpodTProxyMark), Counter,
		"accept",
	)

	// Do not redirect app calls to back itself via Ztunnel when using the endpoint address
	// e.g. appN => appN by lo
	// CLI: nft add rule inet istio-ambient-nat istio-output oifname "lo" ip daddr != 127.0.0.1 counter accept
	cfg.ruleBuilder.AppendRule(
		IstioOutputChain, AmbientNatTable,
		"oifname", "lo",
		"ip daddr",
		"!=", "127.0.0.1/32", Counter,
		"accept",
	)

	// CLI: nft add rule inet istio-ambient-nat istio-output oifname "lo" ip6 daddr != ::1/128 counter accept
	cfg.ruleBuilder.AppendRule(
		IstioOutputChain, AmbientNatTable,
		"oifname", "lo",
		"ip6 daddr",
		"!=", "::1/128", Counter,
		"accept",
	)

	// CLI: nft add rule inet istio-ambient-nat istio-output meta l4proto tcp ip daddr != 127.0.0.1 mark and 0xfff != 0x539 counter redirect to :15001
	//
	// DESC: If this is outbound, not bound for localhost, and does not have our packet mark, redirect to ztunnel proxy <OUTPORT>
	cfg.ruleBuilder.AppendRule(
		IstioOutputChain, AmbientNatTable,
		"meta l4proto tcp",
		"ip daddr",
		"!=", "127.0.0.1/32",
		"mark and", fmt.Sprintf("0x%x", config.InpodMask),
		"!=", fmt.Sprintf("0x%x", config.InpodMark), Counter,
		"redirect to", ":"+fmt.Sprintf("%d", config.ZtunnelOutboundPort),
	)

	cfg.ruleBuilder.AppendRule(
		IstioOutputChain, AmbientNatTable,
		"meta l4proto tcp",
		"ip6 daddr",
		"!=", "::1/128",
		"mark and", fmt.Sprintf("0x%x", config.InpodMask),
		"!=", fmt.Sprintf("0x%x", config.InpodMark), Counter,
		"redirect to", ":"+fmt.Sprintf("%d", config.ZtunnelOutboundPort),
	)

	return cfg.executeCommands()
}

// DeleteInpodRules removes nftables rules from a pod's network namespace
func (cfg *NftablesConfigurator) DeleteInpodRules(log *istiolog.Scope) error {
	log.Info("removing nftables inpod rules")

	nft, err := cfg.nftProvider("", "")
	if err != nil {
		return err
	}

	tx := nft.NewTransaction()
	// nftables delete command will delete the table along with the chains and rules.
	tx.Delete(&knftables.Table{Name: AmbientNatTable, Family: knftables.InetFamily})
	tx.Delete(&knftables.Table{Name: AmbientMangleTable, Family: knftables.InetFamily})
	tx.Delete(&knftables.Table{Name: AmbientRawTable, Family: knftables.InetFamily})

	if tx.NumOperations() > 0 {
		if err := nft.Run(context.TODO(), tx); err != nil {
			log.Errorf("error while trying to delete the ambient nftable rules: %w", err)
		}
	}

	var inpodErrs []error
	inpodErrs = append(inpodErrs, cfg.delInpodMarkIPRule(), cfg.delLoopbackRoute())
	return errors.Join(inpodErrs...)
}

// CreateHostRulesForHealthChecks creates host-level nftables rules for health check handling
func (cfg *NftablesConfigurator) CreateHostRulesForHealthChecks() error {
	log := log.WithLabels("component", "host")
	log.Info("Adding nftable rules on the host network namespace")

	cfg.ruleBuilder = builder.NewNftablesRuleBuilder(config.GetConfig(cfg.cfg))

	// TODO: Investigate how to support "-m owner --socket-exists" with nftable rules.
	cfg.ruleBuilder.AppendRule(PostroutingChain, AmbientNatTable, "ip", "daddr", fmt.Sprintf("@%s-v4", config.ProbeIPSet),
		"meta l4proto tcp", Counter, "snat", "to", cfg.cfg.HostProbeSNATAddress.String())

	// For V6 we have to use a different set and a different SNAT IP
	cfg.ruleBuilder.AppendRule(PostroutingChain, AmbientNatTable, "ip6", "daddr", fmt.Sprintf("@%s-v6", config.ProbeIPSet),
		"meta l4proto tcp", Counter, "snat", "to", cfg.cfg.HostProbeV6SNATAddress.String())

	return util.RunAsHost(func() error {
		tx, err := cfg.executeHostCommands()
		if err != nil {
			log.Errorf("failed to program nftable rules in the host network namespace: %v", err)
			return err
		}
		builder.LogNftRules(tx)
		return nil
	})
}

// DeleteHostRules removes host-level nftables rules
func (cfg *NftablesConfigurator) DeleteHostRules() {
	log := log.WithLabels("component", "host")
	log.Debug("Attempting to delete hostside nftables rules")

	nft, err := cfg.nftProvider("", "")
	if err != nil {
		log.Errorf("error creating the nftProvider while deleting the host rules: %v", err)
		return
	}

	tx := nft.NewTransaction()

	// TODO: REVISIT: Ensure that the table exists, so that delete does not return any error.
	tx.Add(&knftables.Table{Name: AmbientNatTable, Family: knftables.InetFamily})

	// nftables delete command will delete the table along with the sets, chains and rules.
	tx.Flush(&knftables.Table{Name: AmbientNatTable, Family: knftables.InetFamily})

	err = util.RunAsHost(func() error {
		if err := nft.Run(context.TODO(), tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Errorf("error while trying to delete the ambient nftable rules from the host network: %w", err)
		return
	}

	log.Debug("hostside nftables rules successfully deleted")
}

// ReconcileModeEnabled returns true if reconciliation mode is enabled
func (cfg *NftablesConfigurator) ReconcileModeEnabled() bool {
	return cfg.cfg.Reconcile
}

// Helper functions for route and rule management (delegated to netlink dependencies)
func (cfg *NftablesConfigurator) addLoopbackRoute() error {
	return cfg.nlDeps.AddLoopbackRoutes(cfg.cfg)
}

func (cfg *NftablesConfigurator) delLoopbackRoute() error {
	return cfg.nlDeps.DelLoopbackRoutes(cfg.cfg)
}

func (cfg *NftablesConfigurator) addInpodMarkIPRule() error {
	return cfg.nlDeps.AddInpodMarkIPRule(cfg.cfg)
}

func (cfg *NftablesConfigurator) delInpodMarkIPRule() error {
	return cfg.nlDeps.DelInpodMarkIPRule(cfg.cfg)
}

// executeCommands creates a transaction including all needed modifications and runs it.
func (cfg *NftablesConfigurator) executeCommands() (*knftables.Transaction, error) {
	nft, err := cfg.nftProvider("", "")
	if err != nil {
		return nil, err
	}

	tx := nft.NewTransaction()
	tx = cfg.addIstioNatTableRules(tx)
	tx = cfg.addIstioMangleTableRules(tx)
	tx = cfg.addIstioRawTableRules(tx)

	// If there are any transactions to apply, run them in a batch.
	if tx.NumOperations() > 0 {
		if err := nft.Run(context.TODO(), tx); err != nil {
			return tx, fmt.Errorf("nftables run failed: %w", err)
		}
	}
	return tx, nil
}

// executeCommands creates a transaction including all needed modifications and runs it.
func (cfg *NftablesConfigurator) executeHostCommands() (*knftables.Transaction, error) {
	nft, err := cfg.nftProvider("", "")
	if err != nil {
		return nil, err
	}

	tx := nft.NewTransaction()

	if len(cfg.ruleBuilder.Rules[AmbientNatTable]) == 0 {
		return tx, nil
	}

	chains := []knftables.Chain{
		{
			Name:     PostroutingChain,
			Table:    AmbientNatTable,
			Family:   knftables.InetFamily,
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.PostroutingHook),
			Priority: knftables.PtrTo(knftables.SNATPriority),
		},
	}

	rules := cfg.ruleBuilder.Rules[AmbientNatTable]
	tx = cfg.addIstioTableRules(tx, AmbientNatTable, chains, rules)
	if tx.NumOperations() > 0 {
		if err := nft.Run(context.TODO(), tx); err != nil {
			return tx, fmt.Errorf("nftables run failed: %w", err)
		}
	}
	return tx, nil
}

// addIstioNatTableRules updates a transaction to include the nftables rules for the AmbientNatTable table.
// It makes sure the table and the necessary chains exist, then adds rules from the rule builder.
func (cfg *NftablesConfigurator) addIstioNatTableRules(tx *knftables.Transaction) *knftables.Transaction {
	if len(cfg.ruleBuilder.Rules[AmbientNatTable]) == 0 {
		return tx
	}

	chains := []knftables.Chain{
		{
			Name:     PreroutingChain,
			Table:    AmbientNatTable,
			Family:   knftables.InetFamily,
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.DNATPriority),
		},
		{
			Name:     OutputChain,
			Table:    AmbientNatTable,
			Family:   knftables.InetFamily,
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.DNATPriority),
		},
		{Name: IstioPreroutingChain, Table: AmbientNatTable, Family: knftables.InetFamily},
		{Name: IstioOutputChain, Table: AmbientNatTable, Family: knftables.InetFamily},
	}

	rules := cfg.ruleBuilder.Rules[AmbientNatTable]

	return cfg.addIstioTableRules(tx, AmbientNatTable, chains, rules)
}

// addIstioMangleTableRules updates a transaction to include the nftables rules for the AmbientMangleTable table.
// It makes sure the table and the necessary chains exist, then adds rules from the rule builder.
func (cfg *NftablesConfigurator) addIstioMangleTableRules(tx *knftables.Transaction) *knftables.Transaction {
	if len(cfg.ruleBuilder.Rules[AmbientMangleTable]) == 0 {
		return tx
	}

	chains := []knftables.Chain{
		{
			Name:     PreroutingChain,
			Table:    AmbientMangleTable,
			Family:   knftables.InetFamily,
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     OutputChain,
			Table:    AmbientMangleTable,
			Family:   knftables.InetFamily,
			Type:     knftables.PtrTo(knftables.RouteType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{Name: IstioPreroutingChain, Table: AmbientMangleTable, Family: knftables.InetFamily},
		{Name: IstioOutputChain, Table: AmbientMangleTable, Family: knftables.InetFamily},
	}

	rules := cfg.ruleBuilder.Rules[AmbientMangleTable]

	return cfg.addIstioTableRules(tx, AmbientMangleTable, chains, rules)
}

// addIstioRawTableRules updates a transaction to include the nftables rules for the AmbientRawTable table.
// It makes sure the table and the necessary chains exist, then adds rules from the rule builder.
func (cfg *NftablesConfigurator) addIstioRawTableRules(tx *knftables.Transaction) *knftables.Transaction {
	if len(cfg.ruleBuilder.Rules[AmbientRawTable]) == 0 {
		return tx
	}

	chains := []knftables.Chain{
		{
			Name:     PreroutingChain,
			Table:    AmbientRawTable,
			Family:   knftables.InetFamily,
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.RawPriority),
		},
		{
			Name:     OutputChain,
			Table:    AmbientRawTable,
			Family:   knftables.InetFamily,
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.RawPriority),
		},
		{Name: IstioPreroutingChain, Table: AmbientRawTable, Family: knftables.InetFamily},
		{Name: IstioOutputChain, Table: AmbientRawTable, Family: knftables.InetFamily},
	}

	rules := cfg.ruleBuilder.Rules[AmbientRawTable]

	return cfg.addIstioTableRules(tx, AmbientRawTable, chains, rules)
}

func (cfg *NftablesConfigurator) addIstioTableRules(
	tx *knftables.Transaction,
	tableName string,
	chains []knftables.Chain,
	rules []knftables.Rule,
) *knftables.Transaction {
	// Track how many rules have been added to each chain
	chainRuleCount := make(map[string]int)

	// Count the number of rules present in each of the chains
	for _, rule := range rules {
		chainRuleCount[rule.Chain]++
	}

	// Let's filter out the chains that have rules
	chainsWithRules := []knftables.Chain{}
	for _, chain := range chains {
		if chainRuleCount[chain.Name] > 0 {
			chainsWithRules = append(chainsWithRules, chain)
		}
	}

	// Skip creating the table itself if none of the chains have any rules
	if len(chainsWithRules) == 0 {
		return tx
	}

	// Ensure that the table exists
	tx.Add(&knftables.Table{Name: tableName, Family: knftables.InetFamily})

	// Flush the table to remove all existing rules before applying new ones
	tx.Flush(&knftables.Table{Name: tableName, Family: knftables.InetFamily})

	// Add the chains that have rules
	for _, chain := range chainsWithRules {
		tx.Add(&chain)
	}

	// Reset chainRuleCount to handle the use-case mentioned below.
	chainRuleCount = make(map[string]int)

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

	return tx
}
