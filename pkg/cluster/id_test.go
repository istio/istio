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

package cluster

import "testing"

func TestEquals(t *testing.T) {
	tests := []struct {
		name     string
		clustera ID
		clusterb ID
		expected bool
	}{
		{
			name:     "clustera and clusterb are both empty",
			expected: true,
		},
		{
			name:     "clusterb is empty",
			clustera: "Cluster1",
			expected: true,
		},
		{
			name:     "clustera is empty",
			clusterb: "Cluster2",
			expected: true,
		},
		{
			name:     "clustera and clusterb are same",
			clustera: "Cluster1",
			clusterb: "Cluster1",
			expected: true,
		},
		{
			name:     "clustera and clusterb are not same",
			clustera: "Cluster1",
			clusterb: "Cluster2",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := tt.clusterb.Equals(tt.clustera)
			if res != tt.expected {
				t.Errorf("Equals() = %v, want %v", res, tt.expected)
			}
		})
	}
}
