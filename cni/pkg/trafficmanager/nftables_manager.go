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

package trafficmanager

import (
	"fmt"

	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/nftables"
	istiolog "istio.io/istio/pkg/log"
)

// NftablesTrafficManager implements TrafficRuleManager using nftables backend
type NftablesTrafficManager struct {
	hostNftables *nftables.NftablesConfigurator
	podNftables  *nftables.NftablesConfigurator
}

var _ TrafficRuleManager = &NftablesTrafficManager{}

// NewNftablesTrafficManager creates both host and pod nftables-based traffic managers
func NewNftablesTrafficManager(cfg *TrafficRuleManagerConfig) (hostManager, podManager TrafficRuleManager, err error) {
	hostNftables, podNftables, err := nftables.NewNftablesConfigurator(
		cfg.HostConfig,
		cfg.PodConfig,
		nil,
		nil,
		cfg.NlDeps,
	)
	if err != nil {
		return nil, nil, err
	}

	hostManager = &NftablesTrafficManager{
		hostNftables: hostNftables,
		podNftables:  nil, // Host manager doesn't need pod nftables
	}

	podManager = &NftablesTrafficManager{
		hostNftables: nil, // Pod manager doesn't need host nftables
		podNftables:  podNftables,
	}

	return hostManager, podManager, nil
}

// CreateInpodRules creates nftables rules within a pod's network namespace
func (m *NftablesTrafficManager) CreateInpodRules(log *istiolog.Scope, podOverrides iptables.PodLevelOverrides) error {
	if m.podNftables == nil {
		return fmt.Errorf("pod nftables configurator not available (this is likely a host-only traffic manager)")
	}
	return m.podNftables.CreateInpodRules(log, podOverrides)
}

// DeleteInpodRules removes nftables rules from a pod's network namespace
func (m *NftablesTrafficManager) DeleteInpodRules(log *istiolog.Scope) error {
	if m.podNftables == nil {
		return fmt.Errorf("pod nftables configurator not available (this is likely a host-only traffic manager)")
	}
	return m.podNftables.DeleteInpodRules(log)
}

// CreateHostRulesForHealthChecks creates host-level nftables rules for health check handling
func (m *NftablesTrafficManager) CreateHostRulesForHealthChecks() error {
	if m.hostNftables == nil {
		return fmt.Errorf("host nftables configurator not available (this is likely a pod-only traffic manager)")
	}
	return m.hostNftables.CreateHostRulesForHealthChecks()
}

// DeleteHostRules removes host-level nftables rules
func (m *NftablesTrafficManager) DeleteHostRules() {
	if m.hostNftables != nil {
		m.hostNftables.DeleteHostRules()
	}
}

// ReconcileModeEnabled returns true if reconciliation mode is enabled
func (m *NftablesTrafficManager) ReconcileModeEnabled() bool {
	if m.podNftables == nil {
		// Default to false if pod nftables configurator not available
		return false
	}
	return m.podNftables.ReconcileModeEnabled()
}
