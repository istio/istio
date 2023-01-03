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

package xds

import (
	"net"
	"strconv"
	"strings"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/rbacapi"
)

func authorizationPolicyToRBAC(authzPolicy model.AuthorizationPolicy, rootNS string) *rbacapi.RBAC {
	rbac := &rbacapi.RBAC{
		Name: authzPolicy.Name + "/" + authzPolicy.Namespace,
	}

	if authzPolicy.Spec.GetAction() == v1beta1.AuthorizationPolicy_DENY {
		rbac.Action = rbacapi.RBACPolicyAction_DENY
	} else {
		// TODO Check whether AUDIT and CUSTOM can indeed be treated as ALLOW
		rbac.Action = rbacapi.RBACPolicyAction_ALLOW
	}

	if authzPolicy.Namespace == rootNS {
		rbac.Scope = rbacapi.RBACScope_GLOBAL
	} else {
		rbac.Scope = rbacapi.RBACScope_NAMESPACE
	}

	for _, rule := range authzPolicy.Spec.GetRules() {
		rbac.Groups = append(rbac.Groups, addRuleGroup(rule))
	}

	return rbac
}

func addRuleGroup(rule *v1beta1.Rule) *rbacapi.RBACPolicyRulesGroup {
	rules := []*rbacapi.RBACPolicyRules{}

	// From conditions (if exist) are mapped to single rules element
	if len(rule.GetFrom()) > 0 {
		rules = append(rules, handleFrom(rule.From))
	}

	// From conditions (if exist) are mapped to single rules element
	if len(rule.GetTo()) > 0 {
		rules = append(rules, handleTo(rule.To))
	}

	// Each when condition is mapped to rules element
	if len(rule.GetWhen()) > 0 {
		rules = append(rules, handleWhen(rule.When)...)
	}

	group := &rbacapi.RBACPolicyRulesGroup{
		Rules: rules,
	}
	return group
}

func handleFrom(condition []*v1beta1.Rule_From) *rbacapi.RBACPolicyRules {
	matches := []*rbacapi.RBACPolicyRuleMatch{}

	for _, from := range condition {
		matches = append(matches, &rbacapi.RBACPolicyRuleMatch{
			Principals:    stringsToStringMatch(from.GetSource().GetPrincipals()),
			NotPrincipals: stringsToStringMatch(from.GetSource().GetNotPrincipals()),
			Namespaces:    stringsToStringMatch(from.GetSource().GetNamespaces()),
			NotNamespaces: stringsToStringMatch(from.GetSource().GetNotNamespaces()),
			SourceIps:     ipBlockStringsToAddresses(from.GetSource().GetIpBlocks()),
			NotSourceIps:  ipBlockStringsToAddresses(from.GetSource().GetNotIpBlocks()),
		})
	}

	rules := &rbacapi.RBACPolicyRules{
		Matches: matches,
	}

	return rules
}

func handleTo(condition []*v1beta1.Rule_To) *rbacapi.RBACPolicyRules {
	matches := []*rbacapi.RBACPolicyRuleMatch{}

	for _, to := range condition {
		operation := to.Operation
		matches = append(matches, &rbacapi.RBACPolicyRuleMatch{
			DestinationPorts:    parsePortsStrings(operation.Ports),
			NotDestinationPorts: parsePortsStrings(operation.NotPorts),
		})
	}

	rules := &rbacapi.RBACPolicyRules{
		Matches: matches,
	}

	return rules
}

func handleWhen(condition []*v1beta1.Condition) []*rbacapi.RBACPolicyRules {
	rules := []*rbacapi.RBACPolicyRules{}

	for _, when := range condition {
		var ruleMatch *rbacapi.RBACPolicyRuleMatch
		switch key := when.GetKey(); key {
		case "source.ip":
			ruleMatch = &rbacapi.RBACPolicyRuleMatch{
				SourceIps:    ipBlockStringsToAddresses(when.GetValues()),
				NotSourceIps: ipBlockStringsToAddresses(when.GetNotValues()),
			}
		case "source.namespace":
			ruleMatch = &rbacapi.RBACPolicyRuleMatch{
				Namespaces:    stringsToStringMatch(when.GetValues()),
				NotNamespaces: stringsToStringMatch(when.GetNotValues()),
			}
		case "source.principal":
			ruleMatch = &rbacapi.RBACPolicyRuleMatch{
				Principals:    stringsToStringMatch(when.GetValues()),
				NotPrincipals: stringsToStringMatch(when.GetNotValues()),
			}
		case "destination.ip":
			ruleMatch = &rbacapi.RBACPolicyRuleMatch{
				DestinationIps:    ipBlockStringsToAddresses(when.GetValues()),
				NotDestinationIps: ipBlockStringsToAddresses(when.GetNotValues()),
			}
		case "destination.port":
			ruleMatch = &rbacapi.RBACPolicyRuleMatch{
				DestinationPorts:    parsePortsStrings(when.GetValues()),
				NotDestinationPorts: parsePortsStrings(when.GetNotValues()),
			}
		default:
			log.Debugf("Ignoring when condition with key: %s", key)
		}
		rule := &rbacapi.RBACPolicyRules{
			Matches: []*rbacapi.RBACPolicyRuleMatch{ruleMatch},
		}
		rules = append(rules, rule)
	}

	return rules
}

func parsePortsStrings(ports []string) []uint32 {
	parsed := []uint32{}
	for _, port := range ports {
		p, err := strconv.ParseUint(port, 10, 32)
		if err != nil {
			log.Errorf("Error parsing port string: %s", port)
			continue
		}
		parsed = append(parsed, uint32(p))
	}
	return parsed
}

func stringsToStringMatch(strs []string) []*rbacapi.StringMatch {
	stringMatches := []*rbacapi.StringMatch{}
	for _, str := range strs {
		var stringMatch *rbacapi.StringMatch

		if str == "*" {
			stringMatch = &rbacapi.StringMatch{
				MatchType: &rbacapi.StringMatch_Presence{},
			}
		} else if strings.HasPrefix(str, "*") {
			stringMatch = &rbacapi.StringMatch{
				MatchType: &rbacapi.StringMatch_Suffix{Suffix: strings.TrimPrefix(str, "*")},
			}
		} else if strings.HasSuffix(str, "*") {
			stringMatch = &rbacapi.StringMatch{
				MatchType: &rbacapi.StringMatch_Prefix{Prefix: strings.TrimSuffix(str, "*")},
			}
		} else {
			stringMatch = &rbacapi.StringMatch{
				MatchType: &rbacapi.StringMatch_Exact{Exact: str},
			}
		}

		stringMatches = append(stringMatches, stringMatch)
	}
	return stringMatches
}

func ipBlockStringsToAddresses(ipBlocks []string) []*rbacapi.Address {
	addresses := []*rbacapi.Address{}
	for _, ipBlock := range ipBlocks {
		ip := net.ParseIP(ipBlock)
		if ip == nil {
			log.Errorf("Invalid IP address: %s", ipBlock)
			continue
		}
		addr := &rbacapi.Address{}
		if isIPv4(ipBlock) {
			addr.Address = ip[len(ip)-4:]
			addr.Length = 4
		} else {
			addr.Address = ip
			addr.Length = 16
		}
		addresses = append(addresses, addr)
	}
	return addresses
}

func isIPv4(address string) bool {
	return strings.Count(address, ":") < 2
}
