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

type ProtocolAdressesAnalyzer struct{}

var _ analysis.Analyzer = &ProtocolAdressesAnalyzer{}

func (serviceEntry *ProtocolAdressesAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "serviceentry.Analyzer",
		Description: "Checks the validity of ServiceEntry",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Serviceentries.Name(),
		},
	}
}

func (serviceEntry *ProtocolAdressesAnalyzer) Analyze(context analysis.Context) {
	context.ForEach(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), func(resource *resource.Instance) bool {
		serviceEntry.analyzeProtocolAddresses(resource, context)
		return true
	})
}

func (serviceEntry *ProtocolAdressesAnalyzer) analyzeProtocolAddresses(resource *resource.Instance, context analysis.Context) {
	se := resource.Message.(*v1alpha3.ServiceEntry)

	if se.Addresses == nil {
		for index, port := range se.Ports {
			if port.Protocol == "" || port.Protocol == "TCP" {
				message := msg.NewServiceEntryAddressesRequired(resource)

				if line, ok := util.ErrorLine(resource, fmt.Sprintf(util.ServiceEntryPort, index)); ok {
					message.Line = line
				}

				context.Report(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), message)
			}
		}
	}
}
