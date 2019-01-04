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

package clusters

import (
	"bytes"
	"io/ioutil"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestConfigWriter_PrintEndpointSummary(t *testing.T) {
	tests := []struct {
		name           string
		filter         EndpointFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all endpoints when no filter is passed",
			filter:         EndpointFilter{},
			wantOutputFile: "testdata/clustersummary.txt",
			callPrime:      true,
		},
		{
			name:           "filter endpoints in the summary",
			filter:         EndpointFilter{Cluster: "outbound|9093||istio-policy.istio-system.svc.cluster.local"},
			wantOutputFile: "testdata/clustersummaryfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles address filtering",
			filter:         EndpointFilter{Address: "172.17.0.14"},
			wantOutputFile: "testdata/clustersummaryfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles port filtering",
			filter:         EndpointFilter{Port: 9093},
			wantOutputFile: "testdata/clustersummaryfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles status filtering",
			filter:         EndpointFilter{Status: "unhealthy"},
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
			cd, _ := ioutil.ReadFile("testdata/clusters.json")
			if tt.callPrime {
				cw.Prime(cd)
			}
			err := cw.PrintEndpointsSummary(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintEndpointSummary (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintEndpointSummary (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}

func TestConfigWriter_PrintClusterDump(t *testing.T) {
	tests := []struct {
		name           string
		filter         EndpointFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all endpoints when no filter is passed",
			filter:         EndpointFilter{},
			wantOutputFile: "testdata/clustersnofiltered.txt",
			callPrime:      true,
		},
		{
			name:           "filter endpoints",
			filter:         EndpointFilter{Cluster: "outbound|9093||istio-policy.istio-system.svc.cluster.local"},
			wantOutputFile: "testdata/clusterfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles Address filtering",
			filter:         EndpointFilter{Address: "172.17.0.14"},
			wantOutputFile: "testdata/clusterfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles port filtering",
			filter:         EndpointFilter{Port: 9093},
			wantOutputFile: "testdata/clusterfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles status filtering",
			filter:         EndpointFilter{Status: "unhealthy"},
			wantOutputFile: "testdata/clusterfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles multi hosts cluster filtering",
			filter:         EndpointFilter{Cluster: "outbound|9080||reviews.default.svc.cluster.local"},
			wantOutputFile: "testdata/clustermultihostsfiltered.txt",
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
			cd, _ := ioutil.ReadFile("testdata/clusters.json")
			if tt.callPrime {
				cw.Prime(cd)
			}
			err := cw.PrintEndpoints(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintEndpoint (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintEndpoint (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}
