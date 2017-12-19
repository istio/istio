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

package config

import (
	"strings"
	"testing"

	"github.com/golang/protobuf/ptypes/wrappers"
	"go.uber.org/zap/zapcore"
	"istio.io/istio/pkg/log"

	descriptorpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	configpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/template"
)

var tests = []struct {
	Name string

	// expected
	E string

	// Initial state
	Initial map[store.Key]*store.Resource

	// update events
	Events1 []*store.Event

	// update events
	Events2 []*store.Event

	// template
	T map[string]*template.Info

	// adapter
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
		Name: "unchanged attributes",
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
		Name: "initial state attributes deleted",
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
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "attributes deleted",
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
		Name: "adapter mismatch",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "a1",
					Namespace: "ns",
					Kind:      "adapter2",
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
Handlers:
Instances:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "basic template",
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
		Name: "template mismatch",
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
		Name: "rule with empty action is omitted",
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
		Name: "unknown instance",
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
									"instance2.check.ns",
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
}

var noTemplates = make(map[string]*template.Info, 0)

var noAdapters = make(map[string]*adapter.Info, 0)

var stdAdapters = map[string]*adapter.Info{
	"adapter1": {
		Name: "adapter1",
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

func TestConfigs(t *testing.T) {
	runTests(t)

	// enable debug logging and run again to ensure debug logging won't cause a crash.
	o := log.NewOptions()
	o.SetOutputLevel(zapcore.DebugLevel)
	log.Configure(o)
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
				e.ApplyEvents(test.Events1)
				s = e.BuildSnapshot()
			}

			if test.Events2 != nil {
				e.ApplyEvents(test.Events2)
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
