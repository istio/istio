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

package kube

import (
	"bytes"
	"encoding/json"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/istioctl/pkg/internaldebug"
	"istio.io/istio/pkg/test/util/assert"
)

func TestParseDebugOutput(t *testing.T) {
	cases := []struct {
		name string
		// responses holds the debug endpoint's responses, by the name istioctl prints each one under.
		responses map[string]string
	}{
		{name: "text", responses: map[string]string{"istiod-a": "404 page not found\n"}},
		{
			name:      "JSON array",
			responses: map[string]string{"istiod-a": `[{"kind":"Gateway","apiVersion":"networking.istio.io/v1","metadata":{"name":"gw"}}]`},
		},
		{name: "JSON object", responses: map[string]string{"istiod-a": `{"kind":"Gateway","apiVersion":"networking.istio.io/v1"}`}},
		{name: "JSON null", responses: map[string]string{"istiod-a": "null"}},
		{name: "two responses", responses: map[string]string{"istiod-a": `[{"name":"a"}]`, "istiod-b": `[{"name":"b"}]`}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// What `istioctl x internal-debug --all` prints for these responses.
			drs := map[string]*discovery.DiscoveryResponse{}
			for istiod, response := range tt.responses {
				drs[istiod] = &discovery.DiscoveryResponse{Resources: []*anypb.Any{{Value: []byte(response)}}}
			}
			var stdout bytes.Buffer
			w := internaldebug.DebugWriter{Writer: &stdout, InternalDebugAllIstiod: true}
			assert.NoError(t, w.PrintAll(drs))

			got, err := parseDebugOutput(stdout.String())
			assert.NoError(t, err)
			// istioctl indents a JSON response. Anything else has to come back as it was sent.
			for istiod, out := range got {
				var compact bytes.Buffer
				if json.Valid([]byte(tt.responses[istiod])) && json.Compact(&compact, []byte(out)) == nil {
					got[istiod] = compact.String()
				}
			}
			assert.Equal(t, got, tt.responses)
		})
	}
}

func TestParseDebugOutputInvalid(t *testing.T) {
	// A message in front of the JSON object, as istioctl prints for an unknown debug type.
	_, err := parseDebugOutput("error: according to below command list, please check all supported internal debug commands\n{}")
	assert.Error(t, err)
}
