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
	"testing"

	"istio.io/istio/pilot/adapter/config/file"
	"istio.io/istio/pilot/adapter/config/memory"
	"istio.io/istio/pilot/model"
)

func newConfig(schemaType string, name string, filePath string) *file.ConfigRef {
	return file.Defaults(&file.ConfigRef{
		Meta: &model.ConfigMeta{
			Name: name,
			Type: schemaType,
		},
		FilePath: filePath,
	})
}

func TestAllConfigs(t *testing.T) {
	cases := []*file.ConfigRef{
		newConfig(model.DestinationPolicy.Type, "circuit-breaker", "testdata/cb-policy.yaml.golden"),
		newConfig(model.RouteRule.Type, "timeout", "testdata/timeout-route-rule.yaml.golden"),
		newConfig(model.RouteRule.Type, "weighted", "testdata/weighted-route.yaml.golden"),
		newConfig(model.RouteRule.Type, "fault", "testdata/fault-route.yaml.golden"),
		newConfig(model.RouteRule.Type, "redirect", "testdata/redirect-route.yaml.golden"),
		newConfig(model.RouteRule.Type, "rewrite", "testdata/rewrite-route.yaml.golden"),
		newConfig(model.RouteRule.Type, "websocket", "testdata/websocket-route.yaml.golden"),
		newConfig(model.EgressRule.Type, "google", "testdata/egress-rule.yaml.golden"),
		newConfig(model.DestinationPolicy.Type, "egress-circuit-breaker", "testdata/egress-rule-cb-policy.yaml.golden"),
		newConfig(model.RouteRule.Type, "egress-timeout", "testdata/egress-rule-timeout-route-rule.yaml.golden"),
		newConfig(model.IngressRule.Type, "world", "testdata/ingress-route-world.yaml.golden"),
		newConfig(model.IngressRule.Type, "foo", "testdata/ingress-route-foo.yaml.golden"),
	}

	for _, input := range cases {
		t.Run("test case name", func(t *testing.T) {
			mockStore := memory.Make(model.IstioConfigTypes)
			configStore := file.NewConfigStore(mockStore)
			inputMeta := input.Meta
			if err := configStore.CreateFromFile(*input); err != nil {
				t.Fatalf("failed creating config ", input)
			} else if _, exists := configStore.Get(inputMeta.Type, inputMeta.Name, inputMeta.Namespace); !exists {
				t.Fatalf("missing config ", input)
			}
		})
		// TODO(nmittler): Do we care about doing a deep comparison?
	}
}
