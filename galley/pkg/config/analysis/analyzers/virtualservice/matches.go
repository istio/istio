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
	"encoding/json"
	"fmt"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
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

	ma.analyzeUnreachableHTTPRules(r, c, vs.Http)
	ma.analyzeUnreachableTCPRules(r, c, vs.Tcp)
	ma.analyzeUnreachableTLSRules(r, c, vs.Tls)
}

func (ma *MatchesAnalyzer) analyzeUnreachableHTTPRules(r *resource.Instance, c analysis.Context, routes []*v1alpha3.HTTPRoute) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
					msg.NewVirtualServiceUnreachableRule(r,
						routeName(route, rulen), "only the last rule can have no matches"))
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
					msg.NewVirtualServiceIneffectiveMatch(r, routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn)))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
				msg.NewVirtualServiceUnreachableRule(r, routeName(route, rulen), "all matches used by prior rules"))
		}
	}
}

// NOTE: This method identical to analyzeUnreachableHTTPRules.
func (ma *MatchesAnalyzer) analyzeUnreachableTCPRules(r *resource.Instance, c analysis.Context, routes []*v1alpha3.TCPRoute) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
					msg.NewVirtualServiceUnreachableRule(r,
						routeName(route, rulen), "only the last rule can have no matches"))
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
					msg.NewVirtualServiceIneffectiveMatch(r, routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn)))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
				msg.NewVirtualServiceUnreachableRule(r, routeName(route, rulen), "all matches used by prior rules"))
		}
	}
}

// NOTE: This method identical to analyzeUnreachableHTTPRules.
func (ma *MatchesAnalyzer) analyzeUnreachableTLSRules(r *resource.Instance, c analysis.Context, routes []*v1alpha3.TLSRoute) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
					msg.NewVirtualServiceUnreachableRule(r,
						routeName(route, rulen), "only the last rule can have no matches"))
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
					msg.NewVirtualServiceIneffectiveMatch(r, routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn)))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
				msg.NewVirtualServiceUnreachableRule(r, routeName(route, rulen), "all matches used by prior rules"))
		}
	}
}

func asJSON(data interface{}) string {
	// Remove the name, so we can create a serialization that only includes traffic routing config
	switch mr := data.(type) {
	case *v1alpha3.HTTPMatchRequest:
		if mr.Name != "" {
			unnamed := *mr
			unnamed.Name = ""
			data = &unnamed
		}
	}

	b, err := json.Marshal(data)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func routeName(route interface{}, routen int) string {
	switch r := route.(type) {
	case *v1alpha3.HTTPRoute:
		if r.Name != "" {
			return fmt.Sprintf("%q", r.Name)
		}

		// TCP and TLS routes have no names
	}

	return fmt.Sprintf("#%d", routen)
}

func requestName(match interface{}, matchn int) string {
	switch mr := match.(type) {
	case *v1alpha3.HTTPMatchRequest:
		if mr.Name != "" {
			return fmt.Sprintf("%q", mr.Name)
		}

		// TCP and TLS matches have no names
	}

	return fmt.Sprintf("#%d", matchn)
}
