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

	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"

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

	for i, route := range vs.GetHttp() {
		for j, m := range route.GetMatch() {

			pathInfo := []interface{}{i, j}

			analyzeStringMatch(r, m.GetUri(), ctx, "uri", pathInfo...)
			analyzeStringMatch(r, m.GetScheme(), ctx, "scheme", pathInfo...)
			analyzeStringMatch(r, m.GetMethod(), ctx, "method", pathInfo...)
			analyzeStringMatch(r, m.GetAuthority(), ctx, "authority", pathInfo...)
			for key, h := range m.GetHeaders() {
				pathInfo := append(pathInfo, key)
				analyzeStringMatch(r, h, ctx, "headers", pathInfo...)
			}
			for key, qp := range m.GetQueryParams() {
				pathInfo := append(pathInfo, key)
				analyzeStringMatch(r, qp, ctx, "queryParams", pathInfo...)
			}
			// We don't validate withoutHeaders, because they are undocumented
		}
		for j, origin := range route.GetCorsPolicy().GetAllowOrigins() {
			pathInfo := []interface{}{i, j}
			analyzeStringMatch(r, origin, ctx, "corsPolicy.allowOrigins", pathInfo...)
		}
	}
}

func analyzeStringMatch(r *resource.Instance, sm *v1alpha3.StringMatch, ctx analysis.Context, where string, p ...interface{}) {
	re := sm.GetRegex()
	if re == "" {
		return
	}

	_, err := regexp.Compile(re)
	if err == nil {
		return
	}

	// Get line number for different match field
	var line int
	var ok bool
	switch where {
	case "corsPolicy.allowOrigins":
		line, ok = util.ErrorLineForHTTPRegexAllowOrigins(r, p[0].(int), p[1].(int))
	case "queryParams":
		line, ok = util.ErrorLineForHTTPRegexHeaderAndQueryParams(r, p[0].(int), p[1].(int), where, p[2].(string))
	case "headers":
		line, ok = util.ErrorLineForHTTPRegexHeaderAndQueryParams(r, p[0].(int), p[1].(int), where, p[2].(string))
	case "uri":
		line, ok = util.ErrorLineForHTTPRegexURISchemeMethodAuthority(r, p[0].(int), p[1].(int), where)
	case "scheme":
		line, ok = util.ErrorLineForHTTPRegexURISchemeMethodAuthority(r, p[0].(int), p[1].(int), where)
	case "method":
		line, ok = util.ErrorLineForHTTPRegexURISchemeMethodAuthority(r, p[0].(int), p[1].(int), where)
	case "authority":
		line, ok = util.ErrorLineForHTTPRegexURISchemeMethodAuthority(r, p[0].(int), p[1].(int), where)
	}

	m := msg.NewInvalidRegexp(r, where, re, err.Error())

	if ok {
		m.Line = line
	}

	ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
}
