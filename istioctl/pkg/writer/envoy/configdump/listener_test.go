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

func TestConfigWriter_PrintListenerSummary(t *testing.T) {
	tests := []struct {
		name           string
		filter         ListenerFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all listeners when no filter is passed",
			filter:         ListenerFilter{},
			wantOutputFile: "testdata/listenersummary.txt",
			callPrime:      true,
		},
		{
			name:           "filter listeners in the summary",
			filter:         ListenerFilter{Address: "172.21.134.116"},
			wantOutputFile: "testdata/listenersummaryfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles port filtering",
			filter:         ListenerFilter{Port: 443},
			wantOutputFile: "testdata/listenersummaryfiltered.txt",
			callPrime:      true,
		},
		{
			name:           "handles type filtering",
			filter:         ListenerFilter{Type: "TCP"},
			wantOutputFile: "testdata/listenersummaryfiltered.txt",
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
			err := cw.PrintListenerSummary(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintListenerSummary (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintListenerSummary (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}

func TestConfigWriter_PrintListenerDump(t *testing.T) {
	tests := []struct {
		name           string
		filter         ListenerFilter
		wantOutputFile string
		callPrime      bool
		wantErr        bool
	}{
		{
			name:           "display all listeners when no filter is passed",
			filter:         ListenerFilter{},
			wantOutputFile: "testdata/listenerdump.json",
			callPrime:      true,
		},
		{
			name:           "filter listeners in the dump",
			filter:         ListenerFilter{Address: "172.21.134.116"},
			wantOutputFile: "testdata/listenerdumpfiltered.json",
			callPrime:      true,
		},
		{
			name:           "handles port filtering",
			filter:         ListenerFilter{Port: 443},
			wantOutputFile: "testdata/listenerdumpfiltered.json",
			callPrime:      true,
		},
		{
			name:           "handles type filtering",
			filter:         ListenerFilter{Type: "TCP"},
			wantOutputFile: "testdata/listenerdumpfiltered.json",
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
			err := cw.PrintListenerDump(tt.filter)
			if tt.wantOutputFile != "" {
				util.CompareContent(gotOut.Bytes(), tt.wantOutputFile, t)
			}
			if err == nil && tt.wantErr {
				t.Errorf("PrintListenerDump (%v) did not produce expected err", tt.name)
			} else if err != nil && !tt.wantErr {
				t.Errorf("PrintListenerDump (%v) produced unexpected err: %v", tt.name, err)
			}
		})
	}
}
