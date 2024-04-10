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

package matcher

import (
	"testing"

	uri_template "github.com/envoyproxy/go-control-plane/envoy/extensions/path/match/uri_template/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pkg/test/util/assert"
)

func TestPathTemplateMatcher(t *testing.T) {
	testCases := []struct {
		name string
		path string
		want *uri_template.UriTemplateMatchConfig
	}{
		{
			name: "matchOneOnly",
			path: "foo/bar/{*}",
			want: &uri_template.UriTemplateMatchConfig{
				PathTemplate: "foo/bar/*",
			},
		},
		{
			name: "matchAnyOnly",
			path: "foo/{**}/bar.tmp",
			want: &uri_template.UriTemplateMatchConfig{
				PathTemplate: "foo/**/bar.tmp",
			},
		},
		{
			name: "matchAnyAndOne",
			path: "{*}/foo/{**}/bar.tmp",
			want: &uri_template.UriTemplateMatchConfig{
				PathTemplate: "*/foo/**/bar.tmp",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := PathTemplateMatcher(tc.path)
			if !cmp.Equal(got, tc.want, protocmp.Transform()) {
				t.Errorf("want %v but got %v", tc.want, got)
			}
		})
	}
}

func TestIsPathTemplate(t *testing.T) {
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
			name:           "matchOneOnly",
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
			pathTemplate := IsPathTemplate(tc.path)
			assert.Equal(t, tc.isPathTemplate, pathTemplate)
		})
	}
}
