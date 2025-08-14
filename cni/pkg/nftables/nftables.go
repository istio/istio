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
	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/scopes"
	istiolog "istio.io/istio/pkg/log"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
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
)

var log = scopes.CNIAgent

// NftablesConfigurator handles nftables rule management for Ambient mode
// This follows the same pattern as iptables.IptablesConfigurator
type NftablesConfigurator struct {
	ext    dep.Dependencies
	nlDeps iptables.NetlinkDependencies
	cfg    *config.IptablesConfig
}

// NewNftablesConfigurator creates both host and pod nftables configurators
func NewNftablesConfigurator(
	hostCfg *config.IptablesConfig,
	podCfg *config.IptablesConfig,
	hostDeps dep.Dependencies,
	podDeps dep.Dependencies,
	nlDeps iptables.NetlinkDependencies,
) (*NftablesConfigurator, *NftablesConfigurator, error) {
	if hostCfg == nil {
		hostCfg = &config.IptablesConfig{}
	}
	if podCfg == nil {
		podCfg = &config.IptablesConfig{}
	}

	hostConfigurator := &NftablesConfigurator{
		ext:    hostDeps,
		nlDeps: nlDeps,
		cfg:    hostCfg,
	}

	podConfigurator := &NftablesConfigurator{
		ext:    podDeps,
		nlDeps: nlDeps,
		cfg:    podCfg,
	}

	return hostConfigurator, podConfigurator, nil
}

// CreateInpodRules creates nftables rules within a pod's network namespace
func (cfg *NftablesConfigurator) CreateInpodRules(log *istiolog.Scope, podOverrides config.PodLevelOverrides) error {
	log.Info("native nftables enabled, using nft rules for inpod traffic redirection")

	// TODO: Implement nftables rule creation for inpod traffic redirection

	// Add loopback routes (still needed for nftables)
	if err := cfg.addLoopbackRoute(); err != nil {
		return err
	}

	// Add inpod mark IP rule (still needed for nftables)
	if err := cfg.addInpodMarkIPRule(); err != nil {
		return err
	}

	log.Info("nftables inpod rules creation completed (stub implementation)")
	return nil
}

// DeleteInpodRules removes nftables rules from a pod's network namespace
func (cfg *NftablesConfigurator) DeleteInpodRules(log *istiolog.Scope) error {
	log.Info("removing nftables inpod rules")

	// TODO: Implement nftables rule deletion for inpod traffic

	// Remove loopback routes
	if err := cfg.delLoopbackRoute(); err != nil {
		log.Warnf("failed to remove loopback route: %v", err)
	}

	// Remove inpod mark IP rule
	if err := cfg.delInpodMarkIPRule(); err != nil {
		log.Warnf("failed to remove inpod mark IP rule: %v", err)
	}

	log.Info("nftables inpod rules deletion completed (stub implementation)")
	return nil
}

// CreateHostRulesForHealthChecks creates host-level nftables rules for health check handling
func (cfg *NftablesConfigurator) CreateHostRulesForHealthChecks() error {
	log.Info("configuring host-level nftables rules (healthchecks, etc)")

	// TODO: Implement host-level nftables rules for health check handling
	log.Info("host-level nftables rules creation completed (stub implementation)")
	return nil
}

// DeleteHostRules removes host-level nftables rules
func (cfg *NftablesConfigurator) DeleteHostRules() {
	log.Debug("Attempting to delete hostside nftables rules (if they exist)")

	// TODO: Implement host-level nftables rule deletion
	log.Debug("hostside nftables rules deletion completed (stub implementation)")
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
