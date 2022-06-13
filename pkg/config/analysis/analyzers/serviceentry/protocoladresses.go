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

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
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
			collections.IstioMeshV1Alpha1MeshConfig.Name(),
		},
	}
}

func (serviceEntry *ProtocolAdressesAnalyzer) Analyze(context analysis.Context) {
	autoAllocated := false
	context.ForEach(collections.IstioMeshV1Alpha1MeshConfig.Name(), func(r *resource.Instance) bool {
		mc := r.Message.(*meshconfig.MeshConfig)
		if v, ok := mc.DefaultConfig.ProxyMetadata["ISTIO_META_DNS_CAPTURE"]; !ok || v != "true" {
			return true
		}
		if v, ok := mc.DefaultConfig.ProxyMetadata["ISTIO_META_DNS_AUTO_ALLOCATE"]; ok && v == "true" {
			autoAllocated = true
		}
		return true
	})

	context.ForEach(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), func(resource *resource.Instance) bool {
		serviceEntry.analyzeProtocolAddresses(resource, context, autoAllocated)
		return true
	})
}

func (serviceEntry *ProtocolAdressesAnalyzer) analyzeProtocolAddresses(r *resource.Instance, ctx analysis.Context, metaDNSAutoAllocated bool) {
	se := r.Message.(*v1alpha3.ServiceEntry)
	if se.Addresses == nil && !metaDNSAutoAllocated {
		for index, port := range se.Ports {
			if port.Protocol == "" || port.Protocol == "TCP" {
				message := msg.NewServiceEntryAddressesRequired(r)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.ServiceEntryPort, index)); ok {
					message.Line = line
				}

				ctx.Report(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), message)
			}
		}
	}
}
