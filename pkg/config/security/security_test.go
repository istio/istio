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

package security_test

import (
	"reflect"
	"testing"

	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/test/util/assert"
)

func TestParseJwksURI(t *testing.T) {
	cases := []struct {
		in            string
		expected      security.JwksInfo
		expectedError bool
	}{
		{
			in:            "foo.bar.com",
			expectedError: true,
		},
		{
			in:            "tcp://foo.bar.com:abc",
			expectedError: true,
		},
		{
			in:            "http://foo.bar.com:abc",
			expectedError: true,
		},
		{
			in: "http://foo.bar.com",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "http",
				Port:     80,
				UseSSL:   false,
			},
		},
		{
			in: "https://foo.bar.com",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "https",
				Port:     443,
				UseSSL:   true,
			},
		},
		{
			in: "http://foo.bar.com:1234",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "http",
				Port:     1234,
				UseSSL:   false,
			},
		},
		{
			in: "https://foo.bar.com:1234/secure/key",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "https",
				Port:     1234,
				UseSSL:   true,
			},
		},
	}
	for _, c := range cases {
		actual, err := security.ParseJwksURI(c.in)
		if c.expectedError == (err == nil) {
			t.Fatalf("ParseJwksURI(%s): expected error (%v), got (%v)", c.in, c.expectedError, err)
		}
		if !reflect.DeepEqual(c.expected, actual) {
			t.Fatalf("expected %+v, got %+v", c.expected, actual)
		}
	}
}

func TestValidateCondition(t *testing.T) {
	cases := []struct {
		key       string
		values    []string
		wantError bool
	}{
		{
			key:       "request.headers[:authority]",
			values:    []string{"productpage", ""},
			wantError: true,
		},
		{
			key:       "request.experimental.inline.headers[:authority]",
			values:    []string{"productpage", ""},
			wantError: true,
		},
		{
			key:    "request.headers[:authority]",
			values: []string{"productpage"},
		},
		{
			key:    "request.experimental.inline.headers[:authority]",
			values: []string{"productpage"},
		},
		{
			key:       "request.headers[]",
			values:    []string{"productpage"},
			wantError: true,
		},
		{
			key:       "request.experimental.inline.headers[]",
			values:    []string{"productpage"},
			wantError: true,
		},
		{
			key:    "source.ip",
			values: []string{"1.2.3.4", "5.6.7.0/24"},
		},
		{
			key:       "source.ip",
			values:    []string{"a.b.c.d"},
			wantError: true,
		},
		{
			key:    "remote.ip",
			values: []string{"1.2.3.4", "5.6.7.0/24"},
		},
		{
			key:       "remote.ip",
			values:    []string{"a.b.c.d"},
			wantError: true,
		},
		{
			key:    "source.namespace",
			values: []string{"value"},
		},
		{
			key:       "source.user",
			values:    []string{"value"},
			wantError: true,
		},
		{
			key:    "source.principal",
			values: []string{"value"},
		},
		{
			key:       "source.serviceAccount",
			values:    []string{"too/many/slashes"},
			wantError: true,
		},
		{
			key:       "source.serviceAccount",
			values:    []string{"/emptyns"},
			wantError: true,
		},
		{
			key:       "source.serviceAccount",
			values:    []string{"emptysa/"},
			wantError: true,
		},
		{
			key:    "source.serviceAccount",
			values: []string{"ns/sa"},
		},
		{
			key:    "source.serviceAccount",
			values: []string{"sa"},
		},
		{
			key:    "request.auth.principal",
			values: []string{"value"},
		},
		{
			key:    "request.auth.audiences",
			values: []string{"value"},
		},
		{
			key:    "request.auth.presenter",
			values: []string{"value"},
		},
		{
			key:    "request.auth.claims[id]",
			values: []string{"123"},
		},
		{
			key:       "request.auth.claims[]",
			values:    []string{"value"},
			wantError: true,
		},
		{
			key:    "destination.ip",
			values: []string{"1.2.3.4", "5.6.7.0/24"},
		},
		{
			key:       "destination.ip",
			values:    []string{"a.b.c.d"},
			wantError: true,
		},
		{
			key:    "destination.port",
			values: []string{"80", "90"},
		},
		{
			key:       "destination.port",
			values:    []string{"80", "x"},
			wantError: true,
		},
		{
			key:       "destination.labels[app]",
			values:    []string{"value"},
			wantError: true,
		},
		{
			key:       "destination.name",
			values:    []string{"value"},
			wantError: true,
		},
		{
			key:       "destination.namespace",
			values:    []string{"value"},
			wantError: true,
		},
		{
			key:       "destination.user",
			values:    []string{"value"},
			wantError: true,
		},
		{
			key:    "connection.sni",
			values: []string{"value"},
		},
		{
			key:    "experimental.envoy.filters.a.b[c]",
			values: []string{"value"},
		},
		{
			key:       "experimental.envoy.filters.a.b.x",
			values:    []string{"value"},
			wantError: true,
		},
	}
	for _, c := range cases {
		err := security.ValidateAttribute(c.key, c.values)
		if c.wantError == (err == nil) {
			t.Fatalf("ValidateAttribute(%s): want error (%v) but got (%v)", c.key, c.wantError, err)
		}
	}
}

func TestCheckValidPathTemplate(t *testing.T) {
	cases := []struct {
		name      string
		values    []string
		wantError bool
	}{
		{
			name:   "valid path template - matchOneTemplate",
			values: []string{"/{*}/foo"},
		},
		{
			name:   "valid path template - matchOneTemplate with trailing slash",
			values: []string{"/{*}/foo/"},
		},
		{
			name:   "valid path template - matchAnyTemplate",
			values: []string{"/foo/{**}/bar"},
		},
		{
			name:   "valid path template - matchAnyTemplate at end",
			values: []string{"/foo/bar/{**}"},
		},
		{
			name:      "unsupported path template - matchAnyTemplate with additional chars",
			values:    []string{"/foo/{**}buzz/bar"},
			wantError: true,
		},
		{
			name:      "unsupported path template - empty curly braces",
			values:    []string{"/{*}/foo/{}/bar"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchOneTemplate with `*`",
			values:    []string{"/foo/{*}/bar/*"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchOneTemplate with `**`",
			values:    []string{"/foo/{*}/bar/**/buzz"},
			wantError: true,
		},
		{
			name:      "unsupported path/path template - named var: {buzz}",
			values:    []string{"/foo/{buzz}/bar"},
			wantError: true,
		},
		{
			name:      "unsupported path/path template - only named var: {buzz}",
			values:    []string{"/{buzz}"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchAnyTemplate with named var: {buzz}",
			values:    []string{"/foo/{buzz}/bar/{**}"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchAnyTemplate with named var: {buzz=*}",
			values:    []string{"/foo/{buzz=*}/bar/{**}"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchAnyTemplate with named var: {buzz=**}",
			values:    []string{"/{*}/foo/{buzz=**}/bar"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchAnyTemplate with additional chars at end",
			values:    []string{"/{*}/foo/{**}bar"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchOneTemplate with file extension",
			values:    []string{"/{*}/foo/{*}.txt"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchOneTemplate with unmatched open curly brace",
			values:    []string{"/{*}/foo/{temp"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchOneTemplate with unmatched closed curly brace",
			values:    []string{"/{*}/foo/temp}/bar"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchOneTemplate with unmatched closed curly brace and `*`",
			values:    []string{"/{*}/foo/temp}/*"},
			wantError: true,
		},
		{
			name:      "unsupported path template - matchAnyTemplate is not the last operator in the template",
			values:    []string{"/{**}/foo/temp/{*}/bar"},
			wantError: true,
		},
		{
			name:      "unsupported path template - simple multiple matchAnyTemplates",
			values:    []string{"/{**}/{**}"},
			wantError: true,
		},
		{
			name:      "unsupported path template - multiple matchAnyTemplates",
			values:    []string{"/{**}/foo/temp/{**}/bar"},
			wantError: true,
		},
		{
			name:      "unsupported path template - invalid literal in path template",
			values:    []string{"/{*}/foo/?abc"},
			wantError: true,
		},
	}
	for _, c := range cases {
		err := security.CheckValidPathTemplate(c.name, c.values)
		if c.wantError == (err == nil) {
			t.Fatalf("CheckValidPathTemplate(%s): want error (%v) but got (%v)", c.name, c.wantError, err)
		}
	}
}

func TestIsValidLiteral(t *testing.T) {
	// Valid literals:
	//   "a-zA-Z0-9-._~" - Unreserved
	//   "%"             - pct-encoded
	//   "!$&'()+,;"     - sub-delims excluding *=
	//   ":@"
	//   "="             - user included "=" allowed
	cases := []struct {
		name     string
		glob     string
		expected bool
	}{
		{
			name:     "valid - alphabet chars and nums",
			glob:     "123abcABC",
			expected: true,
		},
		{
			name:     "valid - unreserved chars",
			glob:     "._~-",
			expected: true,
		},
		{
			name:     "valid - equals",
			glob:     "a=c",
			expected: true,
		},
		{
			name:     "valid - mixed chars",
			glob:     "-._~%20!$&'()+,;:@",
			expected: true,
		},
		{
			name:     "invalid - mixed chars",
			glob:     "`~!@#$%^&()-_+;:,<.>'\"\\| ",
			expected: false,
		},
		{
			name:     "invalid - slash",
			glob:     "abc/",
			expected: false,
		},
		{
			name:     "invalid - star with other chars",
			glob:     "ab*c",
			expected: false,
		},
		{
			name:     "invalid - star",
			glob:     "*",
			expected: false,
		},
		{
			name:     "invalid - double star with other chars",
			glob:     "ab**c",
			expected: false,
		},
		{
			name:     "invalid - double star",
			glob:     "**",
			expected: false,
		},
		{
			name:     "invalid - question mark",
			glob:     "?abc",
			expected: false,
		},
		{
			name:     "invalid - question mark with equals",
			glob:     "?a=c",
			expected: false,
		},
		{
			name:     "invalid - left curly brace",
			glob:     "{abc",
			expected: false,
		},
		{
			name:     "invalid - right curly brace",
			glob:     "abc}",
			expected: false,
		},
		{
			name:     "invalid - curly brace set",
			glob:     "{abc}",
			expected: false,
		},
	}
	for _, c := range cases {
		actual := security.IsValidLiteral(c.glob)
		if c.expected != actual {
			t.Fatalf("IsValidLiteral(%s): expected (%v), got (%v)", c.name, c.expected, actual)
		}
	}
}

func TestContainsPathTemplate(t *testing.T) {
	testCases := []struct {
		name           string
		path           string
		isPathTemplate bool
	}{
		{
			name:           "matchOneOnly",
			path:           "foo/bar/{*}",
			isPathTemplate: true,
		},
		{
			name:           "matchAnyOnly",
			path:           "foo/{**}/bar",
			isPathTemplate: true,
		},
		{
			name:           "matchAnyAndOne",
			path:           "{*}/bar/{**}",
			isPathTemplate: true,
		},
		{
			name:           "stringMatch",
			path:           "foo/bar/*",
			isPathTemplate: false,
		},
		{
			name:           "namedVariable",
			path:           "foo/bar/{buzz}",
			isPathTemplate: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pathTemplate := security.ContainsPathTemplate(tc.path)
			assert.Equal(t, tc.isPathTemplate, pathTemplate)
		})
	}
}
