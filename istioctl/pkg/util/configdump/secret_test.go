// Copyright 2019 Istio Authors
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
)

func TestWrapper_GetSecretConfigDump(t *testing.T) {
	tests := []struct {
		name           string
		noSecretConfig bool
		wantErr        bool
	}{
		{
			name:           "retrieves secret config dump",
			noSecretConfig: false,
			wantErr:        false,
		},
		{
			name:           "returns an error if no secret config section exists",
			noSecretConfig: true,
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := setupWrapper(t)
			if tt.noSecretConfig {
				w.Configs = nil
			}
			_, err := w.GetSecretConfigDump()
			if (err != nil) != tt.wantErr {
				t.Errorf("Wrapper.GetClusterConfigDump() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
