// Copyright 2017 Istio Authors
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

package file_test

import (
	"istio.io/istio/pilot/adapter/config/file"
	"istio.io/istio/pilot/adapter/config/memory"
	"istio.io/istio/pilot/model"
	"testing"
)

var (
	allConfigs = []file.FileConfig{{
		Meta: model.ConfigMeta{Type: model.DestinationPolicy.Type, Name: "circuit-breaker"},
		File: "testdata/cb-policy.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "timeout"},
		File: "testdata/timeout-route-rule.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "weighted"},
		File: "testdata/weighted-route.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "fault"},
		File: "testdata/fault-route.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "redirect"},
		File: "testdata/redirect-route.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "rewrite"},
		File: "testdata/rewrite-route.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "websocket"},
		File: "testdata/websocket-route.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.EgressRule.Type, Name: "google"},
		File: "testdata/egress-rule.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.DestinationPolicy.Type, Name: "egress-circuit-breaker"},
		File: "testdata/egress-rule-cb-policy.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "egress-timeout"},
		File: "testdata/egress-rule-timeout-route-rule.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.IngressRule.Type, Name: "world"},
		File: "testdata/ingress-route-world.yaml.golden",
	}, {
		Meta: model.ConfigMeta{Type: model.IngressRule.Type, Name: "foo"},
		File: "testdata/ingress-route-foo.yaml.golden",
	}}
)

func TestAllConfigs(t *testing.T) {
	mockStore := memory.Make(model.IstioConfigTypes)
	configStore := file.NewFileConfigStore(mockStore)

	for i := range allConfigs {
		input := allConfigs[i]
		configStore.CreateFromFile(input)
		_, exists := configStore.GetForFile(input)
		if (!exists) {
			t.Fatalf("missing config ", input)
		}
		// TODO(nmmittler): Compare meta? Do we care?
	}
}
