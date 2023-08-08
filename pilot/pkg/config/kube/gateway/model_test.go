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

	"sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/sets"
)

func TestAllowedReferences_SecretAllowed(t *testing.T) {
	type args struct {
		resourceName string
		namespace    string
	}
	tests := []struct {
		name string
		refs AllowedReferences
		args args
		want bool
	}{
		{
			name: "test parse resource name failed",
			args: args{
				resourceName: "test-not-allowed-resource",
			},
			want: false,
		},
		{
			name: "test not allowed secret",
			args: args{
				resourceName: "kubernetes://test-ns/test-secret",
			},
			refs: map[Reference]map[Reference]*Grants{},
			want: false,
		},
		{
			name: "test allowed all",
			args: args{
				resourceName: "kubernetes://test-ns/test-secret",
				namespace:    "test-ns",
			},
			refs: map[Reference]map[Reference]*Grants{
				Reference{Kind: gvk.KubernetesGateway, Namespace: v1alpha2.Namespace("test-ns")}: {
					Reference{Kind: gvk.Secret, Namespace: v1alpha2.Namespace("test-ns")}: &Grants{
						AllowAll: true,
					},
				},
			},
			want: true,
		},
		{
			name: "test allowed by name",
			args: args{
				resourceName: "kubernetes://test-ns/test-secret",
				namespace:    "test-ns",
			},
			refs: map[Reference]map[Reference]*Grants{
				Reference{Kind: gvk.KubernetesGateway, Namespace: v1alpha2.Namespace("test-ns")}: {
					Reference{Kind: gvk.Secret, Namespace: v1alpha2.Namespace("test-ns")}: &Grants{
						AllowedNames: sets.String{"test-secret": {}},
					},
				},
			},
			want: true,
		},
		{
			name: "test not allowed by name",
			args: args{
				resourceName: "kubernetes://test-ns/test-secret-1",
				namespace:    "test-ns",
			},
			refs: map[Reference]map[Reference]*Grants{
				Reference{Kind: gvk.KubernetesGateway, Namespace: v1alpha2.Namespace("test-ns")}: {
					Reference{Kind: gvk.Secret, Namespace: v1alpha2.Namespace("test-ns")}: &Grants{
						AllowedNames: sets.String{"test-secret": {}},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.refs.SecretAllowed(tt.args.resourceName, tt.args.namespace); got != tt.want {
				t.Errorf("SecretAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllowedReferences_BackendAllowed(t *testing.T) {
	type args struct {
		k                config.GroupVersionKind
		backendName      v1alpha2.ObjectName
		backendNamespace v1alpha2.Namespace
		routeNamespace   string
	}
	tests := []struct {
		name string
		refs AllowedReferences
		args args
		want bool
	}{
		{
			name: "test not allowed backend",
			args: args{
				k:                gvk.KubernetesGateway,
				backendName:      "test-name",
				backendNamespace: "test-ns",
				routeNamespace:   "test-ns",
			},
			refs: map[Reference]map[Reference]*Grants{},
			want: false,
		},
		{
			name: "test allowed all",
			args: args{
				k:                gvk.Service,
				backendName:      "test-name",
				backendNamespace: "test-ns",
				routeNamespace:   "test-ns",
			},
			refs: map[Reference]map[Reference]*Grants{
				Reference{Kind: gvk.Service, Namespace: v1alpha2.Namespace("test-ns")}: {
					Reference{Kind: gvk.Service, Namespace: v1alpha2.Namespace("test-ns")}: &Grants{
						AllowAll: true,
					},
				},
			},
			want: true,
		},
		{
			name: "test allowed by name",
			args: args{
				k:                gvk.Service,
				backendName:      "test-name",
				backendNamespace: "test-ns",
				routeNamespace:   "test-ns",
			},
			refs: map[Reference]map[Reference]*Grants{
				Reference{Kind: gvk.Service, Namespace: v1alpha2.Namespace("test-ns")}: {
					Reference{Kind: gvk.Service, Namespace: v1alpha2.Namespace("test-ns")}: &Grants{
						AllowedNames: sets.String{"test-name": {}},
					},
				},
			},
			want: true,
		},
		{
			name: "test not allowed by name",
			args: args{
				k:                gvk.Service,
				backendName:      "test-name",
				backendNamespace: "test-ns",
				routeNamespace:   "test-ns",
			},
			refs: map[Reference]map[Reference]*Grants{
				Reference{Kind: gvk.Service, Namespace: v1alpha2.Namespace("test-ns")}: {
					Reference{Kind: gvk.Service, Namespace: v1alpha2.Namespace("test-ns")}: &Grants{
						AllowedNames: sets.String{"test-name-1": {}},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.refs.BackendAllowed(tt.args.k, tt.args.backendName, tt.args.backendNamespace, tt.args.routeNamespace); got != tt.want {
				t.Errorf("BackendAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
