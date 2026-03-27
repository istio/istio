//go:build windows

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
)

// WFPTrafficManager implements TrafficRuleManager with WFP backend
type WFPTrafficManager struct {
	wfp *iptables.WFPConfigurator
}

var _ TrafficRuleManager = &WFPTrafficManager{}

// NewWFPTrafficManager creates both host and pod WFP-based traffic managers
func NewWFPTrafficManager(wfp *iptables.WFPConfigurator) (hostManager TrafficRuleManager, err error) {
	manager := &WFPTrafficManager{
		wfp: wfp,
	}

	return manager, nil
}

// CreateInpodRules creates iptables rules within a pod's network namespace
func (m *WFPTrafficManager) CreateInpodRules(log *istiolog.Scope, podOverrides config.PodLevelOverrides, guid string) error {
	if m.wfp == nil {
		return fmt.Errorf("WFP configurator not available (this is likely a host-only traffic manager)")
	}
	return m.wfp.CreateInpodRules(log, podOverrides, guid)
}

// DeleteInpodRules removes iptables rules from a pod's network namespace
func (m *WFPTrafficManager) DeleteInpodRules(log *istiolog.Scope, guid string) error {
	if m.wfp == nil {
		return fmt.Errorf("WFP configurator not available (this is likely a host-only traffic manager)")
	}
	return m.wfp.DeleteInpodRules(log, guid)
}

// CreateHostRulesForHealthChecks creates host-level iptables rules for health check handling
func (m *WFPTrafficManager) CreateHostRulesForHealthChecks() error {
	return nil
}

// DeleteHostRules removes host-level iptables rules
func (m *WFPTrafficManager) DeleteHostRules() {}

// ReconcileModeEnabled returns true if reconciliation mode is enabled
func (m *WFPTrafficManager) ReconcileModeEnabled() bool {
	if m.wfp == nil {
		// Default to false if WFP configurator not available
		return false
	}
	return m.wfp.ReconcileModeEnabled()
}
