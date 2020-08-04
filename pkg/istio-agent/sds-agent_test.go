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

package istioagent

import (
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/security"

	"testing"
)

func TestNewAgent(t *testing.T) {
	type testResult struct {
		optCAEndpoint  string
		optUseLocalJWT bool
	}
	proxyConfig := mesh.DefaultProxyConfig()
	tests := []struct {
		name             string
		fileMountedCerts bool
		caEndpoint       string
		expected         *testResult
	}{
		{
			name:             "istiod",
			fileMountedCerts: false,
			caEndpoint:       "",
			expected: &testResult{
				optCAEndpoint:  "istiod.istio-system.svc:15012",
				optUseLocalJWT: true,
			},
		},
		{
			name:             "customCA",
			fileMountedCerts: false,
			caEndpoint:       "MyCA",
			expected: &testResult{
				optCAEndpoint:  "MyCA",
				optUseLocalJWT: true,
			},
		},
		{
			name:             "istiod-filemount",
			fileMountedCerts: true,
			caEndpoint:       "",
			expected: &testResult{
				optCAEndpoint:  "istiod.istio-system.svc:15012",
				optUseLocalJWT: false,
			},
		},
	}
	for _, tt := range tests {
		secOpts := &security.Options{
			FileMountedCerts: tt.fileMountedCerts,
			CAEndpoint:       tt.caEndpoint,
		}
		sa := NewAgent(&proxyConfig,
			&AgentConfig{}, secOpts)
		if sa == nil {
			t.Fatalf("failed to create SDS agent: %s", tt.name)
		}
		if sa.secOpts.CAEndpoint != tt.expected.optCAEndpoint {
			t.Errorf("Test %s failed, expected: %s got: %s", tt.name,
				tt.expected.optCAEndpoint, sa.secOpts.CAEndpoint)
		}
		if sa.secOpts.UseLocalJWT != tt.expected.optUseLocalJWT {
			t.Errorf("Test %s failed, expected: %t got: %t", tt.name,
				tt.expected.optUseLocalJWT, sa.secOpts.UseLocalJWT)
		}
	}
}
