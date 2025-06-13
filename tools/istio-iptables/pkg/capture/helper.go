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

package capture

import (
	"strings"

	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/builder"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func CombineMatchers(values []string, matcher func(value string) []string) []string {
	matchers := make([][]string, 0, len(values))
	for _, value := range values {
		matchers = append(matchers, matcher(value))
	}
	return Flatten(matchers...)
}

func Flatten(lists ...[]string) []string {
	var result []string
	for _, list := range lists {
		result = append(result, list...)
	}
	return result
}

// VerifyIptablesState function verifies the current iptables state against the expected state.
// The current state is considered equal to the expected state if the following three conditions are met:
//   - Every ISTIO_* chain in the expected state must also exist in the current state.
//   - Every ISTIO_* chain must have the same number of elements in both the current and expected state.
//   - Every rule in the expected state (whether it is in an ISTIO or non-ISTIO chain) must also exist in the current state.
//     The verification is performed by using "iptables -C" on the rule produced by our iptables builder. No comparison of the parsed rules is done.
//
// Note: The order of the rules is not checked and is not used to determine the equivalence of the two states.
// The function returns two boolean values, the first one indicates whether residues exist,
// and the second one indicates whether differences were found between the current and expected state.
func VerifyIptablesState(log *istiolog.Scope, ext dep.Dependencies, ruleBuilder *builder.IptablesRuleBuilder,
	iptVer, ipt6Ver *dep.IptablesVersion,
) (bool, bool) {
	// These variables track the status of iptables installation
	residueExists := false            // Flag to indicate if iptables residues from previous executions are found
	deltaExists := false              // Flag to indicate if a difference is found between expected and current state
	foundCleanStateWithDelta := false // tracks whether at least one IP family has a completely clean iptables state that requires new rules

check_loop:
	for _, ipCfg := range []struct {
		ver        *dep.IptablesVersion
		expected   string
		checkRules [][]string
	}{
		{iptVer, ruleBuilder.BuildV4Restore(), ruleBuilder.BuildCheckV4()},
		{ipt6Ver, ruleBuilder.BuildV6Restore(), ruleBuilder.BuildCheckV6()},
	} {
		if ipCfg.ver == nil {
			continue
		}
		output, err := ext.Run(log, true, constants.IPTablesSave, ipCfg.ver, nil)
		if err == nil {
			currentState := ruleBuilder.GetStateFromSave(output.String())
			log.Debugf("Current iptables state: %#v", currentState)
			for _, value := range currentState {
				if residueExists {
					break
				}
				residueExists = len(value) != 0
			}
			expectedState := ruleBuilder.GetStateFromSave(ipCfg.expected)
			log.Debugf("Expected iptables state: %#v", expectedState)

			nonEmptyExpectedState := false
			for _, chains := range expectedState {
				if len(chains) != 0 {
					nonEmptyExpectedState = true
					break
				}
			}
			if !residueExists && nonEmptyExpectedState {
				foundCleanStateWithDelta = true
				continue
			} else if !residueExists {
				continue
			}
			for table, chains := range expectedState {
				_, ok := currentState[table]
				if !ok {
					deltaExists = true
					log.Debugf("Can't find expected table %s in current state", table)
					break check_loop
				}
				for chain, rules := range chains {
					currentRules, ok := currentState[table][chain]
					if !ok || (strings.HasPrefix(chain, "ISTIO_") && len(rules) != len(currentRules)) {
						deltaExists = true
						log.Debugf("Mismatching number of rules in chain %s (table: %s) between current and expected state", chain, table)
						break check_loop
					}
				}
			}
			for table, chains := range currentState {
				for chain := range chains {
					if strings.HasPrefix(chain, "ISTIO_") {
						_, ok := expectedState[table][chain]
						if !ok {
							deltaExists = true
							log.Debugf("Found chain %s (table: %s) in current state which is missing in expected state", chain, table)
							break check_loop
						}
					}
				}
			}
			for _, cmd := range ipCfg.checkRules {
				if _, err := ext.Run(log, true, constants.IPTables, ipCfg.ver, nil, cmd...); err != nil {
					deltaExists = true
					log.Debugf("iptables check rules failed")
					break
				}
			}
			if foundCleanStateWithDelta {
				if nonEmptyExpectedState {
					log.Info("Found a clean state in IPv4 requiring new rules while IPv6 had compatible residues. Reconciliation is recommended")
					return true, true
				} else {
					log.Info("Found a clean state in IPv4 requiring new rules while IPv6 had compatible residues but no planned rules from Istio. Reconciliation is not needed")
					return false, true
				}
			}
		}

	}

	if !residueExists {
		log.Info("Clean-state detected, new iptables are needed")
		return false, true
	}

	if deltaExists {
		log.Info("Found residues of old iptables rules/chains, reconciliation is recommended")
	} else {
		log.Info("Found compatible residues of old iptables rules/chains, reconciliation not needed")
	}

	return residueExists, deltaExists
}

// HasIstioLeftovers checks the given iptables state for any chains or rules related to Istio.
// It scans the provided map of tables, chains, and rules to identify any chains that start with the "ISTIO_" prefix,
// as well as any rules that involve Istio-specific jumps.
// The function returns a map where the keys are the tables, and the values are structs containing the leftover
// "ISTIO_" chains and jump rules for each table. Only tables with Istio-related leftovers are included in the result.
func HasIstioLeftovers(state map[string]map[string][]string) map[string]struct{ Chains, Rules []string } {
	output := make(map[string]struct{ Chains, Rules []string })
	for table, chains := range state {
		istioChains := []string{}
		istioJumps := []string{}
		for chain, rules := range chains {
			if strings.HasPrefix(chain, "ISTIO_") {
				istioChains = append(istioChains, chain)
			}
			for _, rule := range rules {
				if isIstioJump(rule) {
					istioJumps = append(istioJumps, rule)
				}
			}
		}
		if len(istioChains) != 0 || len(istioJumps) != 0 {
			output[table] = struct{ Chains, Rules []string }{
				Chains: istioChains,
				Rules:  istioJumps,
			}
		}
	}
	return output
}

// isIstioJump checks if the given rule is a jump to an Istio chain
func isIstioJump(rule string) bool {
	// Split the rule into fields
	fields := strings.Fields(rule)
	for i, field := range fields {
		// Check for --jump or -j
		if field == "--jump" || field == "-j" {
			// Check if there's a next field (the target)
			if i+1 < len(fields) {
				target := strings.Trim(fields[i+1], "'\"")
				// Check if the target starts with ISTIO_
				return strings.HasPrefix(target, "ISTIO_")
			}
		}
	}
	return false
}
