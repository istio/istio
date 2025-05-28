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

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	status "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/test/util/assert"
	istioversion "istio.io/istio/pkg/version"
)

func TestXdsStatusWriter_PrintAll(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]*discovery.DiscoveryResponse
		wantFile string
		wantErr  bool
	}{
		{
			name: "prints multiple istiod inputs to buffer in alphabetical order by pod name",
			input: map[string]*discovery.DiscoveryResponse{
				"istiod1": xdsResponseInput("istiod1", []clientConfigInput{
					{
						proxyID:        "proxy1",
						clusterID:      "cluster1",
						version:        "1.20",
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
						version:        "1.19",
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
						version:        "1.20",
						cdsSyncStatus:  status.ConfigStatus_NOT_SENT,
						ldsSyncStatus:  status.ConfigStatus_ERROR,
						rdsSyncStatus:  status.ConfigStatus_NOT_SENT,
						edsSyncStatus:  status.ConfigStatus_STALE,
						ecdsSyncStatus: status.ConfigStatus_NOT_SENT,
					},
				}),
				"istiod4": xdsResponseInput("istiod4", []clientConfigInput{
					{
						proxyID:        "proxy4",
						clusterID:      "cluster4",
						version:        "1.20",
						cdsSyncStatus:  status.ConfigStatus_UNKNOWN,
						ldsSyncStatus:  status.ConfigStatus_UNKNOWN,
						rdsSyncStatus:  status.ConfigStatus_UNKNOWN,
						edsSyncStatus:  status.ConfigStatus_UNKNOWN,
						ecdsSyncStatus: status.ConfigStatus_UNKNOWN,
					},
				}),
			},
			wantFile: "testdata/multiXdsStatusMultiPilot.txt",
		},
		{
			name: "prints single istiod input to buffer in alphabetical order by pod name",
			input: map[string]*discovery.DiscoveryResponse{
				"istiod1": xdsResponseInput("istiod1", []clientConfigInput{
					{
						proxyID:        "proxy1",
						clusterID:      "cluster1",
						version:        "1.20",
						cdsSyncStatus:  status.ConfigStatus_STALE,
						ldsSyncStatus:  status.ConfigStatus_SYNCED,
						rdsSyncStatus:  status.ConfigStatus_NOT_SENT,
						edsSyncStatus:  status.ConfigStatus_SYNCED,
						ecdsSyncStatus: status.ConfigStatus_NOT_SENT,
					},
					{
						proxyID:        "proxy2",
						clusterID:      "cluster2",
						version:        "1.20",
						cdsSyncStatus:  status.ConfigStatus_STALE,
						ldsSyncStatus:  status.ConfigStatus_SYNCED,
						rdsSyncStatus:  status.ConfigStatus_SYNCED,
						edsSyncStatus:  status.ConfigStatus_STALE,
						ecdsSyncStatus: status.ConfigStatus_NOT_SENT,
					},
				}),
			},
			wantFile: "testdata/multiXdsStatusSinglePilot.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &bytes.Buffer{}
			sw := XdsStatusWriter{Writer: got}
			input := map[string]*discovery.DiscoveryResponse{}
			for key, ss := range tt.input {
				input[key] = ss
			}

			err := sw.PrintAll(input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			want, _ := os.ReadFile(tt.wantFile)
			if got.String() != string(want) {
				t.Errorf("Unexpected output for '%s'\n got: %q\nwant: %q", tt.name, got.String(), string(want))
			}
		})
	}
}

const clientConfigType = "type.googleapis.com/envoy.service.status.v3.ClientConfig"

type clientConfigInput struct {
	proxyID   string
	clusterID string
	version   string

	cdsSyncStatus  status.ConfigStatus
	ldsSyncStatus  status.ConfigStatus
	rdsSyncStatus  status.ConfigStatus
	edsSyncStatus  status.ConfigStatus
	ecdsSyncStatus status.ConfigStatus
}

func newXdsClientConfig(config clientConfigInput) *status.ClientConfig {
	meta := model.NodeMetadata{
		ClusterID:    cluster.ID(config.clusterID),
		IstioVersion: config.version,
	}
	return &status.ClientConfig{
		Node: &core.Node{
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

func xdsResponseInput(istiodID string, configInputs []clientConfigInput) *discovery.DiscoveryResponse {
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

	return &discovery.DiscoveryResponse{
		VersionInfo: "1.1",
		TypeUrl:     clientConfigType,
		Resources:   resources,
		ControlPlane: &core.ControlPlane{
			Identifier: string(identifier),
		},
	}
}
