// Copyright 2017 Istio Authors
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

package envoy

import (
	"fmt"
	"regexp"
	"sort"

	routing "istio.io/api/routing/v1alpha1"
	routing_v1alpha2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/model"
)

func buildHTTPRouteMatch(matches *routing.MatchCondition) *HTTPRoute {
	path := ""
	prefix := "/"
	var headers Headers
	if matches != nil && matches.Request != nil {
		for name, match := range matches.Request.Headers {
			if name == model.HeaderURI {
				// assumes `uri` condition is non-empty
				switch m := match.MatchType.(type) {
				case *routing.StringMatch_Exact:
					path = m.Exact
					prefix = ""
				case *routing.StringMatch_Prefix:
					path = ""
					prefix = m.Prefix
				case *routing.StringMatch_Regex:
					headers = append(headers, buildHeader(name, match))
				}
			} else {
				headers = append(headers, buildHeader(name, match))
			}
		}
		sort.Sort(headers)
	}
	return &HTTPRoute{
		Path:    path,
		Prefix:  prefix,
		Headers: headers,
	}
}

func buildHTTPRouteMatches(matches []*routing_v1alpha2.HTTPMatchRequest) []*HTTPRoute {
	routes := make([]*HTTPRoute, 0, len(matches))
	for _, match := range matches {
		route := &HTTPRoute{}
		for name, stringMatch := range match.Headers {
			// TODO: skip URI, scheme, method, authority headers?
			route.Headers = append(route.Headers, buildHeaderV1Alpha2(name, stringMatch))
		}

		if match.Uri != nil {
			switch m := match.Uri.MatchType.(type) {
			case *routing_v1alpha2.StringMatch_Exact:
				route.Path = m.Exact
			case *routing_v1alpha2.StringMatch_Prefix:
				route.Prefix = m.Prefix
			case *routing_v1alpha2.StringMatch_Regex:
				route.Regex = m.Regex
			}
		} else {
			route.Prefix = "/" // default
		}

		specialHeaders := []struct {
			Name string
			Match *routing_v1alpha2.StringMatch
		} {
			{":method", match.Method},
			{":authority", match.Authority},
			{":scheme", match.Scheme}, // FIXME: ensure header name is valid for HTTP 1.1
		}
		for _, specialHeader := range specialHeaders {
			if specialHeader.Match != nil {
				route.Headers = append(route.Headers,
					buildHeaderV1Alpha2(specialHeader.Name, specialHeader.Match))
			}
		}

		// TODO: match.DestinationPorts

		routes = append(routes, route)
	}
	return routes
}

func buildHeader(name string, match *routing.StringMatch) Header {
	header := Header{Name: name}

	switch m := match.MatchType.(type) {
	case *routing.StringMatch_Exact:
		header.Value = m.Exact
	case *routing.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		header.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		header.Regex = true
	case *routing.StringMatch_Regex:
		header.Value = m.Regex
		header.Regex = true
	}

	return header
}

func buildHeaderV1Alpha2(name string, match *routing_v1alpha2.StringMatch) Header {
	header := Header{Name: name}

	switch m := match.MatchType.(type) {
	case *routing_v1alpha2.StringMatch_Exact:
		header.Value = m.Exact
	case *routing_v1alpha2.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		header.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		header.Regex = true
	case *routing_v1alpha2.StringMatch_Regex:
		header.Value = m.Regex
		header.Regex = true
	}

	return header
}
