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

package configdump

import (
	"testing"
)

func TestDescribeRouteDomains(t *testing.T) {
	tests := []struct {
		desc     string
		domains  []string
		expected string
	}{
		{
			desc:     "test zero domain",
			domains:  []string{},
			expected: "",
		},
		{
			desc:     "test only one domain",
			domains:  []string{"example.com"},
			expected: "example.com",
		},
		{
			desc:     "test domains with port",
			domains:  []string{"example.com", "example.com:8080"},
			expected: "example.com",
		},
		{
			desc:     "test domains with ipv4 addresses",
			domains:  []string{"example.com", "example.com:8080", "1.2.3.4", "1.2.3.4:8080"},
			expected: "example.com, 1.2.3.4",
		},
		{
			desc:     "test domains with ipv6 addresses",
			domains:  []string{"example.com", "example.com:8080", "[fd00:10:96::7fc7]", "[fd00:10:96::7fc7]:8080"},
			expected: "example.com, [fd00:10:96::7fc7]",
		},
		{
			desc:     "test with more domains",
			domains:  []string{"example.com", "example.com:8080", "www.example.com", "www.example.com:8080", "[fd00:10:96::7fc7]", "[fd00:10:96::7fc7]:8080"},
			expected: "example.com, www.example.com + 1 more...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := describeRouteDomains(tt.domains); got != tt.expected {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expected, got)
			}
		})
	}
}
