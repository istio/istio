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

	"istio.io/istio/pkg/config/constants"
)

func TestSystemNamespaces_Contains(t *testing.T) {
	tests := []struct {
		ns       string
		expected bool
	}{
		{
			ns:       constants.KubeSystemNamespace,
			expected: true,
		},
		{
			ns:       constants.KubePublicNamespace,
			expected: true,
		},
		{
			ns:       constants.KubeNodeLeaseNamespace,
			expected: true,
		},
		{
			ns:       constants.LocalPathStorageNamespace,
			expected: true,
		},
		{
			ns:       "fake",
			expected: false,
		},
	}
	for _, test := range tests {
		if IgnoredNamespaces.Contains(test.ns) != test.expected {
			t.Fatal("the system namespaces are incorrect")
		}
	}
}
