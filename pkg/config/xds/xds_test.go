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
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

func TestAnyToTypedStruct(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "known",
			in: `
typed_config:
  "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"
  http2_protocol_options:
    initial_stream_window_size: 65536
    initial_connection_window_size: 65536
`,
			want: "",
		},
		{
			name: "unknown",
			in: `
typed_config:
  "@type": "type.googleapis.com/some.fake.package.Filter"
  opts:
    initial_stream_window_size: 65536
    initial_connection_window_size: 65536
`,
			want: `
typed_config:
 '@type': type.googleapis.com/udpa.type.v1.TypedStruct
 type_url: type.googleapis.com/some.fake.package.Filter
 value:
   opts:
     initial_connection_window_size: 65536
     initial_stream_window_size: 65536`,
		},
		{
			name: "typed input",
			in: `
typed_config:
 '@type': type.googleapis.com/udpa.type.v1.TypedStruct
 type_url: type.googleapis.com/some.fake.package.Filter
 value:
   opts:
     initial_connection_window_size: 65536
     initial_stream_window_size: 65536
`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &anyTransformer{
				unknownTypes: make(sets.String),
			}
			in := &structpb.Struct{}
			assert.NoError(t, protomarshal.ApplyYAML(tt.in, in))
			want := &structpb.Struct{}
			assert.NoError(t, protomarshal.ApplyYAML(tt.want, want))
			if tt.want == "" {
				want = nil
			}
			got := a.AnyToTypedStruct(in)
			assert.Equal(t, got, want)
		})
	}
}
