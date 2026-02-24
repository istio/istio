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
	"reflect"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestOmitNil(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{
			name: "no nils",
			in:   map[string]any{"a": 1, "b": "c"},
			want: map[string]any{"a": 1, "b": "c"},
		},
		{
			name: "top level nil",
			in:   map[string]any{"a": nil, "b": "c"},
			want: map[string]any{"b": "c"},
		},
		{
			name: "nested nil",
			in:   map[string]any{"a": map[string]any{"x": nil, "y": 2}, "b": "c"},
			want: map[string]any{"a": map[string]any{"y": 2}, "b": "c"},
		},
		{
			name: "slice with nil",
			in:   []any{1, nil, 3},
			want: []any{1, 3},
		},
		{
			name: "nested slice nil",
			in:   map[string]any{"a": []any{nil, map[string]any{"x": nil}}},
			want: nil,
		},
		{
			name: "all nils map",
			in:   map[string]any{"a": nil, "b": map[string]any{"x": nil}},
			want: nil,
		},
		{
			name: "complex mixed",
			in: map[string]any{
				"global": map[string]any{
					"proxy": map[string]any{
						"resources": map[string]any{
							"limits": map[string]any{
								"cpu":    nil,
								"memory": "500Mi",
							},
						},
					},
				},
			},
			want: map[string]any{
				"global": map[string]any{
					"proxy": map[string]any{
						"resources": map[string]any{
							"limits": map[string]any{
								"memory": "500Mi",
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := omitNil(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func otelSemConvMeshConfig() *meshconfig.MeshConfig {
	return &meshconfig.MeshConfig{
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "otel",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
					Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
						Service:                    "otel-collector",
						Port:                       4317,
						ServiceAttributeEnrichment: meshconfig.MeshConfig_ExtensionProvider_OTEL_SEMANTIC_CONVENTIONS,
					},
				},
			},
		},
	}
}

func TestOtelResourceAttributes(t *testing.T) {
	tests := []struct {
		name        string
		mc          *meshconfig.MeshConfig
		annotations map[string]string
		labels      map[string]string
		namespace   string
		want        string
	}{
		{
			name:      "no OTel semantic conventions provider returns empty",
			mc:        &meshconfig.MeshConfig{},
			namespace: "default",
			want:      "",
		},
		{
			name: "ISTIO_CANONICAL provider returns empty",
			mc: &meshconfig.MeshConfig{
				ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
					{
						Name: "otel",
						Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
							Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{
								Service:                    "otel-collector",
								Port:                       4317,
								ServiceAttributeEnrichment: meshconfig.MeshConfig_ExtensionProvider_ISTIO_CANONICAL,
							},
						},
					},
				},
			},
			namespace: "default",
			want:      "",
		},
		{
			name:      "defaults: namespace + Pod UID",
			mc:        otelSemConvMeshConfig(),
			namespace: "my-namespace",
			want:      "service.namespace=my-namespace,service.instance.id=$(POD_UID)",
		},
		{
			name:      "all annotations override defaults",
			mc:        otelSemConvMeshConfig(),
			namespace: "my-namespace",
			annotations: map[string]string{
				"resource.opentelemetry.io/service.namespace":   "custom-ns",
				"resource.opentelemetry.io/service.version":     "v2.0",
				"resource.opentelemetry.io/service.instance.id": "custom-id",
			},
			labels: map[string]string{
				"app.kubernetes.io/version": "should-not-use",
			},
			want: "service.namespace=custom-ns,service.version=v2.0,service.instance.id=custom-id",
		},
		{
			name:      "service.version from label",
			mc:        otelSemConvMeshConfig(),
			namespace: "default",
			labels: map[string]string{
				"app.kubernetes.io/version": "v1.2.3",
			},
			want: "service.namespace=default,service.version=v1.2.3,service.instance.id=$(POD_UID)",
		},
		{
			name:      "service.namespace annotation overrides k8s namespace",
			mc:        otelSemConvMeshConfig(),
			namespace: "actual-namespace",
			annotations: map[string]string{
				"resource.opentelemetry.io/service.namespace": "annotated-ns",
			},
			want: "service.namespace=annotated-ns,service.instance.id=$(POD_UID)",
		},
		{
			name:      "service.version annotation overrides label",
			mc:        otelSemConvMeshConfig(),
			namespace: "default",
			annotations: map[string]string{
				"resource.opentelemetry.io/service.version": "annotated-v1",
			},
			labels: map[string]string{
				"app.kubernetes.io/version": "label-v2",
			},
			want: "service.namespace=default,service.version=annotated-v1,service.instance.id=$(POD_UID)",
		},
		{
			name:      "empty namespace",
			mc:        otelSemConvMeshConfig(),
			namespace: "",
			want:      "service.instance.id=$(POD_UID)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := otelResourceAttributes(tt.mc, tt.annotations, tt.labels, tt.namespace)
			if got != tt.want {
				t.Errorf("otelResourceAttributes() = %q, want %q", got, tt.want)
			}
		})
	}
}
