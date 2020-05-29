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
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"

	"istio.io/istio/pilot/test/util"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := &ConfigWriter{}
			cd, _ := ioutil.ReadFile(tt.inputFile)
			err := cw.Prime(cd)
			if cw.configDump == nil {
				if tt.wantConfigs != 0 {
					t.Errorf("wanted some configs loaded but config dump was nil")
				}
			} else if len(cw.configDump.Configs) != tt.wantConfigs {
				t.Errorf("wanted %v configs loaded in got %v", tt.wantConfigs, len(cw.configDump.Configs))
			}
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigWriter_PrintBootstrapDump(t *testing.T) {
	tests := []struct {
		name           string
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		// TODO: Turn on when protobuf bug is resolved - https://github.com/golang/protobuf/issues/632
		// {
		// 	name:           "returns expected bootstrap dump from Envoy onto Stdout",
		// 	callPrime:      true,
		// 	wantOutputFile: "testdata/bootstrapdump.json",
		// },
		{
			name:    "errors if config dump is not primed",
			wantErr: true,
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
			err := cw.PrintBootstrapDump()
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintBootstrapDump (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintBootstrapDump (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}
