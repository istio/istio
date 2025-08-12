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
	"istio.io/istio/cni/pkg/iptables"
	istiolog "istio.io/istio/pkg/log"
)

// TrafficRuleManager defines the interface for managing traffic redirection rules
// in Ambient mode. This abstraction allows switching between iptables and nftables
// implementations without changing the higher-level logic.
type TrafficRuleManager interface {
	CreateInpodRules(log *istiolog.Scope, podOverrides iptables.PodLevelOverrides) error
	DeleteInpodRules(log *istiolog.Scope) error
	CreateHostRulesForHealthChecks() error
	DeleteHostRules()
	ReconcileModeEnabled() bool
}

type TrafficRuleManagerConfig struct {
	// Use native nftables instead of iptables
	NativeNftables bool

	// Host-level configuration
	HostConfig *iptables.IptablesConfig

	// Pod-level configuration
	PodConfig *iptables.IptablesConfig

	// Dependencies for iptables (host and pod)
	HostDeps interface{}
	PodDeps  interface{}

	NlDeps iptables.NetlinkDependencies
}

// NewTrafficRuleManager creates both host and pod traffic rule managers based on configuration
func NewTrafficRuleManager(cfg *TrafficRuleManagerConfig) (hostManager, podManager TrafficRuleManager, err error) {
	if cfg.NativeNftables {
		return NewNftablesTrafficManager(cfg)
	}
	return NewIptablesTrafficManager(cfg)
}
