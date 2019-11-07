// Copyright 2019 Istio Authors
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

package sdscompare

import (
	"bytes"
	"io/ioutil"
	"testing"

	"istio.io/istio/security/pkg/nodeagent/sds"
)

var debugResponse = map[string]sds.Debug{}

func loadEnvoyDump() []byte {
	bytes, _ := ioutil.ReadFile("testdata/envoyconfigdumpsds.json")
	return bytes
}

func TestNewSDSComparator(t *testing.T) {
	tests := []struct {
		name              string
		envoyResponse     []byte
		nodeAgentResponse map[string]sds.Debug
		wantErr           bool
	}{
		{
			name:              "valid envoy config dump and node agent debug response should succeed",
			envoyResponse:     loadEnvoyDump(),
			nodeAgentResponse: debugResponse,
			wantErr:           false,
		},
		{
			name:              "invalid envoy config dump and valid node agent debug response should fail",
			envoyResponse:     []byte("sak<>djfi21ehaksdhf1o21809fasajmhannah<33k123la"),
			nodeAgentResponse: debugResponse,
			wantErr:           true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			mockWriter := NewSDSWriter(w, JSON)
			_, err := NewSDSComparator(mockWriter, tt.nodeAgentResponse, tt.envoyResponse, "")
			if (err != nil) != tt.wantErr {
				t.Errorf("NewComparator() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
		})
	}
}
