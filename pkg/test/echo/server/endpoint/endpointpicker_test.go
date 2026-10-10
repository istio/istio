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

package endpoint

import (
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/structpb"
)

func lbMetadata(ns string, fields map[string]any) *corev3.Metadata {
	s, err := structpb.NewStruct(fields)
	if err != nil {
		panic(err)
	}
	return &corev3.Metadata{FilterMetadata: map[string]*structpb.Struct{ns: s}}
}

func TestServedEndpoint(t *testing.T) {
	cases := []struct {
		name string
		md   *corev3.Metadata
		want string
	}{
		{
			name: "reports the served endpoint",
			md: lbMetadata("envoy.lb", map[string]any{
				"x-gateway-destination-endpoint":        "10.0.0.1:8000",
				"x-gateway-destination-endpoint-served": "10.0.0.2:8000",
			}),
			want: "10.0.0.2:8000",
		},
		{
			// The served key is written by the override_host load balancer, not by the picker, so
			// its absence has to read as empty rather than fall back to the requested endpoint.
			name: "hint key alone is not a served endpoint",
			md: lbMetadata("envoy.lb", map[string]any{
				"x-gateway-destination-endpoint": "10.0.0.1:8000",
			}),
			want: "",
		},
		{
			name: "wrong namespace",
			md: lbMetadata("envoy.filters.http.ext_proc", map[string]any{
				"x-gateway-destination-endpoint-served": "10.0.0.2:8000",
			}),
			want: "",
		},
		{
			name: "no metadata",
			md:   nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := servedEndpoint(tc.md); got != tc.want {
				t.Errorf("servedEndpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}
