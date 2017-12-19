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

package routing

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
)

var tests = []struct {
	Name          string
	ServiceConfig string
	Configs       []string
	E             string
}{
	{
		Name:          "basic",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.RuleR1I1,
		},
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Conditional: <NONE>
        [#0] i1.t1.istio-system {I}
`,
	},

	{
		Name:          "multi-instance",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.InstanceI2,
			data.RuleR2I1I2,
		},
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Conditional: <NONE>
        [#0] i1.t1.istio-system {I}
        [#1] i2.t1.istio-system {I}
`,
	},

	{
		Name:          "multi-instance-conditional",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.InstanceI2,
			data.InstanceI3,
			data.RuleR3I1I2,
		},
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Conditional: match(target.name, "*")
        [#0] i1.t1.istio-system {I}
        [#1] i2.t1.istio-system {I}
`,
	},

	{
		Name:          "multi-rule-to-same-target",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.InstanceI2,
			data.InstanceI3,
			data.RuleR1I1,
			data.RuleR2I1I2,
		},
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Conditional: <NONE>
        [#0] i1.t1.istio-system {I}
        [#1] i1.t1.istio-system {I}
        [#2] i2.t1.istio-system {I}
`,
	},

	{
		Name:          "multi-rule-to-same-target-with-conditional",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.InstanceI2,
			data.InstanceI3,
			data.RuleR1I1,
			data.RuleR3I1I2,
		},
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Conditional: <NONE>
        [#0] i1.t1.istio-system {I}
      [#1]
        Conditional: match(target.name, "*")
        [#0] i1.t1.istio-system {I}
        [#1] i2.t1.istio-system {I}
`,
	},
}

func TestFoo(t *testing.T) {
	for _, tst := range tests {
		t.Run(tst.Name, func(tt *testing.T) {

			templates := data.BuildTemplates(nil)
			adapters := data.BuildAdapters(nil)

			serviceConfig := tst.ServiceConfig
			if len(serviceConfig) == 0 {
				serviceConfig = data.ServiceConfig
			}

			globalConfig := data.JoinConfigs(tst.Configs...)

			s := util.GetSnapshot(templates, adapters, serviceConfig, globalConfig)

			ht := handler.Instantiate(handler.Empty(), s, &data.FakeEnv{})

			expb := compiled.NewBuilder(s.Attributes)
			t := BuildTable(ht, s, expb, "destination.service", "istio-system", true)

			actual := fmt.Sprintf("%v", t)

			if strings.TrimSpace(actual) != strings.TrimSpace(tst.E) {
				tt.Logf("Config:\n%s\n\n", globalConfig)
				tt.Logf("Snapshot:\n%s\n\n", s)
				tt.Fatalf("%v\n!=\n%v", actual, tst.E)
			}
		})
	}
}
