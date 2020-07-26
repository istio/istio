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

package routing

import (
	"context"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/handler"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
	"istio.io/istio/mixer/pkg/template"
)

// tests is a declarative test suite for the routing table. It covers both table building as well as table
// usage scenarios.
var tests = []struct {
	// Name of the test
	Name string

	// Config values to use when using the builder to build the table. If ServiceConfig is empty, the default
	// one from the testing/data package is used instead.
	ServiceConfig string
	Configs       []string

	Adapters  map[string]*adapter.Info
	Templates map[string]*template.Info

	// ExpectedTable is the expected routing table output w/ debug info. Used by builder tests to verify the
	// table structure.
	ExpectedTable string
}{
	{
		Name:          "basic",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},

		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
`,
	},

	{
		Name:          "multiple-instances-check",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.RuleCheck1WithInstance1And2,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
        [#1] icheck2.tcheck.istio-system {I}
`,
	},

	{
		Name:          "multiple-instances-report",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerAReport1,
			data.InstanceReport1,
			data.InstanceReport2,
			data.RuleReport1And2,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_REPORT {V}
  [#0] istio-system {NS}
    [#0] hreport1.areport.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] ireport1.treport.istio-system {I}
        [#1] ireport2.treport.istio-system {I}
`,
	},

	{
		Name:          "check-instance-with-conditional",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1WithMatchClause,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: match(destination.name, "foo*")
        [#0] icheck1.tcheck.istio-system {I}
`,
	},

	{
		Name:          "instance-with-conditional-true",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1TrueCondition,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
`,
	},

	{
		Name:          "multi-instance-with-conditional",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.RuleCheck1WithInstance1And2WithMatchClause,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: match(destination.name, "foo*")
        [#0] icheck1.tcheck.istio-system {I}
        [#1] icheck2.tcheck.istio-system {I}
`,
	},

	{
		Name:          "multi-instance-multi-conditional",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.InstanceCheck3,
			data.RuleCheck1WithInstance1And2WithMatchClause,
			data.RuleCheck2WithInstance2And3WithMatchClause,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: destination.name.startsWith("foo")
        [#0] icheck2.tcheck.istio-system {I}
		[#1] icheck3.tcheck.istio-system {I}
      [#1]
        Condition: match(destination.name, "foo*")
        [#0] icheck1.tcheck.istio-system {I}
        [#1] icheck2.tcheck.istio-system {I}
`,
	},

	{
		Name:          "multi-rule-to-same-target",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.InstanceCheck3,
			data.RuleCheck1WithInstance1And2,
			data.RuleCheck2WithInstance2And3,
		},
		// TODO(Issue #2690): We should dedupe instances that are being dispatched to a particular handler.
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
        [#1] icheck2.tcheck.istio-system {I}
        [#2] icheck2.tcheck.istio-system {I}
        [#3] icheck3.tcheck.istio-system {I}
`,
	},

	{
		Name:          "multi-rule-to-same-target-with-one-conditional",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.InstanceCheck3,
			data.RuleCheck1,
			data.RuleCheck2WithInstance2And3WithMatchClause,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
      [#1]
        Condition: destination.name.startsWith("foo")
        [#0] icheck2.tcheck.istio-system {I}
        [#1] icheck3.tcheck.istio-system {I}
`,
	},

	{
		Name:          "multi-rule-to-multiple-target",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.HandlerACheck2,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.RuleCheck1,
			data.RuleCheck2WithHandler2AndInstance2,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
    [#1] hcheck2.acheck.istio-system {H}
        [#0]
          Condition: <NONE>
          [#0] icheck2.tcheck.istio-system {I}
`,
	},

	{
		Name:          "bad-condition",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1WithBadCondition,
		},
		// No routes are set, as the only rule is bad.
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
`,
	},

	{
		Name:          "bad-handler-name",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1WithBadHandler,
		},
		// No routes are set, as the only rule is bad.
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
`,
	},

	{
		Name:          "bad-handler-builder",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		Adapters: data.BuildAdapters(nil, data.FakeAdapterSettings{Name: "acheck", ErrorAtBuild: true}),
		// No routes are set, as the only rule is bad.
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
`,
	},

	{
		Name:          "handler-does-not-support-template",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		Templates: data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", HandlerDoesNotSupportTemplate: true}),
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
`,
	},

	{
		Name:          "different-namespace",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck3NS2,
			data.InstanceCheck4NS2,
			data.RuleCheck3NS2,
		},
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] ns2 {NS}
    [#0] hcheck3.acheck.ns2 {H}
      [#0]
        Condition: <NONE>
        [#0] icheck4.tcheck.ns2 {I}
`,
	},

	{
		Name:          "non-default-namespace-rules-subsume-default-namespace-rules",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			// default
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,

			// ns2
			data.HandlerACheck3NS2,
			data.InstanceCheck4NS2,
			data.RuleCheck3NS2,
		},

		// ns2 ruleset is a superset of the default set.
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
  [#1] ns2 {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
    [#1] hcheck3.acheck.ns2 {H}
        [#0]
          Condition: <NONE>
          [#0] icheck4.tcheck.ns2 {I}
`,
	},

	{
		Name:          "builder-mapper-error-causes-dependent-rules-to-be-omitted",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		Templates: data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tcheck", ErrorAtCreateInstanceBuilder: true}),
		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
`},

	{
		Name:          "match-condition-wrong-return-type-in-match-claus-causes-rule-to-be-omitted.",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1WithNonBooleanCondition,
		},

		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
`},

	{
		Name:          "apa",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerAPA1,
			data.InstanceAPA1,
			data.RuleApa1,
		},

		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {V}
  [#0] istio-system {NS}
    [#0] hapa1.apa.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] iapa1.tapa.istio-system {I}
`},

	{
		Name:          "apa-error-at-output-expressions",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerAPA1,
			data.InstanceAPA1,
			data.RuleApa1,
		},
		Templates: data.BuildTemplates(nil, data.FakeTemplateSettings{Name: "tapa", ErrorAtCreateOutputExpressions: true}),

		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
`,
	},

	{
		Name:          "different-templates-in-same-rule",
		ServiceConfig: data.ServiceConfig,
		Configs: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceHalt1,
			data.Rule4CheckAndHalt,
		},

		ExpectedTable: `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
    [#1] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] ihalt1.thalt.istio-system {I}`,
	},
}

func TestBuilder(t *testing.T) {
	for _, tst := range tests {
		t.Run(tst.Name, func(tt *testing.T) {

			serviceConfig := tst.ServiceConfig
			if len(serviceConfig) == 0 {
				serviceConfig = data.ServiceConfig
			}

			templates := tst.Templates
			if templates == nil {
				templates = data.BuildTemplates(nil)
			}
			adapters := tst.Adapters
			if adapters == nil {
				adapters = data.BuildAdapters(nil)
			}
			t, s := buildTableWithTemplatesAndAdapters(templates, adapters, serviceConfig, tst.Configs, true)

			actual := t.String()

			if normalize(actual) != normalize(tst.ExpectedTable) {
				tt.Logf("Config:\n%v\n\n", tst.Configs)
				tt.Logf("Snapshot:\n%s\n\n", s)
				tt.Logf("Debug: true")
				tt.Fatalf("got:\n%v\nwant:\n%v\n", actual, tst.ExpectedTable)
			}

			reachedEnd := false
			defer func() {
				r := recover()
				if !reachedEnd {
					tt.Fatalf("buildTable(debugInfo=false) failed with a panic: '%v'", r)
				}
			}()

			// rerun with debug = false to ensure there is no crash.
			t, _ = buildTable(serviceConfig, tst.Configs, false)
			_ = t.String()

			reachedEnd = true
		})
	}
}

var (
	normalizer = strings.NewReplacer("\t", "", "\n", "", " ", "")
)

// Normalize a string for textual comparison.
func normalize(str string) string {
	return normalizer.Replace(str)
}

// Convenience method for building a routing Table for tests.
func buildTable(serviceConfig string, globalConfigs []string, debugInfo bool) (*Table, *config.Snapshot) {
	return buildTableWithTemplatesAndAdapters(data.BuildTemplates(nil), data.BuildAdapters(nil), serviceConfig, globalConfigs, debugInfo)
}

// Convenience method for building a routing Table for tests.
func buildTableWithTemplatesAndAdapters(templates map[string]*template.Info, adapters map[string]*adapter.Info,
	serviceConfig string, globalConfigs []string, debugInfo bool) (*Table, *config.Snapshot) {

	if len(serviceConfig) == 0 {
		serviceConfig = data.ServiceConfig
	}

	globalConfig := data.JoinConfigs(globalConfigs...)

	s, _ := config.GetSnapshotForTest(templates, adapters, serviceConfig, globalConfig)
	ht := handler.NewTable(handler.Empty(), s, nil, []string{metav1.NamespaceAll})

	return BuildTable(ht, s, "istio-system", debugInfo), s
}

func TestAddRuleOperations(t *testing.T) {
	b := &builder{
		table: &Table{entries: make(map[tpb.TemplateVariety]*varietyTable)},
	}
	// put something into the table for a different namespace
	b.table.entries[tpb.TEMPLATE_VARIETY_CHECK] = &varietyTable{
		entries: map[string]*NamespaceTable{
			"ns2": {
				entries:    []*Destination{},
				directives: []*DirectiveGroup{},
			},
		},
	}

	// catch the panic
	defer func() {
		if r := recover(); r != nil {
			t.Fail()
		}
	}()

	b.addRuleOperations("ns1", nil, nil)
}

func TestNonPointerAdapter(t *testing.T) {

	templates := data.BuildTemplates(nil)

	adapters := map[string]*adapter.Info{
		"acheck": {
			Name:               "acheck",
			SupportedTemplates: []string{"tcheck", "thalt"},
			DefaultConfig:      &types.Struct{},
			NewBuilder: func() adapter.HandlerBuilder {
				return nonPointerBuilder{}
			},
		},
	}

	globalConfigs := []string{
		data.HandlerACheck1,
		data.InstanceCheck1,
		data.InstanceCheck2,
		data.RuleCheck1WithInstance1And2,
	}

	expected := `
[Routing ExpectedTable]
ID: 0
[#0] TEMPLATE_VARIETY_CHECK {V}
  [#0] istio-system {NS}
    [#0] hcheck1.acheck.istio-system {H}
      [#0]
        Condition: <NONE>
        [#0] icheck1.tcheck.istio-system {I}
        [#1] icheck2.tcheck.istio-system {I}
`

	table, _ := buildTableWithTemplatesAndAdapters(templates, adapters, data.ServiceConfig, globalConfigs, true)

	actual := table.String()

	if normalize(actual) != normalize(expected) {
		t.Fatalf("got:\n%v\nwant:\n%v\n", actual, expected)
	}
}

type nonPointerBuilder struct{}

var _ adapter.HandlerBuilder = nonPointerBuilder{}

func (n nonPointerBuilder) SetAdapterConfig(adapter.Config) {
}

func (n nonPointerBuilder) Validate() *adapter.ConfigErrors {
	return nil
}

func (n nonPointerBuilder) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	return nonPointerHandler{fn: func() {}}, nil
}

type nonPointerHandler struct {
	// Make handler non-comparable.
	fn func()
}

var _ adapter.Handler = nonPointerHandler{}

func (h nonPointerHandler) Close() error {
	h.fn()
	return nil
}
