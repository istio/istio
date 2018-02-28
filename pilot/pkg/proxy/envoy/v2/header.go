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

package v2

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/envoyproxy/go-control-plane/api"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"

	google_protobuf "github.com/gogo/protobuf/types"
	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
)

func buildHTTPHeaderMatcher(matches *routing.MatchCondition) []*api.HeaderMatcher {
	var out []*api.HeaderMatcher
	if matches != nil && matches.Request != nil {
		for name, match := range matches.Request.Headers {
			if name == model.HeaderURI {
				// assumes `uri` condition is non-empty
				switch match.MatchType.(type) {
				case *routing.StringMatch_Regex:
					out = append(out, buildHeaderMatcher(name, match))
				}
			} else {
				switch name {
				case model.HeaderAuthority, model.HeaderScheme, model.HeaderMethod:
					name = ":" + name // convert to Envoy header name
				}
				out = append(out, buildHeaderMatcher(name, match))
			}
		}
		// TODO(mostrowski): sort output
	}
	return out
}

func buildHTTPHeaderMatcherV2(match *routingv2.HTTPMatchRequest) []*api.HeaderMatcher {
	if match == nil {
		return nil
	}

	var out []*api.HeaderMatcher
	for name, stringMatch := range match.Headers {
		out = append(out, buildHeaderMatcherV2(name, stringMatch))
	}

	// guarantee ordering of headers
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Value < out[j].Value
		}
		return out[i].Name < out[j].Name
	})

	if match.Method != nil {
		out = append(out, buildHeaderMatcherV2(v1.HeaderMethod, match.Method))
	}

	if match.Authority != nil {
		out = append(out, buildHeaderMatcherV2(v1.HeaderAuthority, match.Authority))
	}

	if match.Scheme != nil {
		out = append(out, buildHeaderMatcherV2(v1.HeaderScheme, match.Scheme))
	}

	// TODO: match.DestinationPorts

	return out
}

func buildHeaderMatcher(name string, match *routing.StringMatch) *api.HeaderMatcher {
	header := &api.HeaderMatcher{Name: name}

	switch m := match.MatchType.(type) {
	case *routing.StringMatch_Exact:
		header.Value = m.Exact
	case *routing.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		header.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		header.Regex = &google_protobuf.BoolValue{true}
	case *routing.StringMatch_Regex:
		header.Value = m.Regex
		header.Regex = &google_protobuf.BoolValue{true}
	}

	return header
}

func buildHeaderMatcherV2(name string, match *routingv2.StringMatch) *api.HeaderMatcher {
	header := &api.HeaderMatcher{Name: name}

	switch m := match.MatchType.(type) {
	case *routingv2.StringMatch_Exact:
		header.Value = m.Exact
	case *routingv2.StringMatch_Prefix:
		// Envoy regex grammar is ECMA-262 (http://en.cppreference.com/w/cpp/regex/ecmascript)
		// Golang has a slightly different regex grammar
		header.Value = fmt.Sprintf("^%s.*", regexp.QuoteMeta(m.Prefix))
		header.Regex = &google_protobuf.BoolValue{true}
	case *routingv2.StringMatch_Regex:
		header.Value = m.Regex
		header.Regex = &google_protobuf.BoolValue{true}
	}

	return header
}
