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

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/model"
)

func buildHTTPRouteMatch(matches *proxyconfig.MatchCondition) *HTTPRoute {
	path := ""
	prefix := "/"
	var headers Headers
	if matches != nil && matches.Request != nil {
		for name, match := range matches.Request.Headers {
			if name == model.HeaderURI {
				// assumes `uri` condition is non-empty
				switch m := match.MatchType.(type) {
				case *proxyconfig.StringMatch_Exact:
					path = m.Exact
					prefix = ""
				case *proxyconfig.StringMatch_Prefix:
					path = ""
					prefix = m.Prefix
				case *proxyconfig.StringMatch_Regex:
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

func buildHeader(name string, match *proxyconfig.StringMatch) Header {
	header := Header{Name: name}

	switch m := match.MatchType.(type) {
	case *proxyconfig.StringMatch_Exact:
		header.Value = m.Exact
	case *proxyconfig.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		header.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		header.Regex = true
	case *proxyconfig.StringMatch_Regex:
		header.Value = m.Regex
		header.Regex = true
	}

	return header
}
