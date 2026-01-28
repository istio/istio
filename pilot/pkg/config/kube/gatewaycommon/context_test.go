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

package gatewaycommon

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
)

func TestNewGatewayContext(t *testing.T) {
	ps := &model.PushContext{}
	clusterID := cluster.ID("test-cluster")

	gc := NewGatewayContext(ps, clusterID)

	if gc.PushContext() != ps {
		t.Errorf("expected PushContext to be %v, got %v", ps, gc.PushContext())
	}
	if gc.Cluster() != clusterID {
		t.Errorf("expected Cluster to be %v, got %v", clusterID, gc.Cluster())
	}
}

func TestInstancesEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    map[int][]*model.IstioEndpoint
		expected bool
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: true,
		},
		{
			name:     "empty map",
			input:    map[int][]*model.IstioEndpoint{},
			expected: true,
		},
		{
			name:     "map with empty slice",
			input:    map[int][]*model.IstioEndpoint{80: {}},
			expected: true,
		},
		{
			name:     "map with non-empty slice",
			input:    map[int][]*model.IstioEndpoint{80: {{}}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := InstancesEmpty(tt.input)
			if result != tt.expected {
				t.Errorf("InstancesEmpty(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
