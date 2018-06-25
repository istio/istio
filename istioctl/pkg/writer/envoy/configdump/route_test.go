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

package configdump

import (
	"bytes"
	"io/ioutil"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestConfigWriter_PrintRouteSummary(t *testing.T) {
	tests := []struct {
		name           string
		filter         RouteFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all routes when no filter is passed",
			filter:         RouteFilter{},
			wantOutputFile: "testdata/routesummary.txt",
			callPrime:      true,
		},
		{
			name:           "filter routes in the summary",
			filter:         RouteFilter{Name: "15004"},
			wantOutputFile: "testdata/routesummaryfiltered.txt",
			callPrime:      true,
		},
		{
			name:      "errors if config writer is not primed",
			callPrime: false,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut := &bytes.Buffer{}
			cw := &ConfigWriter{Stdout: gotOut}
			cd, _ := ioutil.ReadFile("testdata/configdump.json")
			if tt.callPrime {
				cw.Prime(cd)
			}
			err := cw.PrintRouteSummary(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintRouteSummary (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintRouteSummary (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}

func TestConfigWriter_PrintRouteDump(t *testing.T) {
	tests := []struct {
		name           string
		filter         RouteFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all routes when no filter is passed",
			filter:         RouteFilter{},
			wantOutputFile: "testdata/routedump.json",
			callPrime:      true,
		},
		{
			name:           "filter routes in the dump",
			filter:         RouteFilter{Name: "15004"},
			wantOutputFile: "testdata/routedumpfiltered.json",
			callPrime:      true,
		},
		{
			name:      "errors if config writer is not primed",
			callPrime: false,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut := &bytes.Buffer{}
			cw := &ConfigWriter{Stdout: gotOut}
			cd, _ := ioutil.ReadFile("testdata/configdump.json")
			if tt.callPrime {
				cw.Prime(cd)
			}
			err := cw.PrintRouteDump(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintRouteDump (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintRouteDump (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}
