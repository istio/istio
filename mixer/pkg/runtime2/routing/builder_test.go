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
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
)

var tests = []struct {
	// Name of the test
	Name string

	//ServiceConfig to use
	ServiceConfig string

	// Configs is the collection of all runtime configs to use.
	Configs []string

	// E is the expected routing table output w/ debug info.
	E string
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
        Condition: <NONE>
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
        Condition: <NONE>
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
        Condition: match(target.name, "*")
        [#0] i1.t1.istio-system {I}
        [#1] i2.t1.istio-system {I}
`,
	},

	{
		Name:          "multi-instance-multi-conditional",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.InstanceI2,
			data.InstanceI3,
			data.RuleR3I1I2,
			data.RuleR8I1I2AnotherConditional,
		},
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Condition: match(target.name, "*")
        [#0] i1.t1.istio-system {I}
        [#1] i2.t1.istio-system {I}
      [#1]
        Condition: target.name.startsWith("foo")
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
        Condition: <NONE>
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
        Condition: <NONE>
        [#0] i1.t1.istio-system {I}
      [#1]
        Condition: match(target.name, "*")
        [#0] i1.t1.istio-system {I}
        [#1] i2.t1.istio-system {I}
`,
	},

	{
		Name:          "multi-rule-to-multiple-target",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH4AnotherHandler,
			data.HandlerH1,
			data.InstanceI1,
			data.InstanceI2,
			data.InstanceI3,
			data.RuleR1I1,
			data.RuleR9H4AnotherHandler,
		},
		E: `
 [Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] i1.t1.istio-system {I}
    [#1] h4.a1.istio-system {H}
        [#0]
          Condition: <NONE>
          [#0] i1.t1.istio-system {I}
`,
	},

	{
		Name:          "bad-condition",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.RuleR4I1BadCondition,
		},
		// No routes are set, as the only rule is bad.
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
`,
	},

	{
		Name:          "bad-handler-name",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1,
			data.InstanceI1,
			data.RuleR5I1BadHandlerName,
		},
		// No routes are set, as the only rule is bad.
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
`,
	},

	{
		Name:          "bad-handler-builder",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH2BadBuilder,
			data.InstanceI1,
			data.RuleR6I1BadHandlerBuilder,
		},
		// No routes are set, as the only rule is bad.
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
`,
	},

	{
		Name:          "handler-does-not-support-template",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH3HandlerDoesNotSupportTemplate,
			data.InstanceI1,
			data.RuleR7I1HandlerDoesNotSupportTemplate,
		},
		// No routes are set, as the only rule is bad.
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
`,
	},

	{
		Name:          "different-namespace",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerH1NS2,
			data.InstanceI1NS2,
			data.RuleR1I1NS2,
		},
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] ns2 {NS}
    [#0] h1.a1.ns2 {H}
      [#0]
        Condition: <NONE>
        [#0] i1.t1.ns2 {I}
`,
	},

	{
		Name:          "non-default-namespace-rules-subsume-default-namespace-rules",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			// default
			data.HandlerH1,
			data.InstanceI1,
			data.RuleR1I1,

			// ns2
			data.HandlerH1NS2,
			data.InstanceI1NS2,
			data.RuleR1I1NS2,
		},

		// ns2 ruleset is a superset of the default set.
		E: `
[Routing Table]
ID: 1
Identity Attr: destination.service
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] i1.t1.istio-system {I}
  [#1] ns2 {NS}
    [#0] h1.a1.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] i1.t1.istio-system {I}
    [#1] h1.a1.ns2 {H}
        [#0]
          Condition: <NONE>
          [#0] i1.t1.ns2 {I}

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

			actual := t.String()

			if strings.TrimSpace(actual) != strings.TrimSpace(tst.E) {
				tt.Logf("Config:\n%s\n\n", globalConfig)
				tt.Logf("Snapshot:\n%s\n\n", s)
				tt.Logf("Debug: true")
				tt.Fatalf("%v\n!=\n%v", actual, tst.E)
			}

			// rerun with debug = false to ensure there is no crash.
			t = BuildTable(ht, s, expb, "destination.service", "istio-system", false)
			_ = t.String()
		})
	}
}
