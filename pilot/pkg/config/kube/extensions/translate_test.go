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

package extensions

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	extensions "istio.io/api/extensions/v1alpha1"
	typeapi "istio.io/api/type/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
)

func TestTranslateWasmPlugin(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	pluginCfg, _ := structpb.NewStruct(map[string]any{"key": "value"})
	priority := &wrapperspb.Int32Value{Value: 10}
	vmCfg := &extensions.VmConfig{Env: []*extensions.EnvVar{{Name: "FOO", Value: "bar"}}}
	selector := &typeapi.WorkloadSelector{MatchLabels: map[string]string{"app": "my-app"}}
	singularRef := &typeapi.PolicyTargetReference{Kind: "Gateway", Name: "my-gateway"}
	pluralRefs := []*typeapi.PolicyTargetReference{
		{Kind: "Service", Name: "svc-a"},
		{Kind: "Service", Name: "svc-b"},
	}

	cases := []struct {
		desc  string
		input config.Config
		want  *config.Config
	}{
		{
			desc: "non-WasmPlugin spec returns nil",
			input: config.Config{
				Meta: config.Meta{Name: "test", Namespace: "default"},
				Spec: &extensions.TrafficExtension{},
			},
			want: nil,
		},
		{
			desc: "meta fields are copied correctly",
			input: config.Config{
				Meta: config.Meta{
					Name:              "my-plugin",
					Namespace:         "my-ns",
					ResourceVersion:   "42",
					CreationTimestamp: ts,
				},
				Spec: &extensions.WasmPlugin{Url: "oci://example.com/filter:v1"},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind:  gvk.TrafficExtension,
					Name:              "my-plugin" + syntheticMarker,
					Namespace:         "my-ns",
					ResourceVersion:   "42",
					CreationTimestamp: ts,
				},
				Spec: &extensions.TrafficExtension{
					FilterConfig: &extensions.TrafficExtension_Wasm{
						Wasm: &extensions.WasmConfig{Url: "oci://example.com/filter:v1"},
					},
				},
			},
		},
		{
			desc: "phase UNSPECIFIED maps to UNSPECIFIED",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{Phase: extensions.PluginPhase_UNSPECIFIED_PHASE},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					Phase:        extensions.TrafficExtension_UNSPECIFIED,
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "phase AUTHN maps to AUTHN",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{Phase: extensions.PluginPhase_AUTHN},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					Phase:        extensions.TrafficExtension_AUTHN,
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "phase AUTHZ maps to AUTHZ",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{Phase: extensions.PluginPhase_AUTHZ},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					Phase:        extensions.TrafficExtension_AUTHZ,
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "phase STATS maps to STATS",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{Phase: extensions.PluginPhase_STATS},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					Phase:        extensions.TrafficExtension_STATS,
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "all wasm config fields are copied",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{
					Url:             "oci://example.com/filter:v1",
					Sha256:          "abc123",
					ImagePullPolicy: extensions.PullPolicy_Always,
					ImagePullSecret: "my-secret",
					PluginConfig:    pluginCfg,
					PluginName:      "my-plugin",
					FailStrategy:    extensions.FailStrategy_FAIL_OPEN,
					VmConfig:        vmCfg,
					Type:            extensions.PluginType_HTTP,
					Priority:        priority,
				},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					Priority: priority,
					FilterConfig: &extensions.TrafficExtension_Wasm{
						Wasm: &extensions.WasmConfig{
							Url:             "oci://example.com/filter:v1",
							Sha256:          "abc123",
							ImagePullPolicy: extensions.PullPolicy_Always,
							ImagePullSecret: "my-secret",
							PluginConfig:    pluginCfg,
							PluginName:      "my-plugin",
							FailStrategy:    extensions.FailStrategy_FAIL_OPEN,
							VmConfig:        vmCfg,
							Type:            extensions.PluginType_HTTP,
						},
					},
				},
			},
		},
		{
			desc: "selector is passed through",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{Selector: selector},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					Selector:     selector,
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "plural targetRefs passed through as-is",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{TargetRefs: pluralRefs},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					TargetRefs:   pluralRefs,
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "singular targetRef promoted into TargetRefs",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{TargetRef: singularRef},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					TargetRefs:   []*typeapi.PolicyTargetReference{singularRef},
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "plural targetRefs takes precedence over singular",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{TargetRef: singularRef, TargetRefs: pluralRefs},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					TargetRefs:   pluralRefs,
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
		{
			desc: "match selectors are translated and wired through",
			input: config.Config{
				Meta: config.Meta{Name: "p", Namespace: "ns"},
				Spec: &extensions.WasmPlugin{
					Match: []*extensions.WasmPlugin_TrafficSelector{
						{Mode: typeapi.WorkloadMode_SERVER, Ports: []*typeapi.PortSelector{{Number: 9090}}},
					},
				},
			},
			want: &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TrafficExtension,
					Name:             "p" + syntheticMarker,
					Namespace:        "ns",
				},
				Spec: &extensions.TrafficExtension{
					Match: []*extensions.TrafficSelector{
						{Mode: typeapi.WorkloadMode_SERVER, Ports: []*typeapi.PortSelector{{Number: 9090}}},
					},
					FilterConfig: &extensions.TrafficExtension_Wasm{Wasm: &extensions.WasmConfig{}},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := translateWasmPlugin(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestConvertTrafficSelectors(t *testing.T) {
	ports := []*typeapi.PortSelector{{Number: 8080}}

	cases := []struct {
		desc  string
		input []*extensions.WasmPlugin_TrafficSelector
		want  []*extensions.TrafficSelector
	}{
		{
			desc:  "nil input returns nil",
			input: nil,
			want:  nil,
		},
		{
			desc:  "empty input returns nil",
			input: []*extensions.WasmPlugin_TrafficSelector{},
			want:  nil,
		},
		{
			desc:  "nil element is skipped",
			input: []*extensions.WasmPlugin_TrafficSelector{nil},
			want:  []*extensions.TrafficSelector{},
		},
		{
			desc: "mode and ports are copied",
			input: []*extensions.WasmPlugin_TrafficSelector{
				{Mode: typeapi.WorkloadMode_CLIENT, Ports: ports},
			},
			want: []*extensions.TrafficSelector{
				{Mode: typeapi.WorkloadMode_CLIENT, Ports: ports},
			},
		},
		{
			desc: "nil elements mixed in are skipped",
			input: []*extensions.WasmPlugin_TrafficSelector{
				{Mode: typeapi.WorkloadMode_CLIENT},
				nil,
				{Mode: typeapi.WorkloadMode_SERVER},
			},
			want: []*extensions.TrafficSelector{
				{Mode: typeapi.WorkloadMode_CLIENT},
				{Mode: typeapi.WorkloadMode_SERVER},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := convertTrafficSelectors(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
