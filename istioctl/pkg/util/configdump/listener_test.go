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
	"testing"
	proto "github.com/gogo/protobuf/types"
)

func TestWrapper_GetListenerConfigDump(t *testing.T) {
	tests := []struct {
		name                    string
		noConfigs               bool
		noListener              bool
		wantVersion             string
		wantStatic, wantDynamic int
		wantErr                 bool
	}{
		{
			name:        "retrieves listener config dump",
			wantVersion: "2018-05-29 20:35:10.051043472 +0000 UTC m=+615.036247510",
			wantStatic:  0,
			wantDynamic: 2,
		},
		{
			name:       "returns an error if no listener dump exists",
			noListener: true,
			wantErr:    true,
		},
		{
			name:      "returns an error if no configs exists",
			noConfigs: true,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := setupWrapper(t)
			if tt.noListener {
				w.Configs = []proto.Any{}
			}
			if tt.noConfigs {
				w.Configs = nil
			}
			got, err := w.GetListenerConfigDump()
			if (err != nil) != tt.wantErr {
				t.Errorf("Wrapper.GetListenerConfigDump() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got == nil && tt.wantErr {
				return
			}
			if tt.wantVersion != got.VersionInfo {
				t.Errorf("wanted version %v, got %v", tt.wantVersion, got.VersionInfo)
			}
			if tt.wantStatic != len(got.StaticListeners) {
				t.Errorf("wanted static len %v, got %v", tt.wantStatic, len(got.StaticListeners))
			}
			if tt.wantDynamic != len(got.DynamicActiveListeners) {
				t.Errorf("wanted dynamic len %v, got %v", tt.wantDynamic, len(got.DynamicActiveListeners))
			}

		})
	}
}

func TestWrapper_GetDynamicListenerDump(t *testing.T) {
	tests := []struct {
		name                                string
		noListener                          bool
		stripVersion, wantVersion, wantLast bool
		wantStatic, wantDynamic             int
		wantErr                             bool
	}{
		{
			name:         "retrieves listener config dump without any static listeners",
			stripVersion: true,
			wantVersion:  false,
			wantLast:     false,
			wantStatic:   0,
			wantDynamic:  2,
		},
		{
			name:         "retrieves listener config dump with versions",
			stripVersion: false,
			wantVersion:  true,
			wantLast:     true,
			wantStatic:   0,
			wantDynamic:  2,
		},
		{
			name:       "returns an error if no listener dump exists",
			noListener: true,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := setupWrapper(t)
			if tt.noListener {
				w.Configs = []proto.Any{}
			}
			got, err := w.GetDynamicListenerDump(tt.stripVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("Wrapper.GetListenerConfigDump() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got == nil && tt.wantErr {
				return
			}
			for _, c := range got.DynamicActiveListeners {
				if tt.wantVersion != (c.VersionInfo != "") {
					t.Errorf("wanted listener version %v, got %v", tt.wantVersion, c.VersionInfo)
				}
				if tt.wantLast != (c.LastUpdated != nil) {
					t.Errorf("wanted listener last updated %v, got %v", tt.wantLast, c.LastUpdated)
				}
			}
			if tt.wantStatic != len(got.StaticListeners) {
				t.Errorf("wanted static len %v, got %v", tt.wantStatic, len(got.StaticListeners))
			}
			if tt.wantDynamic != len(got.DynamicActiveListeners) {
				t.Errorf("wanted dynamic len %v, got %v", tt.wantDynamic, len(got.DynamicActiveListeners))
			}

		})
	}
}
