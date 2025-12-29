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

package builder

import (
	"sigs.k8s.io/knftables"

	"istio.io/istio/tools/common/config"
	"istio.io/istio/tools/istio-nftables/pkg/constants"
)

var IstioTableNames = []string{
	constants.IstioProxyNatTable, constants.IstioProxyMangleTable, constants.IstioProxyRawTable,
}

// NftablesRuleBuilder is a builder struct to manage nftables rules in memory before applying them on the platform.
type NftablesRuleBuilder struct {
	Rules map[string][]knftables.Rule
	cfg   *config.Config
}

// NewNftablesRuleBuilder creates a new rule builder with an empty rule list for each Istio table.
func NewNftablesRuleBuilder(cfg *config.Config) *NftablesRuleBuilder {
	if cfg == nil {
		cfg = &config.Config{}
	}
	rules := make(map[string][]knftables.Rule)
	for _, table := range IstioTableNames {
		rules[table] = []knftables.Rule{}
	}
	return &NftablesRuleBuilder{
		Rules: rules,
		cfg:   cfg,
	}
}

// InsertRule adds a rule at a specific position in the given chain and table.
func (rb *NftablesRuleBuilder) InsertRule(chain string, table string, position int, params ...string) *NftablesRuleBuilder {
	rule := knftables.Rule{
		Chain:  chain,
		Table:  table,
		Family: knftables.InetFamily,
		Rule:   knftables.Concat(params),
		Index:  knftables.PtrTo(position),
	}
	rb.Rules[table] = append(rb.Rules[table], rule)
	return rb
}

// InsertV6RuleIfSupported inserts a rule only if IPv6 is enabled in the config.
func (rb *NftablesRuleBuilder) InsertV6RuleIfSupported(chain string, table string, position int, params ...string) *NftablesRuleBuilder {
	if rb.cfg.EnableIPv6 {
		return rb.InsertRule(chain, table, position, params...)
	}

	return nil
}

// AppendRule adds a rule to the end of the chain in the given table.
func (rb *NftablesRuleBuilder) AppendRule(chain string, table string, params ...string) *NftablesRuleBuilder {
	rule := knftables.Rule{
		Chain:  chain,
		Table:  table,
		Family: knftables.InetFamily,
		Rule:   knftables.Concat(params),
	}
	rb.Rules[table] = append(rb.Rules[table], rule)
	return rb
}

// AppendV6RuleIfSupported appends a rule only if IPv6 is enabled in the config.
func (rb *NftablesRuleBuilder) AppendV6RuleIfSupported(chain string, table string, params ...string) *NftablesRuleBuilder {
	if rb.cfg.EnableIPv6 {
		return rb.AppendRule(chain, table, params...)
	}

	return nil
}
