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

package authz

import (
	"bytes"
	"reflect"
	"testing"

	envoy_admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	any1 "github.com/golang/protobuf/ptypes/any"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	"istio.io/istio/istioctl/pkg/util/configdump"
)

func TestNewAnalyzer(t *testing.T) {
	tests := []struct {
		name    string
		input   *configdump.Wrapper
		wantErr error
	}{
		{
			name: "Test1",
			input: &configdump.Wrapper{
				ConfigDump: &envoy_admin.ConfigDump{
					Configs: []*any1.Any{
						{
							TypeUrl: "type.googleapis.com/envoy.admin.v3.ListenersConfigDump",
						},
					},
				},
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, wanted2 := NewAnalyzer(tt.input); wanted2 != nil {
				t.Errorf("Wanted %v, got %v", tt.wantErr, wanted2)
			}
		})
	}
}

func TestPrint(t *testing.T) {
	a := &Analyzer{
		listenerDump: &envoy_admin.ListenersConfigDump{
			DynamicListeners: []*envoy_admin.ListenersConfigDump_DynamicListener{
				{
					Name: "10.102.11.148_15021",
					ActiveState: &envoy_admin.ListenersConfigDump_DynamicListenerState{
						VersionInfo: "2023-06-20T09:07:41Z/3",
						Listener: &any1.Any{
							TypeUrl: "type.googleapis.com/envoy.config.listener.v3.Listener",
						},
						LastUpdated: timestamppb.Now(),
					},
					ClientStatus: 453,
				},
			},
		},
	}
	tests := []struct {
		name  string
		input *envoy_admin.ListenersConfigDump_DynamicListener
	}{
		{
			name: "Test2",
			input: &envoy_admin.ListenersConfigDump_DynamicListener{
				Name: "First_Listener",
				ActiveState: &envoy_admin.ListenersConfigDump_DynamicListenerState{
					VersionInfo: "version1.5",
					Listener: &any1.Any{
						TypeUrl: "type.googleapis.com/envoy.admin.v3.ListenersConfigDump",
					},
					LastUpdated: timestamppb.Now(),
				},
				ClientStatus: 453,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			a.Print(&buf)
			expectedOutput := "ACTION   AuthorizationPolicy   RULES\n"
			actualOutput := buf.String()
			if !reflect.DeepEqual(expectedOutput, actualOutput) {
				t.Errorf("Found %v, wanted %v", actualOutput, expectedOutput)
			}
		})
	}
}
