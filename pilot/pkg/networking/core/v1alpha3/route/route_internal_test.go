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

package route

import (
	"reflect"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	xdsfault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/common/fault/v3"
	xdshttpfault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/ptypes/duration"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	authzmatcher "istio.io/istio/pilot/pkg/security/authz/matcher"
	authz "istio.io/istio/pilot/pkg/security/authz/model"
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
			name: "catch all prefix >= 1.14",
			route: &route.Route{
				Name: "catch-all",
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_PathSeparatedPrefix{
						PathSeparatedPrefix: "/",
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
					PathSpecifier: &route.RouteMatch_SafeRegex{
						SafeRegex: &matcher.RegexMatcher{
							EngineType: &matcher.RegexMatcher_GoogleRe2{GoogleRe2: &matcher.RegexMatcher_GoogleRE2{}},
							Regex:      "*",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "catch all prefix with headers",
			route: &route.Route{
				Name: "catch-all",
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{
						Prefix: "/",
					},
					Headers: []*route.HeaderMatcher{
						{
							Name: "Authentication",
							HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
								ExactMatch: "test",
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "uri regex with headers",
			route: &route.Route{
				Name: "non-catch-all",
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_SafeRegex{
						SafeRegex: &matcher.RegexMatcher{
							// nolint: staticcheck
							EngineType: &matcher.RegexMatcher_GoogleRe2{},
							Regex:      "*",
						},
					},
					Headers: []*route.HeaderMatcher{
						{
							Name: "Authentication",
							HeaderMatchSpecifier: &route.HeaderMatcher_StringMatch{
								StringMatch: &matcher.StringMatcher{
									MatchPattern: &matcher.StringMatcher_SafeRegex{
										SafeRegex: &matcher.RegexMatcher{
											EngineType: util.RegexEngine,
											Regex:      "*",
										},
									},
								},
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
					PathSpecifier: &route.RouteMatch_SafeRegex{
						SafeRegex: &matcher.RegexMatcher{
							// nolint: staticcheck
							EngineType: &matcher.RegexMatcher_GoogleRe2{},
							Regex:      "*",
						},
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
		http  *networking.HTTPMatchRequest
		match bool
	}{
		{
			name: "catch all virtual service",
			http: &networking.HTTPMatchRequest{
				Name: "catch-all",
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{
						Prefix: "/",
					},
				},
			},
			match: true,
		},
		{
			name: "uri regex",
			http: &networking.HTTPMatchRequest{
				Name: "regex-catch-all",
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{
						Regex: "*",
					},
				},
			},
			match: true,
		},
		{
			name: "uri regex with query params",
			http: &networking.HTTPMatchRequest{
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
			match: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			match := isCatchAllMatch(tt.http)
			if tt.match != match {
				t.Errorf("Expected a match=%v but got match=%v", tt.match, match)
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
	expectedCorsPolicy := &route.CorsPolicy{
		AllowOriginStringMatch: []*matcher.StringMatcher{
			{MatchPattern: &matcher.StringMatcher_Exact{Exact: "exact"}},
			{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: "prefix"}},
			{
				MatchPattern: &matcher.StringMatcher_SafeRegex{
					SafeRegex: &matcher.RegexMatcher{
						EngineType: util.RegexEngine,
						Regex:      "regex",
					},
				},
			},
		},
		EnabledSpecifier: &route.CorsPolicy_FilterEnabled{
			FilterEnabled: &core.RuntimeFractionalPercent{
				DefaultValue: &xdstype.FractionalPercent{
					Numerator:   100,
					Denominator: xdstype.FractionalPercent_HUNDRED,
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
		want  *core.RuntimeFractionalPercent
	}{
		{
			name: "zero mirror percent",
			route: &networking.HTTPRoute{
				Mirror:        &networking.Destination{},
				MirrorPercent: &wrappers.UInt32Value{Value: 0.0},
			},
			want: nil,
		},
		{
			name: "mirror with no value given",
			route: &networking.HTTPRoute{
				Mirror: &networking.Destination{},
			},
			want: &core.RuntimeFractionalPercent{
				DefaultValue: &xdstype.FractionalPercent{
					Numerator:   100,
					Denominator: xdstype.FractionalPercent_HUNDRED,
				},
			},
		},
		{
			name: "mirror with actual percent",
			route: &networking.HTTPRoute{
				Mirror:        &networking.Destination{},
				MirrorPercent: &wrappers.UInt32Value{Value: 50},
			},
			want: &core.RuntimeFractionalPercent{
				DefaultValue: &xdstype.FractionalPercent{
					Numerator:   50,
					Denominator: xdstype.FractionalPercent_HUNDRED,
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
			want: &core.RuntimeFractionalPercent{
				DefaultValue: &xdstype.FractionalPercent{
					Numerator:   500000,
					Denominator: xdstype.FractionalPercent_MILLION,
				},
			},
		},
		{
			name: "mirrorpercentage takes precedence when both are given",
			route: &networking.HTTPRoute{
				Mirror:           &networking.Destination{},
				MirrorPercent:    &wrappers.UInt32Value{Value: 40},
				MirrorPercentage: &networking.Percent{Value: 50.0},
			},
			want: &core.RuntimeFractionalPercent{
				DefaultValue: &xdstype.FractionalPercent{
					Numerator:   500000,
					Denominator: xdstype.FractionalPercent_MILLION,
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
		proxyLabels    labels.Instance
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

func TestTranslateMetadataMatch(t *testing.T) {
	cases := []struct {
		name string
		in   *networking.StringMatch
		want *matcher.MetadataMatcher
	}{
		{
			name: "@request.auth.claims",
		},
		{
			name: "@request.auth.claims-",
		},
		{
			name: "request.auth.claims.",
		},
		{
			name: "@request.auth.claims-",
		},
		{
			name: "@request.auth.claims-abc",
		},
		{
			name: "x-some-other-header",
		},
		{
			name: "@request.auth.claims.key1",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"key1"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims.key1.KEY2",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"key1", "KEY2"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims.key1-key2",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"key1-key2"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims.prefix",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Prefix{Prefix: "prefix"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"prefix"}, authzmatcher.StringMatcher("prefix*")),
		},
		{
			name: "@request.auth.claims.regex",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Regex{Regex: ".+?\\..+?\\..+?"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"regex"}, authzmatcher.StringMatcherRegex(".+?\\..+?\\..+?")),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := translateMetadataMatch(tc.name, tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Unexpected metadata matcher want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestTranslateFault(t *testing.T) {
	cases := []struct {
		name  string
		fault *networking.HTTPFaultInjection
		want  *xdshttpfault.HTTPFault
	}{
		{
			name: "http delay",
			fault: &networking.HTTPFaultInjection{
				Delay: &networking.HTTPFaultInjection_Delay{
					HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
						FixedDelay: &duration.Duration{
							Seconds: int64(3),
						},
					},
					Percentage: &networking.Percent{
						Value: float64(50),
					},
				},
			},
			want: &xdshttpfault.HTTPFault{
				Delay: &xdsfault.FaultDelay{
					Percentage: &xdstype.FractionalPercent{
						Numerator:   uint32(50 * 10000),
						Denominator: xdstype.FractionalPercent_MILLION,
					},
					FaultDelaySecifier: &xdsfault.FaultDelay_FixedDelay{
						FixedDelay: &duration.Duration{
							Seconds: int64(3),
						},
					},
				},
			},
		},
		{
			name: "grpc abort",
			fault: &networking.HTTPFaultInjection{
				Abort: &networking.HTTPFaultInjection_Abort{
					ErrorType: &networking.HTTPFaultInjection_Abort_GrpcStatus{
						GrpcStatus: "DEADLINE_EXCEEDED",
					},
					Percentage: &networking.Percent{
						Value: float64(50),
					},
				},
			},
			want: &xdshttpfault.HTTPFault{
				Abort: &xdshttpfault.FaultAbort{
					Percentage: &xdstype.FractionalPercent{
						Numerator:   uint32(50 * 10000),
						Denominator: xdstype.FractionalPercent_MILLION,
					},
					ErrorType: &xdshttpfault.FaultAbort_GrpcStatus{
						GrpcStatus: uint32(4),
					},
				},
			},
		},
		{
			name: "both delay and abort",
			fault: &networking.HTTPFaultInjection{
				Delay: &networking.HTTPFaultInjection_Delay{
					HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
						FixedDelay: &duration.Duration{
							Seconds: int64(3),
						},
					},
					Percentage: &networking.Percent{
						Value: float64(50),
					},
				},
				Abort: &networking.HTTPFaultInjection_Abort{
					ErrorType: &networking.HTTPFaultInjection_Abort_GrpcStatus{
						GrpcStatus: "DEADLINE_EXCEEDED",
					},
					Percentage: &networking.Percent{
						Value: float64(50),
					},
				},
			},
			want: &xdshttpfault.HTTPFault{
				Delay: &xdsfault.FaultDelay{
					Percentage: &xdstype.FractionalPercent{
						Numerator:   uint32(50 * 10000),
						Denominator: xdstype.FractionalPercent_MILLION,
					},
					FaultDelaySecifier: &xdsfault.FaultDelay_FixedDelay{
						FixedDelay: &duration.Duration{
							Seconds: int64(3),
						},
					},
				},
				Abort: &xdshttpfault.FaultAbort{
					Percentage: &xdstype.FractionalPercent{
						Numerator:   uint32(50 * 10000),
						Denominator: xdstype.FractionalPercent_MILLION,
					},
					ErrorType: &xdshttpfault.FaultAbort_GrpcStatus{
						GrpcStatus: uint32(4),
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tf := translateFault(tt.fault)
			if !reflect.DeepEqual(tf, tt.want) {
				t.Errorf("Unexpected translate fault want %v, got %v", tt.want, tf)
			}
		})
	}
}
