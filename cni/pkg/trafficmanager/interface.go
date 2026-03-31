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
	"istio.io/istio/cni/pkg/config"
	istiolog "istio.io/istio/pkg/log"
)

// TrafficRuleManager defines the interface for managing traffic redirection rules
// in Ambient mode. This abstraction allows switching between iptables and nftables
// implementations without changing the higher-level logic.
type TrafficRuleManager interface {
	CreateInpodRules(log *istiolog.Scope, podOverrides config.PodLevelOverrides, guid string) error
	DeleteInpodRules(log *istiolog.Scope, guid string) error
	CreateHostRulesForHealthChecks() error
	DeleteHostRules()
	ReconcileModeEnabled() bool
}
