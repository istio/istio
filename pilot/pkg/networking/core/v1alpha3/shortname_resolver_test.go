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

package v1alpha3

import (
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
)

func TestResolveHost(t *testing.T) {
	tests := []struct {
		name           string
		hostname       string
		namespace      string
		featureEnabled bool
		expectedHost   string
		expectedResolved bool
	}{
		{
			name:           "feature disabled",
			hostname:       "myservice",
			namespace:      "default",
			featureEnabled: false,
			expectedHost:   "myservice",
			expectedResolved: false,
		},
		{
			name:           "short name in default namespace",
			hostname:       "myservice",
			namespace:      "default",
			featureEnabled: true,
			expectedHost:   "myservice.default.svc.cluster.local",
			expectedResolved: true,
		},
		{
			name:           "short name in custom namespace",
			hostname:       "backend",
			namespace:      "production",
			featureEnabled: true,
			expectedHost:   "backend.production.svc.cluster.local",
			expectedResolved: true,
		},
		{
			name:           "already FQDN",
			hostname:       "myservice.default.svc.cluster.local",
			namespace:      "production",
			featureEnabled: true,
			expectedHost:   "myservice.default.svc.cluster.local",
			expectedResolved: false,
		},
		{
			name:           "mesh gateway constant",
			hostname:       "mesh",
			namespace:      "default",
			featureEnabled: true,
			expectedHost:   "mesh",
			expectedResolved: false,
		},
		{
			name:           "wildcard",
			hostname:       "*",
			namespace:      "default",
			featureEnabled: true,
			expectedHost:   "*",
			expectedResolved: false,
		},
		{
			name:           "external service with domain",
			hostname:       "example.com",
			namespace:      "default",
			featureEnabled: true,
			expectedHost:   "example.com",
			expectedResolved: false,
		},
		{
			name:           "empty namespace",
			hostname:       "myservice",
			namespace:      "",
			featureEnabled: true,
			expectedHost:   "myservice",
			expectedResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set feature flag
			test.SetForTest(t, &features.EnableShortNameResolution, tt.featureEnabled)

			resolver := NewShortNameResolver(nil)
			resolved, wasResolved := resolver.ResolveHost(tt.hostname, tt.namespace)

			if resolved != tt.expectedHost {
				t.Errorf("Expected host %q, got %q", tt.expectedHost, resolved)
			}
			if wasResolved != tt.expectedResolved {
				t.Errorf("Expected resolved=%v, got %v", tt.expectedResolved, wasResolved)
			}
		})
	}
}

func TestResolveHostWithTargetNamespace(t *testing.T) {
	tests := []struct {
		name              string
		hostname          string
		sourceNamespace   string
		targetNamespace   string
		featureEnabled    bool
		expectedHost      string
		expectedResolved  bool
	}{
		{
			name:             "target namespace override",
			hostname:         "backend",
			sourceNamespace:  "app-ns",
			targetNamespace:  "other-ns",
			featureEnabled:   true,
			expectedHost:     "backend.other-ns.svc.cluster.local",
			expectedResolved: true,
		},
		{
			name:             "empty target namespace uses source",
			hostname:         "backend",
			sourceNamespace:  "app-ns",
			targetNamespace:  "",
			featureEnabled:   true,
			expectedHost:     "backend.app-ns.svc.cluster.local",
			expectedResolved: true,
		},
		{
			name:             "feature disabled",
			hostname:         "backend",
			sourceNamespace:  "app-ns",
			targetNamespace:  "other-ns",
			featureEnabled:   false,
			expectedHost:     "backend",
			expectedResolved: false,
		},
		{
			name:             "FQDN with target namespace",
			hostname:         "backend.default.svc.cluster.local",
			sourceNamespace:  "app-ns",
			targetNamespace:  "other-ns",
			featureEnabled:   true,
			expectedHost:     "backend.default.svc.cluster.local",
			expectedResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableShortNameResolution, tt.featureEnabled)

			resolver := NewShortNameResolver(nil)
			resolved, wasResolved := resolver.ResolveHostWithTargetNamespace(
				tt.hostname, tt.sourceNamespace, tt.targetNamespace)

			if resolved != tt.expectedHost {
				t.Errorf("Expected host %q, got %q", tt.expectedHost, resolved)
			}
			if wasResolved != tt.expectedResolved {
				t.Errorf("Expected resolved=%v, got %v", tt.expectedResolved, wasResolved)
			}
		})
	}
}

func TestResolveBatch(t *testing.T) {
	tests := []struct {
		name             string
		hostnames        []string
		namespace        string
		featureEnabled   bool
		expectedHosts    []string
		expectedResolved bool
	}{
		{
			name:             "mixed short and FQDN names",
			hostnames:        []string{"service1", "service2.default.svc.cluster.local", "service3"},
			namespace:        "default",
			featureEnabled:   true,
			expectedHosts:    []string{"service1.default.svc.cluster.local", "service2.default.svc.cluster.local", "service3.default.svc.cluster.local"},
			expectedResolved: true,
		},
		{
			name:             "all FQDNs",
			hostnames:        []string{"service1.default.svc.cluster.local", "service2.default.svc.cluster.local"},
			namespace:        "default",
			featureEnabled:   true,
			expectedHosts:    []string{"service1.default.svc.cluster.local", "service2.default.svc.cluster.local"},
			expectedResolved: false,
		},
		{
			name:             "feature disabled",
			hostnames:        []string{"service1", "service2"},
			namespace:        "default",
			featureEnabled:   false,
			expectedHosts:    []string{"service1", "service2"},
			expectedResolved: false,
		},
		{
			name:             "empty list",
			hostnames:        []string{},
			namespace:        "default",
			featureEnabled:   true,
			expectedHosts:    []string{},
			expectedResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableShortNameResolution, tt.featureEnabled)

			resolver := NewShortNameResolver(nil)
			resolved, wasResolved := resolver.ResolveBatch(tt.hostnames, tt.namespace)

			if len(resolved) != len(tt.expectedHosts) {
				t.Errorf("Expected %d hosts, got %d", len(tt.expectedHosts), len(resolved))
			}

			for i, h := range resolved {
				if h != tt.expectedHosts[i] {
					t.Errorf("Host %d: expected %q, got %q", i, tt.expectedHosts[i], h)
				}
			}

			if wasResolved != tt.expectedResolved {
				t.Errorf("Expected resolved=%v, got %v", tt.expectedResolved, wasResolved)
			}
		})
	}
}

func TestExpandToFQDN(t *testing.T) {
	tests := []struct {
		name        string
		shortName   string
		namespace   string
		expectedFQDN string
	}{
		{
			name:        "simple service",
			shortName:   "myservice",
			namespace:   "default",
			expectedFQDN: "myservice.default.svc.cluster.local",
		},
		{
			name:        "service with hyphen",
			shortName:   "my-service",
			namespace:   "production",
			expectedFQDN: "my-service.production.svc.cluster.local",
		},
		{
			name:        "service with number",
			shortName:   "service123",
			namespace:   "app-ns",
			expectedFQDN: "service123.app-ns.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewShortNameResolver(nil)
			fqdn := resolver.expandToFQDN(tt.shortName, tt.namespace)

			if fqdn != tt.expectedFQDN {
				t.Errorf("Expected FQDN %q, got %q", tt.expectedFQDN, fqdn)
			}
		})
	}
}
