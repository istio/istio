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

func TestWrapper_GetBootstrapConfigDump(t *testing.T) {
	tests := []struct {
		name                    string
		noConfigs               bool
		noBootstrap             bool
		wantStatic, wantDynamic bool
		wantErr                 bool
	}{
		{
			name:        "retrieves bootstrap config dump",
			wantStatic:  true,
			wantDynamic: true,
		},
		{
			name:        "returns an error if no bootstrap dump exists",
			noBootstrap: true,
			wantErr:     true,
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
			if tt.noBootstrap {
				w.Configs = []proto.Any{}
			}
			if tt.noConfigs {
				w.Configs = nil
			}
			got, err := w.GetBootstrapConfigDump()
			if (err != nil) != tt.wantErr {
				t.Errorf("Wrapper.GetBootstrapConfigDump() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got == nil && tt.wantErr {
				return
			}
			if tt.wantStatic != (got.Bootstrap.StaticResources != nil) {
				t.Errorf("wanted static resource %v, got %v", tt.wantStatic, (got.Bootstrap.StaticResources != nil))
			}
			if tt.wantStatic != (got.Bootstrap.StaticResources != nil) {
				t.Errorf("wanted dynamic resource %v, got %v", tt.wantDynamic, (got.Bootstrap.StaticResources != nil))
			}

		})
	}
}
