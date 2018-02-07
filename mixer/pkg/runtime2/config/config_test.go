// Copyright 2018 Istio Authors
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

package config

import (
	"strings"
	"testing"

	"github.com/golang/protobuf/ptypes/wrappers"

	configpb "istio.io/api/mixer/v1/config"
	descriptorpb "istio.io/api/mixer/v1/config/descriptor"
	istio_mixer_v1_template "istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

// The tests in this package feeds incremental change events to the Ephemeral state and prints out a stable
// view of the Snapshot constructed from these events. The author can specify an initial state, and two sets
// of change events. All of them are optional.
var tests = []struct {
	// Name of the test
	Name string

	// expected state printout
	E string

	// Initial state. Optional.
	Initial map[store.Key]*store.Resource

	// first set of update events. Optional.
	Events1 []*store.Event

	// second set of update events. Optional.
	Events2 []*store.Event

	// templates to use
	T map[string]*template.Info

	// adapters to use.
	A map[string]*adapter.Info
}{

	{
		Name: "empty",
		T:    noTemplates,
		A:    noAdapters,
		E: `
ID: 1
Templates:
Adapters:
Handlers:
Instances:
Rules:
Attributes:
`,
	},

	{
		Name: "adapters only",
		T:    noTemplates,
		E: `
ID: 1
Templates:
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
`,
	},

	{
		Name: "templates only",
		A:    noAdapters,
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "templates and adapters only",
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "basic attributes",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "a1",
					Namespace: "ns",
					Kind:      "missingadapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "attributes",
					Namespace: "ns",
					Kind:      "attributemanifest",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.AttributeManifest{
						Attributes: map[string]*configpb.AttributeManifest_AttributeInfo{
							"foo": {
								ValueType: descriptorpb.STRING,
							},
							"bar": {
								ValueType: descriptorpb.INT64,
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
  bar: INT64
  foo: STRING
  template.attr: BOOL
`,
	},

	{
		Name: "unchanged attributes are preserved",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "attributes",
					Namespace: "ns",
					Kind:      "attributemanifest",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.AttributeManifest{
						Attributes: map[string]*configpb.AttributeManifest_AttributeInfo{
							"foo": {
								ValueType: descriptorpb.STRING,
							},
							"bar": {
								ValueType: descriptorpb.INT64,
							},
						},
					},
				},
			},
		},
		Events2: []*store.Event{
			{
				Key: store.Key{
					Name:      "a1",
					Namespace: "ns",
					Kind:      "missingadapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
		},
		E: `
ID: 2
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
  bar: INT64
  foo: STRING
  template.attr: BOOL
`,
	},

	{
		Name: "initial state attributes gets deleted after delete manifest event",
		Initial: map[store.Key]*store.Resource{
			{
				Name:      "attributes",
				Namespace: "ns",
				Kind:      "attributemanifest",
			}: {
				Spec: &configpb.AttributeManifest{
					Attributes: map[string]*configpb.AttributeManifest_AttributeInfo{
						"foo": {
							ValueType: descriptorpb.STRING,
						},
						"bar": {
							ValueType: descriptorpb.INT64,
						},
					},
				},
			},
		},
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "attributes",
					Namespace: "ns",
					Kind:      "attributemanifest",
				},
				Type: store.Delete,
			},
		},
		Events2: []*store.Event{
			{
				Key: store.Key{
					Name:      "a1",
					Namespace: "ns",
					Kind:      "missingadapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
		},
		E: `
ID: 3
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "attributes coming in from an update event get deleted with a later delete event",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "attributes",
					Namespace: "ns",
					Kind:      "attributemanifest",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.AttributeManifest{
						Attributes: map[string]*configpb.AttributeManifest_AttributeInfo{
							"foo": {
								ValueType: descriptorpb.STRING,
							},
							"bar": {
								ValueType: descriptorpb.INT64,
							},
						},
					},
				},
			},
		},
		Events2: []*store.Event{
			{
				Key: store.Key{
					Name:      "attributes",
					Namespace: "ns",
					Kind:      "attributemanifest",
				},
				Type: store.Delete,
			},
		},
		E: `
ID: 2
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "basic adapter config",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "a1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    a1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "no handler due to adapter mismatch",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "a1",
					Namespace: "ns",
					Kind:      "adapterFoo",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "basic instance",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "i1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
  Name:     i1.check.ns
  Template: check
  Params:   value:"param1"
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "instance referencing missing template is omitted",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "i1",
					Namespace: "ns",
					Kind:      "check1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "empty rule is omitted",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "adapt1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    adapt1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "rule with no action is omitted",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Actions: []*configpb.Action{
							{
								Handler: "handler1",
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "basic rule",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
								},
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ResourceType: ResourceType:{HTTP / Check Report Preprocess}
  Actions:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "multiple rules with multiple actions referencing multiple instances",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "handler2",
					Namespace: "ns",
					Kind:      "adapter2",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance2",
					Namespace: "ns",
					Kind:      "report",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "instance3",
					Namespace: "ns",
					Kind:      "quota",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam3,
				},
			},
			{
				Key: store.Key{
					Name:      "instance4",
					Namespace: "ns",
					Kind:      "apa",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam4,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
									"instance2.report.ns",
								},
							},
							{
								Handler: "handler2.adapter2",
								Instances: []string{
									"instance3.quota.ns",
									"instance4.apa.ns",
								},
							},
						},
					},
				},
			},
			{
				Key: store.Key{
					Name:      "rule2",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Match: `destination.service == "foo"`,
						Actions: []*configpb.Action{
							{
								Handler: "handler2.adapter2",
								Instances: []string{
									"instance1.check.ns",
									"instance2.report.ns",
									"instance3.quota.ns",
									"instance4.apa.ns",
								},
							},
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
								},
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
  Name:    handler2.adapter2.ns
  Adapter: adapter2
  Params:  value:"param2"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param1"
  Name:     instance2.report.ns
  Template: report
  Params:   value:"param2"
  Name:     instance3.quota.ns
  Template: quota
  Params:   value:"param3"
  Name:     instance4.apa.ns
  Template: apa
  Params:   value:"param4"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ResourceType: ResourceType:{HTTP /Check Report Preprocess}
  Actions:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
      Name: instance2.report.ns
    Handler: handler2.adapter2.ns
    Instances:
      Name: instance3.quota.ns
      Name: instance4.apa.ns
  Name:      rule2.rule.ns
  Namespace: ns
  Match:   destination.service == "foo"
  ResourceType: ResourceType:{HTTP /Check Report Preprocess}
  Actions:
    Handler: handler2.adapter2.ns
    Instances:
      Name: instance1.check.ns
      Name: instance2.report.ns
      Name: instance3.quota.ns
      Name: instance4.apa.ns
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
	},

	// TODO(Issue #2139): Once that issue is resolved, this test can be removed.
	{
		Name: "basic rule with istio protocol label",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Metadata: store.ResourceMeta{
						Labels: map[string]string{istioProtocol: ContextProtocolTCP},
					},
					Spec: &configpb.Rule{
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
								},
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ResourceType: ResourceType:{TCP / Check Report Preprocess}
  Actions:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "unknown instance in action is omitted",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
									"some-unknown-instance.check.ns", // gets omitted
								},
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ResourceType: ResourceType:{HTTP /Check Report Preprocess}
  Actions:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.nsAttributes:
  template.attr: BOOL
`,
	},

	{
		Name: "If all the instance references in an action is broken then it is omitted",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"some-unknown-instance.check.ns",
								},
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "duplicate instance references in an action gets normalized to a single instance",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
									"instance1.check.ns",
								},
							},
						},
					},
				},
			},
		},
		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ResourceType: ResourceType:{HTTP /Check Report Preprocess}
  Actions:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "erroneous match clause is retained",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Match: "flurb++",
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
								},
							},
						},
					},
				},
			},
		},

		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:   flurb++
  ResourceType: ResourceType:{HTTP /Check Report Preprocess}
  Actions:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
	},

	// TODO(Issue #2139): Once that issue is resolved, this test can be removed.
	{
		Name: "match clause with context.protocol == tcp",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "handler1",
					Namespace: "ns",
					Kind:      "adapter1",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam1,
				},
			},
			{
				Key: store.Key{
					Name:      "instance1",
					Namespace: "ns",
					Kind:      "check",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: testParam2,
				},
			},
			{
				Key: store.Key{
					Name:      "rule1",
					Namespace: "ns",
					Kind:      "rule",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &configpb.Rule{
						Match: `foo == bar && context.protocol == "tcp"`,
						Actions: []*configpb.Action{
							{
								Handler: "handler1.adapter1",
								Instances: []string{
									"instance1.check.ns",
								},
							},
						},
					},
				},
			},
		},

		E: `
ID: 1
Templates:
  Name: apa
  Name: check
  Name: quota
  Name: report
Adapters:
  Name: adapter1
  Name: adapter2
Handlers:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
Instances:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:   foo == bar && context.protocol == "tcp"
  ResourceType: ResourceType:{TCP /Check Report Preprocess}
  Actions:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
	},
}

var noTemplates = make(map[string]*template.Info)

var noAdapters = make(map[string]*adapter.Info)

var stdAdapters = map[string]*adapter.Info{
	"adapter1": {
		Name: "adapter1",
	},
	"adapter2": {
		Name: "adapter2",
	},
}

var stdTemplates = map[string]*template.Info{
	"quota": {
		Name:    "quota",
		Variety: istio_mixer_v1_template.TEMPLATE_VARIETY_QUOTA,
	},
	"check": {
		Name:    "check",
		Variety: istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK,
	},
	"report": {
		Name:    "report",
		Variety: istio_mixer_v1_template.TEMPLATE_VARIETY_REPORT,
	},
	"apa": {
		Name:    "apa",
		Variety: istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR,
		AttributeManifests: []*configpb.AttributeManifest{
			{
				Attributes: map[string]*configpb.AttributeManifest_AttributeInfo{
					"template.attr": {
						ValueType: descriptorpb.BOOL,
					},
				},
			},
		},
	},
}

var testParam1 = &wrappers.StringValue{Value: "param1"}
var testParam2 = &wrappers.StringValue{Value: "param2"}
var testParam3 = &wrappers.StringValue{Value: "param3"}
var testParam4 = &wrappers.StringValue{Value: "param4"}

func TestConfigs(t *testing.T) {
	runTests(t)

	// enable debug logging and run again to ensure debug logging won't cause a crash.
	o := log.NewOptions()
	_ = o.SetOutputLevel(log.DebugLevel)
	_ = log.Configure(o)
	runTests(t)
}

func runTests(t *testing.T) {
	for _, test := range tests {

		t.Run(test.Name, func(tt *testing.T) {
			templates := test.T
			if templates == nil {
				templates = stdTemplates
			}

			adapters := test.A
			if adapters == nil {
				adapters = stdAdapters
			}

			e := NewEphemeral(templates, adapters)

			var s *Snapshot

			if test.Initial != nil {
				e.SetState(test.Initial)
				s = e.BuildSnapshot()
			}

			if test.Events1 != nil {
				for _, event := range test.Events1 {
					e.ApplyEvent(event)
				}
				s = e.BuildSnapshot()
			}

			if test.Events2 != nil {
				for _, event := range test.Events2 {
					e.ApplyEvent(event)
				}
				s = e.BuildSnapshot()
			}

			if s == nil {
				s = e.BuildSnapshot()
			}

			str := s.String()

			if normalize(str) != normalize(test.E) {
				tt.Fatalf("config mismatch:\n%s\n != \n%s\n", str, test.E)
			}
		})
	}
}

func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Replace(s, "\t", "", -1)
	s = strings.Replace(s, "\n", "", -1)
	s = strings.Replace(s, " ", "", -1)
	return s
}
