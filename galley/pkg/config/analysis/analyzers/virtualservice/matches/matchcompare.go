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

package matches

import (
	"encoding/json"
	"fmt"

	"istio.io/api/networking/v1alpha3"
)

func AnalyzeUnreachableHTTPRules(routes []*v1alpha3.HTTPRoute,
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string)) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "only the last rule can have no matches")
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				reportIneffective(routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			reportUnreachable(routeName(route, rulen), "all matches used by prior rules")
		}
	}
}

// NOTE: This method identical to analyzeUnreachableHTTPRules.
func AnalyzeUnreachableTCPRules(routes []*v1alpha3.TCPRoute,
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string)) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "only the last rule can have no matches")
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				reportIneffective(routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			reportUnreachable(routeName(route, rulen), "all matches used by prior rules")
		}
	}
}

// NOTE: This method identical to analyzeUnreachableHTTPRules.
func AnalyzeUnreachableTLSRules(routes []*v1alpha3.TLSRoute,
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string)) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "only the last rule can have no matches")
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				reportIneffective(routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			reportUnreachable(routeName(route, rulen), "all matches used by prior rules")
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
