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
	"strings"

	"istio.io/istio/pkg/log"
)

// CheckRules generates a set of iptables rules that are used to verify the existence of the input rules.
// The function transforms -A/--append and -I/--insert flags into -C/--check flags while preserving the
// structure of other parameters.
// The transformation allows for checking whether the corresponding rules are already present in the iptables configuration.
func CheckRules(rules []Rule) []Rule {
	output := make([]Rule, 0)
	for _, r := range rules {
		var modifiedParams []string
		insertIndex := -1
		for i, element := range r.params {
			// insert index of a previous -I flag must be skipped
			if insertIndex >= 0 && i == insertIndex+2 {
				continue
			}
			if element == "-A" || element == "--append" {
				// -A/--append is transformed to -D
				modifiedParams = append(modifiedParams, "-C")
			} else if element == "-I" || element == "--insert" {
				// -I/--insert is transformed to -D, insert index at i+2 must be skipped
				insertIndex = i
				modifiedParams = append(modifiedParams, "-C")
			} else {
				// Every other flag/value is kept as it is
				modifiedParams = append(modifiedParams, element)
			}
		}
		output = append(output, Rule{
			chain:  r.chain,
			table:  r.table,
			params: modifiedParams,
		})
	}
	log.Debugf("Generated check-rules: %+v", output)
	return output
}

// UndoRules generates the minimal set of rules that are necessary to undo the changes made by the input rules.
// The function transforms -A/--append and -I/--insert flags into -D/--delete flags while preserving the
// structure of other parameters.
// Non-jump rules in ISTIO_* chains are skipped as these chains will be flushed, but jump rules are retained to ensure proper reversal.
// Note: This function does not support converting rules with -D/--delete flags back to -A/-I flags.
func UndoRules(rules []Rule) []Rule {
	output := make([]Rule, 0)
	for _, r := range rules {
		var modifiedParams []string
		skip := false
		insertIndex := -1
		for i, element := range r.params {
			// insert index of a previous -I flag must be skipped
			if insertIndex >= 0 && i == insertIndex+2 {
				continue
			}
			if element == "-A" || element == "--append" {
				// -A/--append is transformed to -D
				modifiedParams = append(modifiedParams, "-D")
			} else if element == "-I" || element == "--insert" {
				// -I/--insert is transformed to -D, insert index at i+2 must be skipped
				insertIndex = i
				modifiedParams = append(modifiedParams, "-D")
			} else {
				// Every other flag/value is kept as it is
				modifiedParams = append(modifiedParams, element)
			}

			if ((element == "-A" || element == "--append") || (element == "-I" || element == "--insert")) &&
				i < len(r.params)-1 && strings.HasPrefix(r.params[i+1], "ISTIO_") {
				// Ignore every non-jump rule in ISTIO_* chains as we will flush the chain anyway
				skip = true
			} else if (element == "-j" || element == "--jump") && i < len(r.params)-1 && strings.HasPrefix(r.params[i+1], "ISTIO_") {
				// Override previous skip if this is a jump-rule
				skip = false
			}
		}
		if skip {
			continue
		}

		output = append(output, Rule{
			chain:  r.chain,
			table:  r.table,
			params: modifiedParams,
		})
	}
	log.Debugf("Generated undo-rules: %+v", output)
	return output
}

// BuildCleanupFromState generates a set of iptables commands to clean up unexpected leftover rules and chains.
// The function takes the current state of iptables, represented by a map of table names to their associated chains and rules.
// It first transforms the provided rules into corresponding undo rules.
// It then appends flush and delete commands for each ISTIO_* chain.
// This function is used to clean up any leftover state that does not match the iptables configuration.
func BuildCleanupFromState(tableState map[string]struct{ Chains, Rules []string }) [][]string {
	output := make([][]string, 0)
	var rules []Rule
	for table, content := range tableState {
		for _, rule := range content.Rules {
			params := strings.Fields(rule)
			chain := ""
			for i, item := range params {
				if (item == "--append" || item == "-A" || item == "--insert" || item == "-I") && i+1 < len(params) {
					chain = params[i+1]
					break
				}
			}
			rules = append(rules, Rule{
				chain:  chain,
				table:  table,
				params: params,
			})
		}
	}
	for _, r := range UndoRules(rules) {
		output = append(output, append([]string{"-t", r.table}, r.params...))
	}

	for table, content := range tableState {
		for _, chain := range content.Chains {
			if strings.HasPrefix(chain, "ISTIO_") {
				output = append(output, []string{"-t", table, "-F", chain}, []string{"-t", table, "-X", chain})
			}
		}
	}
	return output
}
