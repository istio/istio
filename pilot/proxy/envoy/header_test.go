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
	"reflect"
	"testing"

	routing "istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/model"
)

func TestHTTPMatch(t *testing.T) {
	testCases := []struct {
		in   *routing.MatchCondition
		want *HTTPRoute
	}{
		{
			in:   &routing.MatchCondition{},
			want: &HTTPRoute{Path: "", Prefix: "/"},
		},
		{
			in: &routing.MatchCondition{
				Request: &routing.MatchRequest{
					Headers: map[string]*routing.StringMatch{
						model.HeaderURI: {MatchType: &routing.StringMatch_Exact{Exact: "/path"}},
					},
				},
			},
			want: &HTTPRoute{Path: "/path", Prefix: ""},
		},
		{
			in: &routing.MatchCondition{
				Request: &routing.MatchRequest{
					Headers: map[string]*routing.StringMatch{
						model.HeaderURI: {MatchType: &routing.StringMatch_Prefix{Prefix: "/prefix"}},
					},
				},
			},
			want: &HTTPRoute{Path: "", Prefix: "/prefix"},
		},
		{
			in: &routing.MatchCondition{
				Request: &routing.MatchRequest{
					Headers: map[string]*routing.StringMatch{
						model.HeaderURI: {MatchType: &routing.StringMatch_Regex{Regex: "/.*"}},
					},
				},
			},
			want: &HTTPRoute{Path: "", Prefix: "/", Headers: Headers{
				{Name: model.HeaderURI, Value: "/.*", Regex: true},
			}},
		},
		{
			in: &routing.MatchCondition{
				Request: &routing.MatchRequest{
					Headers: map[string]*routing.StringMatch{
						model.HeaderURI: {MatchType: &routing.StringMatch_Regex{Regex: "/.*"}},
						"cookie":        {MatchType: &routing.StringMatch_Prefix{Prefix: "user=jason?"}},
						"test":          {MatchType: &routing.StringMatch_Exact{Exact: "value"}},
					},
				},
			},
			want: &HTTPRoute{Path: "", Prefix: "/", Headers: Headers{
				{Name: "cookie", Value: "^user=jason\\?.*", Regex: true},
				{Name: "test", Value: "value"},
				{Name: model.HeaderURI, Value: "/.*", Regex: true},
			}},
		},
	}
	for _, test := range testCases {
		got := buildHTTPRouteMatch(test.in)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("buildHTTPRouteMatch(%#v) => got %#v, want %#v", test.in, got, test.want)
		}
	}
}
