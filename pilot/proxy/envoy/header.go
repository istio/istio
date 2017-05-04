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

// Functions related to header configuration in envoy: match conditions

package envoy

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
)

func buildURIPathPrefix(matches *proxyconfig.MatchCondition) (path string, prefix string) {
	path = ""
	prefix = "/"
	if matches != nil {
		if uri, ok := matches.HttpHeaders[model.HeaderURI]; ok {
			switch m := uri.MatchType.(type) {
			case *proxyconfig.StringMatch_Exact:
				path = m.Exact
				prefix = ""
			case *proxyconfig.StringMatch_Prefix:
				path = ""
				prefix = m.Prefix
			case *proxyconfig.StringMatch_Regex:
				glog.Warningf("Unsupported uri match condition: regex")
			}
		}
	}
	return
}

// TODO: test sorting, translation

// buildHeaders skips over URI as it has special meaning
func buildHeaders(matches *proxyconfig.MatchCondition) []Header {
	if matches == nil {
		return nil
	}

	headers := make([]Header, 0, len(matches.HttpHeaders))
	for name, match := range matches.HttpHeaders {
		if name != model.HeaderURI {
			headers = append(headers, buildHeader(name, match))
		}
	}
	sort.Sort(Headers(headers))
	return headers
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
