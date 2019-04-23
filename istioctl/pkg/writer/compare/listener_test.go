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

	"istio.io/istio/tests/util"
)

func TestComparator_ListenerDiff(t *testing.T) {
	tests := []struct {
		name             string
		envoy            []byte
		pilot            map[string][]byte
		wantDiff         string
		wantListenerDump bool
	}{
		{
			name:     "prints a diff",
			envoy:    loadDiffEnvoyDump(),
			pilot:    map[string][]byte{"pilot": loadPilotDump()},
			wantDiff: "testdata/listenerdiff.txt",
		},
		{
			name:     "Prints match",
			envoy:    loadEnvoyDump(),
			pilot:    map[string][]byte{"pilot": loadPilotDump()},
			wantDiff: "",
		},
		{
			name:             "prints match if envoy/pilot has no listener dump",
			envoy:            loadEnvoyDump(),
			pilot:            map[string][]byte{"pilot": loadPilotDump()},
			wantListenerDump: false,
			wantDiff:         "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			c, err := NewComparator(got, tt.pilot, tt.envoy)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantListenerDump {
				c.envoy.Configs = nil
				c.pilot.Configs = nil
			}
			c.ListenerDiff()
			if tt.wantDiff != "" {
				want, _ := ioutil.ReadFile(tt.wantDiff)
				if err := util.Compare(got.Bytes(), want); err != nil {
					t.Error(err.Error())
				}
			} else if got.String() != "Listeners Match\n" {
				t.Errorf("wanted match but got a diff")
			}
		})
	}
}
