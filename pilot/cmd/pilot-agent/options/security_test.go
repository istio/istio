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

package options

import (
	"os"
	"testing"

	"istio.io/istio/pkg/security"
)

func TestCheckGkeWorkloadCertificate(t *testing.T) {
	cert, err := os.CreateTemp("", "existing-cert-file")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cert.Name())

	tests := []struct {
		name     string
		paths    []string
		expected bool
	}{
		{
			name: "non-existing cert paths",
			paths: []string{
				"/this-is-a-nonexisting-path-1", "/this-is-a-nonexisting-path-2",
				"/this-is-a-nonexisting-path-3",
			},
			expected: false,
		},
		{
			name:     "existing cert paths",
			paths:    []string{cert.Name(), cert.Name(), cert.Name()},
			expected: true,
		},
		{
			name:     "mixed non-existing and existing cert paths",
			paths:    []string{cert.Name(), "/this-is-a-nonexisting-path-1", "/this-is-a-nonexisting-path-2"},
			expected: false,
		},
	}
	for _, tt := range tests {
		result := security.CheckWorkloadCertificate(tt.paths[0], tt.paths[1], tt.paths[2])
		if result != tt.expected {
			t.Errorf("Test %s failed, expected: %t got: %t", tt.name, tt.expected, result)
		}
	}
}
