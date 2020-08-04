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

package util

import (
	"strings"
	"testing"
)

func TestDualUseCommonName(t *testing.T) {
	tt := []struct {
		name       string
		host       string
		expectedCN string
		expectErr  bool
	}{
		{
			name:       "single host",
			host:       "a.com",
			expectedCN: "a.com",
		},
		{
			name:       "multiple hosts",
			host:       "a.com,b.org,c.groups",
			expectedCN: "a.com",
		},
		{
			name:      "long host",
			host:      strings.Repeat("a", 61) + ".com",
			expectErr: true,
		},
	}

	for _, tc := range tt {
		cn, err := DualUseCommonName(tc.host)
		if tc.expectErr {
			if err == nil {
				t.Errorf("[%s] passed - expected error", tc.name)
			}
			continue
		}

		if err != nil {
			t.Errorf("[%s] unexpected error: %v", tc.name, err)
		}

		if tc.expectedCN != cn {
			t.Errorf("[%s] unexpected CN: wanted %q got %q", tc.name, tc.expectedCN, cn)
		}
	}
}
