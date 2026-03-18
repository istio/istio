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

package configdump

import (
	"bytes"
	"os"
	"testing"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/util/assert"
)

func TestConfigWriter_Prime(t *testing.T) {
	tests := []struct {
		name        string
		wantConfigs int
		inputFile   string
		wantErr     bool
	}{
		{
			name:        "errors if unable to unmarshal bytes",
			inputFile:   "",
			wantConfigs: 0,
			wantErr:     true,
		},
		{
			name:        "loads valid ztunnel config_dump",
			inputFile:   "testdata/dump.json",
			wantConfigs: 27,
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := &ConfigWriter{}
			cd, _ := os.ReadFile(tt.inputFile)
			err := cw.Prime(cd)
			if cw.ztunnelDump == nil {
				if tt.wantConfigs != 0 {
					t.Errorf("wanted some configs loaded but config dump was nil")
				}
			} else if len(cw.ztunnelDump.Workloads) != tt.wantConfigs {
				t.Errorf("wanted %v configs loaded in got %v", tt.wantConfigs, len(cw.ztunnelDump.Workloads))
			}
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigWriter_PrintSummary(t *testing.T) {
	tests := []struct {
		name                     string
		wantOutputSecret         string
		wantOutputWorkload       string
		wantOutputPolicies       string
		wantOutputAll            string
		wantOutputConn           string
		configNamespace          string
		workloadName             string
		connWorkload             string
		wantOutputAllwithHeaders string
	}{
		{
			name:             "secret",
			wantOutputSecret: "testdata/secretsummary.txt",
		},
		{
			name:               "workload",
			wantOutputWorkload: "testdata/workloadsummary.txt",
		},
		{
			name:               "filtered workload",
			configNamespace:    "default",
			wantOutputWorkload: "testdata/workloadsummary_default.txt",
		},
		{
			name:               "filtered workload by name",
			workloadName:       "productpage-v1-675fc69cf-jscn2",
			configNamespace:    "bookinfo",
			wantOutputWorkload: "testdata/workloadsummary_workload.txt",
		},
		{
			name:               "policies",
			wantOutputPolicies: "testdata/policies.txt",
		},
		{
			name:           "connections",
			wantOutputConn: "testdata/connectionsummary.txt",
		},
		{
			name:           "filtered connections by workload",
			connWorkload:   "productpage-v1-796f87b58-97bjk.bookinfo",
			wantOutputConn: "testdata/connectionsummary_workload.txt",
		},
		{
			name:          "all",
			wantOutputAll: "testdata/allsummary.txt",
		},
		{
			name:                     "all with headers",
			wantOutputAllwithHeaders: "testdata/allsummary_withheaders.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut := &bytes.Buffer{}
			cw := &ConfigWriter{Stdout: gotOut}
			cd := util.ReadFile(t, "testdata/dump.json")
			assert.NoError(t, cw.Prime(cd))
			if tt.wantOutputSecret != "" {
				assert.NoError(t, cw.PrintSecretSummary())
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputSecret)
			}
			if tt.wantOutputWorkload != "" {
				wf := WorkloadFilter{Namespace: tt.configNamespace, Name: tt.workloadName}
				assert.NoError(t, cw.PrintWorkloadSummary(wf))
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputWorkload)
			}
			if tt.wantOutputPolicies != "" {
				assert.NoError(t, cw.PrintPolicySummary(PolicyFilter{}))
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputPolicies)
			}
			if tt.wantOutputAll != "" {
				assert.NoError(t, cw.PrintFullSummary(false))
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputAll)
			}
			if tt.wantOutputAllwithHeaders != "" {
				assert.NoError(t, cw.PrintFullSummary(true))
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputAllwithHeaders)
			}
			if tt.wantOutputConn != "" {
				assert.NoError(t, cw.PrintConnectionsSummary(ConnectionsFilter{Workload: tt.connWorkload}))
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputConn)
			}
		})
	}
}
