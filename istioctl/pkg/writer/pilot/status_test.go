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

package pilot

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/tests/util"
)

var (
	preDefinedNonce = newNonce()
)

func newNonce() string {
	return uuid.New().String()
}

func TestStatusWriter_PrintAll(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string][]xds.SyncStatus
		want    string
		wantErr bool
	}{
		{
			name: "prints multiple pilot inputs to buffer in alphabetical order by pod name",
			input: map[string][]xds.SyncStatus{
				"pilot1": statusInput1(),
				"pilot2": statusInput2(),
				"pilot3": statusInput3(),
			},
			want: "testdata/multiStatusMultiPilot.txt",
		},
		{
			name: "prints single pilot input to buffer in alphabetical order by pod name",
			input: map[string][]xds.SyncStatus{
				"pilot1": append(statusInput1(), statusInput2()...),
			},
			want: "testdata/multiStatusSinglePilot.txt",
		},
		{
			name: "error if given non-syncstatus info",
			input: map[string][]xds.SyncStatus{
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
		input     map[string][]xds.SyncStatus
		filterPod string
		want      string
		wantErr   bool
	}{
		{
			name: "prints multiple pilot inputs to buffer filtering for pod",
			input: map[string][]xds.SyncStatus{
				"pilot1": statusInput1(),
				"pilot2": statusInput2(),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatus.txt",
		},
		{
			name: "single pilot input to buffer filtering for pod",
			input: map[string][]xds.SyncStatus{
				"pilot2": append(statusInput1(), statusInput2()...),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatus.txt",
		},
		{
			name: "fallback to proxy version",
			input: map[string][]xds.SyncStatus{
				"pilot2": statusInputProxyVersion(),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatusFallback.txt",
		},
		{
			name: "error if given non-syncstatus info",
			input: map[string][]xds.SyncStatus{
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

func statusInput1() []xds.SyncStatus {
	return []xds.SyncStatus{
		{
			ProxyID:       "proxy1",
			IstioVersion:  "1.1",
			ClusterSent:   preDefinedNonce,
			ClusterAcked:  newNonce(),
			ListenerSent:  preDefinedNonce,
			ListenerAcked: preDefinedNonce,
			EndpointSent:  preDefinedNonce,
			EndpointAcked: preDefinedNonce,
		},
	}
}

func statusInput2() []xds.SyncStatus {
	return []xds.SyncStatus{
		{
			ProxyID:       "proxy2",
			IstioVersion:  "1.1",
			ClusterSent:   preDefinedNonce,
			ClusterAcked:  newNonce(),
			ListenerSent:  preDefinedNonce,
			ListenerAcked: preDefinedNonce,
			EndpointSent:  preDefinedNonce,
			EndpointAcked: newNonce(),
			RouteSent:     preDefinedNonce,
			RouteAcked:    preDefinedNonce,
		},
	}
}

func statusInput3() []xds.SyncStatus {
	return []xds.SyncStatus{
		{
			ProxyID:       "proxy3",
			IstioVersion:  "1.1",
			ClusterSent:   preDefinedNonce,
			ClusterAcked:  "",
			ListenerAcked: preDefinedNonce,
			EndpointSent:  preDefinedNonce,
			EndpointAcked: "",
			RouteSent:     preDefinedNonce,
			RouteAcked:    preDefinedNonce,
		},
	}
}

func statusInputProxyVersion() []xds.SyncStatus {
	return []xds.SyncStatus{
		{
			ProxyID:       "proxy2",
			ProxyVersion:  "1.1",
			ClusterSent:   preDefinedNonce,
			ClusterAcked:  newNonce(),
			ListenerSent:  preDefinedNonce,
			ListenerAcked: preDefinedNonce,
			EndpointSent:  preDefinedNonce,
			EndpointAcked: newNonce(),
			RouteSent:     preDefinedNonce,
			RouteAcked:    preDefinedNonce,
		},
	}
}
