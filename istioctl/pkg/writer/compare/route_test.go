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
	"strings"
	"testing"

	"istio.io/istio/tests/util"
)

func TestComparator_RouteDiff(t *testing.T) {
	tests := []struct {
		name          string
		envoy         []byte
		pilot         map[string][]byte
		wantRouteDump bool
		wantMatch     bool
		wantDiff      string
	}{
		{
			name:     "prints a diff",
			envoy:    loadDiffEnvoyDump(),
			pilot:    map[string][]byte{"pilot": loadPilotDump()},
			wantDiff: "testdata/routediff.txt",
		},
		{
			name:     "Prints match",
			envoy:    loadEnvoyDump(),
			pilot:    map[string][]byte{"pilot": loadPilotDump()},
			wantDiff: "",
		},
		{
			name:          "prints match if envoy/pilot has no cluster dump",
			envoy:         loadEnvoyDump(),
			pilot:         map[string][]byte{"pilot": loadPilotDump()},
			wantRouteDump: false,
			wantDiff:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			c, err := NewComparator(got, tt.pilot, tt.envoy)
			c.location = "UTC"
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantRouteDump {
				c.envoy.Configs = nil
				c.pilot.Configs = nil
			}
			c.RouteDiff()
			if tt.wantDiff != "" {
				want, _ := ioutil.ReadFile(tt.wantDiff)
				if err := util.Compare(got.Bytes(), want); err != nil {
					t.Error(err.Error())
				}
			} else if !strings.HasPrefix(got.String(), "Routes Match") {
				t.Errorf("wanted match but got a diff")
			}
		})
	}
}
