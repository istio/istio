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

package inject

import (
	"testing"
)

func Test_parsePort(t *testing.T) {
	tests := []struct {
		name    string
		portStr string
		wantErr bool
	}{
		{
			name:    "valid single port",
			portStr: "8080",
			wantErr: false,
		},
		{
			name:    "valid port range",
			portStr: "8080:9090",
			wantErr: false,
		},
		{
			name:    "valid port range with empty end port",
			portStr: "8080:",
			wantErr: false,
		},
		{
			name:    "start port greater than end port",
			portStr: "9090:8080",
			wantErr: true,
		},
		{
			name:    "invalid port range format",
			portStr: "8080::",
			wantErr: true,
		},
		{
			name:    "invalid port number",
			portStr: "abc",
			wantErr: true,
		},
		{
			name:    "leading/trailing whitespace",
			portStr: " 8080 : 9090 ",
			wantErr: true,
		},
		{
			name:    "invalid port range with empty start port",
			portStr: ":9090",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := parsePort(tt.portStr); (err != nil) != tt.wantErr {
				t.Errorf("parsePort() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
