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
	"istio.io/istio/galley/pkg/config/analysis/analyzers/virtualservice/matches"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// MatchesAnalyzer checks if virtual services
// associated with the same gateway have duplicate matches.
type MatchesAnalyzer struct{}

var _ analysis.Analyzer = &MatchesAnalyzer{}

// Metadata implements Analyzer
func (ma *MatchesAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.MatchesAnalyzer",
		Description: "Checks virtual services for unreachable and duplicate matches",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
		},
	}
}

// Analyze implements Analyzer
func (ma *MatchesAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		ma.analyzeVirtualService(r, ctx)
		return true
	})
}

func (ma *MatchesAnalyzer) analyzeVirtualService(r *resource.Instance, c analysis.Context) {
	vs := r.Message.(*v1alpha3.VirtualService)

	// Look for unreachable rules.  For each type (http, tls, tcp) ideally we would
	// check to see that no rule has any *MatchRequest that is a equal or subset of a previous
	// rules' *MatchRequest.  That *MatchRequest is "dead" and if a rule has no living
	// *MatchRequest the rule itself is dead.

	// Unfortunately we have no logic to compare *MatchRequests.
	// Instead we just check them for having identical JSON serializations.

	// Note that this code is only analyzes a single VirtualService.  TODO Look for duplicates
	// among all the VirtualSerices on a Gateway

	matches.AnalyzeUnreachableHTTPRules(vs.Http, func(ruleno, reason string) {
		c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewVirtualServiceUnreachableRule(r, ruleno, reason))
	}, func(ruleno, matchno, dupno string) {
		c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewVirtualServiceIneffectiveMatch(r, ruleno, matchno, dupno))
	})
	matches.AnalyzeUnreachableTCPRules(vs.Tcp, func(ruleno, reason string) {
		c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewVirtualServiceUnreachableRule(r, ruleno, reason))
	}, func(ruleno, matchno, dupno string) {
		c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewVirtualServiceIneffectiveMatch(r, ruleno, matchno, dupno))
	})
	matches.AnalyzeUnreachableTLSRules(vs.Tls, func(ruleno, reason string) {
		c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewVirtualServiceUnreachableRule(r, ruleno, reason))
	}, func(ruleno, matchno, dupno string) {
		c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewVirtualServiceIneffectiveMatch(r, ruleno, matchno, dupno))
	})
}
