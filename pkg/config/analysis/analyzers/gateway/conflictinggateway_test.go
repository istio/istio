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

package gateway

import (
	"testing"

	"istio.io/api/networking/v1alpha3"
)

func TestGenGatewayMapKey(t *testing.T) {
	tests := []struct {
		name       string
		selector   string
		portNumber string
		expected   string
	}{
		{
			name:       "simple selector and port",
			selector:   "istio=ingressgateway",
			portNumber: "80",
			expected:   "istio=ingressgateway~80",
		},
		{
			name:       "empty selector",
			selector:   "",
			portNumber: "443",
			expected:   "~443",
		},
		{
			name:       "empty port",
			selector:   "app=gateway",
			portNumber: "",
			expected:   "app=gateway~",
		},
		{
			name:       "both empty",
			selector:   "",
			portNumber: "",
			expected:   "~",
		},
		{
			name:       "complex selector",
			selector:   "app=gateway,version=v1",
			portNumber: "8080",
			expected:   "app=gateway,version=v1~8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := genGatewayMapKey(tt.selector, tt.portNumber)
			if result != tt.expected {
				t.Errorf("genGatewayMapKey(%q, %q) = %q, expected %q", tt.selector, tt.portNumber, result, tt.expected)
			}
		})
	}
}

func TestParseFromGatewayMapKey(t *testing.T) {
	tests := []struct {
		name             string
		key              string
		expectedSelector string
		expectedPort     string
	}{
		{
			name:             "simple key",
			key:              "istio=ingressgateway~80",
			expectedSelector: "istio=ingressgateway",
			expectedPort:     "80",
		},
		{
			name:             "empty selector",
			key:              "~443",
			expectedSelector: "",
			expectedPort:     "443",
		},
		{
			name:             "empty port",
			key:              "app=gateway~",
			expectedSelector: "app=gateway",
			expectedPort:     "",
		},
		{
			name:             "both empty",
			key:              "~",
			expectedSelector: "",
			expectedPort:     "",
		},
		{
			name:             "complex selector",
			key:              "app=gateway,version=v1~8080",
			expectedSelector: "app=gateway,version=v1",
			expectedPort:     "8080",
		},
		{
			name:             "invalid key without separator",
			key:              "invalid-key",
			expectedSelector: "",
			expectedPort:     "",
		},
		{
			name:             "key with multiple separators",
			key:              "a~b~c",
			expectedSelector: "",
			expectedPort:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector, port := parseFromGatewayMapKey(tt.key)
			if selector != tt.expectedSelector {
				t.Errorf("parseFromGatewayMapKey(%q) selector = %q, expected %q", tt.key, selector, tt.expectedSelector)
			}
			if port != tt.expectedPort {
				t.Errorf("parseFromGatewayMapKey(%q) port = %q, expected %q", tt.key, port, tt.expectedPort)
			}
		})
	}
}

func TestParseSelectorFromString(t *testing.T) {
	tests := []struct {
		name           string
		selectorString string
		matchLabels    map[string]string
		shouldMatch    bool
	}{
		{
			name:           "single label",
			selectorString: "app=gateway",
			matchLabels:    map[string]string{"app": "gateway"},
			shouldMatch:    true,
		},
		{
			name:           "multiple labels",
			selectorString: "app=gateway,version=v1",
			matchLabels:    map[string]string{"app": "gateway", "version": "v1"},
			shouldMatch:    true,
		},
		{
			name:           "partial match should fail",
			selectorString: "app=gateway,version=v1",
			matchLabels:    map[string]string{"app": "gateway"},
			shouldMatch:    false,
		},
		{
			name:           "empty selector matches all",
			selectorString: "",
			matchLabels:    map[string]string{"app": "anything"},
			shouldMatch:    true,
		},
		{
			name:           "mismatched value",
			selectorString: "app=gateway",
			matchLabels:    map[string]string{"app": "other"},
			shouldMatch:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector := parseSelectorFromString(tt.selectorString)
			labels := make(map[string]string)
			for k, v := range tt.matchLabels {
				labels[k] = v
			}

			// For partial match test, add extra label to ensure selector requires all
			if tt.name == "partial match should fail" {
				// selector requires version=v1 but labels don't have it
			}

			matches := selector.Matches(labelSet(labels))
			if matches != tt.shouldMatch {
				t.Errorf("parseSelectorFromString(%q).Matches(%v) = %v, expected %v",
					tt.selectorString, tt.matchLabels, matches, tt.shouldMatch)
			}
		})
	}
}

// labelSet implements labels.Labels interface for testing
type labelSet map[string]string

func (l labelSet) Has(key string) bool {
	_, ok := l[key]
	return ok
}

func (l labelSet) Get(key string) string {
	return l[key]
}

func (l labelSet) Lookup(key string) (string, bool) {
	v, ok := l[key]
	return v, ok
}

func TestIsGWConflict(t *testing.T) {
	tests := []struct {
		name        string
		server      *v1alpha3.Server
		hostsBind   gatewayHostsBind
		expectConfl bool
	}{
		{
			name: "no conflict with different hosts",
			server: &v1alpha3.Server{
				Hosts: []string{"foo.example.com"},
			},
			hostsBind:   gatewayHostsBind{"bar.example.com": ""},
			expectConfl: false,
		},
		{
			name: "conflict with same host",
			server: &v1alpha3.Server{
				Hosts: []string{"foo.example.com"},
			},
			hostsBind:   gatewayHostsBind{"foo.example.com": ""},
			expectConfl: true,
		},
		{
			name: "wildcard host does not conflict with specific host when checking duplicates",
			server: &v1alpha3.Server{
				Hosts: []string{"*.example.com"},
			},
			hostsBind:   gatewayHostsBind{"foo.example.com": ""},
			expectConfl: false, // CheckDuplicates only checks exact matches, not wildcard patterns
		},
		{
			name: "no conflict with empty hosts",
			server: &v1alpha3.Server{
				Hosts: []string{},
			},
			hostsBind:   gatewayHostsBind{"foo.example.com": ""},
			expectConfl: false,
		},
		{
			name: "conflict with multiple hosts",
			server: &v1alpha3.Server{
				Hosts: []string{"foo.example.com", "bar.example.com"},
			},
			hostsBind:   gatewayHostsBind{"foo.example.com": ""},
			expectConfl: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGWConflict(tt.server, tt.hostsBind)
			if result != tt.expectConfl {
				t.Errorf("isGWConflict() = %v, expected %v", result, tt.expectConfl)
			}
		})
	}
}
