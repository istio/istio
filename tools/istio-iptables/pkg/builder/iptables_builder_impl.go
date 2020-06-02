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

	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

const chainNotExistErr = "iptables: No chain/target/match by that name.\n"

// Rule represents iptables rule - chain, table and options
type Rule struct {
	chain     string
	table     string
	extraArgs []string
	params    []string
}

// Rules represents iptables for V4 and V6
type Rules struct {
	rulesv4 []*Rule
	rulesv6 []*Rule
}

// IptablesBuilderImpl is an implementation for IptablesBuilder interface
type IptablesBuilderImpl struct {
	rules Rules
	ext   dep.Dependencies
}

// NewIptablesBuilders creates a new IptablesBuilder
func NewIptablesBuilder(ext dep.Dependencies) *IptablesBuilderImpl {
	return &IptablesBuilderImpl{
		rules: Rules{
			rulesv4: []*Rule{},
			rulesv6: []*Rule{},
		},
		ext: ext,
	}
}

func (rb *IptablesBuilderImpl) InsertRuleV4(chain string, table string, position int, params ...string) IptablesProducer {
	rb.rules.rulesv4 = append(rb.rules.rulesv4, &Rule{
		chain:     chain,
		table:     table,
		extraArgs: []string{"-I", chain, fmt.Sprint(position)},
		params:    params,
	})
	return rb
}

func (rb *IptablesBuilderImpl) InsertRuleV6(chain string, table string, position int, params ...string) IptablesProducer {
	rb.rules.rulesv6 = append(rb.rules.rulesv6, &Rule{
		chain:     chain,
		table:     table,
		extraArgs: []string{"-I", chain, fmt.Sprint(position)},
		params:    params,
	})
	return rb
}

func (rb *IptablesBuilderImpl) AppendRuleV4(chain string, table string, params ...string) IptablesProducer {
	rb.rules.rulesv4 = append(rb.rules.rulesv4, &Rule{
		chain:     chain,
		table:     table,
		extraArgs: []string{"-A", chain},
		params:    params,
	})
	return rb
}

func (rb *IptablesBuilderImpl) AppendRuleV6(chain string, table string, params ...string) IptablesProducer {
	rb.rules.rulesv6 = append(rb.rules.rulesv6, &Rule{
		chain:     chain,
		table:     table,
		extraArgs: []string{"-A", chain},
		params:    params,
	})
	return rb
}

func (rb *IptablesBuilderImpl) buildRules(command string, rules []*Rule) [][]string {
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
		params := append(r.extraArgs, r.params...)
		cmd := append([]string{command, "-t", r.table}, params...)
		output = append(output, cmd)
	}
	return output
}

func (rb *IptablesBuilderImpl) BuildV4() [][]string {
	return rb.buildRules(constants.IPTABLES, rb.rules.rulesv4)
}

func (rb *IptablesBuilderImpl) BuildV6() [][]string {
	return rb.buildRules(constants.IP6TABLES, rb.rules.rulesv6)
}

func (rb *IptablesBuilderImpl) constructIptablesRestoreContents(tableRulesMap map[string][]string) string {
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

func (rb *IptablesBuilderImpl) buildRestore(rules []*Rule) ([]*Rule, []*Rule, string) {
	tableRulesMap := map[string][]string{
		constants.FILTER: {},
		constants.NAT:    {},
		constants.MANGLE: {},
	}
	buildInTableRules := []*Rule{}
	definedChain := []*Rule{}

	chainTableLookupMap := make(map[string]struct{})
	for _, r := range rules {
		chainTable := fmt.Sprintf("%s:%s", r.chain, r.table)
		// Create new chain if key: `chainTable` isn't present in map
		if _, present := chainTableLookupMap[chainTable]; !present {
			// Ignore chain creation for built-in chains for iptables
			if _, present := constants.BuiltInChainsMap[r.chain]; !present {
				tableRulesMap[r.table] = append(tableRulesMap[r.table], fmt.Sprintf(":%s - [0:0]", r.chain))
				definedChain = append(definedChain, &Rule{
					chain:     r.chain,
					table:     r.table,
					extraArgs: []string{"-N", r.chain, "-t", r.table},
				})
				chainTableLookupMap[chainTable] = struct{}{}
			}
		}

		if _, buildIn := constants.BuiltInChainsMap[r.chain]; buildIn {
			buildInTableRules = append(buildInTableRules, &Rule{
				chain:     r.chain,
				table:     r.table,
				extraArgs: []string{"-A", r.chain, "-t", r.table},
				params:    r.params,
			})
		} else {
			tableRulesMap[r.table] = append(tableRulesMap[r.table], strings.Join(append(r.extraArgs, r.params...), " "))
		}
	}

	return buildInTableRules, definedChain, rb.constructIptablesRestoreContents(tableRulesMap)
}

func (rb *IptablesBuilderImpl) BuildV4Restore() ([]*Rule, []*Rule, string) {
	return rb.buildRestore(rb.rules.rulesv4)
}

func (rb *IptablesBuilderImpl) BuildV6Restore() ([]*Rule, []*Rule, string) {
	return rb.buildRestore(rb.rules.rulesv6)
}

func (rb *IptablesBuilderImpl) ruleExists(command string, rule *Rule) (bool, error) {
	args := append([]string{"-C", rule.chain, "-t", rule.table}, rule.params...)

	if stdout, err := rb.ext.Command(command, args...); err != nil {
		if chainNotExistErr == string(stdout) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (rb *IptablesBuilderImpl) ensureRule(command string, rule *Rule) error {
	params := append(rule.extraArgs, rule.params...)
	fmt.Printf("Ensure Rule: %s %s\n", command, strings.Join(params, " "))

	exist, err := rb.ruleExists(command, rule)
	if err != nil {
		return err
	}
	if exist {
		return nil
	}
	if err := rb.ext.Run(command, params...); err != nil {
		return err
	}
	return nil
}

// EnsureV4Rule ensure rule writed in given table
func (rb *IptablesBuilderImpl) EnsureV4Rule(rule *Rule) error {
	return rb.ensureRule(constants.IPTABLES, rule)
}

// EnsureV6Rule ensure rule writed in given table
func (rb *IptablesBuilderImpl) EnsureV6Rule(rule *Rule) error {
	return rb.ensureRule(constants.IP6TABLES, rule)
}

func (rb *IptablesBuilderImpl) chainExists(command string, rule *Rule) (bool, error) {
	if stdout, err := rb.ext.Command(command, "-L", rule.chain, "-t", rule.table); err != nil {
		if chainNotExistErr == string(stdout) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (rb *IptablesBuilderImpl) ensureChain(command string, rule *Rule) error {
	params := append(rule.extraArgs, rule.params...)
	fmt.Printf("Ensure Chain: %s %s\n", command, strings.Join(params, " "))

	exist, err := rb.chainExists(command, rule)
	if err != nil {
		return err
	}
	if exist {
		return nil
	}
	if err := rb.ext.Run(command, params...); err != nil {
		return err
	}
	return nil
}

// EnsureV4Rule ensure chain exist in given table
func (rb *IptablesBuilderImpl) EnsureV4Chain(rule *Rule) error {
	return rb.ensureChain(constants.IPTABLES, rule)
}

// EnsureV6Rule ensure chain exist in given table
func (rb *IptablesBuilderImpl) EnsureV6Chain(rule *Rule) error {
	return rb.ensureChain(constants.IP6TABLES, rule)
}
