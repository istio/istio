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

func TestStatusWriter_PrintAll(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string][]v2.SyncStatus
		want    string
		wantErr bool
	}{
		{
			name: "prints multiple pilot inputs to buffer in alphabetical order by pod name",
			input: map[string][]v2.SyncStatus{
				"pilot1": statusInput1(),
				"pilot2": statusInput2(),
			},
			want: "testdata/multiStatusMultiPilot.txt",
		},
		{
			name: "prints single pilot input to buffer in alphabetical order by pod name",
			input: map[string][]v2.SyncStatus{
				"pilot1": append(statusInput1(), statusInput2()...),
			},
			want: "testdata/multiStatusSinglePilot.txt",
		},
		{
			name: "error if given non-syncstatus info",
			input: map[string][]v2.SyncStatus{
				"pilot1": {},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			sw := StatusWriter{Writer: got}
			input := map[string][]byte{}
			for key, ss := range tt.input {
				b, _ := json.Marshal(ss)
				input[key] = b
			}
			if len(tt.input["pilot1"]) == 0 {
				input = map[string][]byte{
					"pilot1": []byte(`gobbledygook`),
				}
			}
			err := sw.PrintAll(input)
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

func TestStatusWriter_PrintSingle(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string][]v2.SyncStatus
		filterPod string
		want      string
		wantErr   bool
	}{
		{
			name: "prints multiple pilot inputs to buffer filtering for pod",
			input: map[string][]v2.SyncStatus{
				"pilot1": statusInput1(),
				"pilot2": statusInput2(),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatus.txt",
		},
		{
			name: "single pilot input to buffer filtering for pod",
			input: map[string][]v2.SyncStatus{
				"pilot2": append(statusInput1(), statusInput2()...),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatus.txt",
		},
		{
			name: "error if given non-syncstatus info",
			input: map[string][]v2.SyncStatus{
				"pilot1": {},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			sw := StatusWriter{Writer: got}
			input := map[string][]byte{}
			for key, ss := range tt.input {
				b, _ := json.Marshal(ss)
				input[key] = b
			}
			if len(tt.input["pilot1"]) == 0 && len(tt.input["pilot2"]) == 0 {
				input = map[string][]byte{
					"pilot1": []byte(`gobbledygook`),
				}
			}
			err := sw.PrintSingle(input, tt.filterPod)
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

func statusInput1() []v2.SyncStatus {
	return []v2.SyncStatus{
		{
			ProxyID:         "proxy1",
			ClusterSent:     "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			ClusterAcked:    "2009-11-10 22:00:00 +0000 UTC m=+0.000000001",
			ListenerSent:    "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			ListenerAcked:   "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			EndpointSent:    "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			EndpointAcked:   "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			EndpointPercent: 100,
		},
	}
}

func statusInput2() []v2.SyncStatus {
	return []v2.SyncStatus{
		{
			ProxyID:       "proxy2",
			ClusterSent:   "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			ClusterAcked:  "2009-11-10 22:00:00 +0000 UTC m=+0.000000001",
			ListenerSent:  "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			ListenerAcked: "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			EndpointSent:  "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			EndpointAcked: "2009-11-10 22:00:00 +0000 UTC m=+0.000000001",
			RouteSent:     "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
			RouteAcked:    "2009-11-10 23:00:00 +0000 UTC m=+0.000000001",
		},
	}
}
