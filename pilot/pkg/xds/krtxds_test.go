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

package xds

import (
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/krt"
	pkgmodel "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestGetGatewayNameForProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   *model.Proxy
		wantNN  types.NamespacedName
		wantErr bool
	}{
		{
			name: "gateway name from label",
			proxy: &model.Proxy{
				Labels: map[string]string{
					gatewayNameLabel: "my-gateway",
				},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
				},
			},
			wantNN: types.NamespacedName{
				Namespace: "default",
				Name:      "my-gateway",
			},
			wantErr: false,
		},
		{
			name: "gateway name from role fallback - same namespace",
			proxy: &model.Proxy{
				Labels: map[string]string{},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
					Raw: map[string]any{
						"role": "default~my-gateway",
					},
				},
			},
			wantNN: types.NamespacedName{
				Namespace: "default",
				Name:      "my-gateway",
			},
			wantErr: false,
		},
		{
			name: "gateway name from role fallback - different namespace",
			proxy: &model.Proxy{
				Labels: map[string]string{},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "wrong-namespace",
					Raw: map[string]any{
						"role": "other-ns~gw",
					},
				},
			},
			wantNN: types.NamespacedName{
				Namespace: "other-ns",
				Name:      "gw",
			},
			wantErr: false,
		},
		{
			name: "label takes precedence over role - gateway names should be the same regardless",
			proxy: &model.Proxy{
				Labels: map[string]string{
					gatewayNameLabel: "label-gateway",
				},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
					Raw: map[string]any{
						"role": "other-ns~role-gateway",
					},
				},
			},
			wantNN: types.NamespacedName{
				Namespace: "default",
				Name:      "label-gateway",
			},
			wantErr: false,
		},
		{
			name: "no gateway name available",
			proxy: &model.Proxy{
				Labels: map[string]string{},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
					Raw:       map[string]any{},
				},
			},
			wantNN:  types.NamespacedName{},
			wantErr: true,
		},
		{
			name: "invalid role format - missing tilde",
			proxy: &model.Proxy{
				Labels: map[string]string{},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
					Raw: map[string]any{
						"role": "just-a-name",
					},
				},
			},
			wantNN:  types.NamespacedName{},
			wantErr: true,
		},
		{
			name: "role is not a string",
			proxy: &model.Proxy{
				Labels: map[string]string{},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
					Raw: map[string]any{
						"role": 123,
					},
				},
			},
			wantNN:  types.NamespacedName{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNN, err := getGatewayNameForProxy(tt.proxy)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Errorf("getGatewayNameForProxy() gotErr = %v, wantErr %v", gotErr, tt.wantErr)
			}
			if gotNN != tt.wantNN {
				t.Errorf("getGatewayNameForProxy() gotNN = %v, want %v", gotNN, tt.wantNN)
			}
		})
	}
}

func TestGenerateDeltas_RoleNamespace(t *testing.T) {
	// Create a simple test collection with some discovery resources
	staticCollection := krt.NewStaticCollection(nil, []DiscoveryResource{
		{
			Resource: &discovery.Resource{
				Name:     "resource-1",
				Resource: &anypb.Any{},
			},
			ForGateway: &types.NamespacedName{
				Namespace: "other-ns",
				Name:      "gw",
			},
		},
		{
			Resource: &discovery.Resource{
				Name:     "resource-2",
				Resource: &anypb.Any{},
			},
			ForGateway: &types.NamespacedName{
				Namespace: "default",
				Name:      "gw",
			},
		},
		{
			Resource: &discovery.Resource{
				Name:     "global-resource",
				Resource: &anypb.Any{},
			},
			ForGateway: nil, // Global resource
		},
	})

	gen := CollectionGenerator{
		PerGateway: true,
		Col:        staticCollection,
	}

	tests := []struct {
		name          string
		proxy         *model.Proxy
		wantResources []string
		wantError     bool
	}{
		{
			name: "proxy with role pointing to other-ns should get other-ns resource",
			proxy: &model.Proxy{
				Labels: map[string]string{},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "wrong-namespace", // This should be ignored
					Raw: map[string]any{
						"role": "other-ns~gw", // This specifies the correct namespace
					},
				},
			},
			wantResources: []string{"resource-1", "global-resource"},
			wantError:     false,
		},
		{
			name: "proxy with label should use proxy namespace",
			proxy: &model.Proxy{
				Labels: map[string]string{
					gatewayNameLabel: "gw",
				},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
				},
			},
			wantResources: []string{"resource-2", "global-resource"},
			wantError:     false,
		},
		{
			name: "proxy without gateway name should error",
			proxy: &model.Proxy{
				Labels: map[string]string{},
				Metadata: &pkgmodel.NodeMetadata{
					Namespace: "default",
					Raw:       map[string]any{},
				},
			},
			wantResources: nil,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &model.PushRequest{
				Reason: model.NewReasonStats(model.ProxyRequest),
			}
			w := &model.WatchedResource{
				TypeUrl:       "test.type.url",
				ResourceNames: sets.New[string](),
			}

			resources, _, _, _, err := gen.GenerateDeltas(tt.proxy, req, w)
			if (err != nil) != tt.wantError {
				t.Errorf("GenerateDeltas() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err != nil {
				return
			}

			gotResourceNames := make([]string, len(resources))
			for i, r := range resources {
				gotResourceNames[i] = r.Name
			}

			assert.Equal(t, sets.New(tt.wantResources...), sets.New(gotResourceNames...))
		})
	}
}
