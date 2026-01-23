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

package k8sgateway

import (
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// mockContext implements a minimal analysis.Context for testing gatewayPodsLabelMap
type mockContext struct {
	pods []*resource.Instance
}

func (m *mockContext) ForEach(gvkType config.GroupVersionKind, fn analysis.IteratorFn) {
	for _, pod := range m.pods {
		if !fn(pod) {
			break
		}
	}
}

func (m *mockContext) Exists(gvkType config.GroupVersionKind, name resource.FullName) bool {
	return false
}

func (m *mockContext) Find(gvkType config.GroupVersionKind, name resource.FullName) *resource.Instance {
	return nil
}

func (m *mockContext) Report(gvkType config.GroupVersionKind, msg diag.Message) {}

func (m *mockContext) Canceled() bool {
	return false
}

func (m *mockContext) SetAnalyzer(name string) {}

func TestGatewayPodsLabelMap(t *testing.T) {
	tests := []struct {
		name           string
		pods           []*resource.Instance
		expectedNsKeys []string
		expectedCount  int
	}{
		{
			name:           "no pods",
			pods:           []*resource.Instance{},
			expectedNsKeys: []string{},
			expectedCount:  0,
		},
		{
			name: "pods without gateway label",
			pods: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "pod-1"),
						Labels:   map[string]string{"app": "myapp"},
					},
				},
			},
			expectedNsKeys: []string{},
			expectedCount:  0,
		},
		{
			name: "single pod with gateway label",
			pods: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "gateway-pod-1"),
						Labels: map[string]string{
							label.IoK8sNetworkingGatewayGatewayName.Name: "my-gateway",
							"app": "gateway",
						},
					},
				},
			},
			expectedNsKeys: []string{"default"},
			expectedCount:  1,
		},
		{
			name: "multiple pods with gateway label in same namespace",
			pods: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "gateway-pod-1"),
						Labels: map[string]string{
							label.IoK8sNetworkingGatewayGatewayName.Name: "gateway-1",
						},
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "gateway-pod-2"),
						Labels: map[string]string{
							label.IoK8sNetworkingGatewayGatewayName.Name: "gateway-2",
						},
					},
				},
			},
			expectedNsKeys: []string{"default"},
			expectedCount:  2,
		},
		{
			name: "pods in different namespaces",
			pods: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("ns1", "gateway-pod-1"),
						Labels: map[string]string{
							label.IoK8sNetworkingGatewayGatewayName.Name: "gateway-1",
						},
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("ns2", "gateway-pod-2"),
						Labels: map[string]string{
							label.IoK8sNetworkingGatewayGatewayName.Name: "gateway-2",
						},
					},
				},
			},
			expectedNsKeys: []string{"ns1", "ns2"},
			expectedCount:  2,
		},
		{
			name: "mixed pods with and without gateway label",
			pods: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "gateway-pod"),
						Labels: map[string]string{
							label.IoK8sNetworkingGatewayGatewayName.Name: "my-gateway",
						},
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "regular-pod"),
						Labels:   map[string]string{"app": "myapp"},
					},
				},
			},
			expectedNsKeys: []string{"default"},
			expectedCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &mockContext{pods: tt.pods}
			result := gatewayPodsLabelMap(ctx)

			// Check number of namespaces
			if len(result) != len(tt.expectedNsKeys) {
				t.Errorf("expected %d namespace keys, got %d", len(tt.expectedNsKeys), len(result))
			}

			// Check namespace keys exist
			for _, ns := range tt.expectedNsKeys {
				if _, ok := result[ns]; !ok {
					t.Errorf("expected namespace %q in result", ns)
				}
			}

			// Check total count of pods
			totalPods := 0
			for _, pods := range result {
				totalPods += len(pods)
			}
			if totalPods != tt.expectedCount {
				t.Errorf("expected %d total pods, got %d", tt.expectedCount, totalPods)
			}
		})
	}
}

func TestSelectorAnalyzerMetadata(t *testing.T) {
	analyzer := &SelectorAnalyzer{}
	metadata := analyzer.Metadata()

	if metadata.Name != "k8sgateway.SelectorAnalyzer" {
		t.Errorf("expected name 'k8sgateway.SelectorAnalyzer', got %q", metadata.Name)
	}

	expectedInputs := []interface{}{
		gvk.AuthorizationPolicy,
		gvk.RequestAuthentication,
		gvk.Telemetry,
		gvk.WasmPlugin,
		gvk.Pod,
	}

	if len(metadata.Inputs) != len(expectedInputs) {
		t.Errorf("expected %d inputs, got %d", len(expectedInputs), len(metadata.Inputs))
	}
}
