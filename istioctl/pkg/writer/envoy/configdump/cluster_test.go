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

func TestConfigWriter_PrintClusterSummary(t *testing.T) {
	tests := []struct {
		name           string
		filter         ClusterFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all clusters when no filter is passed",
			filter:         ClusterFilter{},
			wantOutputFile: "testdata/clustersummary.txt",
			callPrime:      true,
		},
		{
			name:           "filter clusters in the summary",
			filter:         ClusterFilter{FQDN: "istio-policy.istio-system.svc.cluster.local"},
			wantOutputFile: "testdata/clustersummaryfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles port filtering",
			filter:         ClusterFilter{Port: 15004},
			wantOutputFile: "testdata/clustersummaryfiltered.txt",
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
			err := cw.PrintClusterSummary(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintClusterSummary (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintClusterSummary (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}

func TestConfigWriter_PrintClusterDump(t *testing.T) {
	tests := []struct {
		name           string
		filter         ClusterFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all clusters when no filter is passed",
			filter:         ClusterFilter{},
			wantOutputFile: "testdata/clusterdump.json",
			callPrime:      true,
		},
		{
			name:           "filter clusters in the dump",
			filter:         ClusterFilter{FQDN: "istio-policy.istio-system.svc.cluster.local"},
			wantOutputFile: "testdata/clusterdumpfiltered.json",
			callPrime:      true,
		},
		{
			name:           "handles port filtering",
			filter:         ClusterFilter{Port: 15004},
			wantOutputFile: "testdata/clusterdumpfiltered.json",
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
			err := cw.PrintClusterDump(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintClusterDump (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintClusterDump (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}
