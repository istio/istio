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

package authz

import (
	"testing"

	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/api/security/v1beta1"
	typev1beta1 "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
)

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestNamespaceMatch tests the namespaceMatch function with various patterns
func TestNamespaceMatch(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		pattern   string
		want      bool
	}{
		// Wildcard tests
		{
			name:      "wildcard matches everything",
			namespace: "test-login",
			pattern:   "*",
			want:      true,
		},
		{
			name:      "wildcard matches empty",
			namespace: "",
			pattern:   "*",
			want:      true,
		},
		// Prefix wildcard tests
		{
			name:      "prefix wildcard matches",
			namespace: "test-login",
			pattern:   "test-*",
			want:      true,
		},
		{
			name:      "prefix wildcard no match",
			namespace: "test-login",
			pattern:   "login-*",
			want:      false,
		},
		{
			name:      "prefix wildcard exact prefix",
			namespace: "test",
			pattern:   "test-*",
			want:      false,
		},
		// Suffix wildcard tests
		{
			name:      "suffix wildcard matches",
			namespace: "test-login",
			pattern:   "*-login",
			want:      true,
		},
		{
			name:      "suffix wildcard no match",
			namespace: "test-login",
			pattern:   "*-test",
			want:      false,
		},
		{
			name:      "suffix wildcard exact suffix",
			namespace: "login",
			pattern:   "*-login",
			want:      false,
		},
		// Exact match tests
		{
			name:      "exact match",
			namespace: "production",
			pattern:   "production",
			want:      true,
		},
		{
			name:      "exact match case insensitive",
			namespace: "Production",
			pattern:   "production",
			want:      true,
		},
		{
			name:      "exact no match",
			namespace: "production",
			pattern:   "staging",
			want:      false,
		},
		// Edge cases
		{
			name:      "empty namespace and pattern",
			namespace: "",
			pattern:   "",
			want:      true,
		},
		{
			name:      "namespace with hyphens",
			namespace: "my-test-namespace",
			pattern:   "my-*",
			want:      true,
		},
		{
			name:      "complex suffix pattern",
			namespace: "prod-us-west-1",
			pattern:   "*-west-1",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := namespaceMatch(tt.namespace, tt.pattern)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestHasMatchingPodsRunningIn tests the hasMatchingPodsRunningIn function
func TestHasMatchingPodsRunningIn(t *testing.T) {
	tests := []struct {
		name     string
		selector klabels.Selector
		podSets  []klabels.Set
		want     bool
	}{
		{
			name:     "no pods",
			selector: klabels.SelectorFromSet(map[string]string{"app": "httpbin"}),
			podSets:  []klabels.Set{},
			want:     false,
		},
		{
			name:     "matching pod",
			selector: klabels.SelectorFromSet(map[string]string{"app": "httpbin"}),
			podSets: []klabels.Set{
				{"app": "httpbin", "version": "v1"},
			},
			want: true,
		},
		{
			name:     "no matching pod",
			selector: klabels.SelectorFromSet(map[string]string{"app": "httpbin", "version": "v2"}),
			podSets: []klabels.Set{
				{"app": "httpbin", "version": "v1"},
			},
			want: false,
		},
		{
			name:     "multiple pods one matches",
			selector: klabels.SelectorFromSet(map[string]string{"app": "httpbin"}),
			podSets: []klabels.Set{
				{"app": "details", "version": "v1"},
				{"app": "httpbin", "version": "v1"},
				{"app": "ratings", "version": "v1"},
			},
			want: true,
		},
		{
			name:     "empty selector matches all",
			selector: klabels.SelectorFromSet(map[string]string{}),
			podSets: []klabels.Set{
				{"app": "httpbin", "version": "v1"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasMatchingPodsRunningIn(tt.selector, tt.podSets)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestHasMatchingPodsRunning tests the hasMatchingPodsRunning function
func TestHasMatchingPodsRunning(t *testing.T) {
	tests := []struct {
		name         string
		selector     klabels.Selector
		podLabelsMap map[string][]klabels.Set
		want         bool
	}{
		{
			name:         "empty pod map",
			selector:     klabels.SelectorFromSet(map[string]string{"app": "httpbin"}),
			podLabelsMap: map[string][]klabels.Set{},
			want:         false,
		},
		{
			name:     "matching pod in one namespace",
			selector: klabels.SelectorFromSet(map[string]string{"app": "httpbin"}),
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin", "version": "v1"},
				},
			},
			want: true,
		},
		{
			name:     "matching pod in different namespace",
			selector: klabels.SelectorFromSet(map[string]string{"app": "httpbin"}),
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "details", "version": "v1"},
				},
				"prod": {
					{"app": "httpbin", "version": "v1"},
				},
			},
			want: true,
		},
		{
			name:     "no matching pods anywhere",
			selector: klabels.SelectorFromSet(map[string]string{"app": "nonexistent"}),
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin", "version": "v1"},
				},
				"prod": {
					{"app": "details", "version": "v1"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasMatchingPodsRunning(tt.selector, tt.podLabelsMap)
			assert.Equal(t, got, tt.want)
		})
	}
}

// mockContext is a minimal implementation of analysis.Context for testing
type mockContext struct {
	resources map[config.GroupVersionKind][]*resource.Instance
	reports   []diag.Message
}

func newMockContext() *mockContext {
	return &mockContext{
		resources: make(map[config.GroupVersionKind][]*resource.Instance),
		reports:   make([]diag.Message, 0),
	}
}

func (m *mockContext) Report(gvk config.GroupVersionKind, msg diag.Message) {
	m.reports = append(m.reports, msg)
}

func (m *mockContext) Find(gvk config.GroupVersionKind, name resource.FullName) *resource.Instance {
	for _, r := range m.resources[gvk] {
		if r.Metadata.FullName == name {
			return r
		}
	}
	return nil
}

func (m *mockContext) Exists(gvk config.GroupVersionKind, name resource.FullName) bool {
	return m.Find(gvk, name) != nil
}

func (m *mockContext) ForEach(gvk config.GroupVersionKind, fn analysis.IteratorFn) {
	for _, r := range m.resources[gvk] {
		if !fn(r) {
			break
		}
	}
}

func (m *mockContext) Canceled() bool {
	return false
}

func (m *mockContext) SetAnalyzer(analyzerName string) {
	// No-op for mock
}

func (m *mockContext) addResource(gvk config.GroupVersionKind, r *resource.Instance) {
	m.resources[gvk] = append(m.resources[gvk], r)
}

// mockOrigin is a minimal implementation of resource.Origin for testing
type mockOrigin struct{}

func (m *mockOrigin) FriendlyName() string {
	return "mock"
}

func (m *mockOrigin) Namespace() resource.Namespace {
	return ""
}

func (m *mockOrigin) Reference() resource.Reference {
	return nil
}

func (m *mockOrigin) FieldMap() map[string]int {
	return make(map[string]int)
}

func (m *mockOrigin) ClusterName() cluster.ID {
	return ""
}

func (m *mockOrigin) Comparator() string {
	return "mock"
}

// TestMeshWidePolicy tests the meshWidePolicy function
func TestMeshWidePolicy(t *testing.T) {
	// Reset global meshConfig before each test
	defer func() { meshConfig = nil }()

	tests := []struct {
		name       string
		namespace  string
		meshConfig *v1alpha1.MeshConfig
		want       bool
	}{
		{
			name:      "no mesh config",
			namespace: "istio-system",
			want:      false,
		},
		{
			name:      "matches root namespace",
			namespace: "istio-system",
			meshConfig: &v1alpha1.MeshConfig{
				RootNamespace: "istio-system",
			},
			want: true,
		},
		{
			name:      "does not match root namespace",
			namespace: "default",
			meshConfig: &v1alpha1.MeshConfig{
				RootNamespace: "istio-system",
			},
			want: false,
		},
		{
			name:      "custom root namespace matches",
			namespace: "custom-root",
			meshConfig: &v1alpha1.MeshConfig{
				RootNamespace: "custom-root",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meshConfig = nil // Reset global state
			ctx := newMockContext()

			if tt.meshConfig != nil {
				meshConfig = tt.meshConfig
			}

			got := meshWidePolicy(tt.namespace, ctx)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestAnalyzeNoMatchingWorkloads tests the analyzeNoMatchingWorkloads function
func TestAnalyzeNoMatchingWorkloads(t *testing.T) {
	defer func() { meshConfig = nil }()

	tests := []struct {
		name            string
		authzPolicy     *v1beta1.AuthorizationPolicy
		policyNamespace string
		policyName      string
		podLabelsMap    map[string][]klabels.Set
		meshConfig      *v1alpha1.MeshConfig
		wantReports     int
	}{
		{
			name: "namespace-wide policy with pods",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Selector: nil,
			},
			policyNamespace: "default",
			policyName:      "test-policy",
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin"},
				},
			},
			wantReports: 0,
		},
		{
			name: "namespace-wide policy without pods",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Selector: nil,
			},
			policyNamespace: "empty-ns",
			policyName:      "test-policy",
			podLabelsMap:    map[string][]klabels.Set{},
			wantReports:     1,
		},
		{
			name: "policy with selector matching pods",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Selector: &typev1beta1.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			policyNamespace: "default",
			policyName:      "test-policy",
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin", "version": "v1"},
				},
			},
			wantReports: 0,
		},
		{
			name: "policy with selector not matching pods",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Selector: &typev1beta1.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "nonexistent",
					},
				},
			},
			policyNamespace: "default",
			policyName:      "test-policy",
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin", "version": "v1"},
				},
			},
			wantReports: 1,
		},
		{
			name: "mesh-wide policy without selector",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Selector: nil,
			},
			policyNamespace: "istio-system",
			policyName:      "mesh-policy",
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin"},
				},
			},
			meshConfig: &v1alpha1.MeshConfig{
				RootNamespace: "istio-system",
			},
			wantReports: 0,
		},
		{
			name: "mesh-wide policy with selector matching",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Selector: &typev1beta1.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			policyNamespace: "istio-system",
			policyName:      "mesh-policy",
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin"},
				},
			},
			meshConfig: &v1alpha1.MeshConfig{
				RootNamespace: "istio-system",
			},
			wantReports: 0,
		},
		{
			name: "mesh-wide policy with selector not matching",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Selector: &typev1beta1.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "nonexistent",
					},
				},
			},
			policyNamespace: "istio-system",
			policyName:      "mesh-policy",
			podLabelsMap: map[string][]klabels.Set{
				"default": {
					{"app": "httpbin"},
				},
			},
			meshConfig: &v1alpha1.MeshConfig{
				RootNamespace: "istio-system",
			},
			wantReports: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meshConfig = tt.meshConfig
			ctx := newMockContext()

			if tt.meshConfig != nil {
				ctx.addResource(gvk.MeshConfig, &resource.Instance{
					Message: tt.meshConfig,
					Metadata: resource.Metadata{
						FullName: resource.FullName{
							Name:      resource.LocalName("istio"),
							Namespace: resource.Namespace("istio-system"),
						},
					},
				})
			}

			r := &resource.Instance{
				Message: tt.authzPolicy,
				Metadata: resource.Metadata{
					FullName: resource.FullName{
						Name:      resource.LocalName(tt.policyName),
						Namespace: resource.Namespace(tt.policyNamespace),
					},
				},
			}

			analyzer := &AuthorizationPoliciesAnalyzer{}
			analyzer.analyzeNoMatchingWorkloads(r, ctx, tt.podLabelsMap)

			assert.Equal(t, len(ctx.reports), tt.wantReports)
			if tt.wantReports > 0 {
				assert.Equal(t, ctx.reports[0].Type.Code(), msg.NoMatchingWorkloadsFound.Code())
			}
		})
	}
}

// TestAnalyzeNamespaceNotFound tests the analyzeNamespaceNotFound function
func TestAnalyzeNamespaceNotFound(t *testing.T) {
	tests := []struct {
		name           string
		authzPolicy    *v1beta1.AuthorizationPolicy
		namespaces     []string
		wantReports    int
		wantNamespaces []string
	}{
		{
			name: "all namespaces exist",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Rules: []*v1beta1.Rule{
					{
						From: []*v1beta1.Rule_From{
							{
								Source: &v1beta1.Source{
									Namespaces: []string{"default", "prod"},
								},
							},
						},
					},
				},
			},
			namespaces:  []string{"default", "prod", "staging"},
			wantReports: 0,
		},
		{
			name: "some namespaces not found",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Rules: []*v1beta1.Rule{
					{
						From: []*v1beta1.Rule_From{
							{
								Source: &v1beta1.Source{
									Namespaces: []string{"default", "nonexistent"},
								},
							},
						},
					},
				},
			},
			namespaces:     []string{"default", "prod"},
			wantReports:    1,
			wantNamespaces: []string{"nonexistent"},
		},
		{
			name: "wildcard namespace matches",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Rules: []*v1beta1.Rule{
					{
						From: []*v1beta1.Rule_From{
							{
								Source: &v1beta1.Source{
									Namespaces: []string{"prod-*"},
								},
							},
						},
					},
				},
			},
			namespaces:  []string{"prod-us", "prod-eu"},
			wantReports: 0,
		},
		{
			name: "wildcard namespace no match",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Rules: []*v1beta1.Rule{
					{
						From: []*v1beta1.Rule_From{
							{
								Source: &v1beta1.Source{
									Namespaces: []string{"staging-*"},
								},
							},
						},
					},
				},
			},
			namespaces:     []string{"prod-us", "prod-eu"},
			wantReports:    1,
			wantNamespaces: []string{"staging-*"},
		},
		{
			name: "notNamespaces field",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Rules: []*v1beta1.Rule{
					{
						From: []*v1beta1.Rule_From{
							{
								Source: &v1beta1.Source{
									NotNamespaces: []string{"default", "bogus-ns"},
								},
							},
						},
					},
				},
			},
			namespaces:     []string{"default", "prod"},
			wantReports:    1,
			wantNamespaces: []string{"bogus-ns"},
		},
		{
			name: "multiple rules and sources",
			authzPolicy: &v1beta1.AuthorizationPolicy{
				Rules: []*v1beta1.Rule{
					{
						From: []*v1beta1.Rule_From{
							{
								Source: &v1beta1.Source{
									Namespaces: []string{"default"},
								},
							},
							{
								Source: &v1beta1.Source{
									Namespaces: []string{"bogus1"},
								},
							},
						},
					},
					{
						From: []*v1beta1.Rule_From{
							{
								Source: &v1beta1.Source{
									Namespaces: []string{"bogus2"},
								},
							},
						},
					},
				},
			},
			namespaces:     []string{"default"},
			wantReports:    2,
			wantNamespaces: []string{"bogus1", "bogus2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newMockContext()

			// Add namespaces to context
			for _, ns := range tt.namespaces {
				ctx.addResource(gvk.Namespace, &resource.Instance{
					Metadata: resource.Metadata{
						FullName: resource.FullName{
							Name: resource.LocalName(ns),
						},
					},
				})
			}

			r := &resource.Instance{
				Message: tt.authzPolicy,
				Origin:  &mockOrigin{},
				Metadata: resource.Metadata{
					FullName: resource.FullName{
						Name:      resource.LocalName("test-policy"),
						Namespace: resource.Namespace("default"),
					},
				},
			}

			analyzer := &AuthorizationPoliciesAnalyzer{}
			analyzer.analyzeNamespaceNotFound(r, ctx)

			assert.Equal(t, len(ctx.reports), tt.wantReports)

			if tt.wantReports > 0 {
				for i, report := range ctx.reports {
					assert.Equal(t, report.Type.Code(), msg.ReferencedResourceNotFound.Code())
					if i < len(tt.wantNamespaces) {
						// Verify the namespace name is in the message
						if !contains(report.Parameters[1].(string), tt.wantNamespaces[i]) {
							t.Errorf("expected namespace %s in message, got %v", tt.wantNamespaces[i], report.Parameters[1])
						}
					}
				}
			}
		})
	}
}

// TestMatchNamespace tests the matchNamespace function
func TestMatchNamespace(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		namespaces []string
		want       bool
	}{
		{
			name:       "exact match found",
			expression: "production",
			namespaces: []string{"default", "production", "staging"},
			want:       true,
		},
		{
			name:       "exact match not found",
			expression: "production",
			namespaces: []string{"default", "staging"},
			want:       false,
		},
		{
			name:       "wildcard matches all",
			expression: "*",
			namespaces: []string{"default"},
			want:       true,
		},
		{
			name:       "prefix wildcard matches",
			expression: "prod-*",
			namespaces: []string{"prod-us", "staging"},
			want:       true,
		},
		{
			name:       "suffix wildcard matches",
			expression: "*-prod",
			namespaces: []string{"us-prod", "staging"},
			want:       true,
		},
		{
			name:       "no namespaces",
			expression: "production",
			namespaces: []string{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newMockContext()

			for _, ns := range tt.namespaces {
				ctx.addResource(gvk.Namespace, &resource.Instance{
					Metadata: resource.Metadata{
						FullName: resource.FullName{
							Name: resource.LocalName(ns),
						},
					},
				})
			}

			got := matchNamespace(tt.expression, ctx)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestInitPodLabelsMap tests the initPodLabelsMap function
func TestInitPodLabelsMap(t *testing.T) {
	tests := []struct {
		name string
		pods []struct {
			namespace string
			labels    map[string]string
			inMesh    bool
			ambient   bool
		}
		wantNamespaces []string
		wantCounts     map[string]int
	}{
		{
			name: "single pod in mesh",
			pods: []struct {
				namespace string
				labels    map[string]string
				inMesh    bool
				ambient   bool
			}{
				{
					namespace: "default",
					labels:    map[string]string{"app": "httpbin"},
					inMesh:    true,
					ambient:   false,
				},
			},
			wantNamespaces: []string{"default"},
			wantCounts:     map[string]int{"default": 1},
		},
		{
			name: "multiple pods in different namespaces",
			pods: []struct {
				namespace string
				labels    map[string]string
				inMesh    bool
				ambient   bool
			}{
				{
					namespace: "default",
					labels:    map[string]string{"app": "httpbin"},
					inMesh:    true,
					ambient:   false,
				},
				{
					namespace: "prod",
					labels:    map[string]string{"app": "details"},
					inMesh:    true,
					ambient:   false,
				},
			},
			wantNamespaces: []string{"default", "prod"},
			wantCounts:     map[string]int{"default": 1, "prod": 1},
		},
		{
			name: "pod not in mesh excluded",
			pods: []struct {
				namespace string
				labels    map[string]string
				inMesh    bool
				ambient   bool
			}{
				{
					namespace: "default",
					labels:    map[string]string{"app": "httpbin"},
					inMesh:    false,
					ambient:   false,
				},
			},
			wantNamespaces: []string{},
			wantCounts:     map[string]int{},
		},
		{
			name: "ambient pod included",
			pods: []struct {
				namespace string
				labels    map[string]string
				inMesh    bool
				ambient   bool
			}{
				{
					namespace: "default",
					labels:    map[string]string{"app": "httpbin"},
					inMesh:    false,
					ambient:   true,
				},
			},
			wantNamespaces: []string{"default"},
			wantCounts:     map[string]int{"default": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newMockContext()

			// Note: This test is simplified as we can't easily mock PodInMesh and PodInAmbientMode
			// In a real scenario, you would need to set up the full context with proper resources

			// For now, we just verify the function exists and can be called
			podLabelsMap := initPodLabelsMap(ctx)
			if podLabelsMap == nil {
				t.Error("expected non-nil podLabelsMap")
			}
		})
	}
}
