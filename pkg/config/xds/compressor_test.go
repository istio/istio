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

package xds

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
	"istio.io/istio/pkg/util/protomarshal"
)

const example = `
  name: envoy.filters.http.compressor
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
    compressor_library:
      name: text_optimized
      typed_config:
        '@type': type.googleapis.com/envoy.extensions.compression.gzip.compressor.v3.Gzip
        compression_level: BEST_SPEED
        compression_strategy: DEFAULT_STRATEGY
        memory_level: 8
        window_bits: 15
        chunk_size: 4096
    response_direction_config:
      remove_accept_encoding_header: true
      common_config:
        enabled:
          default_value: true
          runtime_key: response_direction_config_enabled
        content_type:
          - application/hal+json
          - application/atom+xml
          - application/javascript
          - application/x-javascript
          - application/json
          - application/rss+xml
          - application/vnd.ms-fontobject
          - application/x-font-ttf
          - application/x-web-app-manifest+json
          - application/xhtml+xml
          - application/xml
          - font/opentype
          - image/svg+xml
          - image/x-icon
          - text/css
          - text/html
          - text/plain
          - text/xml
          - text/x-component
        min_content_length: 860
    request_direction_config:
      common_config:
        enabled:
          default_value: false
          runtime_key: request_direction_config_enabled
`
const missingCompressor = `
  name: envoy.filters.http.compressor
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
`
const missingConfig = `
  name: envoy.filters.http.compressor
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
    compressor_library:
      name: text_optimized
`
const unknownConfig = `
  name: envoy.filters.http.compressor
  typed_config:
    '@type': type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
    compressor_library:
      name: text_optimized
      typed_config:
        '@type': type.googleapis.com/google.protobuf.Struct
        value: {}
`

func TestCompressor(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		valid bool
		error string
	}{
		{name: "baseline", input: example, valid: true},
		{name: "missing compressor", input: missingCompressor, valid: false, error: "empty compressor"},
		{name: "missing compressor config", input: missingConfig, valid: false, error: "empty compressor"},
		{name: "unknown compressor config", input: unknownConfig, valid: false, error: "unknown"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pbst := &structpb.Struct{}
			if err := protomarshal.ApplyYAML(tc.input, pbst); err != nil {
				t.Fatal(err)
			}
			out, err := ConvertHTTPFilter(pbst)
			if tc.valid {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if out == nil {
					t.Fatal("unexpected empty output")
				}
			} else {
				if err == nil {
					t.Fatal("unexpected empty error")
				}
				if !strings.Contains(err.Error(), tc.error) {
					t.Fatalf("must contain %q: %v", tc.error, err)
				}
			}
		})
	}
}
