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

package virtualservice

import (
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// GatewayAnalyzer checks the gateways associated with each virtual service
type GatewayAnalyzer struct{}

var _ analysis.Analyzer = &GatewayAnalyzer{}

// Metadata implements Analyzer
func (s *GatewayAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.GatewayAnalyzer",
		Description: "Checks the gateways associated with each virtual service",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Gateways.Name(),
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
		},
	}
}

// Analyze implements Analyzer
func (s *GatewayAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		s.analyzeVirtualService(r, c)
		return true
	})
}

func (s *GatewayAnalyzer) analyzeVirtualService(r *resource.Instance, c analysis.Context) {
	vs := r.Message.(*v1alpha3.VirtualService)

	vsNs := r.Metadata.FullName.Namespace
	for _, gwName := range vs.Gateways {
		// This is a special-case accepted value
		if gwName == util.MeshGateway {
			continue
		}

		if !c.Exists(collections.IstioNetworkingV1Alpha3Gateways.Name(), resource.NewShortOrFullName(vsNs, gwName)) {
			c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), msg.NewReferencedResourceNotFound(r, "gateway", gwName))
		}
	}
}
