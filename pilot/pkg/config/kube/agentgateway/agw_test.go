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

package agentgateway

import (
	"testing"

	"github.com/agentgateway/agentgateway/go/api"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
)

func TestCreateAgwPathMatch(t *testing.T) {
	tests := []struct {
		name      string
		match     gatewayv1.HTTPRouteMatch
		want      *api.PathMatch
		wantError bool
	}{
		{
			name:  "nil path returns nil",
			match: gatewayv1.HTTPRouteMatch{Path: nil},
			want:  nil,
		},
		{
			name: "prefix match with default type",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Value: ptr.Of("/api/v1"),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_PathPrefix{PathPrefix: "/api/v1"},
			},
		},
		{
			name: "prefix match strips trailing slash",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchPathPrefix),
					Value: ptr.Of("/api/v1/"),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_PathPrefix{PathPrefix: "/api/v1"},
			},
		},
		{
			name: "prefix root path preserved",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchPathPrefix),
					Value: ptr.Of("/"),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_PathPrefix{PathPrefix: "/"},
			},
		},
		{
			name: "exact match preserves trailing slash",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchExact),
					Value: ptr.Of("/api/v1/"),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_Exact{Exact: "/api/v1/"},
			},
		},
		{
			name: "exact match",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchExact),
					Value: ptr.Of("/healthz"),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_Exact{Exact: "/healthz"},
			},
		},
		{
			name: "regex match",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchRegularExpression),
					Value: ptr.Of("/api/v[0-9]+/.*"),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_Regex{Regex: "/api/v[0-9]+/.*"},
			},
		},
		{
			name: "nil value coerces to root prefix",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type: ptr.Of(gatewayv1.PathMatchPathPrefix),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_PathPrefix{PathPrefix: "/"},
			},
		},
		{
			name: "empty value coerces to root prefix",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchPathPrefix),
					Value: ptr.Of(""),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_PathPrefix{PathPrefix: "/"},
			},
		},
		{
			name: "missing leading slash is prepended",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchExact),
					Value: ptr.Of("foo/bar"),
				},
			},
			want: &api.PathMatch{
				Kind: &api.PathMatch_Exact{Exact: "/foo/bar"},
			},
		},
		{
			name: "unsupported path match type returns error",
			match: gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  ptr.Of(gatewayv1.PathMatchType("Invalid")),
					Value: ptr.Of("/foo"),
				},
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cond := CreateAgwPathMatch(tt.match)
			if tt.wantError {
				if cond == nil || cond.error == nil {
					t.Fatal("expected error condition but got nil")
				}
				return
			}
			if cond != nil && cond.error != nil {
				t.Fatalf("unexpected error: %v", cond.error.Message)
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwHeadersMatch(t *testing.T) {
	tests := []struct {
		name      string
		match     gatewayv1.HTTPRouteMatch
		want      []*api.HeaderMatch
		wantError bool
	}{
		{
			name:  "no headers returns nil",
			match: gatewayv1.HTTPRouteMatch{},
			want:  nil,
		},
		{
			name: "single exact header match (default type)",
			match: gatewayv1.HTTPRouteMatch{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{Name: "Content-Type", Value: "application/json"},
				},
			},
			want: []*api.HeaderMatch{
				{Name: "Content-Type", Value: &api.HeaderMatch_Exact{Exact: "application/json"}},
			},
		},
		{
			name: "explicit exact header match",
			match: gatewayv1.HTTPRouteMatch{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{
						Type:  ptr.Of(gatewayv1.HeaderMatchExact),
						Name:  "X-Request-ID",
						Value: "abc-123",
					},
				},
			},
			want: []*api.HeaderMatch{
				{Name: "X-Request-ID", Value: &api.HeaderMatch_Exact{Exact: "abc-123"}},
			},
		},
		{
			name: "regex header match",
			match: gatewayv1.HTTPRouteMatch{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{
						Type:  ptr.Of(gatewayv1.HeaderMatchRegularExpression),
						Name:  "User-Agent",
						Value: "Mozilla/.*",
					},
				},
			},
			want: []*api.HeaderMatch{
				{Name: "User-Agent", Value: &api.HeaderMatch_Regex{Regex: "Mozilla/.*"}},
			},
		},
		{
			name: "multiple headers mixed types",
			match: gatewayv1.HTTPRouteMatch{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{
						Type:  ptr.Of(gatewayv1.HeaderMatchExact),
						Name:  "X-Env",
						Value: "production",
					},
					{
						Type:  ptr.Of(gatewayv1.HeaderMatchRegularExpression),
						Name:  "X-Version",
						Value: "v[0-9]+",
					},
				},
			},
			want: []*api.HeaderMatch{
				{Name: "X-Env", Value: &api.HeaderMatch_Exact{Exact: "production"}},
				{Name: "X-Version", Value: &api.HeaderMatch_Regex{Regex: "v[0-9]+"}},
			},
		},
		{
			name: "unsupported header match type returns error",
			match: gatewayv1.HTTPRouteMatch{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{
						Type:  ptr.Of(gatewayv1.HeaderMatchType("Invalid")),
						Name:  "X-Bad",
						Value: "foo",
					},
				},
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cond := CreateAgwHeadersMatch(tt.match)
			if tt.wantError {
				if cond == nil || cond.error == nil {
					t.Fatal("expected error condition but got nil")
				}
				return
			}
			if cond != nil && cond.error != nil {
				t.Fatalf("unexpected error: %v", cond.error.Message)
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwQueryMatch(t *testing.T) {
	tests := []struct {
		name      string
		match     gatewayv1.HTTPRouteMatch
		want      []*api.QueryMatch
		wantError bool
	}{
		{
			name:  "no query params returns nil",
			match: gatewayv1.HTTPRouteMatch{},
			want:  nil,
		},
		{
			name: "exact query param match (default type)",
			match: gatewayv1.HTTPRouteMatch{
				QueryParams: []gatewayv1.HTTPQueryParamMatch{
					{Name: "version", Value: "2"},
				},
			},
			want: []*api.QueryMatch{
				{Name: "version", Value: &api.QueryMatch_Exact{Exact: "2"}},
			},
		},
		{
			name: "regex query param match",
			match: gatewayv1.HTTPRouteMatch{
				QueryParams: []gatewayv1.HTTPQueryParamMatch{
					{
						Type:  ptr.Of(gatewayv1.QueryParamMatchRegularExpression),
						Name:  "token",
						Value: "[a-f0-9]{32}",
					},
				},
			},
			want: []*api.QueryMatch{
				{Name: "token", Value: &api.QueryMatch_Regex{Regex: "[a-f0-9]{32}"}},
			},
		},
		{
			name: "multiple query params",
			match: gatewayv1.HTTPRouteMatch{
				QueryParams: []gatewayv1.HTTPQueryParamMatch{
					{
						Type:  ptr.Of(gatewayv1.QueryParamMatchExact),
						Name:  "page",
						Value: "1",
					},
					{
						Type:  ptr.Of(gatewayv1.QueryParamMatchRegularExpression),
						Name:  "sort",
						Value: "(asc|desc)",
					},
				},
			},
			want: []*api.QueryMatch{
				{Name: "page", Value: &api.QueryMatch_Exact{Exact: "1"}},
				{Name: "sort", Value: &api.QueryMatch_Regex{Regex: "(asc|desc)"}},
			},
		},
		{
			name: "unsupported query param match type returns error",
			match: gatewayv1.HTTPRouteMatch{
				QueryParams: []gatewayv1.HTTPQueryParamMatch{
					{
						Type:  ptr.Of(gatewayv1.QueryParamMatchType("Invalid")),
						Name:  "bad",
						Value: "val",
					},
				},
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cond := CreateAgwQueryMatch(tt.match)
			if tt.wantError {
				if cond == nil || cond.error == nil {
					t.Fatal("expected error condition but got nil")
				}
				return
			}
			if cond != nil && cond.error != nil {
				t.Fatalf("unexpected error: %v", cond.error.Message)
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwMethodMatch(t *testing.T) {
	tests := []struct {
		name      string
		match     gatewayv1.HTTPRouteMatch
		want      *api.MethodMatch
		wantError bool
	}{
		{
			name:  "nil method returns nil",
			match: gatewayv1.HTTPRouteMatch{},
			want:  nil,
		},
		{
			name: "GET method",
			match: gatewayv1.HTTPRouteMatch{
				Method: ptr.Of(gatewayv1.HTTPMethodGet),
			},
			want: &api.MethodMatch{Exact: "GET"},
		},
		{
			name: "POST method",
			match: gatewayv1.HTTPRouteMatch{
				Method: ptr.Of(gatewayv1.HTTPMethodPost),
			},
			want: &api.MethodMatch{Exact: "POST"},
		},
		{
			name: "DELETE method",
			match: gatewayv1.HTTPRouteMatch{
				Method: ptr.Of(gatewayv1.HTTPMethodDelete),
			},
			want: &api.MethodMatch{Exact: "DELETE"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cond := CreateAgwMethodMatch(tt.match)
			if tt.wantError {
				if cond == nil || cond.error == nil {
					t.Fatal("expected error condition but got nil")
				}
				return
			}
			if cond != nil && cond.error != nil {
				t.Fatalf("unexpected error: %v", cond.error.Message)
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwHeadersFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter *gatewayv1.HTTPHeaderFilter
		want   *api.HeaderModifier
	}{
		{
			name:   "nil filter returns nil",
			filter: nil,
			want:   nil,
		},
		{
			name: "add headers",
			filter: &gatewayv1.HTTPHeaderFilter{
				Add: []gatewayv1.HTTPHeader{
					{Name: "X-Request-ID", Value: "123"},
					{Name: "X-Forwarded-For", Value: "10.0.0.1"},
				},
			},
			want: &api.HeaderModifier{
				Add: []*api.Header{
					{Name: "X-Request-ID", Value: "123"},
					{Name: "X-Forwarded-For", Value: "10.0.0.1"},
				},
				Set:    nil,
				Remove: nil,
			},
		},
		{
			name: "set headers",
			filter: &gatewayv1.HTTPHeaderFilter{
				Set: []gatewayv1.HTTPHeader{
					{Name: "Cache-Control", Value: "no-cache"},
				},
			},
			want: &api.HeaderModifier{
				Add: nil,
				Set: []*api.Header{
					{Name: "Cache-Control", Value: "no-cache"},
				},
				Remove: nil,
			},
		},
		{
			name: "remove headers",
			filter: &gatewayv1.HTTPHeaderFilter{
				Remove: []string{"X-Internal-Header", "X-Debug"},
			},
			want: &api.HeaderModifier{
				Add:    nil,
				Set:    nil,
				Remove: []string{"X-Internal-Header", "X-Debug"},
			},
		},
		{
			name: "combined add set remove",
			filter: &gatewayv1.HTTPHeaderFilter{
				Add: []gatewayv1.HTTPHeader{
					{Name: "X-Added", Value: "true"},
				},
				Set: []gatewayv1.HTTPHeader{
					{Name: "X-Replaced", Value: "new-value"},
				},
				Remove: []string{"X-Removed"},
			},
			want: &api.HeaderModifier{
				Add: []*api.Header{
					{Name: "X-Added", Value: "true"},
				},
				Set: []*api.Header{
					{Name: "X-Replaced", Value: "new-value"},
				},
				Remove: []string{"X-Removed"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateAgwHeadersFilter(tt.filter)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwRewriteFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter *gatewayv1.HTTPURLRewriteFilter
		want   *api.TrafficPolicySpec
	}{
		{
			name:   "nil filter returns nil",
			filter: nil,
			want:   nil,
		},
		{
			name: "hostname rewrite only",
			filter: &gatewayv1.HTTPURLRewriteFilter{
				Hostname: ptr.Of(gatewayv1.PreciseHostname("new.example.com")),
			},
			want: &api.TrafficPolicySpec{
				Kind: &api.TrafficPolicySpec_UrlRewrite{
					UrlRewrite: &api.UrlRewrite{
						Host: "new.example.com",
					},
				},
			},
		},
		{
			name: "prefix path rewrite",
			filter: &gatewayv1.HTTPURLRewriteFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:               gatewayv1.PrefixMatchHTTPPathModifier,
					ReplacePrefixMatch: ptr.Of("/v2"),
				},
			},
			want: &api.TrafficPolicySpec{
				Kind: &api.TrafficPolicySpec_UrlRewrite{
					UrlRewrite: &api.UrlRewrite{
						Path: &api.UrlRewrite_Prefix{Prefix: "/v2"},
					},
				},
			},
		},
		{
			name: "full path rewrite",
			filter: &gatewayv1.HTTPURLRewriteFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:            gatewayv1.FullPathHTTPPathModifier,
					ReplaceFullPath: ptr.Of("/new/path"),
				},
			},
			want: &api.TrafficPolicySpec{
				Kind: &api.TrafficPolicySpec_UrlRewrite{
					UrlRewrite: &api.UrlRewrite{
						Path: &api.UrlRewrite_Full{Full: "/new/path"},
					},
				},
			},
		},
		{
			name: "hostname and prefix path rewrite",
			filter: &gatewayv1.HTTPURLRewriteFilter{
				Hostname: ptr.Of(gatewayv1.PreciseHostname("api.example.com")),
				Path: &gatewayv1.HTTPPathModifier{
					Type:               gatewayv1.PrefixMatchHTTPPathModifier,
					ReplacePrefixMatch: ptr.Of("/api/v2"),
				},
			},
			want: &api.TrafficPolicySpec{
				Kind: &api.TrafficPolicySpec_UrlRewrite{
					UrlRewrite: &api.UrlRewrite{
						Host: "api.example.com",
						Path: &api.UrlRewrite_Prefix{Prefix: "/api/v2"},
					},
				},
			},
		},
		{
			name: "trailing slash stripped from prefix rewrite",
			filter: &gatewayv1.HTTPURLRewriteFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:               gatewayv1.PrefixMatchHTTPPathModifier,
					ReplacePrefixMatch: ptr.Of("/v2/"),
				},
			},
			want: &api.TrafficPolicySpec{
				Kind: &api.TrafficPolicySpec_UrlRewrite{
					UrlRewrite: &api.UrlRewrite{
						Path: &api.UrlRewrite_Prefix{Prefix: "/v2"},
					},
				},
			},
		},
		{
			name: "trailing slash stripped from full path rewrite",
			filter: &gatewayv1.HTTPURLRewriteFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:            gatewayv1.FullPathHTTPPathModifier,
					ReplaceFullPath: ptr.Of("/new/path/"),
				},
			},
			want: &api.TrafficPolicySpec{
				Kind: &api.TrafficPolicySpec_UrlRewrite{
					UrlRewrite: &api.UrlRewrite{
						Path: &api.UrlRewrite_Full{Full: "/new/path"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateAgwRewriteFilter(tt.filter)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwRedirectFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter *gatewayv1.HTTPRequestRedirectFilter
		want   *api.RequestRedirect
	}{
		{
			name:   "nil filter returns nil",
			filter: nil,
			want:   nil,
		},
		{
			name: "scheme redirect",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				Scheme: ptr.Of("https"),
			},
			want: &api.RequestRedirect{
				Scheme: "https",
			},
		},
		{
			name: "hostname redirect",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				Hostname: ptr.Of(gatewayv1.PreciseHostname("new.example.com")),
			},
			want: &api.RequestRedirect{
				Host: "new.example.com",
			},
		},
		{
			name: "port redirect",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				Port: ptr.Of(gatewayv1.PortNumber(8443)),
			},
			want: &api.RequestRedirect{
				Port: 8443,
			},
		},
		{
			name: "status code 301 permanent redirect",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				StatusCode: ptr.Of(301),
			},
			want: &api.RequestRedirect{
				Status: 301,
			},
		},
		{
			name: "full redirect with scheme hostname port and status",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				Scheme:     ptr.Of("https"),
				Hostname:   ptr.Of(gatewayv1.PreciseHostname("secure.example.com")),
				Port:       ptr.Of(gatewayv1.PortNumber(443)),
				StatusCode: ptr.Of(301),
			},
			want: &api.RequestRedirect{
				Scheme: "https",
				Host:   "secure.example.com",
				Port:   443,
				Status: 301,
			},
		},
		{
			name: "redirect with prefix path replacement",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:               gatewayv1.PrefixMatchHTTPPathModifier,
					ReplacePrefixMatch: ptr.Of("/new-prefix"),
				},
				StatusCode: ptr.Of(302),
			},
			want: &api.RequestRedirect{
				Status: 302,
				Path:   &api.RequestRedirect_Prefix{Prefix: "/new-prefix"},
			},
		},
		{
			name: "redirect with full path replacement",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:            gatewayv1.FullPathHTTPPathModifier,
					ReplaceFullPath: ptr.Of("/completely/new"),
				},
				StatusCode: ptr.Of(308),
			},
			want: &api.RequestRedirect{
				Status: 308,
				Path:   &api.RequestRedirect_Full{Full: "/completely/new"},
			},
		},
		{
			name: "redirect path trailing slash stripped",
			filter: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:               gatewayv1.PrefixMatchHTTPPathModifier,
					ReplacePrefixMatch: ptr.Of("/api/"),
				},
			},
			want: &api.RequestRedirect{
				Path: &api.RequestRedirect_Prefix{Prefix: "/api"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateAgwRedirectFilter(tt.filter)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwGRPCHeadersMatch(t *testing.T) {
	tests := []struct {
		name      string
		match     gatewayv1.GRPCRouteMatch
		want      []*api.HeaderMatch
		wantError bool
	}{
		{
			name:  "no headers returns nil",
			match: gatewayv1.GRPCRouteMatch{},
			want:  nil,
		},
		{
			name: "exact gRPC header match (default type)",
			match: gatewayv1.GRPCRouteMatch{
				Headers: []gatewayv1.GRPCHeaderMatch{
					{Name: "grpc-timeout", Value: "10S"},
				},
			},
			want: []*api.HeaderMatch{
				{Name: "grpc-timeout", Value: &api.HeaderMatch_Exact{Exact: "10S"}},
			},
		},
		{
			name: "regex gRPC header match",
			match: gatewayv1.GRPCRouteMatch{
				Headers: []gatewayv1.GRPCHeaderMatch{
					{
						Type:  ptr.Of(gatewayv1.GRPCHeaderMatchRegularExpression),
						Name:  "x-request-id",
						Value: "[a-f0-9-]{36}",
					},
				},
			},
			want: []*api.HeaderMatch{
				{Name: "x-request-id", Value: &api.HeaderMatch_Regex{Regex: "[a-f0-9-]{36}"}},
			},
		},
		{
			name: "multiple gRPC header matches",
			match: gatewayv1.GRPCRouteMatch{
				Headers: []gatewayv1.GRPCHeaderMatch{
					{
						Type:  ptr.Of(gatewayv1.GRPCHeaderMatchExact),
						Name:  "content-type",
						Value: "application/grpc",
					},
					{
						Type:  ptr.Of(gatewayv1.GRPCHeaderMatchRegularExpression),
						Name:  "authorization",
						Value: "Bearer .*",
					},
				},
			},
			want: []*api.HeaderMatch{
				{Name: "content-type", Value: &api.HeaderMatch_Exact{Exact: "application/grpc"}},
				{Name: "authorization", Value: &api.HeaderMatch_Regex{Regex: "Bearer .*"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cond := CreateAgwGRPCHeadersMatch(tt.match)
			if tt.wantError {
				if cond == nil || cond.error == nil {
					t.Fatal("expected error condition but got nil")
				}
				return
			}
			if cond != nil && cond.error != nil {
				t.Fatalf("unexpected error: %v", cond.error.Message)
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCreateAgwResponseHeadersFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter *gatewayv1.HTTPHeaderFilter
		want   *api.HeaderModifier
	}{
		{
			name:   "nil filter returns nil",
			filter: nil,
			want:   nil,
		},
		{
			name: "response headers with CORS-style headers",
			filter: &gatewayv1.HTTPHeaderFilter{
				Set: []gatewayv1.HTTPHeader{
					{Name: "Access-Control-Allow-Origin", Value: "*"},
					{Name: "Access-Control-Allow-Methods", Value: "GET, POST"},
				},
				Add: []gatewayv1.HTTPHeader{
					{Name: "X-Response-Time", Value: "42ms"},
				},
				Remove: []string{"Server", "X-Powered-By"},
			},
			want: &api.HeaderModifier{
				Add: []*api.Header{
					{Name: "X-Response-Time", Value: "42ms"},
				},
				Set: []*api.Header{
					{Name: "Access-Control-Allow-Origin", Value: "*"},
					{Name: "Access-Control-Allow-Methods", Value: "GET, POST"},
				},
				Remove: []string{"Server", "X-Powered-By"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateAgwResponseHeadersFilter(tt.filter)
			assert.Equal(t, got, tt.want)
		})
	}
}
