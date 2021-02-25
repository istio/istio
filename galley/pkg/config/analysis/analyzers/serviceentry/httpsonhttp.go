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

package serviceentry

import (
	"fmt"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

type HTTPSOnHTTPAnalyzer struct{}

var _ analysis.Analyzer = &HTTPSOnHTTPAnalyzer{}

func (serviceEntry *HTTPSOnHTTPAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "serviceentry.HTTPSOnHTTPAnalyzer",
		Description: "Checks if HTTPS traffic on HTTP port",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Serviceentries.Name(),
			collections.IstioNetworkingV1Alpha3Destinationrules.Name(),
		},
	}
}

func (serviceEntry *HTTPSOnHTTPAnalyzer) Analyze(context analysis.Context) {
	context.ForEach(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), func(resource *resource.Instance) bool {
		serviceEntry.analyzeProtocol(resource, context)
		return true
	})
}

func (serviceEntry *HTTPSOnHTTPAnalyzer) analyzeProtocol(resource *resource.Instance, context analysis.Context) {
	se := resource.Message.(*v1alpha3.ServiceEntry)

	for _, host := range se.Hosts {
		relatedDestinationRules := serviceEntry.getDestinationRules(host, context)

		for index, port := range se.Ports {
			if port.Protocol == "HTTP" {
				usedPort := port.Number
				if port.TargetPort != 0 {
					usedPort = port.TargetPort
				}

				if usedPort == 443 {
					if len(relatedDestinationRules) == 0 {
						serviceEntry.sendMessage(resource, host, index, context)
					}

					if !serviceEntry.checkDestinationRulesPerformTLSOrigination(relatedDestinationRules, usedPort, context) {
						serviceEntry.sendMessage(resource, host, index, context)
					}
				}
			}
		}
	}
}

func (serviceEntry *HTTPSOnHTTPAnalyzer) getDestinationRules(host string, context analysis.Context) []resource.FullName {
	var destinationRules []resource.FullName

	context.ForEach(collections.IstioNetworkingV1Alpha3Destinationrules.Name(), func(r *resource.Instance) bool {
		destinationRule := r.Message.(*v1alpha3.DestinationRule)

		if host == destinationRule.Host {
			destinationRules = append(destinationRules, r.Metadata.FullName)
		}

		return true
	})

	return destinationRules
}

func (serviceEntry *HTTPSOnHTTPAnalyzer) checkDestinationRulesPerformTLSOrigination(destinationRulesName []resource.FullName, port uint32, context analysis.Context) bool {
	for _, destinationRuleName := range destinationRulesName {
		destinationRuleInstance := context.Find(collections.IstioNetworkingV1Alpha3Destinationrules.Name(), destinationRuleName)
		destinationRule := destinationRuleInstance.Message.(*v1alpha3.DestinationRule)
		for _, portLevelSetting := range destinationRule.TrafficPolicy.PortLevelSettings {
			if portLevelSetting.Port.Number == port && portLevelSetting.Tls.Mode != 0 {
				return true
			}
		}
	}

	return false
}

func (serviceEntry *HTTPSOnHTTPAnalyzer) sendMessage(resource *resource.Instance, host string, index int, context analysis.Context) {
	message := msg.NewServiceEntryHTTPSTrafficOnHTTPPort(resource, host)

	if line, ok := util.ErrorLine(resource, fmt.Sprintf(util.ServiceEntryPort, index)); ok {
		message.Line = line
	}

	context.Report(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), message)
}
