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

package credentials

import (
	"strings"
	"testing"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
)

func TestParseResourceName(t *testing.T) {
	cases := []struct {
		name             string
		resource         string
		defaultNamespace string
		expected         SecretResource
		err              bool
	}{
		{
			name:             "simple",
			resource:         "kubernetes://cert",
			defaultNamespace: "default",
			expected: SecretResource{
				ResourceType: KubernetesSecretType,
				ResourceKind: kind.Secret,
				Name:         "cert",
				Namespace:    "default",
				ResourceName: "kubernetes://cert",
				Cluster:      "cluster",
			},
		},
		{
			name:             "with namespace",
			resource:         "kubernetes://namespace/cert",
			defaultNamespace: "default",
			expected: SecretResource{
				ResourceType: KubernetesSecretType,
				ResourceKind: kind.Secret,
				Name:         "cert",
				Namespace:    "namespace",
				ResourceName: "kubernetes://namespace/cert",
				Cluster:      "cluster",
			},
		},
		{
			name:             "kubernetes-gateway",
			resource:         "kubernetes-gateway://namespace/cert",
			defaultNamespace: "default",
			expected: SecretResource{
				ResourceType: KubernetesGatewaySecretType,
				ResourceKind: kind.Secret,
				Name:         "cert",
				Namespace:    "namespace",
				ResourceName: "kubernetes-gateway://namespace/cert",
				Cluster:      "config",
			},
		},
		{
			name:             "configmap",
			resource:         "configmap://namespace/cert",
			defaultNamespace: "default",
			expected: SecretResource{
				ResourceType: KubernetesConfigMapType,
				ResourceKind: kind.ConfigMap,
				Name:         "cert",
				Namespace:    "namespace",
				ResourceName: "configmap://namespace/cert",
				Cluster:      "config",
			},
		},
		{
			name:             "configmap-without-namespace",
			resource:         "configmap://cert",
			defaultNamespace: "default",
			err:              true,
		},
		{
			name:             "kubernetes-gateway without namespace",
			resource:         "kubernetes-gateway://cert",
			defaultNamespace: "default",
			err:              true,
		},
		{
			name:             "kubernetes-gateway with empty namespace",
			resource:         "kubernetes-gateway:///cert",
			defaultNamespace: "default",
			err:              true,
		},
		{
			name:             "kubernetes-gateway with empty name",
			resource:         "kubernetes-gateway://ns/",
			defaultNamespace: "default",
			err:              true,
		},
		{
			name:             "plain",
			resource:         "cert",
			defaultNamespace: "default",
			err:              true,
		},
		{
			name:             "non kubernetes",
			resource:         "vault://cert",
			defaultNamespace: "default",
			err:              true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResourceName(tt.resource, tt.defaultNamespace, "cluster", "config")
			if tt.err != (err != nil) {
				t.Fatalf("expected err=%v but got err=%v", tt.err, err)
			}
			if got != tt.expected {
				t.Fatalf("want %+v, got %+v", tt.expected, got)
			}
		})
	}
}

func TestToResourceName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"foo", "kubernetes://foo"},
		{"kubernetes://bar", "kubernetes://bar"},
		{"kubernetes-gateway://bar", "kubernetes-gateway://bar"},
		{"builtin://", "default"},
		{"builtin://extra", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToResourceName(tt.name); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToKubernetesGatewayResource(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		want      string
	}{
		{"foo", "ns", "kubernetes-gateway://ns/foo"},
		{"builtin://", "anything", "builtin://"},
		{"builtin://extra", "anything", "builtin://"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToKubernetesGatewayResource(tt.namespace, tt.name); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecretResourceKey(t *testing.T) {
	tests := []struct {
		name          string
		resource      SecretResource
		want          string
		resource1     *SecretResource
		resource2     *SecretResource
		wantDifferent bool
	}{
		{
			name: "kubernetes secret without namespace in resource name",
			resource: SecretResource{
				ResourceType: KubernetesSecretType,
				ResourceKind: kind.Secret,
				Name:         "bookinfo-certs",
				Namespace:    "bookinfo",
				ResourceName: "kubernetes://bookinfo-certs",
				Cluster:      cluster.ID("cluster1"),
			},
			want: "kubernetes://bookinfo-certs/kubernetes/Secret/bookinfo-certs/bookinfo/cluster1",
		},
		{
			name: "kubernetes secret with namespace in resource name",
			resource: SecretResource{
				ResourceType: KubernetesSecretType,
				ResourceKind: kind.Secret,
				Name:         "bookinfo-certs",
				Namespace:    "bookinfo",
				ResourceName: "kubernetes://bookinfo/bookinfo-certs",
				Cluster:      cluster.ID("cluster1"),
			},
			want: "kubernetes://bookinfo/bookinfo-certs/kubernetes/Secret/bookinfo-certs/bookinfo/cluster1",
		},
		{
			name: "same secret name and namespace but different resource names have different keys",
			resource1: &SecretResource{
				ResourceType: KubernetesSecretType,
				ResourceKind: kind.Secret,
				Name:         "bookinfo-certs",
				Namespace:    "bookinfo",
				ResourceName: "kubernetes://bookinfo-certs",
				Cluster:      cluster.ID("cluster1"),
			},
			resource2: &SecretResource{
				ResourceType: KubernetesSecretType,
				ResourceKind: kind.Secret,
				Name:         "bookinfo-certs",
				Namespace:    "bookinfo",
				ResourceName: "kubernetes://bookinfo/bookinfo-certs",
				Cluster:      cluster.ID("cluster1"),
			},
			wantDifferent: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want != "" {
				if got := tt.resource.Key(); got != tt.want {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
			if tt.wantDifferent && tt.resource1 != nil && tt.resource2 != nil {
				key1 := tt.resource1.Key()
				key2 := tt.resource2.Key()
				if key1 == key2 {
					t.Fatalf("keys should be different: key1=%v, key2=%v", key1, key2)
				}
				// Verify that the difference is in the ResourceName part
				if !strings.Contains(key1, "kubernetes://bookinfo-certs") || !strings.Contains(key2, "kubernetes://bookinfo/bookinfo-certs") {
					t.Fatalf("keys should contain the ResourceName: key1=%v, key2=%v", key1, key2)
				}
			}
		})
	}
}
