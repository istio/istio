// Copyright 2018 Istio Authors.
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

package pilot

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"

	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"
)

func TestTLSCheckWriter_PrintAll(t *testing.T) {
	tests := []struct {
		name    string
		input   []v2.AuthenticationDebug
		want    string
		wantErr bool
	}{
		{
			name:  "prints full auth debug output",
			input: authInput(),
			want:  "testdata/multiAuth.txt",
		},
		{
			name:    "error if given non-auth info",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			tcw := TLSCheckWriter{Writer: got}
			input, _ := json.Marshal(tt.input)
			err := tcw.PrintAll(input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			want, _ := ioutil.ReadFile(tt.want)
			if err := util.Compare(got.Bytes(), want); err != nil {
				t.Errorf(err.Error())
			}
		})
	}
}

func TestTLSCheckWriter_PrintSingle(t *testing.T) {
	tests := []struct {
		name          string
		input         []v2.AuthenticationDebug
		filterService string
		want          string
		wantErr       bool
	}{
		{
			name:          "prints filtered auth debug output",
			filterService: "host2",
			input:         authInput(),
			want:          "testdata/singleAuth.txt",
		},
		{
			name:    "error if given non-auth info",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			tcw := TLSCheckWriter{Writer: got}
			input, _ := json.Marshal(tt.input)
			err := tcw.PrintSingle(input, tt.filterService)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			want, _ := ioutil.ReadFile(tt.want)
			if err := util.Compare(got.Bytes(), want); err != nil {
				t.Errorf(err.Error())
			}
		})
	}
}

func authInput() []v2.AuthenticationDebug {
	return []v2.AuthenticationDebug{
		{
			Host: "host1",
			Port: 1,
			AuthenticationPolicyName: "auth-policy1/namespace1",
			DestinationRuleName:      "destination-rule1/namespace1",
			ServerProtocol:           "HTTP",
			ClientProtocol:           "HTTP",
			TLSConflictStatus:        "OK",
		},
		{
			Host: "host2",
			Port: 2,
			AuthenticationPolicyName: "auth-policy2/namespace2",
			DestinationRuleName:      "destination-rule2/namespace2",
			ServerProtocol:           "HTTP",
			ClientProtocol:           "HTTP",
			TLSConflictStatus:        "OK",
		},
		{
			Host: "",
			Port: 9999,
			AuthenticationPolicyName: "auth-policy/namespace",
			DestinationRuleName:      "destination-rule/namespace",
			ServerProtocol:           "HTTP",
			ClientProtocol:           "HTTP",
			TLSConflictStatus:        "OK",
		},
	}
}
