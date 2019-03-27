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

func TestWrapper_GetRouteConfigDump(t *testing.T) {
	tests := []struct {
		name                    string
		wantStatic, wantDynamic int
		noConfigs               bool
		noRoute                 bool
		wantErr                 bool
	}{
		{
			name:        "retrieves route config dump",
			wantStatic:  1,
			wantDynamic: 1,
		},
		{
			name:    "returns an error if no route dump exists",
			noRoute: true,
			wantErr: true,
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
			if tt.noRoute {
				w.Configs = []proto.Any{}
			}
			if tt.noConfigs {
				w.Configs = nil
			}
			got, err := w.GetRouteConfigDump()
			if (err != nil) != tt.wantErr {
				t.Errorf("Wrapper.GetRouteConfigDump() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got == nil && tt.wantErr {
				return
			}
			if tt.wantStatic != len(got.StaticRouteConfigs) {
				t.Errorf("wanted static len %v, got %v", tt.wantStatic, len(got.StaticRouteConfigs))
			}
			if tt.wantDynamic != len(got.DynamicRouteConfigs) {
				t.Errorf("wanted dynamic len %v, got %v", tt.wantDynamic, len(got.DynamicRouteConfigs))
			}

		})
	}
}

func TestWrapper_GetDynamicRouteDump(t *testing.T) {
	tests := []struct {
		name                                string
		wantStatic, wantDynamic             int
		noRoute                             bool
		stripVersion, wantVersion, wantLast bool
		wantErr                             bool
	}{
		{
			name:         "retrieves route config dump without any static routes",
			stripVersion: true,
			wantVersion:  false,
			wantLast:     false,
			wantStatic:   0,
			wantDynamic:  1,
		},
		{
			name:         "retrieves route config dump with versions",
			stripVersion: false,
			wantVersion:  true,
			wantLast:     true,
			wantStatic:   0,
			wantDynamic:  1,
		},
		{
			name:    "returns an error if no route dump exists",
			noRoute: true,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := setupWrapper(t)
			if tt.noRoute {
				w.Configs = []proto.Any{}
			}
			got, err := w.GetDynamicRouteDump(tt.stripVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("Wrapper.GetRouteConfigDump() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got == nil && tt.wantErr {
				return
			}
			for _, c := range got.DynamicRouteConfigs {
				if tt.wantVersion != (c.VersionInfo != "") {
					t.Errorf("wanted route version %v, got %v", tt.wantVersion, c.VersionInfo)
				}
				if tt.wantLast != (c.LastUpdated != nil) {
					t.Errorf("wanted routes last updated %v, got %v", tt.wantLast, c.LastUpdated)
				}
			}
			if tt.wantStatic != len(got.StaticRouteConfigs) {
				t.Errorf("wanted static len %v, got %v", tt.wantStatic, len(got.StaticRouteConfigs))
			}
			if tt.wantDynamic != len(got.DynamicRouteConfigs) {
				t.Errorf("wanted dynamic len %v, got %v", tt.wantDynamic, len(got.DynamicRouteConfigs))
			}

		})
	}
}
