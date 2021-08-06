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
	"fmt"
	"strings"

	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
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

// IptablesBuilder is an implementation for IptablesBuilder interface
type IptablesBuilder struct {
	rules Rules
	cfg   *config.Config
}

// NewIptablesBuilders creates a new IptablesBuilder
func NewIptablesBuilder(cfg *config.Config) *IptablesBuilder {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &IptablesBuilder{
		rules: Rules{
			rulesv4: []*Rule{},
			rulesv6: []*Rule{},
		},
		cfg: cfg,
	}
}

func (rb *IptablesBuilder) InsertRule(chain string, table string, position int, params ...string) *IptablesBuilder {
	rb.InsertRuleV4(chain, table, position, params...)
	rb.InsertRuleV6(chain, table, position, params...)
	return rb
}

func (rb *IptablesBuilder) InsertRuleV4(chain string, table string, position int, params ...string) *IptablesBuilder {
	rb.rules.rulesv4 = append(rb.rules.rulesv4, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-I", chain, fmt.Sprint(position)}, params...),
	})
	return rb
}

func (rb *IptablesBuilder) InsertRuleV6(chain string, table string, position int, params ...string) *IptablesBuilder {
	if !rb.cfg.EnableInboundIPv6 {
		return rb
	}
	rb.rules.rulesv6 = append(rb.rules.rulesv6, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-I", chain, fmt.Sprint(position)}, params...),
	})
	return rb
}

func (rb *IptablesBuilder) AppendRuleV4(chain string, table string, params ...string) *IptablesBuilder {
	rb.rules.rulesv4 = append(rb.rules.rulesv4, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-A", chain}, params...),
	})
	return rb
}

func (rb *IptablesBuilder) AppendRule(chain string, table string, params ...string) *IptablesBuilder {
	rb.AppendRuleV4(chain, table, params...)
	rb.AppendRuleV6(chain, table, params...)
	return rb
}

func (rb *IptablesBuilder) AppendRuleV6(chain string, table string, params ...string) *IptablesBuilder {
	if !rb.cfg.EnableInboundIPv6 {
		return rb
	}
	rb.rules.rulesv6 = append(rb.rules.rulesv6, &Rule{
		chain:  chain,
		table:  table,
		params: append([]string{"-A", chain}, params...),
	})
	return rb
}

func (rb *IptablesBuilder) buildRules(command string, rules []*Rule) [][]string {
	output := [][]string{}
	chainTableLookupMap := make(map[string]struct{})
	for _, r := range rules {
		chainTable := fmt.Sprintf("%s:%s", r.chain, r.table)
		// Create new chain if key: `chainTable` isn't present in map
		if _, present := chainTableLookupMap[chainTable]; !present {
			// Ignore chain creation for built-in chains for iptables
			if _, present := constants.BuiltInChainsMap[r.chain]; !present {
				cmd := []string{command, "-t", r.table, "-N", r.chain}
				output = append(output, cmd)
				chainTableLookupMap[chainTable] = struct{}{}
			}
		}
	}
	for _, r := range rules {
		cmd := append([]string{command, "-t", r.table}, r.params...)
		output = append(output, cmd)
	}
	return output
}

func (rb *IptablesBuilder) BuildV4() [][]string {
	return rb.buildRules(constants.IPTABLES, rb.rules.rulesv4)
}

func (rb *IptablesBuilder) BuildV6() [][]string {
	return rb.buildRules(constants.IP6TABLES, rb.rules.rulesv6)
}

func (rb *IptablesBuilder) constructIptablesRestoreContents(tableRulesMap map[string][]string) string {
	var b strings.Builder
	for table, rules := range tableRulesMap {
		if len(rules) > 0 {
			fmt.Fprintln(&b, "*", table)
			for _, r := range rules {
				fmt.Fprintln(&b, r)
			}
			fmt.Fprintln(&b, "COMMIT")
		}
	}
	return b.String()
}

func (rb *IptablesBuilder) buildRestore(rules []*Rule) string {
	tableRulesMap := map[string][]string{
		constants.FILTER: {},
		constants.NAT:    {},
		constants.MANGLE: {},
	}

	chainTableLookupMap := make(map[string]struct{})
	for _, r := range rules {
		chainTable := fmt.Sprintf("%s:%s", r.chain, r.table)
		// Create new chain if key: `chainTable` isn't present in map
		if _, present := chainTableLookupMap[chainTable]; !present {
			// Ignore chain creation for built-in chains for iptables
			if _, present := constants.BuiltInChainsMap[r.chain]; !present {
				tableRulesMap[r.table] = append(tableRulesMap[r.table], fmt.Sprintf("-N %s", r.chain))
				chainTableLookupMap[chainTable] = struct{}{}
			}
		}
	}

	for _, r := range rules {
		tableRulesMap[r.table] = append(tableRulesMap[r.table], strings.Join(r.params, " "))
	}
	return rb.constructIptablesRestoreContents(tableRulesMap)
}

func (rb *IptablesBuilder) BuildV4Restore() string {
	return rb.buildRestore(rb.rules.rulesv4)
}

func (rb *IptablesBuilder) BuildV6Restore() string {
	return rb.buildRestore(rb.rules.rulesv6)
}

// AppendVersionedRule takes is a wrapper around AppendRule that substitutes an ipv4/ipv6 specific value
// in place in the params. This allows appending a dual-stack rule that has an IP value in it.
func (rb *IptablesBuilder) AppendVersionedRule(ipv4 string, ipv6 string, chain string, table string, params ...string) {
	rb.AppendRuleV4(chain, table, replaceVersionSpecific(ipv4, params...)...)
	rb.AppendRuleV6(chain, table, replaceVersionSpecific(ipv6, params...)...)
}

func replaceVersionSpecific(contents string, inputs ...string) []string {
	res := make([]string, 0, len(inputs))
	for _, i := range inputs {
		if i == constants.IPVersionSpecific {
			res = append(res, contents)
		} else {
			res = append(res, i)
		}
	}
	return res
}
