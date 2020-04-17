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

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pkg/config/labels"
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
		route *routev3.Route
		want  bool
	}{
		{
			name: "catch all prefix",
			route: &routev3.Route{
				Name: "catch-all",
				Match: &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_Prefix{
						Prefix: "/",
					},
				},
			},
			want: true,
		},
		{
			name: "catch all regex",
			route: &routev3.Route{
				Name: "catch-all",
				Match: &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_HiddenEnvoyDeprecatedRegex{
						HiddenEnvoyDeprecatedRegex: "*",
					},
				},
			},
			want: true,
		},
		{
			name: "uri regex with headers",
			route: &routev3.Route{
				Name: "non-catch-all",
				Match: &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_HiddenEnvoyDeprecatedRegex{
						HiddenEnvoyDeprecatedRegex: "*",
					},
					Headers: []*routev3.HeaderMatcher{
						{
							Name: "Authentication",
							HeaderMatchSpecifier: &routev3.HeaderMatcher_HiddenEnvoyDeprecatedRegexMatch{
								HiddenEnvoyDeprecatedRegexMatch: "Bearer .+?\\..+?\\..+?",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "uri regex with query params",
			route: &routev3.Route{
				Name: "non-catch-all",
				Match: &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_HiddenEnvoyDeprecatedRegex{
						HiddenEnvoyDeprecatedRegex: "*",
					},
					QueryParameters: []*routev3.QueryParameterMatcher{
						{
							Name: "Authentication",
							QueryParameterMatchSpecifier: &routev3.QueryParameterMatcher_PresentMatch{
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
	corsPolicy := &networking.CorsPolicy{
		AllowOrigins: []*networking.StringMatch{
			{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			{MatchType: &networking.StringMatch_Prefix{Prefix: "prefix"}},
			{MatchType: &networking.StringMatch_Regex{Regex: "regex"}},
		},
	}
	expectedCorsPolicy := &routev3.CorsPolicy{
		AllowOriginStringMatch: []*matcherv3.StringMatcher{
			{MatchPattern: &matcherv3.StringMatcher_Exact{Exact: "exact"}},
			{MatchPattern: &matcherv3.StringMatcher_Prefix{Prefix: "prefix"}},
			{
				MatchPattern: &matcherv3.StringMatcher_SafeRegex{
					SafeRegex: &matcherv3.RegexMatcher{
						EngineType: regexEngine,
						Regex:      "regex",
					},
				},
			},
		},
		EnabledSpecifier: &routev3.CorsPolicy_FilterEnabled{
			FilterEnabled: &corev3.RuntimeFractionalPercent{
				DefaultValue: &typev3.FractionalPercent{
					Numerator:   100,
					Denominator: typev3.FractionalPercent_HUNDRED,
				},
			},
		},
	}
	if got := translateCORSPolicy(corsPolicy); !reflect.DeepEqual(got, expectedCorsPolicy) {
		t.Errorf("translateCORSPolicy() = \n%v, want \n%v", got, expectedCorsPolicy)
	}
}

func TestMirrorPercent(t *testing.T) {
	cases := []struct {
		name  string
		route *networking.HTTPRoute
		want  *corev3.RuntimeFractionalPercent
	}{
		{
			name: "zero mirror percent",
			route: &networking.HTTPRoute{
				Mirror:        &networking.Destination{},
				MirrorPercent: &types.UInt32Value{Value: 0.0},
			},
			want: nil,
		},
		{
			name: "mirror with no value given",
			route: &networking.HTTPRoute{
				Mirror: &networking.Destination{},
			},
			want: &corev3.RuntimeFractionalPercent{
				DefaultValue: &typev3.FractionalPercent{
					Numerator:   100,
					Denominator: typev3.FractionalPercent_HUNDRED,
				},
			},
		},
		{
			name: "mirror with actual percent",
			route: &networking.HTTPRoute{
				Mirror:        &networking.Destination{},
				MirrorPercent: &types.UInt32Value{Value: 50},
			},
			want: &corev3.RuntimeFractionalPercent{
				DefaultValue: &typev3.FractionalPercent{
					Numerator:   50,
					Denominator: typev3.FractionalPercent_HUNDRED,
				},
			},
		},
		{
			name: "zero mirror percentage",
			route: &networking.HTTPRoute{
				Mirror:           &networking.Destination{},
				MirrorPercentage: &networking.Percent{Value: 0.0},
			},
			want: nil,
		},
		{
			name: "mirrorpercentage with actual percent",
			route: &networking.HTTPRoute{
				Mirror:           &networking.Destination{},
				MirrorPercentage: &networking.Percent{Value: 50.0},
			},
			want: &corev3.RuntimeFractionalPercent{
				DefaultValue: &typev3.FractionalPercent{
					Numerator:   500000,
					Denominator: typev3.FractionalPercent_MILLION,
				},
			},
		},
		{
			name: "mirrorpercentage takes precedence when both are given",
			route: &networking.HTTPRoute{
				Mirror:           &networking.Destination{},
				MirrorPercent:    &types.UInt32Value{Value: 40},
				MirrorPercentage: &networking.Percent{Value: 50.0},
			},
			want: &corev3.RuntimeFractionalPercent{
				DefaultValue: &typev3.FractionalPercent{
					Numerator:   500000,
					Denominator: typev3.FractionalPercent_MILLION,
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mp := mirrorPercent(tt.route)
			if !reflect.DeepEqual(mp, tt.want) {
				t.Errorf("Unexpected mirro percent want %v, got %v", tt.want, mp)
			}
		})
	}
}

func TestSourceMatchHTTP(t *testing.T) {
	type args struct {
		match          *networking.HTTPMatchRequest
		proxyLabels    labels.Collection
		gatewayNames   map[string]bool
		proxyNamespace string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"source namespace match",
			args{
				match: &networking.HTTPMatchRequest{
					SourceNamespace: "foo",
				},
				proxyNamespace: "foo",
			},
			true,
		},
		{
			"source namespace not match",
			args{
				match: &networking.HTTPMatchRequest{
					SourceNamespace: "foo",
				},
				proxyNamespace: "bar",
			},
			false,
		},
		{
			"source namespace not match when empty",
			args{
				match: &networking.HTTPMatchRequest{
					SourceNamespace: "foo",
				},
				proxyNamespace: "",
			},
			false,
		},
		{
			"source namespace any",
			args{
				match:          &networking.HTTPMatchRequest{},
				proxyNamespace: "bar",
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sourceMatchHTTP(tt.args.match, tt.args.proxyLabels, tt.args.gatewayNames, tt.args.proxyNamespace); got != tt.want {
				t.Errorf("sourceMatchHTTP() = %v, want %v", got, tt.want)
			}
		})
	}
}
