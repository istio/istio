// Copyright 2017 Istio Authors
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

package model

import (
	"bytes"
	"fmt"
	"sort"

	multierror "github.com/hashicorp/go-multierror"

	routing "istio.io/api/routing/v1alpha1"
)

// RejectConflictingEgressRules rejects conflicting egress rules.
// The conflicts occur either than two egress rules share the same domain, or when they define
// different protocols on the same port
func RejectConflictingEgressRules(rules map[string]*routing.EgressRule) ( // long line split
	map[string]*routing.EgressRule, error) {
	rulesWithoutConflictsOnDomain, conflictsOnDomain := rejectConflictingOnDomainEgressRules(rules)
	rulesWithoutConflicts, conflictsOnPort := rejectConflictingOnPortTCPEgressRules(rulesWithoutConflictsOnDomain)
	return rulesWithoutConflicts, multierror.Append(conflictsOnDomain, conflictsOnPort).ErrorOrNil()
}

// rejectConflictingOnDomainEgressRules rejects rules that have the destination which is equal to
// the destionation of some other rule.
// According to Envoy's virtual host specification, no virtual hosts can share the same domain.
// The following code rejects conflicting rules deterministically, by a lexicographical order -
// a rule with a smaller key lexicographically wins.
// Here the key of the rule is the key of the Istio configuration objects - see
// `func (meta *ConfigMeta) Key() string`
func rejectConflictingOnDomainEgressRules(egressRules map[string]*routing.EgressRule) ( // long line split
	map[string]*routing.EgressRule, error) {
	filteredEgressRules := make(map[string]*routing.EgressRule)

	var errs error

	var keys []string

	// the key here is the key of the Istio configuration objects - see
	// `func (meta *ConfigMeta) Key() string`
	for key := range egressRules {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// domains - a map where keys are of the form domain:port and values are the keys of
	// egress-rule configuration objects
	domains := make(map[string]string)
	for _, egressRuleKey := range keys {
		egressRule := egressRules[egressRuleKey]
		domain := egressRule.Destination.Service
		keyOfAnEgressRuleWithTheSameDomain, conflictingRule := domains[domain]
		if conflictingRule {
			errs = multierror.Append(errs,
				fmt.Errorf("rule %s conflicts with rule %s on domain "+
					"%s, is rejected", egressRuleKey,
					keyOfAnEgressRuleWithTheSameDomain, domain))
			continue
		}

		domains[domain] = egressRuleKey
		filteredEgressRules[egressRuleKey] = egressRule
	}

	return filteredEgressRules, errs
}

var supportedHTTPProtocols = map[Protocol]bool{
	ProtocolHTTP:  true,
	ProtocolHTTP2: true,
	ProtocolGRPC:  true,
	ProtocolHTTPS: true,
}

var supportedTCPProtocols = map[Protocol]bool{
	ProtocolTCP:   true,
	ProtocolMongo: true,
}

// IsEgressRulesSupportedHTTPProtocol returns true if the protocol is supported
// by egress rules, as an HTTP protocol (service names can contain wildcard domain names)
func IsEgressRulesSupportedHTTPProtocol(protocol Protocol) bool {
	_, ok := supportedHTTPProtocols[protocol]
	return ok
}

// IsEgressRulesSupportedTCPProtocol returns true if the protocol is supported
// by egress rules, as a TCP protocol (service names can contain CIDR)
func IsEgressRulesSupportedTCPProtocol(protocol Protocol) bool {
	_, ok := supportedTCPProtocols[protocol]
	return ok
}

// IsEgressRulesSupportedProtocol returns true if the protocol is supported by egress rules
func IsEgressRulesSupportedProtocol(protocol Protocol) bool {
	return IsEgressRulesSupportedHTTPProtocol(protocol) || IsEgressRulesSupportedTCPProtocol(protocol)
}

func protocolMapAsString(protocolMap map[Protocol]bool) string {
	first := true
	var result bytes.Buffer

	for key := range protocolMap {
		if !first {
			result.WriteString(",")
		}
		result.WriteString(string(key))
		first = false
	}

	return result.String()
}

func egressRulesSupportedHTTPProtocols() string {
	return protocolMapAsString(supportedHTTPProtocols)
}

func egressRulesSupportedTCPProtocols() string {
	return protocolMapAsString(supportedTCPProtocols)
}

func egressRulesSupportedProtocols() string {
	httpSupportedProtocols := egressRulesSupportedHTTPProtocols()
	tcpSupportedProtocols := egressRulesSupportedTCPProtocols()
	separator := ""
	if httpSupportedProtocols != "" && tcpSupportedProtocols != "" {
		separator = ","
	}
	return httpSupportedProtocols + separator + tcpSupportedProtocols
}

// rejectConflictingOnPortTCPEgressRules rejects rules that have conflicting protocols on the same port
// E.g. Mongo and TCP, or TCP and HTTP. In the current implementation of egress rules support,
// conflicts between TCP protocols and other TCP or HTTP protocols are not allowed.
// The following code rejects conflicting rules deterministically, by a lexicographical order -
// a rule with a smaller key lexicographically wins.
// Here the key of the rule is the key of the Istio configuration objects - see
// `func (meta *ConfigMeta) Key() string`
func rejectConflictingOnPortTCPEgressRules(egressRules map[string]*routing.EgressRule) ( // long line split
	map[string]*routing.EgressRule, error) {
	filteredEgressRules := make(map[string]*routing.EgressRule)
	var errs error

	var keys []string

	// the key here is the key of the Istio configuration objects - see
	// `func (meta *ConfigMeta) Key() string`
	for key := range egressRules {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	protocolsPerPort := make(map[int]Protocol)
	rulesByPort := make(map[int][]string)

	for _, egressRuleKey := range keys {
		egressRule := egressRules[egressRuleKey]
		isRuleConflicting := false
		for _, port := range egressRule.Ports {
			protocol := ConvertCaseInsensitiveStringToProtocol(port.Protocol)
			if IsEgressRulesSupportedHTTPProtocol(protocol) {
				// we treat all the http protocols the same here, since there is no collision
				// between HTTP protocols in the current implementation
				protocol = ProtocolHTTP
			}
			intPort := int(port.Port)
			if protocolUntilNow, ok := protocolsPerPort[intPort]; ok && protocolUntilNow != protocol {
				errs = multierror.Append(errs, fmt.Errorf("rule %s is rejected since it conflicts "+
					"rules %v on port %d, protocol %s vs. protocol %s",
					egressRuleKey, rulesByPort[intPort], intPort, protocol, protocolUntilNow))
				isRuleConflicting = true
				break
			}
			protocolsPerPort[intPort] = protocol
			rulesByPort[intPort] = append(rulesByPort[intPort], egressRuleKey)
		}
		if !isRuleConflicting {
			filteredEgressRules[egressRuleKey] = egressRule
		}
	}

	return filteredEgressRules, errs
}
