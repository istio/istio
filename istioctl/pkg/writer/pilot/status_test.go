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
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/tests/util"
)

var preDefinedNonce = newNonce()

func newNonce() string {
	return uuid.New().String()
}

var commonCols = []string{
	v3.GetShortType(v3.ClusterType),
	v3.GetShortType(v3.ListenerType),
	v3.GetShortType(v3.EndpointType),
	v3.GetShortType(v3.RouteType),
}

func TestStatusWriter_PrintAll(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string][]xds.SyncStatus
		want    string
		wantErr bool
		cols    []string
	}{
		{
			name: "prints multiple istiod inputs to buffer in alphabetical order by pod name",
			input: map[string][]xds.SyncStatus{
				"istiod1": statusInput1(),
				"istiod2": statusInput2(),
				"istiod3": statusInput3(),
			},
			want: "testdata/multiStatusMultiPilot.txt",
		},
		{
			name: "prints single istiod input to buffer in alphabetical order by pod name",
			input: map[string][]xds.SyncStatus{
				"istiod1": append(statusInput1(), statusInput2()...),
			},
			want: "testdata/multiStatusSinglePilot.txt",
		},
		{
			name: "prints required columns to buffer in alphabetical order by pod name",
			input: map[string][]xds.SyncStatus{
				"istiod1": append(statusInput1(), statusInput2()...),
			},
			want: "testdata/selectedColsSinglePilot.txt",
			cols: []string{
				v3.GetShortType(v3.ClusterType),
				v3.GetShortType(v3.ListenerType),
			},
		},
		{
			name: "error if given non-syncstatus info",
			input: map[string][]xds.SyncStatus{
				"istiod1": {},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			cols := tt.cols
			if cols == nil {
				cols = commonCols
			}
			sw := StatusWriter{Writer: got, XDSCols: cols}
			input := map[string][]byte{}
			for key, ss := range tt.input {
				b, _ := json.Marshal(ss)
				input[key] = b
			}
			if len(tt.input["istiod1"]) == 0 {
				input = map[string][]byte{
					"istiod1": []byte(`gobbledygook`),
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
		cols      []string
	}{
		{
			name: "prints multiple istiod inputs to buffer filtering for pod",
			input: map[string][]xds.SyncStatus{
				"istiod1": statusInput1(),
				"istiod2": statusInput2(),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatus.txt",
		},
		{
			name: "single istiod input to buffer filtering for pod",
			input: map[string][]xds.SyncStatus{
				"istiod2": append(statusInput1(), statusInput2()...),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatus.txt",
		},
		{
			name: "fallback to proxy version",
			input: map[string][]xds.SyncStatus{
				"istiod2": statusInputProxyVersion(),
			},
			filterPod: "proxy2",
			want:      "testdata/singleStatusFallback.txt",
		},
		{
			name: "error if given non-syncstatus info",
			input: map[string][]xds.SyncStatus{
				"istiod1": {},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			cols := tt.cols
			if cols == nil {
				cols = commonCols
			}
			sw := StatusWriter{Writer: got, XDSCols: cols}
			input := map[string][]byte{}
			for key, ss := range tt.input {
				b, _ := json.Marshal(ss)
				input[key] = b
			}
			if len(tt.input["istiod1"]) == 0 && len(tt.input["istiod2"]) == 0 {
				input = map[string][]byte{
					"istiod1": []byte(`gobbledygook`),
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
			ProxyID:      "proxy1",
			IstioVersion: "1.1",
			Statuses: map[string]xds.PerXDSSyncStatus{
				v3.GetShortType(v3.ClusterType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: newNonce(),
				},
				v3.GetShortType(v3.ListenerType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: preDefinedNonce,
				},
				v3.GetShortType(v3.EndpointType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: preDefinedNonce,
				},
			},
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
			ProxyID:      "proxy2",
			IstioVersion: "1.1",
			Statuses: map[string]xds.PerXDSSyncStatus{
				v3.GetShortType(v3.ClusterType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: newNonce(),
				},
				v3.GetShortType(v3.ListenerType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: preDefinedNonce,
				},
				v3.GetShortType(v3.EndpointType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: newNonce(),
				},
				v3.GetShortType(v3.RouteType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: preDefinedNonce,
				},
			},
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
			ProxyID:      "proxy3",
			IstioVersion: "1.1",
			Statuses: map[string]xds.PerXDSSyncStatus{
				v3.GetShortType(v3.ClusterType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: "",
				},
				v3.GetShortType(v3.ListenerType): {
					NonceAcked: preDefinedNonce,
				},
				v3.GetShortType(v3.EndpointType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: "",
				},
				v3.GetShortType(v3.RouteType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: preDefinedNonce,
				},
			},
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
			ProxyID:      "proxy2",
			ProxyVersion: "1.1",
			Statuses: map[string]xds.PerXDSSyncStatus{
				v3.GetShortType(v3.ClusterType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: newNonce(),
				},
				v3.GetShortType(v3.ListenerType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: preDefinedNonce,
				},
				v3.GetShortType(v3.EndpointType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: newNonce(),
				},
				v3.GetShortType(v3.RouteType): {
					NonceSent:  preDefinedNonce,
					NonceAcked: preDefinedNonce,
				},
			},
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
