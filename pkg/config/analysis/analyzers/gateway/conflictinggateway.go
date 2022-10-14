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
	"strconv"
	"strings"

	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
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
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Gateways.Name(),
		},
	}
}

// Analyze implements analysis.Analyzer
func (s *ConflictingGatewayAnalyzer) Analyze(c analysis.Context) {
	gwConflictingMap := initGatewaysMap(c)
	c.ForEach(collections.IstioNetworkingV1Alpha3Gateways.Name(), func(r *resource.Instance) bool {
		s.analyzeGateway(r, c, gwConflictingMap)
		return true
	})
}

func (*ConflictingGatewayAnalyzer) analyzeGateway(r *resource.Instance, c analysis.Context,
	gwCMap map[string]map[string][]string,
) {
	gw := r.Message.(*v1alpha3.Gateway)
	gwName := r.Metadata.FullName.String()
	// For pods selected by gw.Selector, find Services that select them and remember those ports
	gwSelector := k8s_labels.SelectorFromSet(gw.Selector)
	sGWSelector := gwSelector.String()

	// Check non-exist gateway with particular selector
	isExists := false
	for gwmKey := range gwCMap {
		if strings.Contains(gwmKey, sGWSelector) {
			isExists = true
			break
		}
	}
	if sGWSelector != "" && !isExists {
		m := msg.NewReferencedResourceNotFound(r, "selector", sGWSelector)
		label := util.ExtractLabelFromSelectorString(sGWSelector)
		if line, ok := util.ErrorLine(r, fmt.Sprintf(util.GatewaySelector, label)); ok {
			m.Line = line
		}
		c.Report(collections.IstioNetworkingV1Alpha3Gateways.Name(), m)
		return
	}

	for _, server := range gw.Servers {
		var rmsg []string
		conflictingGWMatch := 0
		sPortNumber := strconv.Itoa(int(server.Port.Number))
		mapKey := genGatewayMapKey(sGWSelector, sPortNumber)
		for gwNameKey, gwHostsValue := range gwCMap[mapKey] {
			for _, gwHost := range server.Hosts {
				// both selector and portnumber are the same, then check hosts
				if isGWsHostMatched(gwHost, gwHostsValue) {
					if gwName != gwNameKey {
						conflictingGWMatch++
						rmsg = append(rmsg, gwNameKey)
					}
				}
			}
		}
		if conflictingGWMatch > 0 {
			reportMsg := strings.Join(rmsg, ",")
			hostsMsg := strings.Join(server.Hosts, ",")
			m := msg.NewConflictingGateways(r, reportMsg, sGWSelector, sPortNumber, hostsMsg)
			c.Report(collections.IstioNetworkingV1Alpha3Gateways.Name(), m)
		}
	}
}

// isGWsHostMatched implements gateway's hosts match
func isGWsHostMatched(gwInstance string, gwHostList []string) bool {
	gwInstanceNamed := host.Name(gwInstance)
	for _, gwElem := range gwHostList {
		gwElemNamed := host.Name(gwElem)
		if gwInstanceNamed.Matches(gwElemNamed) {
			return true
		}
	}
	return false
}

// initGatewaysMap implements initilization for gateways Map
func initGatewaysMap(ctx analysis.Context) map[string]map[string][]string {
	gwConflictingMap := make(map[string]map[string][]string)
	ctx.ForEach(collections.IstioNetworkingV1Alpha3Gateways.Name(), func(r *resource.Instance) bool {
		gw := r.Message.(*v1alpha3.Gateway)
		gwName := r.Metadata.FullName.String()

		gwSelector := k8s_labels.SelectorFromSet(gw.Selector)
		sGWSelector := gwSelector.String()
		for _, server := range gw.Servers {
			sPortNumber := strconv.Itoa(int(server.Port.Number))
			mapKey := genGatewayMapKey(sGWSelector, sPortNumber)
			if _, exits := gwConflictingMap[mapKey]; !exits {
				objMap := make(map[string][]string)
				objMap[gwName] = server.Hosts
				gwConflictingMap[mapKey] = objMap
			} else {
				gwConflictingMap[mapKey][gwName] = server.Hosts
			}
		}
		return true
	})
	return gwConflictingMap
}

func genGatewayMapKey(selector, portNumber string) string {
	key := selector + "~" + portNumber
	return key
}
