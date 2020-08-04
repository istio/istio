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
	"regexp"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// RegexAnalyzer checks all regexes in a virtual service
type RegexAnalyzer struct{}

var _ analysis.Analyzer = &RegexAnalyzer{}

// Metadata implements Analyzer
func (a *RegexAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.RegexAnalyzer",
		Description: "Checks regex syntax",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
		},
	}
}

// Analyze implements Analyzer
func (a *RegexAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		a.analyzeVirtualService(r, ctx)
		return true
	})
}

func (a *RegexAnalyzer) analyzeVirtualService(r *resource.Instance, ctx analysis.Context) {

	vs := r.Message.(*v1alpha3.VirtualService)

	for _, route := range vs.GetHttp() {
		for _, m := range route.GetMatch() {
			analyzeStringMatch(r, m.GetUri(), ctx, "uri")
			analyzeStringMatch(r, m.GetScheme(), ctx, "scheme")
			analyzeStringMatch(r, m.GetMethod(), ctx, "method")
			analyzeStringMatch(r, m.GetAuthority(), ctx, "authority")
			for _, h := range m.GetHeaders() {
				analyzeStringMatch(r, h, ctx, "headers")
			}
			for _, qp := range m.GetQueryParams() {
				analyzeStringMatch(r, qp, ctx, "queryParams")
			}
			// We don't validate withoutHeaders, because they are undocumented
		}
		for _, origin := range route.GetCorsPolicy().GetAllowOrigins() {
			analyzeStringMatch(r, origin, ctx, "corsPolicy.allowOrigins")
		}
	}
}

func analyzeStringMatch(r *resource.Instance, sm *v1alpha3.StringMatch, ctx analysis.Context, where string) {
	re := sm.GetRegex()
	if re == "" {
		return
	}

	_, err := regexp.Compile(re)
	if err == nil {
		return
	}

	ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
		msg.NewInvalidRegexp(r, where, re, err.Error()))
}
