// Copyright 2018 Istio Authors
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

package route

import (
	"reflect"
	"testing"
	"time"

	envoy_api_v2_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_type "github.com/envoyproxy/go-control-plane/envoy/type"
	envoy_type_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"
)

func TestIsCatchAllMatch(t *testing.T) {
	cases := []struct {
		name  string
		match *networking.HTTPMatchRequest
		want  bool
	}{
		{
			name: "catch all prefix",
			match: &networking.HTTPMatchRequest{
				Name: "catch-all",
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{
						Prefix: "/",
					},
				},
			},
			want: true,
		},
		{
			name: "specific prefix match",
			match: &networking.HTTPMatchRequest{
				Name: "specific match",
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{
						Prefix: "/a",
					},
				},
			},
			want: false,
		},
		{
			name: "uri regex catch all",
			match: &networking.HTTPMatchRequest{
				Name: "regex-catch-all",
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{
						Regex: "*",
					},
				},
			},
			want: true,
		},
		{
			name: "uri regex with headers",
			match: &networking.HTTPMatchRequest{

				Name: "regex with headers",
				Headers: map[string]*networking.StringMatch{
					"Authentication": {
						MatchType: &networking.StringMatch_Regex{
							Regex: "Bearer .+?\\..+?\\..+?",
						},
					},
				},
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{
						Regex: "*",
					},
				},
			},
			want: false,
		},
		{
			name: "uri regex with query params",
			match: &networking.HTTPMatchRequest{
				Name: "regex with query params",
				QueryParams: map[string]*networking.StringMatch{
					"Authentication": {
						MatchType: &networking.StringMatch_Regex{
							Regex: "Bearer .+?\\..+?\\..+?",
						},
					},
				},
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{
						Regex: "*",
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			match := isCatchAllMatch(tt.match)
			if match != tt.want {
				t.Errorf("Unexpected catchAllMatch want %v, got %v", tt.want, match)
			}
		})
	}
}

func TestIsCatchAllRoute(t *testing.T) {
	cases := []struct {
		name  string
		route *route.Route
		want  bool
	}{
		{
			name: "catch all prefix",
			route: &route.Route{
				Name: "catch-all",
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{
						Prefix: "/",
					},
				},
			},
			want: true,
		},
		{
			name: "catch all regex",
			route: &route.Route{
				Name: "catch-all",
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Regex{
						Regex: "*",
					},
				},
			},
			want: true,
		},
		{
			name: "uri regex with headers",
			route: &route.Route{
				Name: "non-catch-all",
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Regex{
						Regex: "*",
					},
					Headers: []*route.HeaderMatcher{
						{
							Name: "Authentication",
							HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
								RegexMatch: "Bearer .+?\\..+?\\..+?",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "uri regex with query params",
			route: &route.Route{
				Name: "non-catch-all",
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Regex{
						Regex: "*",
					},
					QueryParameters: []*route.QueryParameterMatcher{
						{
							Name: "Authentication",
							QueryParameterMatchSpecifier: &route.QueryParameterMatcher_PresentMatch{
								PresentMatch: true,
							},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			catchall := isCatchAllRoute(tt.route)
			if catchall != tt.want {
				t.Errorf("Unexpected catchAllMatch want %v, got %v", tt.want, catchall)
			}
		})
	}
}

func TestCatchAllMatch(t *testing.T) {
	cases := []struct {
		name  string
		http  *networking.HTTPRoute
		match bool
	}{
		{
			name: "catch all virtual service",
			http: &networking.HTTPRoute{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "non-catch-all",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/route/v1",
							},
						},
					},
					{
						Name: "catch-all",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/",
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
			match: true,
		},
		{
			name: "virtual service with no matches",
			http: &networking.HTTPRoute{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
			match: false,
		},
		{
			name: "uri regex",
			http: &networking.HTTPRoute{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "regex-catch-all",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Regex{
								Regex: "*",
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
			match: true,
		},
		{
			name: "uri regex with query params",
			http: &networking.HTTPRoute{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "regex-catch-all",
						QueryParams: map[string]*networking.StringMatch{
							"Authentication": {
								MatchType: &networking.StringMatch_Regex{
									Regex: "Bearer .+?\\..+?\\..+?",
								},
							},
						},
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Regex{
								Regex: "*",
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
			match: false,
		},
		{
			name: "multiple prefix matches with one catch all match and one specific match",
			http: &networking.HTTPRoute{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "catch-all",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/",
							},
						},
						SourceLabels: map[string]string{
							"matchingNoSrc": "xxx",
						},
					},
					{
						Name: "specific match",
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{
								Prefix: "/a",
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
			match: true,
		},
		{
			name: "uri regex with query params",
			http: &networking.HTTPRoute{
				Match: []*networking.HTTPMatchRequest{
					{
						Name: "regex-catch-all",
						QueryParams: map[string]*networking.StringMatch{
							"Authentication": {
								MatchType: &networking.StringMatch_Regex{
									Regex: "Bearer .+?\\..+?\\..+?",
								},
							},
						},
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Regex{
								Regex: "*",
							},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "*.example.org",
							Port: &networking.PortSelector{
								Number: 8484,
							},
						},
						Weight: 100,
					},
				},
			},
			match: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			match := catchAllMatch(tt.http)
			if tt.match && match == nil {
				t.Errorf("Expected a catch all match but got nil")
			}
		})
	}
}

func TestTranslateCORSPolicy(t *testing.T) {
	tests := []struct {
		name string
		in   *networking.CorsPolicy
		want *route.CorsPolicy
	}{
		{
			name: "deprecated matcher",
			in: &networking.CorsPolicy{
				AllowOrigin:      []string{"foo"},
				AllowMethods:     []string{"allow-method-1", "allow-method-2"},
				AllowHeaders:     []string{"allow-header-1", "allow-header-2"},
				ExposeHeaders:    []string{"expose-header-1", "expose-header-2"},
				MaxAge:           types.DurationProto(time.Minute * 2),
				AllowCredentials: &types.BoolValue{Value: true},
			},
			want: &route.CorsPolicy{
				AllowOriginStringMatch: []*envoy_type_matcher.StringMatcher{{
					MatchPattern: &envoy_type_matcher.StringMatcher_Exact{Exact: "foo"},
				}},
				AllowMethods:     "allow-method-1,allow-method-2",
				AllowHeaders:     "allow-header-1,allow-header-2",
				ExposeHeaders:    "expose-header-1,expose-header-2",
				MaxAge:           "120",
				AllowCredentials: &wrappers.BoolValue{Value: true},
				EnabledSpecifier: &route.CorsPolicy_FilterEnabled{
					FilterEnabled: &envoy_api_v2_core.RuntimeFractionalPercent{
						DefaultValue: &envoy_type.FractionalPercent{
							Numerator:   100,
							Denominator: envoy_type.FractionalPercent_HUNDRED,
						},
					},
				},
			},
		},
		{
			name: "string matcher",
			in: &networking.CorsPolicy{
				AllowOrigins: []*networking.StringMatch{
					{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
					{MatchType: &networking.StringMatch_Prefix{Prefix: "prefix"}},
					{MatchType: &networking.StringMatch_Regex{Regex: "regex"}},
				},
			},
			want: &route.CorsPolicy{
				AllowOriginStringMatch: []*envoy_type_matcher.StringMatcher{
					{MatchPattern: &envoy_type_matcher.StringMatcher_Exact{Exact: "exact"}},
					{MatchPattern: &envoy_type_matcher.StringMatcher_Prefix{Prefix: "prefix"}},
					{
						MatchPattern: &envoy_type_matcher.StringMatcher_SafeRegex{
							SafeRegex: &envoy_type_matcher.RegexMatcher{
								EngineType: regexEngine,
								Regex:      "regex",
							},
						},
					},
				},
				EnabledSpecifier: &route.CorsPolicy_FilterEnabled{
					FilterEnabled: &envoy_api_v2_core.RuntimeFractionalPercent{
						DefaultValue: &envoy_type.FractionalPercent{
							Numerator:   100,
							Denominator: envoy_type.FractionalPercent_HUNDRED,
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translateCORSPolicy(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("translateCORSPolicy() = \n%v, want \n%v", got, tt.want)
			}
		})
	}
}
