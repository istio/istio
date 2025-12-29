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

package gateway

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// ConflictingGatewayAnalyzer checks a gateway's selector, port number and hosts.
type ConflictingGatewayAnalyzer struct{}

// (compile-time check that we implement the interface)
var _ analysis.Analyzer = &ConflictingGatewayAnalyzer{}

// Metadata implements analysis.Analyzer
func (*ConflictingGatewayAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "gateway.ConflictingGatewayAnalyzer",
		Description: "Checks a gateway's selector, port number and hosts",
		Inputs: []config.GroupVersionKind{
			gvk.Gateway,
			gvk.Pod,
		},
	}
}

// Analyze implements analysis.Analyzer
func (s *ConflictingGatewayAnalyzer) Analyze(c analysis.Context) {
	gwConflictingMap := initGatewaysMap(c)
	c.ForEach(gvk.Gateway, func(r *resource.Instance) bool {
		s.analyzeGateway(r, c, gwConflictingMap)
		return true
	})
}

func (*ConflictingGatewayAnalyzer) analyzeGateway(r *resource.Instance, c analysis.Context,
	gwCMap gatewaysContextMap,
) {
	gw := r.Message.(*v1alpha3.Gateway)
	gwName := r.Metadata.FullName.String()
	// For pods selected by gw.Selector, find Services that select them and remember those ports
	gwSelector := klabels.SelectorFromSet(gw.Selector)
	sGWSelector := gwSelector.String()

	// Check non-exist gateway with particular selector
	isExists := false
	hitSameGateways := gatewaysContextMap{}
	for gwmKey := range gwCMap {
		matched := false
		xSelectorStr, _ := parseFromGatewayMapKey(gwmKey)

		if sGWSelector == xSelectorStr {
			matched = true
		} else if strings.Contains(xSelectorStr, sGWSelector) || strings.Contains(sGWSelector, xSelectorStr) {
			xSelector := parseSelectorFromString(xSelectorStr)
			c.ForEach(gvk.Pod, func(rPod *resource.Instance) bool {
				// need match the same pod
				podLabels := klabels.Set(rPod.Metadata.Labels)
				if gwSelector.Matches(podLabels) && xSelector.Matches(podLabels) {
					matched = true
					return false
				}
				return true
			})
		}

		if matched {
			isExists = true
			// record match same selector
			hitSameGateways[gwmKey] = gwCMap[gwmKey]
		}
	}

	if sGWSelector != "" && !isExists {
		m := msg.NewReferencedResourceNotFound(r, "selector", sGWSelector)
		label := util.ExtractLabelFromSelectorString(sGWSelector)
		if line, ok := util.ErrorLine(r, fmt.Sprintf(util.GatewaySelector, label)); ok {
			m.Line = line
		}
		c.Report(gvk.Gateway, m)
		return
	}

	for _, server := range gw.Servers {
		var gateways []string
		conflictingGWMatch := 0
		sPortNumber := strconv.Itoa(int(server.GetPort().GetNumber()))
		for key, values := range hitSameGateways {
			if _, port := parseFromGatewayMapKey(key); port != sPortNumber {
				continue
			}

			for gwNameKey, gwHostsBind := range values {
				// both selector and port number are the same, then check hosts and bind
				if gwName != gwNameKey && isGWConflict(server, gwHostsBind) {
					conflictingGWMatch++
					gateways = append(gateways, gwNameKey)
				}
			}
		}

		if conflictingGWMatch > 0 {
			sort.Strings(gateways)
			reportMsg := strings.Join(gateways, ",")
			hostsMsg := strings.Join(server.GetHosts(), ",")
			m := msg.NewConflictingGateways(r, reportMsg, sGWSelector, sPortNumber, hostsMsg)
			c.Report(gvk.Gateway, m)
		}
	}
}

// isGWConflict implements gateway's hosts match
func isGWConflict(server *v1alpha3.Server, knowHostsBind gatewayHostsBind) bool {
	newHostsBind := knowHostsBind
	// CheckDuplicates returns all of the hosts provided that are already known
	// If there were no duplicates, all hosts are added to the known hosts.
	duplicates := model.CheckDuplicates(server.GetHosts(), server.GetBind(), newHostsBind)
	return len(duplicates) > 0
}

// gatewayHostsBind: key host, value bind
type gatewayHostsBind map[string]string

// gatewaysContextMap: key selectors~port, valueKey gatewayName
type gatewaysContextMap map[string]map[string]gatewayHostsBind

// initGatewaysMap implements initialization for gateways Map
func initGatewaysMap(ctx analysis.Context) gatewaysContextMap {
	gwConflictingMap := make(map[string]map[string]gatewayHostsBind)
	ctx.ForEach(gvk.Gateway, func(r *resource.Instance) bool {
		gw := r.Message.(*v1alpha3.Gateway)
		gwName := r.Metadata.FullName.String()

		gwSelector := klabels.SelectorFromSet(gw.GetSelector())
		sGWSelector := gwSelector.String()
		for _, server := range gw.GetServers() {
			sPortNumber := strconv.Itoa(int(server.GetPort().GetNumber()))
			mapKey := genGatewayMapKey(sGWSelector, sPortNumber)
			if _, exits := gwConflictingMap[mapKey]; !exits {
				objMap := make(map[string]gatewayHostsBind)
				gwConflictingMap[mapKey] = objMap
			}
			hb := map[string]string{}
			for _, h := range server.GetHosts() {
				hb[h] = server.GetBind()
			}
			gwConflictingMap[mapKey][gwName] = hb
		}
		return true
	})

	return gwConflictingMap
}

func genGatewayMapKey(selector, portNumber string) string {
	key := selector + "~" + portNumber
	return key
}

func parseFromGatewayMapKey(key string) (selector string, port string) {
	parts := strings.Split(key, "~")
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func parseSelectorFromString(selectorString string) klabels.Selector {
	selector := make(map[string]string)
	selectorParts := strings.Split(selectorString, ",")
	for _, pair := range selectorParts {
		keyValue := strings.Split(pair, "=")
		if len(keyValue) == 2 {
			selector[keyValue[0]] = keyValue[1]
		}
	}
	return klabels.SelectorFromSet(selector)
}
