// Copyright 2018 Istio Authors
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

package compare

import (
	"bytes"
	"io/ioutil"
	"testing"
)

func loadPilotDump() []byte {
	bytes, _ := ioutil.ReadFile("testdata/pilotconfigdump.json")
	return bytes
}
func loadEnvoyDump() []byte {
	bytes, _ := ioutil.ReadFile("testdata/envoyconfigdump.json")
	return bytes
}
func loadDiffEnvoyDump() []byte {
	bytes, _ := ioutil.ReadFile("testdata/diffenvoyconfigdump.json")
	return bytes
}

func TestNewComparator(t *testing.T) {
	tests := []struct {
		name                 string
		pilotResponses       map[string][]byte
		envoyResponse        []byte
		wantEnvoy, wantPilot bool
		wantErr              bool
	}{
		{
			name: "populates envoy and pilot wrappers",
			pilotResponses: map[string][]byte{
				"pilot1": []byte("nope"),
				"pilot2": loadPilotDump(),
			},
			envoyResponse: loadEnvoyDump(),
			wantEnvoy:     true,
			wantPilot:     true,
		},
		{
			name: "errors if pilot can't be populated",
			pilotResponses: map[string][]byte{
				"pilot1": []byte("nope"),
				"pilot2": []byte("nope"),
			},
			envoyResponse: loadEnvoyDump(),
			wantErr:       true,
		},
		{
			name: "errors if pilot can't be populated",
			pilotResponses: map[string][]byte{
				"pilot1": []byte("nope"),
				"pilot2": loadPilotDump(),
			},
			envoyResponse: []byte("nope"),
			wantErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			got, err := NewComparator(w, tt.pilotResponses, tt.envoyResponse)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewComparator() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if tt.wantEnvoy != (got.envoy != nil) {
				t.Errorf("NewComparator() envoy = %v, wantEnvoy %v", got.envoy, tt.wantEnvoy)
			}
			if tt.wantPilot != (got.pilot != nil) {
				t.Errorf("NewComparator() envoy = %v, wantEnvoy %v", got.pilot, tt.wantPilot)
			}
		})
	}
}
