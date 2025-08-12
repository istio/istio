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
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/scopes"
	istiolog "istio.io/istio/pkg/log"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

var log = scopes.CNIAgent

// NftablesConfigurator handles nftables rule management for Ambient mode
type NftablesConfigurator struct {
	ext    dep.Dependencies
	nlDeps iptables.NetlinkDependencies
	cfg    *iptables.IptablesConfig
}

// NewNftablesConfigurator creates both host and pod nftables configurators
func NewNftablesConfigurator(
	hostCfg *iptables.IptablesConfig,
	podCfg *iptables.IptablesConfig,
	hostDeps dep.Dependencies,
	podDeps dep.Dependencies,
	nlDeps iptables.NetlinkDependencies,
) (*NftablesConfigurator, *NftablesConfigurator, error) {
	if hostCfg == nil {
		hostCfg = &iptables.IptablesConfig{}
	}
	if podCfg == nil {
		podCfg = &iptables.IptablesConfig{}
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
func (cfg *NftablesConfigurator) CreateInpodRules(log *istiolog.Scope, podOverrides iptables.PodLevelOverrides) error {
	log.Info("native nftables enabled, using nft rules for inpod traffic redirection")

	return nil
}

// DeleteInpodRules removes nftables rules from a pod's network namespace
func (cfg *NftablesConfigurator) DeleteInpodRules(log *istiolog.Scope) error {
	log.Info("removing nftables inpod rules")

	return nil
}

// CreateHostRulesForHealthChecks creates host-level nftables rules for health check handling
// NOTE: This expects to be run from within the HOST network namespace!
func (cfg *NftablesConfigurator) CreateHostRulesForHealthChecks() error {
	log.Info("configuring host-level nftables rules (healthchecks, etc)")

	return nil
}

// DeleteHostRules removes host-level nftables rules
func (cfg *NftablesConfigurator) DeleteHostRules() {
	log.Debug("Attempting to delete hostside nftables rules (if they exist)")
	log.Debug("hostside nftables rules deletion completed (stub implementation)")
}

// ReconcileModeEnabled returns true if reconciliation mode is enabled
func (cfg *NftablesConfigurator) ReconcileModeEnabled() bool {
	return cfg.cfg.Reconcile
}

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
