// Copyright 2019 Istio Authors
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

package analyzers

import (
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
)

// SampleAnalyzer is a sample analyzer
type SampleAnalyzer struct {
}

var _ analysis.Analyzer = &SampleAnalyzer{}

// Name implements SampleAnalyzer
func (s *SampleAnalyzer) Name() string {
	return "sample"
}

// Analyze implements SampleAnalyzer
func (s *SampleAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		s.analyzeVirtualService(r, c)
		return true
	})
}

func (s *SampleAnalyzer) analyzeVirtualService(r *resource.Entry, c analysis.Context) {
	vs := r.Item.(*v1alpha3.VirtualService)

	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
	for _, gwName := range vs.Gateways {
		gw := c.Find(metadata.IstioNetworkingV1Alpha3Gateways, resource.NewName(ns, gwName))
		if gw == nil {
			c.Report(metadata.IstioNetworkingV1Alpha3Virtualservices, msg.ReferencedResourceNotFound(r, "gateway", gwName))
		}
	}
}
