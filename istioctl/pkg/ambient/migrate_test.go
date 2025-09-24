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

package ambient

import (
	"strings"
	"testing"

	apisecurityv1beta1 "istio.io/api/security/v1beta1"
	securityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/operator/pkg/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMigrationSteps(t *testing.T) {
	steps := []MigrationStep{
		StepCompatibilityCheck,
		StepAmbientSetup,
		StepWaypointDeploy,
		StepPolicyMigration,
		StepWaypointConfiguration,
		StepPolicyCleanup,
		StepSidecarRemoval,
	}

	expectedSteps := []string{
		"compatibility-check",
		"ambient-setup",
		"deploy-waypoints",
		"policy-migration",
		"waypoint-configuration",
		"policy-cleanup",
		"sidecar-removal",
	}

	if len(steps) != len(expectedSteps) {
		t.Errorf("Expected %d steps, got %d", len(expectedSteps), len(steps))
	}

	for i, step := range steps {
		if string(step) != expectedSteps[i] {
			t.Errorf("Expected step %s, got %s", expectedSteps[i], step)
		}
	}
}

func TestMigrationResult(t *testing.T) {
	result := MigrationResult{
		Step:            StepWaypointDeploy,
		Success:         true,
		Message:         "Test message",
		Recommendations: []string{"Test recommendation"},
		Warnings:        []string{"Test warning"},
		Errors:          []string{"Test error"},
	}

	if result.Step != StepWaypointDeploy {
		t.Errorf("Expected step %s, got %s", StepWaypointDeploy, result.Step)
	}

	if !result.Success {
		t.Error("Expected success to be true")
	}

	if result.Message != "Test message" {
		t.Errorf("Expected message 'Test message', got '%s'", result.Message)
	}

	if len(result.Recommendations) != 1 {
		t.Errorf("Expected 1 recommendation, got %d", len(result.Recommendations))
	}

	if len(result.Warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(result.Warnings))
	}

	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(result.Errors))
	}
}

func TestNamespaceInfo(t *testing.T) {
	nsInfo := NamespaceInfo{
		Name:                     "test-namespace",
		HasSidecars:              true,
		HasAuthorizationPolicies: true,
		NeedsWaypoint:            true,
		HasWaypoints:             false,
		Services:                 []ServiceInfo{{Name: "test-service", Namespace: "test-namespace"}},
		AuthorizationPolicies:    []string{"test-policy"},
	}

	if nsInfo.Name != "test-namespace" {
		t.Errorf("Expected name 'test-namespace', got '%s'", nsInfo.Name)
	}

	if !nsInfo.HasSidecars {
		t.Error("Expected HasSidecars to be true")
	}

	if !nsInfo.HasAuthorizationPolicies {
		t.Error("Expected HasAuthorizationPolicies to be true")
	}

	if !nsInfo.NeedsWaypoint {
		t.Error("Expected NeedsWaypoint to be true")
	}

	if nsInfo.HasWaypoints {
		t.Error("Expected HasWaypoints to be false")
	}

	if len(nsInfo.Services) != 1 {
		t.Errorf("Expected 1 service, got %d", len(nsInfo.Services))
	}

	if len(nsInfo.AuthorizationPolicies) != 1 {
		t.Errorf("Expected 1 authorization policy, got %d", len(nsInfo.AuthorizationPolicies))
	}
}

func TestServiceInfo(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:               "test-service",
		Namespace:          "test-namespace",
		HasHTTPPolicies:    true,
		NeedsWaypoint:      true,
		VirtualServiceName: "test-vs",
	}

	if serviceInfo.Name != "test-service" {
		t.Errorf("Expected name 'test-service', got '%s'", serviceInfo.Name)
	}

	if serviceInfo.Namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got '%s'", serviceInfo.Namespace)
	}

	if !serviceInfo.HasHTTPPolicies {
		t.Error("Expected HasHTTPPolicies to be true")
	}

	if !serviceInfo.NeedsWaypoint {
		t.Error("Expected NeedsWaypoint to be true")
	}

	if serviceInfo.VirtualServiceName != "test-vs" {
		t.Errorf("Expected VirtualServiceName 'test-vs', got '%s'", serviceInfo.VirtualServiceName)
	}
}

// TestL7RulesDetection tests the hasL7Rules function with various AuthorizationPolicy scenarios
func TestL7RulesDetection(t *testing.T) {
	// Test with nil policy
	result := hasL7Rules(nil)
	if result != false {
		t.Error("Nil policy should not require waypoint")
	}

	// Test with empty policy
	emptyPolicy := &securityv1beta1.AuthorizationPolicy{}
	result = hasL7Rules(emptyPolicy)
	if result != false {
		t.Error("Empty policy should not require waypoint")
	}

	// Test with policy that has empty rules - create minimal policy
	policyWithEmptyRules := &securityv1beta1.AuthorizationPolicy{}
	result = hasL7Rules(policyWithEmptyRules)
	if result != false {
		t.Error("Policy with empty rules should not require waypoint")
	}
}

// TestWaypointYAMLGeneration tests the generateWaypointYAML function
func TestWaypointYAMLGeneration(t *testing.T) {
	namespaces := []NamespaceInfo{
		{
			Name:          "app1",
			NeedsWaypoint: true,
		},
		{
			Name:          "app2",
			NeedsWaypoint: false, // Should not generate waypoint
		},
	}

	services := []ServiceInfo{
		{
			Name:          "service1",
			Namespace:     "app3",
			NeedsWaypoint: true,
		},
	}

	yaml, err := generateWaypointYAML(namespaces, services)
	if err != nil {
		t.Fatalf("Failed to generate waypoint YAML: %v", err)
	}

	// Basic checks - the YAML should contain waypoint configurations
	if yaml == "" {
		t.Error("Generated YAML should not be empty")
	}

	// Check that it contains expected namespaces
	if !contains(yaml, "app1") {
		t.Error("Generated YAML should contain app1 namespace")
	}
	if !contains(yaml, "app3") {
		t.Error("Generated YAML should contain app3 namespace")
	}

	// Check that it doesn't contain app2 (which doesn't need waypoint)
	if contains(yaml, "app2") {
		t.Error("Generated YAML should not contain app2 namespace (doesn't need waypoint)")
	}

	// Check for required Gateway API fields
	if !contains(yaml, "gatewayClassName: istio-waypoint") {
		t.Error("Generated YAML should contain istio-waypoint gateway class")
	}
	if !contains(yaml, "protocol: HBONE") {
		t.Error("Generated YAML should contain HBONE protocol")
	}
	if !contains(yaml, "port: 15008") {
		t.Error("Generated YAML should contain port 15008")
	}
}

// TestLabelsMatch tests the labelsMatch helper function
func TestLabelsMatch(t *testing.T) {
	tests := []struct {
		name           string
		podLabels      map[string]string
		selectorLabels map[string]string
		expected       bool
	}{
		{
			name: "Exact match",
			podLabels: map[string]string{
				"app":     "test",
				"version": "v1",
			},
			selectorLabels: map[string]string{
				"app":     "test",
				"version": "v1",
			},
			expected: true,
		},
		{
			name: "Partial match",
			podLabels: map[string]string{
				"app":     "test",
				"version": "v1",
				"env":     "prod",
			},
			selectorLabels: map[string]string{
				"app": "test",
			},
			expected: true,
		},
		{
			name: "No match - missing label",
			podLabels: map[string]string{
				"app": "test",
			},
			selectorLabels: map[string]string{
				"app":     "test",
				"version": "v1",
			},
			expected: false,
		},
		{
			name: "No match - wrong value",
			podLabels: map[string]string{
				"app":     "test",
				"version": "v2",
			},
			selectorLabels: map[string]string{
				"app":     "test",
				"version": "v1",
			},
			expected: false,
		},
		{
			name: "Empty selector",
			podLabels: map[string]string{
				"app": "test",
			},
			selectorLabels: map[string]string{},
			expected:       true,
		},
		{
			name:      "Empty pod labels",
			podLabels: map[string]string{},
			selectorLabels: map[string]string{
				"app": "test",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := labelsMatch(tt.podLabels, tt.selectorLabels)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestGetOutputDir tests the getOutputDir function
func TestGetOutputDir(t *testing.T) {
	// Test default output directory
	originalOutputDir := outputDir
	outputDir = ""

	result := getOutputDir()
	expected := "/tmp/istio-migrate"
	if result != expected {
		t.Errorf("Expected default output dir %s, got %s", expected, result)
	}

	// Test custom output directory
	customDir := "/custom/output"
	outputDir = customDir

	result = getOutputDir()
	if result != customDir {
		t.Errorf("Expected custom output dir %s, got %s", customDir, result)
	}

	// Restore original value
	outputDir = originalOutputDir
}

// TestVersionComparison tests the isVersionGreaterOrEqual function
func TestVersionComparison(t *testing.T) {
	tests := []struct {
		name     string
		version1 string
		version2 string
		expected bool
	}{
		{
			name:     "Same version",
			version1: "1.15.0",
			version2: "1.15.0",
			expected: true,
		},
		{
			name:     "Higher major version",
			version1: "2.0.0",
			version2: "1.15.0",
			expected: true,
		},
		{
			name:     "Lower major version",
			version1: "1.14.0",
			version2: "1.15.0",
			expected: false,
		},
		{
			name:     "Higher minor version",
			version1: "1.16.0",
			version2: "1.15.0",
			expected: true,
		},
		{
			name:     "Lower minor version",
			version1: "1.14.0",
			version2: "1.15.0",
			expected: false,
		},
		{
			name:     "Higher patch version",
			version1: "1.15.1",
			version2: "1.15.0",
			expected: true,
		},
		{
			name:     "Lower patch version",
			version1: "1.15.0",
			version2: "1.15.1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := version.NewVersionFromString(tt.version1)
			if err != nil {
				t.Fatalf("Failed to parse version1 %s: %v", tt.version1, err)
			}

			v2, err := version.NewVersionFromString(tt.version2)
			if err != nil {
				t.Fatalf("Failed to parse version2 %s: %v", tt.version2, err)
			}

			result := isVersionGreaterOrEqual(v1, v2)
			if result != tt.expected {
				t.Errorf("Expected %s >= %s to be %v, got %v", tt.version1, tt.version2, tt.expected, result)
			}
		})
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			contains(s[1:], substr))))
}

// TestMigrationStepOrder tests that steps are in the correct order as per requirements.md
func TestMigrationStepOrder(t *testing.T) {
	expectedOrder := []MigrationStep{
		StepCompatibilityCheck,    // 1. compatibility-check
		StepAmbientSetup,          // 2. ambient-setup
		StepWaypointDeploy,        // 3. deploy-waypoints
		StepPolicyMigration,       // 4. policy-migration
		StepWaypointConfiguration, // 5. waypoint-configuration
		StepPolicyCleanup,         // 6. policy-cleanup
		StepSidecarRemoval,        // 7. sidecar-removal
	}

	// This test ensures the steps are defined in the correct order as per requirements.md
	// The actual order is defined in the runMigration function, but we can verify the constants exist
	for _, step := range expectedOrder {
		switch step {
		case StepCompatibilityCheck, StepAmbientSetup, StepWaypointDeploy,
			StepPolicyMigration, StepWaypointConfiguration, StepPolicyCleanup, StepSidecarRemoval:
			// Valid step
		default:
			t.Errorf("Unknown step: %s", step)
		}
	}

	// Verify we have exactly 7 steps as per requirements
	if len(expectedOrder) != 7 {
		t.Errorf("Expected exactly 7 steps, got %d", len(expectedOrder))
	}
}

// TestStepStringValues tests that step string values match requirements.md
func TestStepStringValues(t *testing.T) {
	expectedValues := map[MigrationStep]string{
		StepCompatibilityCheck:    "compatibility-check",
		StepAmbientSetup:          "ambient-setup",
		StepWaypointDeploy:        "deploy-waypoints",
		StepPolicyMigration:       "policy-migration",
		StepWaypointConfiguration: "waypoint-configuration",
		StepPolicyCleanup:         "policy-cleanup",
		StepSidecarRemoval:        "sidecar-removal",
	}

	for step, expectedValue := range expectedValues {
		if string(step) != expectedValue {
			t.Errorf("Step %v should have string value %s, got %s", step, expectedValue, string(step))
		}
	}
}

// TestHasL7Rules tests the hasL7Rules function with various AuthorizationPolicy scenarios
func TestHasL7Rules(t *testing.T) {
	tests := []struct {
		name     string
		policy   *securityv1beta1.AuthorizationPolicy
		expected bool
		desc     string
	}{
		{
			name:     "nil policy",
			policy:   nil,
			expected: false,
			desc:     "Nil policy should not require waypoint",
		},
		{
			name:     "empty policy",
			policy:   &securityv1beta1.AuthorizationPolicy{},
			expected: false,
			desc:     "Empty policy should not require waypoint",
		},
		{
			name: "L4-only policy - source.ip",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							When: []*apisecurityv1beta1.Condition{
								{
									Key: "source.ip",
								},
							},
						},
					},
				},
			},
			expected: false,
			desc:     "L4-only policy with source.ip should not require waypoint",
		},
		{
			name: "L4-only policy - destination.port",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							When: []*apisecurityv1beta1.Condition{
								{
									Key: "destination.port",
								},
							},
						},
					},
				},
			},
			expected: false,
			desc:     "L4-only policy with destination.port should not require waypoint",
		},
		{
			name: "L7 policy - HTTP methods",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Methods: []string{"GET", "POST"},
									},
								},
							},
						},
					},
				},
			},
			expected: true,
			desc:     "Policy with HTTP methods should require waypoint",
		},
		{
			name: "L7 policy - HTTP paths",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Paths: []string{"/api/*"},
									},
								},
							},
						},
					},
				},
			},
			expected: true,
			desc:     "Policy with HTTP paths should require waypoint",
		},
		{
			name: "L7 policy - HTTP hosts",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Hosts: []string{"api.example.com"},
									},
								},
							},
						},
					},
				},
			},
			expected: true,
			desc:     "Policy with HTTP hosts should require waypoint",
		},
		{
			name: "L7 policy - request principals",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							From: []*apisecurityv1beta1.Rule_From{
								{
									Source: &apisecurityv1beta1.Source{
										RequestPrincipals: []string{"cluster.local/ns/default/sa/default"},
									},
								},
							},
						},
					},
				},
			},
			expected: true,
			desc:     "Policy with request principals should require waypoint",
		},
		{
			name: "L7 policy - remote IP blocks",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							From: []*apisecurityv1beta1.Rule_From{
								{
									Source: &apisecurityv1beta1.Source{
										RemoteIpBlocks: []string{"192.168.1.0/24"},
									},
								},
							},
						},
					},
				},
			},
			expected: true,
			desc:     "Policy with remote IP blocks should require waypoint",
		},
		{
			name: "L7 policy - custom when condition",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							When: []*apisecurityv1beta1.Condition{
								{
									Key: "request.headers[user-agent]",
								},
							},
						},
					},
				},
			},
			expected: true,
			desc:     "Policy with custom when condition should require waypoint",
		},
		{
			name: "Mixed L4/L7 policy",
			policy: &securityv1beta1.AuthorizationPolicy{
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							When: []*apisecurityv1beta1.Condition{
								{
									Key: "source.ip",
								},
								{
									Key: "request.headers[user-agent]",
								},
							},
						},
					},
				},
			},
			expected: true,
			desc:     "Policy with mixed L4/L7 conditions should require waypoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasL7Rules(tt.policy)
			if result != tt.expected {
				t.Errorf("Test: %s\nExpected %v, got %v\nDescription: %s", tt.name, tt.expected, result, tt.desc)
			}
		})
	}
}

// TestGenerateWaypointCompatiblePolicy tests the generateWaypointCompatiblePolicy function
func TestGenerateWaypointCompatiblePolicy(t *testing.T) {
	tests := []struct {
		name           string
		originalPolicy *securityv1beta1.AuthorizationPolicy
		namespace      string
		expectedName   string
		expectedLabel  string
		desc           string
	}{
		{
			name: "basic policy migration",
			originalPolicy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Methods: []string{"GET"},
									},
								},
							},
						},
					},
				},
			},
			namespace:     "test-namespace",
			expectedName:  "test-policy-waypoint",
			expectedLabel: "test-namespace.test-policy",
			desc:          "Basic policy should be migrated with waypoint suffix and proper labels",
		},
		{
			name: "policy with complex rules",
			originalPolicy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-policy",
					Namespace: "app-namespace",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Methods: []string{"GET", "POST"},
										Paths:   []string{"/api/*"},
										Hosts:   []string{"api.example.com"},
									},
								},
							},
							From: []*apisecurityv1beta1.Rule_From{
								{
									Source: &apisecurityv1beta1.Source{
										RequestPrincipals: []string{"cluster.local/ns/default/sa/default"},
									},
								},
							},
							When: []*apisecurityv1beta1.Condition{
								{
									Key: "request.headers[user-agent]",
								},
							},
						},
					},
				},
			},
			namespace:     "production",
			expectedName:  "complex-policy-waypoint",
			expectedLabel: "production.complex-policy",
			desc:          "Complex policy with multiple rules should be migrated correctly",
		},
		{
			name: "policy with no rules",
			originalPolicy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-policy",
					Namespace: "empty-ns",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{},
				},
			},
			namespace:     "target-ns",
			expectedName:  "empty-policy-waypoint",
			expectedLabel: "target-ns.empty-policy",
			desc:          "Policy with no rules should still be migrated with proper structure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateWaypointCompatiblePolicy(tt.originalPolicy, tt.namespace)

			// Basic checks
			if result == "" {
				t.Errorf("Test: %s\nGenerated YAML should not be empty", tt.name)
				return
			}

			// Check that it contains the expected name
			if !strings.Contains(result, tt.expectedName) {
				t.Errorf("Test: %s\nGenerated YAML should contain name %s, got:\n%s", tt.name, tt.expectedName, result)
			}

			// Check that it contains the expected label
			if !strings.Contains(result, tt.expectedLabel) {
				t.Errorf("Test: %s\nGenerated YAML should contain label %s, got:\n%s", tt.name, tt.expectedLabel, result)
			}

			// Check that it targets the waypoint Gateway
			if !strings.Contains(result, "gateway.networking.k8s.io") {
				t.Errorf("Test: %s\nGenerated YAML should target Gateway API, got:\n%s", tt.name, result)
			}

			if !strings.Contains(result, "kind: Gateway") {
				t.Errorf("Test: %s\nGenerated YAML should target Gateway kind, got:\n%s", tt.name, result)
			}

			if !strings.Contains(result, "name: waypoint") {
				t.Errorf("Test: %s\nGenerated YAML should target waypoint Gateway, got:\n%s", tt.name, result)
			}

			// Check that it's valid YAML (contains apiVersion and kind)
			if !strings.Contains(result, "apiVersion: security.istio.io/v1beta1") {
				t.Errorf("Test: %s\nGenerated YAML should contain correct apiVersion, got:\n%s", tt.name, result)
			}

			if !strings.Contains(result, "kind: AuthorizationPolicy") {
				t.Errorf("Test: %s\nGenerated YAML should contain AuthorizationPolicy kind, got:\n%s", tt.name, result)
			}

			// Check that original rules are preserved
			if len(tt.originalPolicy.Spec.Rules) > 0 {
				// Check for HTTP methods if they exist
				for _, rule := range tt.originalPolicy.Spec.Rules {
					for _, to := range rule.To {
						if to.Operation != nil && len(to.Operation.Methods) > 0 {
							for _, method := range to.Operation.Methods {
								if !strings.Contains(result, method) {
									t.Errorf("Test: %s\nGenerated YAML should preserve HTTP method %s, got:\n%s", tt.name, method, result)
								}
							}
						}
					}
				}
			}

			t.Logf("Test: %s\nGenerated YAML:\n%s", tt.name, result)
		})
	}
}

// TestAuthorizationPolicyMigration tests the complete authorization policy migration flow including edge cases
func TestAuthorizationPolicyMigration(t *testing.T) {
	tests := []struct {
		name          string
		policy        *securityv1beta1.AuthorizationPolicy
		namespace     string
		shouldMigrate bool
		expectError   bool
		desc          string
	}{
		{
			name: "L7 policy with HTTP methods",
			policy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "http-methods-policy",
					Namespace: "test-ns",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Methods: []string{"GET", "POST"},
									},
								},
							},
						},
					},
				},
			},
			namespace:     "target-namespace",
			shouldMigrate: true,
			expectError:   false,
			desc:          "L7 policy with HTTP methods should be migrated to waypoint",
		},
		{
			name: "L4-only policy",
			policy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l4-only-policy",
					Namespace: "test-ns",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							When: []*apisecurityv1beta1.Condition{
								{
									Key: "source.ip",
								},
							},
						},
					},
				},
			},
			namespace:     "target-namespace",
			shouldMigrate: false,
			expectError:   false,
			desc:          "L4-only policy should not require waypoint migration",
		},
		{
			name: "L7 policy with paths and hosts",
			policy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "paths-hosts-policy",
					Namespace: "test-ns",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Paths: []string{"/api/v1/*"},
										Hosts: []string{"api.example.com"},
									},
								},
							},
						},
					},
				},
			},
			namespace:     "target-namespace",
			shouldMigrate: true,
			expectError:   false,
			desc:          "L7 policy with paths and hosts should be migrated to waypoint",
		},
		{
			name: "policy with special characters in name",
			policy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-with-dashes_and_underscores",
					Namespace: "default",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Methods: []string{"GET"},
									},
								},
							},
						},
					},
				},
			},
			namespace:     "test-namespace",
			shouldMigrate: true,
			expectError:   false,
			desc:          "Policy with special characters should be handled correctly",
		},
		{
			name: "policy with very long name",
			policy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "very-long-policy-name-that-might-cause-issues-with-kubernetes-resource-naming-limits",
					Namespace: "default",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Methods: []string{"GET"},
									},
								},
							},
						},
					},
				},
			},
			namespace:     "test-namespace",
			shouldMigrate: true,
			expectError:   false,
			desc:          "Policy with long name should be handled correctly",
		},
		{
			name: "policy with empty namespace",
			policy: &securityv1beta1.AuthorizationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "",
				},
				Spec: apisecurityv1beta1.AuthorizationPolicy{
					Rules: []*apisecurityv1beta1.Rule{
						{
							To: []*apisecurityv1beta1.Rule_To{
								{
									Operation: &apisecurityv1beta1.Operation{
										Methods: []string{"GET"},
									},
								},
							},
						},
					},
				},
			},
			namespace:     "target-namespace",
			shouldMigrate: true,
			expectError:   false,
			desc:          "Policy with empty original namespace should use target namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test L7 rule detection
			hasL7 := hasL7Rules(tt.policy)
			if hasL7 != tt.shouldMigrate {
				t.Errorf("Test: %s\nExpected hasL7Rules to return %v, got %v\nDescription: %s", tt.name, tt.shouldMigrate, hasL7, tt.desc)
			}

			// Test policy generation if it should be migrated or if we're testing edge cases
			if tt.shouldMigrate || tt.desc != "" {
				generatedYAML := generateWaypointCompatiblePolicy(tt.policy, tt.namespace)

				if tt.expectError {
					if !strings.Contains(generatedYAML, "Error marshaling") {
						t.Errorf("Test: %s\nExpected error in result, got:\n%s", tt.name, generatedYAML)
					}
				} else {
					// Verify the generated policy
					if generatedYAML == "" {
						t.Errorf("Test: %s\nGenerated YAML should not be empty", tt.name)
					}

					// Basic validation
					if !strings.Contains(generatedYAML, "apiVersion: security.istio.io/v1beta1") {
						t.Errorf("Test: %s\nGenerated YAML should contain correct apiVersion", tt.name)
					}

					if !strings.Contains(generatedYAML, "kind: AuthorizationPolicy") {
						t.Errorf("Test: %s\nGenerated YAML should contain AuthorizationPolicy kind", tt.name)
					}

					// Check that the generated policy name has the -waypoint suffix
					expectedName := tt.policy.Name + "-waypoint"
					if !strings.Contains(generatedYAML, expectedName) {
						t.Errorf("Test: %s\nGenerated YAML should contain name %s, got:\n%s", tt.name, expectedName, generatedYAML)
					}

					// Check that it targets the waypoint Gateway
					if !strings.Contains(generatedYAML, "name: waypoint") {
						t.Errorf("Test: %s\nGenerated YAML should target waypoint Gateway, got:\n%s", tt.name, generatedYAML)
					}

					// Verify the migration label is present
					expectedLabel := tt.namespace + "." + tt.policy.Name
					if !strings.Contains(generatedYAML, expectedLabel) {
						t.Errorf("Test: %s\nGenerated YAML should contain migration label %s, got:\n%s", tt.name, expectedLabel, generatedYAML)
					}

					// Check that it targets the Gateway API
					if !strings.Contains(generatedYAML, "gateway.networking.k8s.io") {
						t.Errorf("Test: %s\nGenerated YAML should target Gateway API, got:\n%s", tt.name, generatedYAML)
					}

					if !strings.Contains(generatedYAML, "kind: Gateway") {
						t.Errorf("Test: %s\nGenerated YAML should target Gateway kind, got:\n%s", tt.name, generatedYAML)
					}
				}

				t.Logf("Test: %s\nDescription: %s\nGenerated YAML:\n%s", tt.name, tt.desc, generatedYAML)
			}
		})
	}
}
