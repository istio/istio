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
	cors "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	xdshttpfault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	authzmatcher "istio.io/istio/pilot/pkg/security/authz/matcher"
	authz "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/util/sets"
)

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
							Regex: ".*",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "catch all with empty match",
			route: &route.Route{
				Name:  "catch-all",
				Match: &route.RouteMatch{},
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
							Regex: ".*",
						},
					},
					Headers: []*route.HeaderMatcher{
						{
							Name: "Authentication",
							HeaderMatchSpecifier: &route.HeaderMatcher_StringMatch{
								StringMatch: &matcher.StringMatcher{
									MatchPattern: &matcher.StringMatcher_SafeRegex{
										SafeRegex: &matcher.RegexMatcher{
											Regex: ".*",
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
							Regex: ".*",
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
			catchall := IsCatchAllRoute(tt.route)
			if catchall != tt.want {
				t.Errorf("Unexpected catchAllMatch want %v, got %v", tt.want, catchall)
			}
		})
	}
}

func TestTranslateCORSPolicyForwardNotMatchingPreflights(t *testing.T) {
	node := &model.Proxy{}
	corsPolicy := &networking.CorsPolicy{
		AllowOrigins: []*networking.StringMatch{
			{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			{MatchType: &networking.StringMatch_Prefix{Prefix: "prefix"}},
			{MatchType: &networking.StringMatch_Regex{Regex: "regex"}},
		},
		UnmatchedPreflights: networking.CorsPolicy_IGNORE,
	}
	expectedCorsPolicy := &cors.CorsPolicy{
		ForwardNotMatchingPreflights: wrapperspb.Bool(false),
		AllowOriginStringMatch: []*matcher.StringMatcher{
			{MatchPattern: &matcher.StringMatcher_Exact{Exact: "exact"}},
			{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: "prefix"}},
			{
				MatchPattern: &matcher.StringMatcher_SafeRegex{
					SafeRegex: &matcher.RegexMatcher{
						Regex: "regex",
					},
				},
			},
		},
		FilterEnabled: &core.RuntimeFractionalPercent{
			DefaultValue: &xdstype.FractionalPercent{
				Numerator:   100,
				Denominator: xdstype.FractionalPercent_HUNDRED,
			},
		},
	}
	if got := TranslateCORSPolicy(node, corsPolicy); !reflect.DeepEqual(got, expectedCorsPolicy) {
		t.Errorf("TranslateCORSPolicy() = \n%v, want \n%v", got, expectedCorsPolicy)
	}
}

func TestTranslateCORSPolicy(t *testing.T) {
	node := &model.Proxy{}
	corsPolicy := &networking.CorsPolicy{
		AllowOrigins: []*networking.StringMatch{
			{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			{MatchType: &networking.StringMatch_Prefix{Prefix: "prefix"}},
			{MatchType: &networking.StringMatch_Regex{Regex: "regex"}},
		},
	}
	expectedCorsPolicy := &cors.CorsPolicy{
		AllowOriginStringMatch: []*matcher.StringMatcher{
			{MatchPattern: &matcher.StringMatcher_Exact{Exact: "exact"}},
			{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: "prefix"}},
			{
				MatchPattern: &matcher.StringMatcher_SafeRegex{
					SafeRegex: &matcher.RegexMatcher{
						Regex: "regex",
					},
				},
			},
		},
		ForwardNotMatchingPreflights: wrapperspb.Bool(true),
		FilterEnabled: &core.RuntimeFractionalPercent{
			DefaultValue: &xdstype.FractionalPercent{
				Numerator:   100,
				Denominator: xdstype.FractionalPercent_HUNDRED,
			},
		},
	}
	if got := TranslateCORSPolicy(node, corsPolicy); !reflect.DeepEqual(got, expectedCorsPolicy) {
		t.Errorf("TranslateCORSPolicy() = \n%v, want \n%v", got, expectedCorsPolicy)
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
				MirrorPercent: &wrapperspb.UInt32Value{Value: 0.0},
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
				MirrorPercent: &wrapperspb.UInt32Value{Value: 50},
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
				MirrorPercent:    &wrapperspb.UInt32Value{Value: 40},
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
			mp := MirrorPercent(tt.route)
			if !reflect.DeepEqual(mp, tt.want) {
				t.Errorf("Unexpected mirror percent want %v, got %v", tt.want, mp)
			}
		})
	}
}

func TestMirrorPercentByPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy *networking.HTTPMirrorPolicy
		want   *core.RuntimeFractionalPercent
	}{
		{
			name: "mirror with no value given",
			policy: &networking.HTTPMirrorPolicy{
				Destination: &networking.Destination{},
			},
			want: &core.RuntimeFractionalPercent{
				DefaultValue: &xdstype.FractionalPercent{
					Numerator:   100,
					Denominator: xdstype.FractionalPercent_HUNDRED,
				},
			},
		},
		{
			name: "zero mirror percentage",
			policy: &networking.HTTPMirrorPolicy{
				Destination: &networking.Destination{},
				Percentage:  &networking.Percent{Value: 0.0},
			},
			want: nil,
		},
		{
			name: "mirrorpercentage with actual percent",
			policy: &networking.HTTPMirrorPolicy{
				Destination: &networking.Destination{},
				Percentage:  &networking.Percent{Value: 50.0},
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
			mp := MirrorPercentByPolicy(tt.policy)
			if !reflect.DeepEqual(mp, tt.want) {
				t.Errorf("Unexpected mirror percent want %v, got %v", tt.want, mp)
			}
		})
	}
}

func TestSourceMatchHTTP(t *testing.T) {
	type args struct {
		match          *networking.HTTPMatchRequest
		proxyLabels    labels.Instance
		gatewayNames   sets.String
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
			name: "@request.auth.claims.",
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
		{
			name: "@request.auth.claims[key1",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
		},
		{
			name: "@request.auth.claims]key1",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
		},
		{
			// have `@request.auth.claims` prefix, but no separator
			name: "@request.auth.claimskey1",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
		},
		{
			// if `.` exists, use `.` as separator
			name: "@request.auth.claims.[key1]",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"[key1]"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims[key1]",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"key1"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims[key1][key2]",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"key1", "key2"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims[test-issuer-2@istio.io]",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"test-issuer-2@istio.io"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims[test-issuer-2@istio.io][key1]",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"test-issuer-2@istio.io", "key1"}, authzmatcher.StringMatcher("exact")),
		},
		{
			name: "@request.auth.claims[test-issuer-2@istio.io][key1]",
			in:   &networking.StringMatch{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
			want: authz.MetadataMatcherForJWTClaims([]string{"test-issuer-2@istio.io", "key1"}, authzmatcher.StringMatcher("exact")),
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
						FixedDelay: &durationpb.Duration{
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
						FixedDelay: &durationpb.Duration{
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
						FixedDelay: &durationpb.Duration{
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
						FixedDelay: &durationpb.Duration{
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
			tf := TranslateFault(tt.fault)
			if !reflect.DeepEqual(tf, tt.want) {
				t.Errorf("Unexpected translate fault want %v, got %v", tt.want, tf)
			}
		})
	}
}
