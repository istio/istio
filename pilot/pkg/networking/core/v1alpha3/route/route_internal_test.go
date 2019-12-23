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
	"testing"

	networking "istio.io/api/networking/v1alpha3"

	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
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
