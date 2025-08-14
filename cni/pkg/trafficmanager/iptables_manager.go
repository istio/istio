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

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
	istiolog "istio.io/istio/pkg/log"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

// IptablesTrafficManager implements TrafficRuleManager with iptables backend
type IptablesTrafficManager struct {
	hostIptables *iptables.IptablesConfigurator
	podIptables  *iptables.IptablesConfigurator
}

var _ TrafficRuleManager = &IptablesTrafficManager{}

// NewIptablesTrafficManager creates both host and pod iptables-based traffic managers
func NewIptablesTrafficManager(cfg *TrafficRuleManagerConfig) (hostManager, podManager TrafficRuleManager, err error) {
	hostDeps, ok := cfg.HostDeps.(*dep.RealDependencies)
	if !ok {
		hostDeps = &dep.RealDependencies{
			UsePodScopedXtablesLock: false,
			NetworkNamespace:        "",
		}
	}

	podDeps, ok := cfg.PodDeps.(*dep.RealDependencies)
	if !ok {
		podDeps = &dep.RealDependencies{
			UsePodScopedXtablesLock: true,
			NetworkNamespace:        "",
		}
	}

	hostIptables, podIptables, err := iptables.NewIptablesConfigurator(
		cfg.HostConfig,
		cfg.PodConfig,
		hostDeps,
		podDeps,
		cfg.NlDeps,
	)
	if err != nil {
		return nil, nil, err
	}

	hostManager = &IptablesTrafficManager{
		hostIptables: hostIptables,
		podIptables:  nil, // Host manager doesn't need pod iptables
	}

	podManager = &IptablesTrafficManager{
		hostIptables: nil, // Pod manager doesn't need host iptables
		podIptables:  podIptables,
	}

	return hostManager, podManager, nil
}

// CreateInpodRules creates iptables rules within a pod's network namespace
func (m *IptablesTrafficManager) CreateInpodRules(log *istiolog.Scope, podOverrides config.PodLevelOverrides) error {
	if m.podIptables == nil {
		return fmt.Errorf("pod iptables configurator not available (this is likely a host-only traffic manager)")
	}
	return m.podIptables.CreateInpodRules(log, podOverrides)
}

// DeleteInpodRules removes iptables rules from a pod's network namespace
func (m *IptablesTrafficManager) DeleteInpodRules(log *istiolog.Scope) error {
	if m.podIptables == nil {
		return fmt.Errorf("pod iptables configurator not available (this is likely a host-only traffic manager)")
	}
	return m.podIptables.DeleteInpodRules(log)
}

// CreateHostRulesForHealthChecks creates host-level iptables rules for health check handling
func (m *IptablesTrafficManager) CreateHostRulesForHealthChecks() error {
	if m.hostIptables == nil {
		return fmt.Errorf("host iptables configurator not available (this is likely a pod-only traffic manager)")
	}
	return m.hostIptables.CreateHostRulesForHealthChecks()
}

// DeleteHostRules removes host-level iptables rules
func (m *IptablesTrafficManager) DeleteHostRules() {
	if m.hostIptables != nil {
		m.hostIptables.DeleteHostRules()
	}
}

// ReconcileModeEnabled returns true if reconciliation mode is enabled
func (m *IptablesTrafficManager) ReconcileModeEnabled() bool {
	if m.podIptables == nil {
		// Default to false if pod configurator not available
		return false
	}
	return m.podIptables.ReconcileModeEnabled()
}
