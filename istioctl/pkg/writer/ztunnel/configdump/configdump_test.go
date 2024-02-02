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

func TestConfigWriter_PrintSecretSummary(t *testing.T) {
	tests := []struct {
		name               string
		wantOutputSecret   string
		wantOutputWorkload string
		configNamespace    string
		callPrime          bool
		wantErr            bool
	}{
		{
			name:             "returns expected secret summary onto Stdout",
			callPrime:        true,
			wantOutputSecret: "testdata/secretsummary.txt",
		},
		{
			name:    "errors if config dump is not primed",
			wantErr: true,
		},
		{
			name:               "returns expected workload summary onto Stdout",
			callPrime:          true,
			wantOutputWorkload: "testdata/workloadsummary.txt",
		},
		{
			name:               "returns expected workload summary with the default namespace",
			callPrime:          true,
			configNamespace:    "default",
			wantOutputWorkload: "testdata/workloadsummary_default.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut := &bytes.Buffer{}
			cw := &ConfigWriter{Stdout: gotOut}
			cd, _ := os.ReadFile("testdata/dump.json")
			if tt.callPrime {
				cw.Prime(cd)
			}
			if tt.wantOutputSecret != "" {
				err := cw.PrintSecretSummary()
				if err == nil && tt.wantErr {
					t.Errorf("PrintSecretSummary (%v) did not produce expected err", tt.name)
				} else if err != nil && !tt.wantErr {
					t.Errorf("PrintSecretSummary (%v) produced unexpected err: %v", tt.name, err)
				}
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputSecret)
			}
			if tt.wantOutputWorkload != "" {
				wf := WorkloadFilter{Verbose: true}
				if tt.configNamespace != "" {
					wf.Namespace = tt.configNamespace
				}
				err := cw.PrintWorkloadSummary(wf)
				if err == nil && tt.wantErr {
					t.Errorf("PrintWorkloadSummary (%v) did not produce expected err", tt.name)
				} else if err != nil && !tt.wantErr {
					t.Errorf("PrintWorkloadSummary (%v) produced unexpected err: %v", tt.name, err)
				}
				util.CompareContent(t, gotOut.Bytes(), tt.wantOutputWorkload)
			}
		})
	}
}
