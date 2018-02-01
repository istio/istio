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

package v1

import (
	"fmt"
	"regexp"
	"sort"

	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
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
				switch name {
				case model.HeaderAuthority, model.HeaderScheme, model.HeaderMethod:
					name = ":" + name // convert to Envoy header name
				}
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

func buildHTTPRouteMatchV2(match *routingv2.HTTPMatchRequest) *HTTPRoute {
	if match == nil {
		return &HTTPRoute{Prefix: "/"}
	}

	route := &HTTPRoute{}
	for name, stringMatch := range match.Headers {
		route.Headers = append(route.Headers, buildHeaderV2(name, stringMatch))
	}

	// guarantee ordering of headers
	sort.Slice(route.Headers, func(i, j int) bool {
		if route.Headers[i].Name == route.Headers[j].Name {
			return route.Headers[i].Value < route.Headers[j].Value
		}
		return route.Headers[i].Name < route.Headers[j].Name
	})

	if match.Uri != nil {
		switch m := match.Uri.MatchType.(type) {
		case *routingv2.StringMatch_Exact:
			route.Path = m.Exact
		case *routingv2.StringMatch_Prefix:
			route.Prefix = m.Prefix
		case *routingv2.StringMatch_Regex:
			route.Regex = m.Regex
		}
	} else {
		route.Prefix = "/" // default
	}

	if match.Method != nil {
		route.Headers = append(route.Headers, buildHeaderV2(headerMethod, match.Method))
	}

	if match.Authority != nil {
		route.Headers = append(route.Headers, buildHeaderV2(headerAuthority, match.Authority))
	}

	if match.Scheme != nil {
		route.Headers = append(route.Headers, buildHeaderV2(headerScheme, match.Scheme))
	}

	// TODO: match.DestinationPorts

	return route
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

func buildHeaderV2(name string, match *routingv2.StringMatch) Header {
	header := Header{Name: name}

	switch m := match.MatchType.(type) {
	case *routingv2.StringMatch_Exact:
		header.Value = m.Exact
	case *routingv2.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		header.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		header.Regex = true
	case *routingv2.StringMatch_Regex:
		header.Value = m.Regex
		header.Regex = true
	}

	return header
}
