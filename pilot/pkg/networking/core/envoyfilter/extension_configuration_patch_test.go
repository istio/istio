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

package envoyfilter

import (
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/test/util/assert"
)

func TestInsertedExtensionConfig(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_EXTENSION_CONFIG,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"add-extension-config1"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_EXTENSION_CONFIG,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"add-extension-config2"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_EXTENSION_CONFIG,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"name":"merge-extension-config"}`),
			},
		},
	}

	testCases := []struct {
		name                string
		requestedNames      []string
		wantExtensionConfig []*core.TypedExtensionConfig
	}{
		{
			name:           "add extension config",
			requestedNames: []string{"add-extension-config1", "add-extension-config2"},
			wantExtensionConfig: []*core.TypedExtensionConfig{
				{
					Name: "add-extension-config1",
				},
				{
					Name: "add-extension-config2",
				},
			},
		},
		{
			name:                "miss extension config",
			requestedNames:      []string{"random-extension-config"},
			wantExtensionConfig: []*core.TypedExtensionConfig{},
		},
		{
			name:                "only add extension config",
			requestedNames:      []string{"merge-extension-config"},
			wantExtensionConfig: []*core.TypedExtensionConfig{},
		},
	}

	serviceDiscovery := memory.NewServiceDiscovery()
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(env, nil, nil)

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			gotConfigs := InsertedExtensionConfigurations(push.EnvoyFilters(&model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"}),
				c.requestedNames)
			if len(gotConfigs) != len(c.wantExtensionConfig) {
				t.Fatalf("number of extension config got %v want %v", len(gotConfigs), len(c.wantExtensionConfig))
			}
			for i, gc := range gotConfigs {
				assert.Equal(t, gc, c.wantExtensionConfig[i])
			}
		})
	}
}
