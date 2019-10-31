// Copyright 2019 Istio Authors
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
	"fmt"

	"istio.io/istio/tools/istio-iptables/pkg/constants"

	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

// Rule represents iptables rule - chain, table and options
type Rule struct {
	chain  string
	table  string
	params []string
}

// Rules represents iptables for V4 and V6
type Rules struct {
	rulesv4 []*Rule
	rulesv6 []*Rule
}

// IptablesBuilderImpl is an implementation for IptablesBuilder interface
type IptablesBuilderImpl struct {
	rules Rules
}

// NewIptablesBuilders creates a new IptablesBuilder
func NewIptablesBuilder() *IptablesBuilderImpl {
	return &IptablesBuilderImpl{
		rules: Rules{
			rulesv4: []*Rule{},
			rulesv6: []*Rule{},
		},
	}
}

func (rb *IptablesBuilderImpl) InsertRuleV4(chain string, table string, position int, params ...string) IptablesProducer {
	rb.rules.rulesv4 = append(rb.rules.rulesv4, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-I", chain, fmt.Sprint(position)}, params...),
	})
	return rb
}

func (rb *IptablesBuilderImpl) InsertRuleV6(chain string, table string, position int, params ...string) IptablesProducer {
	rb.rules.rulesv6 = append(rb.rules.rulesv6, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-I", chain, fmt.Sprint(position)}, params...),
	})
	return rb
}

func (rb *IptablesBuilderImpl) AppendRuleV4(chain string, table string, params ...string) IptablesProducer {
	rb.rules.rulesv4 = append(rb.rules.rulesv4, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-A", chain}, params...),
	})
	return rb
}

func (rb *IptablesBuilderImpl) AppendRuleV6(chain string, table string, params ...string) IptablesProducer {
	rb.rules.rulesv6 = append(rb.rules.rulesv6, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-A", chain}, params...),
	})
	return rb
}

func (rb *IptablesBuilderImpl) BuildV4() [][]string {
	rules := [][]string{}
	chainTableLookupMap := make(map[string]struct{})
	for _, r := range rb.rules.rulesv4 {
		chainTable := fmt.Sprintf("%s:%s", r.chain, r.table)
		// Create new chain if key: `chainTable` isn't present in map
		if _, present := chainTableLookupMap[chainTable]; !present {
			// Ignore chain creation for built-in chains for iptables
			if _, present := constants.BuiltInChainsMap[r.chain]; !present {
				cmd := []string{dependencies.IPTABLES, "-t", r.table, "-N", r.chain}
				rules = append(rules, cmd)
				chainTableLookupMap[chainTable] = struct{}{}
			}
		}
	}
	for _, r := range rb.rules.rulesv4 {
		cmd := append([]string{dependencies.IPTABLES, "-t", r.table}, r.params...)
		rules = append(rules, cmd)
	}
	return rules
}

func (rb *IptablesBuilderImpl) BuildV6() [][]string {
	rules := [][]string{}
	chainTableLookupMap := make(map[string]struct{})
	for _, r := range rb.rules.rulesv6 {
		chainTable := fmt.Sprintf("%s:%s", r.chain, r.table)
		// Create new chain if key: `chainTable` isn't present in map
		if _, present := chainTableLookupMap[chainTable]; !present {
			// Ignore chain creation for built-in chains for iptables
			if _, present := constants.BuiltInChainsMap[r.chain]; !present {
				cmd := []string{dependencies.IP6TABLES, "-t", r.table, "-N", r.chain}
				rules = append(rules, cmd)
				chainTableLookupMap[chainTable] = struct{}{}
			}
		}
	}
	for _, r := range rb.rules.rulesv6 {
		cmd := append([]string{dependencies.IP6TABLES, "-t", r.table}, r.params...)
		rules = append(rules, cmd)
	}

	return rules
}

func (rb *IptablesBuilderImpl) BuildV4Restore() string {
	//TODO (abhide): implement this
	return ""
}

func (rb *IptablesBuilderImpl) BuildV6Restore() string {
	//TODO (abhide): implement this
	return ""
}
