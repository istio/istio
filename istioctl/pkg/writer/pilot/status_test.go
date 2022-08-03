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
	"os"
	"testing"

	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	status "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"github.com/google/uuid"
	anypb "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tests/util"
	istioversion "istio.io/pkg/version"
)

var preDefinedNonce = newNonce()

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
			sw := StatusWriter{Writer: got}
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
			want, _ := os.ReadFile(tt.want)
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
			sw := StatusWriter{Writer: got}
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
			want, _ := os.ReadFile(tt.want)
			if err := util.Compare(got.Bytes(), want); err != nil {
				t.Errorf(err.Error())
			}
		})
	}
}

func statusInput1() []xds.SyncStatus {
	return []xds.SyncStatus{
		{
			ClusterID:     "cluster1",
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
			ClusterID:     "cluster2",
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
			ClusterID:     "cluster3",
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
			ClusterID:     "cluster2",
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

func TestXdsStatusWriter_PrintAll(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]*xdsapi.DiscoveryResponse
		want    string
		wantErr bool
	}{
		{
			name: "prints multiple istiod inputs to buffer in alphabetical order by pod name",
			input: map[string]*xdsapi.DiscoveryResponse{
				"istiod1": xdsResponseInput("istiod1", []clientConfigInput{
					{
						proxyID:        "proxy1",
						clusterID:      "cluster1",
						cdsSyncStatus:  status.ConfigStatus_STALE,
						ldsSyncStatus:  status.ConfigStatus_SYNCED,
						rdsSyncStatus:  status.ConfigStatus_NOT_SENT,
						edsSyncStatus:  status.ConfigStatus_SYNCED,
						ecdsSyncStatus: status.ConfigStatus_SYNCED,
					},
				}),
				"istiod2": xdsResponseInput("istiod2", []clientConfigInput{
					{
						proxyID:        "proxy2",
						clusterID:      "cluster2",
						cdsSyncStatus:  status.ConfigStatus_STALE,
						ldsSyncStatus:  status.ConfigStatus_SYNCED,
						rdsSyncStatus:  status.ConfigStatus_SYNCED,
						edsSyncStatus:  status.ConfigStatus_STALE,
						ecdsSyncStatus: status.ConfigStatus_STALE,
					},
				}),
				"istiod3": xdsResponseInput("istiod3", []clientConfigInput{
					{
						proxyID:        "proxy3",
						clusterID:      "cluster3",
						cdsSyncStatus:  status.ConfigStatus_UNKNOWN,
						ldsSyncStatus:  status.ConfigStatus_ERROR,
						rdsSyncStatus:  status.ConfigStatus_NOT_SENT,
						edsSyncStatus:  status.ConfigStatus_STALE,
						ecdsSyncStatus: status.ConfigStatus_NOT_SENT,
					},
				}),
			},
			want: "testdata/multiXdsStatusMultiPilot.txt",
		},
		{
			name: "prints single istiod input to buffer in alphabetical order by pod name",
			input: map[string]*xdsapi.DiscoveryResponse{
				"istiod1": xdsResponseInput("istiod1", []clientConfigInput{
					{
						proxyID:        "proxy1",
						clusterID:      "cluster1",
						cdsSyncStatus:  status.ConfigStatus_STALE,
						ldsSyncStatus:  status.ConfigStatus_SYNCED,
						rdsSyncStatus:  status.ConfigStatus_NOT_SENT,
						edsSyncStatus:  status.ConfigStatus_SYNCED,
						ecdsSyncStatus: status.ConfigStatus_NOT_SENT,
					},
					{
						proxyID:        "proxy2",
						clusterID:      "cluster2",
						cdsSyncStatus:  status.ConfigStatus_STALE,
						ldsSyncStatus:  status.ConfigStatus_SYNCED,
						rdsSyncStatus:  status.ConfigStatus_SYNCED,
						edsSyncStatus:  status.ConfigStatus_STALE,
						ecdsSyncStatus: status.ConfigStatus_NOT_SENT,
					},
				}),
			},
			want: "testdata/multiXdsStatusSinglePilot.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			sw := XdsStatusWriter{Writer: got}
			input := map[string]*xdsapi.DiscoveryResponse{}
			for key, ss := range tt.input {
				input[key] = ss
			}

			err := sw.PrintAll(input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			want, _ := os.ReadFile(tt.want)
			if err := util.Compare(got.Bytes(), want); err != nil {
				t.Errorf(err.Error())
			}
		})
	}
}

const clientConfigType = "type.googleapis.com/envoy.service.status.v3.ClientConfig"

type clientConfigInput struct {
	proxyID   string
	clusterID string

	cdsSyncStatus  status.ConfigStatus
	ldsSyncStatus  status.ConfigStatus
	rdsSyncStatus  status.ConfigStatus
	edsSyncStatus  status.ConfigStatus
	ecdsSyncStatus status.ConfigStatus
}

func newXdsClientConfig(config clientConfigInput) *status.ClientConfig {
	meta := model.NodeMetadata{
		ClusterID: cluster.ID(config.clusterID),
	}
	return &status.ClientConfig{
		Node: &envoycorev3.Node{
			Id:       config.proxyID,
			Metadata: meta.ToStruct(),
		},
		GenericXdsConfigs: []*status.ClientConfig_GenericXdsConfig{
			{
				TypeUrl:      v3.ClusterType,
				ConfigStatus: config.cdsSyncStatus,
			},
			{
				TypeUrl:      v3.ListenerType,
				ConfigStatus: config.ldsSyncStatus,
			},
			{
				TypeUrl:      v3.RouteType,
				ConfigStatus: config.rdsSyncStatus,
			},
			{
				TypeUrl:      v3.EndpointType,
				ConfigStatus: config.edsSyncStatus,
			},
			{
				TypeUrl:      v3.ExtensionConfigurationType,
				ConfigStatus: config.ecdsSyncStatus,
			},
		},
	}
}

func xdsResponseInput(istiodID string, configInputs []clientConfigInput) *xdsapi.DiscoveryResponse {
	icp := &xds.IstioControlPlaneInstance{
		Component: "istiod",
		ID:        istiodID,
		Info: istioversion.BuildInfo{
			Version: "1.1",
		},
	}
	identifier, _ := json.Marshal(icp)

	resources := make([]*anypb.Any, 0)
	for _, input := range configInputs {
		resources = append(resources, protoconv.MessageToAny(newXdsClientConfig(input)))
	}

	return &xdsapi.DiscoveryResponse{
		VersionInfo: "1.1",
		TypeUrl:     clientConfigType,
		Resources:   resources,
		ControlPlane: &envoycorev3.ControlPlane{
			Identifier: string(identifier),
		},
	}
}
