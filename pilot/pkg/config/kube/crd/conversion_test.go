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

package crd

import (
	"encoding/json"
	"testing"

	gateway "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
)

func TestConvertIstioKind(t *testing.T) {
	if _, err := ConvertObject(collections.VirtualService, &IstioKind{Spec: json.RawMessage(`{"x":1}`)}, "local"); err != nil {
		t.Errorf("error for converting object: %s", err)
	}
}

func TestConvert(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
	}{
		{
			name: "istio",
			cfg: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "test",
					Namespace:        "default",
					Domain:           "cluster",
					ResourceVersion:  "1234",
					Labels:           map[string]string{"label": "value"},
					Annotations:      map[string]string{"annotation": "value"},
				},
				Spec: mock.ExampleVirtualService,
			},
		},
		{
			name: "istio status",
			cfg: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "test",
					Namespace:        "default",
					Domain:           "cluster",
					ResourceVersion:  "1234",
					Labels:           map[string]string{"label": "value"},
					Annotations:      map[string]string{"annotation": "value"},
				},
				Spec: mock.ExampleVirtualService,
				Status: &v1alpha1.IstioStatus{
					Conditions: []*v1alpha1.IstioCondition{
						{Type: "Health"},
					},
				},
			},
		},
		{
			name: "gateway",
			cfg: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.HTTPRoute,
					Name:             "test",
					Namespace:        "default",
					Domain:           "cluster",
				},
				Spec: &gateway.HTTPRouteSpec{
					Hostnames: []gateway.Hostname{"example.com"},
				},
			},
		},
		{
			name: "gateway status",
			cfg: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.HTTPRoute,
					Name:             "test",
					Namespace:        "default",
					Domain:           "cluster",
				},
				Spec: &gateway.HTTPRouteSpec{
					Hostnames: []gateway.Hostname{"example.com"},
				},
				Status: &gateway.HTTPRouteStatus{},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := ConvertConfig(tt.cfg)
			if err != nil {
				t.Errorf("ConvertConfig() => unexpected error %v", err)
			}
			col, _ := collections.All.FindByGroupVersionAliasesKind(tt.cfg.GroupVersionKind)
			got, err := ConvertObject(col, obj, "cluster")
			if err != nil {
				t.Errorf("ConvertObject() => unexpected error %v", err)
			}
			assert.Equal(t, &tt.cfg, got)
		})
	}
}

func TestParseInputs(t *testing.T) {
	if varr, _, err := ParseInputs(""); len(varr) > 0 || err != nil {
		t.Errorf(`ParseInput("") => got %v, %v, want nil, nil`, varr, err)
	}
	if _, _, err := ParseInputs("a"); err == nil {
		t.Error(`ParseInput("a") => got no error`)
	}
	if _, others, err := ParseInputs("apiVersion: v1\nkind: Pod"); err != nil || len(others) != 1 {
		t.Errorf(`ParseInput("kind: Pod") => got %v, %v`, others, err)
	}
	if varr, others, err := ParseInputs("---\n"); err != nil || len(varr) != 0 || len(others) != 0 {
		t.Errorf(`ParseInput("---") => got %v, %v, %v`, varr, others, err)
	}
	if _, _, err := ParseInputs("apiVersion: networking.istio.io/v1alpha3\nkind: VirtualService\nspec:\n  destination: x"); err == nil {
		t.Error("ParseInput(bad spec) => got no error")
	}
	if _, _, err := ParseInputs("apiVersion: networking.istio.io/v1alpha3\nkind: VirtualService\nspec:\n  destination:\n    service:"); err == nil {
		t.Error("ParseInput(invalid spec) => got no error")
	}

	// nolint: lll
	validInput := `{"apiVersion": "networking.istio.io/v1alpha3", "kind":"VirtualService", "spec":{"hosts":["foo"],"http":[{"route":[{"destination":{"host":"bar"},"weight":100}]}]}}`
	varr, _, err := ParseInputs(validInput)
	if err != nil || len(varr) == 0 {
		t.Errorf("ParseInputs(correct input) => got %v, %v", varr, err)
	}
}
