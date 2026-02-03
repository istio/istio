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

func TestValidateResourceQuantity(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{
			name:    "valid cpu request",
			value:   "100m",
			wantErr: false,
		},
		{
			name:    "valid memory request",
			value:   "128Mi",
			wantErr: false,
		},
		{
			name:    "valid cpu limit",
			value:   "2",
			wantErr: false,
		},
		{
			name:    "valid memory with Gi",
			value:   "1Gi",
			wantErr: false,
		},
		{
			name:    "newline injection",
			value:   "100m\"\n  - name: injected\n    image: \"busybox",
			wantErr: true,
		},
		{
			name:    "carriage return",
			value:   "100m\r",
			wantErr: true,
		},
		{
			name:    "tab character",
			value:   "100m\t",
			wantErr: true,
		},
		{
			name:    "null byte",
			value:   "100m\x00",
			wantErr: true,
		},
		{
			name:    "invalid quantity format",
			value:   "invalid",
			wantErr: true,
		},
		{
			name:    "empty string",
			value:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResourceQuantity(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResourceQuantity() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantErr     bool
	}{
		{
			name: "valid resource annotations",
			annotations: map[string]string{
				"sidecar.istio.io/proxyCPU":         "100m",
				"sidecar.istio.io/proxyMemory":      "128Mi",
				"sidecar.istio.io/proxyCPULimit":    "1",
				"sidecar.istio.io/proxyMemoryLimit": "1Gi",
			},
			wantErr: false,
		},
		{
			name: "injection attempt in proxyCPU",
			annotations: map[string]string{
				"sidecar.istio.io/proxyCPU": "100m\"\n  - name: injected\n    image: \"busybox",
			},
			wantErr: true,
		},
		{
			name: "injection attempt in proxyMemory",
			annotations: map[string]string{
				"sidecar.istio.io/proxyMemory": "128Mi\nmalicious",
			},
			wantErr: true,
		},
		{
			name: "invalid quantity in proxyCPULimit",
			annotations: map[string]string{
				"sidecar.istio.io/proxyCPULimit": "notaquantity",
			},
			wantErr: true,
		},
		{
			name: "control character in proxyMemoryLimit",
			annotations: map[string]string{
				"sidecar.istio.io/proxyMemoryLimit": "1Gi\x00",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAnnotations(tt.annotations)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAnnotations() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
