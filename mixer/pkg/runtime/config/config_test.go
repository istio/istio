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

// nolint
//go:generate $REPO_ROOT/bin/protoc.sh testdata/tmpl1.proto -otestdata/tmpl1.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/tmpl2.proto -otestdata/tmpl2.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/adptCfg.proto -otestdata/adptCfg.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh testdata/adptCfg2.proto -otestdata/adptCfg2.descriptor -I.

package config

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/wrappers"

	adapter_model "istio.io/api/mixer/adapter/model/v1beta1"
	descriptorpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/log"
)

var tmpl1Base64Str = getFileDescSetBase64("testdata/tmpl1.descriptor")
var tmpl2Base64Str = getFileDescSetBase64("testdata/tmpl2.descriptor")
var adpt1DescBase64 = getFileDescSetBase64("testdata/adptCfg.descriptor")
var adpt2DescBase64 = getFileDescSetBase64("testdata/adptCfg2.descriptor")

type dummyHandlerBuilder struct{}

func (d *dummyHandlerBuilder) SetAdapterConfig(cfg adapter.Config) {}

func (d *dummyHandlerBuilder) Validate() *adapter.ConfigErrors {
	return nil
}

func (d *dummyHandlerBuilder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	return nil, nil
}

var tmpl1InstanceParam = unmarshalTestData(`
s1: source.name | "yoursrc"
`)

var adapter1Params = unmarshalTestData(`
abc: "abcstring"
`)

var adapter2Params = unmarshalTestData(`
pqr: "abcstring"
`)

var invalidHandlerParams = unmarshalTestData(`
fildNotFound: "abcstring"
`)

var tmpl2InstanceParam = unmarshalTestData(`
s2: source.name | "yoursrc"
`)

var badInstanceParamIn = unmarshalTestData(`
badFld: "s1stringVal"
`)

var validCfg = []*store.Event{
	updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
		Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
			"source.name": {
				ValueType: descriptorpb.STRING,
			},
		},
	}),
	updateEvent("t1.template.default", &adapter_model.Template{
		Descriptor_: tmpl1Base64Str,
	}),
	updateEvent("a1.adapter.default", &adapter_model.Info{
		Description:  "testAdapter description",
		SessionBased: true,
		Config:       adpt1DescBase64,
		Templates:    []string{"t1.default"},
	}),
	updateEvent("h1.handler.default", &descriptorpb.Handler{
		Adapter: "a1.default",
		Params:  adapter1Params,
	}),
	updateEvent("i1.instance.default", &descriptorpb.Instance{
		Template: "t1.default",
		Params:   tmpl1InstanceParam,
	}),
	updateEvent("r1.rule.default", &descriptorpb.Rule{
		Match: "true",
		Actions: []*descriptorpb.Action{
			{
				Handler: "h1.default",
				Instances: []string{
					"i1.default",
				},
			},
		},
	}),
}

var validCfgMixShortLongName = []*store.Event{
	updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
		Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
			"source.name": {
				ValueType: descriptorpb.STRING,
			},
		},
	}),
	updateEvent("t1.template.default", &adapter_model.Template{
		Descriptor_: tmpl1Base64Str,
	}),
	updateEvent("t2.template.default", &adapter_model.Template{
		Descriptor_: tmpl2Base64Str,
	}),
	updateEvent("a1.adapter.default", &adapter_model.Info{
		Description:  "testAdapter description",
		SessionBased: true,
		Config:       adpt1DescBase64,
		Templates:    []string{"t1", "t2.default"},
	}),
	updateEvent("a2.adapter.default", &adapter_model.Info{
		Description:  "testAdapter description",
		SessionBased: true,
		Config:       adpt2DescBase64,
		Templates:    []string{"t2.default"},
	}),
	updateEvent("h1.handler.default", &descriptorpb.Handler{
		Adapter: "a1.default",
		Params:  adapter1Params,
	}),
	updateEvent("h2.handler.default", &descriptorpb.Handler{
		Adapter: "a2",
		Params:  adapter2Params,
	}),
	updateEvent("i1.instance.default", &descriptorpb.Instance{
		Template: "t1.default",
		Params:   tmpl1InstanceParam,
	}),
	updateEvent("i2.instance.default", &descriptorpb.Instance{
		Template: "t2",
		Params:   tmpl2InstanceParam,
	}),

	updateEvent("r1.rule.default", &descriptorpb.Rule{
		Match: "true",
		Actions: []*descriptorpb.Action{
			{
				Handler: "h1.default",
				Instances: []string{
					"i1.default",
					"i2",
				},
			},
		},
	}),
	updateEvent("r2.rule.default", &descriptorpb.Rule{
		Match: "true",
		Actions: []*descriptorpb.Action{
			{
				Handler: "h2.default",
				Instances: []string{
					"i2",
				},
			},
		},
	}),
}

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

	wantErr string
}{

	{
		Name: "empty",
		T:    noTemplates,
		A:    noAdapters,
		E: `
ID: 0
TemplatesStatic:
AdaptersStatic:
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
`,
	},

	{
		Name: "adapters only",
		T:    noTemplates,
		E: `
ID: 0
TemplatesStatic:
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
`,
	},

	{
		Name: "templates only",
		A:    noAdapters,
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "templates and adapters only",
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
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
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"foo": {
						ValueType: descriptorpb.STRING,
					},
					"bar": {
						ValueType: descriptorpb.INT64,
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
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
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"foo": {
						ValueType: descriptorpb.STRING,
					},
					"bar": {
						ValueType: descriptorpb.INT64,
					},
				},
			}),
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
ID: 1
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
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
				Spec: &descriptorpb.AttributeManifest{
					Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
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
ID: 2
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "attributes coming in from an update event get deleted with a later delete event",
		Events1: []*store.Event{
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"foo": {
						ValueType: descriptorpb.STRING,
					},
					"bar": {
						ValueType: descriptorpb.INT64,
					},
				},
			}),
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
ID: 1
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    a1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "basic handler cr config",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "a1",
					Namespace: "ns",
					Kind:      "handler",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &descriptorpb.Handler{
						Name:            "a1",
						CompiledAdapter: "adapter1",
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    a1.ns
  Adapter: adapter1
  Params:  &Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
	},
	{
		Name: "basic handler cr config with params",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "a2",
					Namespace: "ns",
					Kind:      "handler",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &descriptorpb.Handler{
						Name:            "a2",
						CompiledAdapter: "adapter2",
						Params:          adapter2Params,
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    a2.ns
  Adapter: adapter2
  Params:  &Struct{Fields:map[string]*Value{pqr: &Value{Kind:&Value_StringValue{StringValue:abcstring,},XXX_unrecognized:[],},},XXX_unrecognized:[],}
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "basic static handler cr with dynamic cr",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "h1",
					Namespace: "ns",
					Kind:      "handler",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &descriptorpb.Handler{
						Name:            "a2",
						CompiledAdapter: "adapter2",
					},
				},
			},
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{},
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
	Name: apa
	Name: check
	Name: quota
	Name: report
AdaptersStatic:
	Name: adapter1
	Name: adapter2
HandlersStatic:
	Name:    h1.ns
	Adapter: adapter2
	Params:  &Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}
InstancesStatic:
Rules:
AdaptersDynamic:
	Name:      a1.adapter.default
	Templates:
HandlersDynamic:
	Name:    h1.handler.default
	Adapter: a1.adapter.default
Attributes:
	template.attr: BOOL
`,
	},

	{
		Name: "basic static handler cr with same-name dynamic cr",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "h1",
					Namespace: "ns",
					Kind:      "handler",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &descriptorpb.Handler{
						Name:            "a2",
						CompiledAdapter: "adapter2",
					},
				},
			},
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{},
			}),
			updateEvent("h1.handler.ns", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
	Name: apa
	Name: check
	Name: quota
	Name: report
AdaptersStatic:
	Name: adapter1
	Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
	Name:      a1.adapter.default
	Templates:
HandlersDynamic:
	Name:    h1.handler.ns
	Adapter: a1.adapter.default
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
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
					Spec: &descriptorpb.Rule{},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    adapt1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "rule=rule1.rule.ns: No valid actions found in rule",
	},

	{
		Name: "rule with bad action is omitted",
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
					Spec: &descriptorpb.Rule{
						Actions: []*descriptorpb.Action{
							{
								Handler: "handler1",
							},
						},
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='rule1.rule.ns[0]': Handler not found: handler='handler1'",
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
					Spec: &descriptorpb.Rule{
						Actions: []*descriptorpb.Action{
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ActionsStatic:
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
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"destination.service": {
						ValueType: descriptorpb.STRING,
					},
				},
			}),
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
					Spec: &descriptorpb.Rule{
						Actions: []*descriptorpb.Action{
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
					Spec: &descriptorpb.Rule{
						Match: `destination.service == "foo"`,
						Actions: []*descriptorpb.Action{
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
  Name:    handler2.adapter2.ns
  Adapter: adapter2
  Params:  value:"param2"
InstancesStatic:
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
  ActionsStatic:
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
  ActionsStatic:
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
  destination.service: STRING
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
					Spec: &descriptorpb.Rule{
						Actions: []*descriptorpb.Action{
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ActionsStatic:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.nsAttributes:
  template.attr: BOOL
`,
		wantErr: "action='rule1.rule.ns[0]': Instance not found: instance='some-unknown-instance.check.ns'",
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
					Spec: &descriptorpb.Rule{
						Actions: []*descriptorpb.Action{
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "rule=rule1.rule.ns: No valid actions found in rule",
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
					Spec: &descriptorpb.Rule{
						Actions: []*descriptorpb.Action{
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:
  ActionsStatic:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='rule1.rule.ns[0]': action specified the same instance multiple times: instance='instance1.check.ns'",
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
					Spec: &descriptorpb.Rule{
						Match: "flurb++",
						Actions: []*descriptorpb.Action{
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:   flurb++
  ActionsStatic:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
		wantErr: "rule='rule1.rule.ns'.Match: failed to parse expression 'flurb++': unable to parse expression 'flurb++': 1:6: expected 'EOF', found '++'",
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
					Spec: &descriptorpb.Rule{
						Match: `foo == bar && context.protocol == "tcp"`,
						Actions: []*descriptorpb.Action{
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
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
  Name:    handler1.adapter1.ns
  Adapter: adapter1
  Params:  value:"param1"
InstancesStatic:
  Name:     instance1.check.ns
  Template: check
  Params:   value:"param2"
Rules:
  Name:      rule1.rule.ns
  Namespace: ns
  Match:   foo == bar && context.protocol == "tcp"
  ActionsStatic:
    Handler: handler1.adapter1.ns
    Instances:
      Name: instance1.check.ns
Attributes:
  template.attr: BOOL
`,
		wantErr: "rule='rule1.rule.ns'.Match: unknown attribute foo",
	},
	{
		Name: "new valid templates",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "metrictemplate",
					Namespace: "ns",
					Kind:      "template",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Template{
						Descriptor_: fooTmpl,
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name: metrictemplate.template.ns
    Name: metrictemplate.template.ns
    InternalPackageDerivedName: foo
Attributes:
  template.attr: BOOL
`,
	},

	{
		Name: "new templates name collision with compiled in templates",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "report",
					Namespace: "ns",
					Kind:      "template",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Template{
						Descriptor_: fooTmpl,
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  report.template.ns
    Name:  report.template.ns
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
		wantErr: "",
	},

	{
		Name: "bad template descriptor is ignored",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "metrictemplate",
					Namespace: "ns",
					Kind:      "template",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Template{
						Descriptor_: "bad descriptor",
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "template='metrictemplate.template.ns': unable to parse descriptor: illegal base64 data at input byte 3",
	},

	{
		Name: "adapter info with valid template reference",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "metrictemplate",
					Namespace: "ns",
					Kind:      "template",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Template{
						Descriptor_: fooTmpl,
					},
				},
			},
			{
				Key: store.Key{
					Name:      "adapterinfo1",
					Namespace: "ns",
					Kind:      "adapter",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Info{
						Name:      "adapter1",
						Templates: []string{"metrictemplate.template"},
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      adapterinfo1.adapter.ns
  Templates:
  -  metrictemplate.template.ns
TemplatesDynamic:
  Resource Name:  metrictemplate.template.ns
    Name: metrictemplate.template.ns
    InternalPackageDerivedName: foo
Attributes:
  template.attr: BOOL
`,
	},
	{
		Name: "adapter info no templates",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "adapterinfo1",
					Namespace: "ns",
					Kind:      "adapter",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Info{
						Name: "adapter1",
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      adapterinfo1.adapter.ns
  Templates:
Attributes:
  template.attr: BOOL
`,
	},
	{
		Name: "adapter info bad adapter config",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "adapterinfo1",
					Namespace: "ns",
					Kind:      "adapter",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Info{
						Config: "bad adapter config",
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "adapter='adapterinfo1.adapter.ns': unable to parse adapter configuration: illegal base64 data at input byte 3",
	},
	{
		Name: "adapter info bad template reference",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "adapterinfo1",
					Namespace: "ns",
					Kind:      "adapter",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Info{
						Name:      "adapter1",
						Templates: []string{"bad template reference"},
					},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "adapter='adapterinfo1.adapter.ns': unable to find template 'bad template reference'",
	},

	{
		Name: "new adapter name collision with compiled in adapters",
		Events1: []*store.Event{
			{
				Key: store.Key{
					Name:      "adapter1",
					Namespace: "ns",
					Kind:      "adapter",
				},
				Type: store.Update,
				Value: &store.Resource{
					Spec: &adapter_model.Info{},
				},
			},
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      adapter1.adapter.ns
  Templates:
Attributes:
  template.attr: BOOL
`,
		wantErr: "",
	},

	{
		Name: "add template",
		Events1: []*store.Event{updateEvent("mymetric.template.default", &adapter_model.Template{
			Descriptor_: tmpl1Base64Str,
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  mymetric.template.default
    Name:  mymetric.template.default
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
	},
	{
		Name: "update template",
		Events1: []*store.Event{
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"source.name": {
						ValueType: descriptorpb.STRING,
					},
				},
			}),
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{"t1.default"},
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   tmpl1InstanceParam,
			}),
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
	},

	{
		Name: "add template - bad descriptor",
		Events1: []*store.Event{updateEvent("mymetric.template.default", &adapter_model.Template{
			Descriptor_: "bad base64 string",
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "template='mymetric.template.default': unable to parse descriptor: illegal base64 data at input byte 3",
	},
	{
		Name: "update template - break instance",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   tmpl1InstanceParam,
			}),
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl2Base64Str,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  bar
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default'.params: config does not conform to schema of template " +
			"'t1.default': fieldEncoder 's1' not found in message 'InstanceMsg'",
	},
	{
		Name: "delete template",
		Events1: []*store.Event{
			updateEvent("mymetric.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("testCR1.adapter.default", &adapter_model.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Templates:    []string{"mymetric.default"},
			}),
			deleteEvent("testCR1.adapter.default"),
			deleteEvent("mymetric.template.default"),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
	},
	{
		Name: "delete template - break adapter",
		Events1: []*store.Event{
			updateEvent("mymetric.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("testCR1.adapter.default", &adapter_model.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Templates:    []string{"mymetric.default"},
			}),
			deleteEvent("mymetric.template.default"),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "adapter='testCR1.adapter.default': unable to find template 'mymetric.default'",
	},
	{
		Name: "delete template - break instance",
		Events1: []*store.Event{
			updateEvent("mymetric.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("testCR1.instance.default", &descriptorpb.Instance{
				Template: "mymetric.default",
			}),
			deleteEvent("mymetric.template.default"),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='testCR1.instance.default'.template: template 'mymetric.default' not found",
	},
	// Adapters
	{
		Name: "add adapter",
		Events1: []*store.Event{updateEvent("testCR1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       "",
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      testCR1.adapter.default
  Templates:
Attributes:
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "add adapter - bad tmpls",
		Events1: []*store.Event{updateEvent("testCR1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       "",
			Templates:    []string{"a.b.c"},
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "adapter='testCR1.adapter.default': unable to find template 'a.b.c'",
	},
	{
		Name: "add adapter - bad cfg",
		Events1: []*store.Event{updateEvent("testCR1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       "bad cfg descriptor",
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "adapter='testCR1.adapter.default': unable to parse adapter configuration: illegal base64 data at input byte 3",
	},
	{
		Name: "add adapter - valid template reference",
		Events1: []*store.Event{
			updateEvent("mymetric.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("testCR1.adapter.default", &adapter_model.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Templates:    []string{"mymetric.default"},
			})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      testCR1.adapter.default
  Templates:
  -  mymetric.template.default

TemplatesDynamic:
  Resource Name:  mymetric.template.default
    Name:  mymetric.template.default
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "add adapter, valid template reference, short tmpl name",
		Events1: []*store.Event{
			updateEvent("mymetric.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("testCR1.adapter.default", &adapter_model.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Templates:    []string{"mymetric.default"},
			})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      testCR1.adapter.default
  Templates:
  -  mymetric.template.default

TemplatesDynamic:
  Resource Name:  mymetric.template.default
    Name:  mymetric.template.default
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "add adapter - bad template kind",
		Events1: []*store.Event{updateEvent("testCR1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Templates:    []string{"notATemplate.rule"},
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "adapter='testCR1.adapter.default': unable to find template 'notATemplate.rule'",
	},
	{
		Name: "add adapter - bad template reference",
		Events1: []*store.Event{updateEvent("testCR1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Templates:    []string{"notexists.default"},
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "adapter='testCR1.adapter.default': unable to find template 'notexists.default'",
	},
	{
		Name: "update adapter",
		Events1: append(validCfg,
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{"t1.default"},
			})),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
  Name:      r1.rule.default
  Namespace: default
  Match:   true
  ActionsStatic:
  ActionsDynamic:
    Handler: h1.handler.default
    Instances:
      Name: i1.instance.default
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "update adapter supported template- break routed instances",
		Events1: append(validCfg,
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description 22",
				SessionBased: true,
				Config:       adpt1DescBase64,
				// stop supporting previously supported template "t1.template.default"
				Templates: []string{},
			})),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': instance 'i1.instance.default' is of template " +
			"'t1.template.default' which is not supported by handler 'h1.handler.default'",
	},
	{
		Name: "update adapter cfg - break handler",
		Events1: append(validCfg,
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt2DescBase64,
				Templates:    []string{"t1.default"},
			}),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "field 'abc' not found in message 'Params'",
	},
	{
		Name: "delete adapter",
		Events1: []*store.Event{
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
			deleteEvent("h1.handler.default"),
			deleteEvent("a1.adapter.default"),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "delete adapter - break handler",
		Events1: []*store.Event{
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{},
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
			deleteEvent("a1.adapter.default"),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "handler='h1.handler.default'.adapter: adapter 'a1.default' not found",
	},

	// handler
	{
		Name: "add handler",
		Events1: []*store.Event{updateEvent("a1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       adpt1DescBase64,
		}), updateEvent("h1.handler.default", &descriptorpb.Handler{
			Adapter: "a1.default",
			Params:  adapter1Params,
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
Attributes:
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "add handler - bad param type",
		Events1: []*store.Event{updateEvent("a1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       adpt1DescBase64,
		}), updateEvent("h1.handler.default", &descriptorpb.Handler{
			Adapter: "a1.default",
			Params: &types.Struct{
				Fields: map[string]*types.Value{
					"foo": nil,
				},
			},
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
Attributes:
  template.attr: BOOL
`,
		wantErr: "handler='h1.handler.default'.params: error converting parameters to dictionary: error serializing struct value: nil",
	},
	{
		Name: "add handler - bad adapter",
		Events1: []*store.Event{updateEvent("h1.handler.default", &descriptorpb.Handler{
			Adapter: "not.an.adapter",
			Params:  adapter1Params,
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "handler='h1.handler.default'.adapter: adapter 'not.an.adapter' not found",
	},
	{
		Name: "add handler - bad adapter reference",
		Events1: []*store.Event{updateEvent("h1.handler.default", &descriptorpb.Handler{
			Adapter: "broken.default",
			Params:  adapter1Params,
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "handler='h1.handler.default'.adapter: adapter 'broken.default' not found",
	},
	{
		Name: "add handler - broken adapter reference",
		Events1: []*store.Event{updateEvent("h1.handler.default", &descriptorpb.Handler{
			Adapter: "broken.default.extra3.extra4",
			Params:  adapter1Params,
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "handler='h1.handler.default'.adapter: adapter 'broken.default.extra3.extra4' not found",
	},
	{
		Name: "add handler - bad param content",
		Events1: []*store.Event{updateEvent("a1.adapter.default", &adapter_model.Info{
			Name:         "testAdapter",
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       adpt1DescBase64,
		}), updateEvent("h1.handler.default", &descriptorpb.Handler{
			Adapter: "a1.default",
			Params:  invalidHandlerParams,
		})},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
Attributes:
  template.attr: BOOL
`,
		wantErr: "handler='h1.handler.default'.params: field 'fildNotFound' not found in message 'Params'",
	},
	{
		Name: "update handler",
		Events1: append(validCfg,
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  nil,
			})),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
  Name:      r1.rule.default
  Namespace: default
  Match:   true
  ActionsStatic:
  ActionsDynamic:
    Handler: h1.handler.default
    Instances:
      Name: i1.instance.default
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "update handler's adapter - break supported tmpls",
		Events1: append(validCfg,
			updateEvent("a2.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				// Does not support any template
				Templates: []string{},
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a2.default",
				Params:  adapter1Params,
			})),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

  Name:      a2.adapter.default
  Templates:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a2.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': instance 'i1.instance.default' is of template " +
			"'t1.template.default' which is not supported by handler 'h1.handler.default'",
	},
	{
		Name: "delete handler",
		Events1: append(validCfg,
			deleteEvent("r1.rule.default"),
			deleteEvent("h1.handler.default"),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},

	// instances
	{
		Name: "add instance",
		Events1: []*store.Event{
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"source.name": {
						ValueType: descriptorpb.STRING,
					},
				},
			}),
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   tmpl1InstanceParam,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "add instance - bad template",
		Events1: []*store.Event{
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "not.a.template",
				Params:   tmpl1InstanceParam,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default'.template: template 'not.a.template' not found",
	},

	{
		Name: "add static instance - missing template",
		Events1: []*store.Event{
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				CompiledTemplate: "check",
				Params: &types.Struct{
					Fields: map[string]*types.Value{
						"extra_field": {},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default': missing compiled template",
	},
	{
		Name: "add static instance - bad params",
		Events1: []*store.Event{
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				CompiledTemplate: "check",
				Params: &types.Struct{
					Fields: map[string]*types.Value{
						"extra_field": {},
					},
				},
				AttributeBindings: map[string]string{"test": "test"},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default': nil Value",
	},

	{
		Name: "add static instance - extra field",
		Events1: []*store.Event{
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				CompiledTemplate: "check",
				Params: &types.Struct{
					Fields: map[string]*types.Value{
						"extra_field": {Kind: &types.Value_StringValue{StringValue: "test"}},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: `instance='i1.instance.default': unknown field "extra_field" in v1beta1.Instance`,
	},

	{
		Name: "add instance - bad param type",
		Events1: []*store.Event{
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"source.name": {
						ValueType: descriptorpb.STRING,
					},
				},
			}),
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params: &types.Struct{
					Fields: map[string]*types.Value{
						"foo": nil,
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default'.params: invalid params block.",
	},
	{
		Name: "add instance - bad template reference",
		Events1: []*store.Event{
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "notExist.default",
				Params:   tmpl1InstanceParam,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default'.template: template 'notExist.default' not found",
	},
	{
		Name: "add instance - bad template string",
		Events1: []*store.Event{
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "oneWordNotValidRef",
				Params:   tmpl1InstanceParam,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default'.template: template 'oneWordNotValidRef' not found",
	},
	{
		Name: "add instance - bad content",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   badInstanceParamIn,
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default'.params: config does not conform to schema of template " +
			"'t1.default': fieldEncoder 'badFld' not found in message 'InstanceMsg'",
	},
	{
		Name: "update instance",
		Events1: append(validCfg,
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   tmpl1InstanceParam,
			})),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
  Name:      r1.rule.default
  Namespace: default
  Match:   true
  ActionsStatic:
  ActionsDynamic:
    Handler: h1.handler.default
    Instances:
      Name: i1.instance.default
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "update instance - bad template",
		Events1: append(validCfg,
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default.extra1.extra2",
				Params:   tmpl1InstanceParam,
			})),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "instance='i1.instance.default'.template: template 't1.default.extra1.extra2' not found",
	},
	{
		Name: "update instance ( change template ) - break rule",
		Events1: append(validCfg,
			updateEvent("t2.template.default", &adapter_model.Template{
				Descriptor_: tmpl2Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t2.default",
				Params:   tmpl2InstanceParam,
			}),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
  Resource Name:  t2.template.default
    Name:  t2.template.default
    InternalPackageDerivedName:  bar
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t2.template.default
  Params:
  - name:"i1.instance.default"
  - s2:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': instance 'i1.instance.default' is of template " +
			"'t2.template.default' which is not supported by handler 'h1.handler.default'",
	},
	{
		Name: "delete handler - break rule",
		Events1: append(validCfg,
			deleteEvent("h1.handler.default"),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Handler not found: handler='h1.default'",
	},
	{
		Name: "delete instance",
		Events1: append(validCfg,
			deleteEvent("r1.rule.default"),
			deleteEvent("i1.instance.default"),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},

	{
		Name: "delete instance - break rule",
		Events1: append(validCfg,
			deleteEvent("i1.instance.default"),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Instance not found: instance='i1.default'",
	},

	// rule
	{
		Name:    "add rule",
		Events1: validCfg,
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
  Name:      r1.rule.default
  Namespace: default
  Match:   true
  ActionsStatic:
  ActionsDynamic:
    Handler: h1.handler.default
    Instances:
      Name: i1.instance.default
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name:    "add rule mix short and long reference names",
		Events1: validCfgMixShortLongName,
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
  Name:      r1.rule.default
  Namespace: default
  Match:   true
  ActionsStatic:
  ActionsDynamic:
    Handler: h1.handler.default
    Instances:
      Name: i1.instance.default
      Name: i2.instance.default
  Name:      r2.rule.default
  Namespace: default
  Match:   true
  ActionsStatic:
  ActionsDynamic:
    Handler: h2.handler.default
    Instances:
      Name: i2.instance.default
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

  -  t2.template.default

  Name:      a2.adapter.default
  Templates:
  -  t2.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
  Resource Name:  t2.template.default
    Name:  t2.template.default
    InternalPackageDerivedName:  bar
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
  Name:    h2.handler.default
  Adapter: a2.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
  Name:     i2.instance.default
  Template: t2.template.default
  Params:
  - name:"i2.instance.default"
  - s2:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "",
	},
	{
		Name: "add rule - bad handler reference",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   tmpl1InstanceParam,
			}),
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "doesNotExist.default",
						Instances: []string{
							"i1.default",
						},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Handler not found: handler='doesNotExist.default'",
	},
	{
		Name: "add rule - bad handler kind",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   tmpl1InstanceParam,
			}),
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "not.handler",
						Instances: []string{
							"i1.default",
						},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Handler not found: handler='not.handler'",
	},
	{
		Name: "add rule - bad handler string",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t1.default",
				Params:   tmpl1InstanceParam,
			}),
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "oneWordNotAValidRef",
						Instances: []string{
							"i1.default",
						},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Handler not found: handler='oneWordNotAValidRef'",
	},
	{
		Name: "add rule - bad instance reference",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{"t1.default"},
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.default",
						Instances: []string{
							"doesnotexist.default",
						},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Instance not found: instance='doesnotexist.default'",
	},
	{
		Name: "add rule - bad instance string",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{"t1.default"},
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.default",
						Instances: []string{
							"onewordNotAValidInstRef",
						},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Instance not found: instance='onewordNotAValidInstRef'",
	},
	{
		Name: "add rule - bad instance kind",
		Events1: []*store.Event{
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{"t1.default"},
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.default",
						Instances: []string{
							"not.a.instance",
						},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
Attributes:
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Instance not found: instance='not.a.instance'",
	},

	{
		Name: "add rule - instances template not supported by handler",
		Events1: []*store.Event{
			updateEvent("attributes.attributemanifest.ns", &descriptorpb.AttributeManifest{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"source.name": {
						ValueType: descriptorpb.STRING,
					},
				},
			}),
			updateEvent("t1.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("t2.template.default", &adapter_model.Template{
				Descriptor_: tmpl1Base64Str,
			}),
			updateEvent("a1.adapter.default", &adapter_model.Info{
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
				Templates:    []string{"t1.default"},
			}),
			updateEvent("h1.handler.default", &descriptorpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			}),
			updateEvent("i1.instance.default", &descriptorpb.Instance{
				Template: "t2.default",
				Params:   tmpl1InstanceParam,
			}),
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.default",
						Instances: []string{
							// i1 is instance for template t2; but handler supports template t1.
							"i1.default",
						},
					},
				},
			}),
		},
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
  Resource Name:  t2.template.default
    Name:  t2.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t2.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': instance 'i1.instance.default' is of template " +
			"'t2.template.default' which is not supported by handler 'h1.handler.default'",
	},
	{
		Name: "update rule - duplicate instances normalized into one",
		Events1: append(validCfg,
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.default",
						Instances: []string{
							"i1.default",
							"i1",
						},
					},
				},
			}),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
  Name:      r1.rule.default
  Namespace: default
  Match:   true
  ActionsStatic:
  ActionsDynamic:
    Handler: h1.handler.default
    Instances:
      Name: i1.instance.default
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': action specified the same instance multiple times: instance='i1.instance.default'",
	},
	{
		Name: "update rule - break handler",
		Events1: append(validCfg,
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "hnotexists.default",
						Instances: []string{
							"i1.default",
						},
					},
				},
			}),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Handler not found: handler='hnotexists.default'",
	},
	{
		Name: "update rule - break instance",
		Events1: append(validCfg,
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.default",
						Instances: []string{
							"inotexist.default",
						},
					},
				},
			}),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Instance not found: instance='inotexist.default'",
	},
	{
		Name: "update rule - bad instance",
		Events1: append(validCfg,
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.default",
						Instances: []string{
							"i1.default.extra3.extra4",
						},
					},
				},
			}),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Instance not found: instance='i1.default.extra3.extra4'",
	},
	{
		Name: "add rule - bad handler",
		Events1: append(validCfg,
			updateEvent("r1.rule.default", &descriptorpb.Rule{
				Match: "true",
				Actions: []*descriptorpb.Action{
					{
						Handler: "h1.h2.h3.h4.h5",
						Instances: []string{
							"i1.instance.default",
						},
					},
				},
			}),
		),
		E: `
ID: 0
TemplatesStatic:
  Name: apa
  Name: check
  Name: quota
  Name: report
AdaptersStatic:
  Name: adapter1
  Name: adapter2
HandlersStatic:
InstancesStatic:
Rules:
AdaptersDynamic:
  Name:      a1.adapter.default
  Templates:
  -  t1.template.default

TemplatesDynamic:
  Resource Name:  t1.template.default
    Name:  t1.template.default
    InternalPackageDerivedName:  foo
HandlersDynamic:
  Name:    h1.handler.default
  Adapter: a1.adapter.default
InstancesDynamic:
  Name:     i1.instance.default
  Template: t1.template.default
  Params:
  - name:"i1.instance.default"
  - s1:source.name | "yoursrc"
Attributes:
  source.name: STRING
  template.attr: BOOL
`,
		wantErr: "action='r1.rule.default[0]': Handler not found: handler='h1.h2.h3.h4.h5'",
	},
}

var noTemplates = make(map[string]*template.Info)

var noAdapters = make(map[string]*adapter.Info)

var stdAdapters = map[string]*adapter.Info{
	"adapter1": {
		Name:          "adapter1",
		DefaultConfig: &types.Struct{},
		NewBuilder: func() adapter.HandlerBuilder {
			return &dummyHandlerBuilder{}
		},
		SupportedTemplates: []string{
			"check",
			"report",
		},
	},
	"adapter2": {
		Name:          "adapter2",
		DefaultConfig: &types.Struct{},
		NewBuilder: func() adapter.HandlerBuilder {
			return &dummyHandlerBuilder{}
		},
		SupportedTemplates: []string{
			"check",
			"report",
			"quota",
			"apa",
		},
	},
}

var stdTemplates = map[string]*template.Info{
	"quota": {
		Name:    "quota",
		Variety: adapter_model.TEMPLATE_VARIETY_QUOTA,
		InferType: func(cp proto.Message, tEvalFn template.TypeEvalFn) (proto.Message, error) {
			return nil, nil
		},
	},
	"check": {
		Name:    "check",
		CtrCfg:  &descriptorpb.Instance{},
		Variety: adapter_model.TEMPLATE_VARIETY_CHECK,
		InferType: func(cp proto.Message, tEvalFn template.TypeEvalFn) (proto.Message, error) {
			return nil, nil
		},
	},
	"report": {
		Name:    "report",
		Variety: adapter_model.TEMPLATE_VARIETY_REPORT,
		InferType: func(cp proto.Message, tEvalFn template.TypeEvalFn) (proto.Message, error) {
			return nil, nil
		},
	},
	"apa": {
		Name:    "apa",
		Variety: adapter_model.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR,
		AttributeManifests: []*descriptorpb.AttributeManifest{
			{
				Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
					"template.attr": {
						ValueType: descriptorpb.BOOL,
					},
				},
			},
		},
		InferType: func(cp proto.Message, tEvalFn template.TypeEvalFn) (proto.Message, error) {
			return nil, nil
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
	o := log.DefaultOptions()
	o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
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
			var err error

			if test.Initial != nil {
				e.SetState(test.Initial)
				s, err = e.BuildSnapshot()
			}

			if test.Events1 != nil {
				for _, event := range test.Events1 {
					e.ApplyEvent([]*store.Event{event})
				}
				s, err = e.BuildSnapshot()
			}

			if test.Events2 != nil {
				for _, event := range test.Events2 {
					e.ApplyEvent([]*store.Event{event})
				}
				s, err = e.BuildSnapshot()
			}

			if s == nil {
				s, err = e.BuildSnapshot()
			}

			if (test.wantErr == "" && err != nil) ||
				(test.wantErr != "" && err == nil) {
				tt.Fatalf("**want error '%s' error; got %v", test.wantErr, err)
			}

			str := s.String()
			if normalize(str) != normalize(test.E) {
				tt.Fatalf("config mismatch:\n%s\n != \n%s\n", str, test.E)
			}

		})
	}
}

func TestGetEntry(t *testing.T) {
	e := NewEphemeral(stdTemplates, stdAdapters)

	res := &store.Resource{
		Spec: &descriptorpb.AttributeManifest{
			Attributes: map[string]*descriptorpb.AttributeManifest_AttributeInfo{
				"foo": {
					ValueType: descriptorpb.STRING,
				},
				"bar": {
					ValueType: descriptorpb.INT64,
				},
			},
		},
	}

	key := store.Key{
		Name:      "attributes",
		Namespace: "ns",
		Kind:      "attributemanifest",
	}

	e.SetState(map[store.Key]*store.Resource{
		key: res,
	})
	gotRes, _ := e.GetEntry(&store.Event{Key: key})
	if !reflect.DeepEqual(res, gotRes) {
		t.Errorf("got :\n%v\n; want: \n%v\n", gotRes, res)
	}
}

func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Replace(s, "\t", "", -1)
	s = strings.Replace(s, "\n", "", -1)
	s = strings.Replace(s, " ", "", -1)
	return s
}

func TestAsAny(t *testing.T) {
	strVal := types.StringValue{Value: "foo"}
	d, _ := strVal.Marshal()
	anyStrVal := asAny("google.protobuf.StringValue", d)
	strValRes := types.StringValue{}
	if err := types.UnmarshalAny(anyStrVal, &strValRes); err != nil {
		t.Errorf("unmarshal failed: %v", err)
	}
	if strVal.Value != strValRes.Value {
		t.Fatalf("asAny failed. want %s; got %s", strVal.Value, strValRes.Value)
	}
}

func updateEvent(keystr string, spec proto.Message) *store.Event {
	keySegments := strings.Split(keystr, ".")
	key := store.Key{Name: keySegments[0], Kind: keySegments[1], Namespace: keySegments[2]}
	return &store.Event{Type: store.Update, Key: key, Value: &store.Resource{
		Metadata: store.ResourceMeta{Name: key.Name, Namespace: key.Namespace},
		Spec:     spec,
	}}
}

func deleteEvent(keystr string) *store.Event {
	keySegments := strings.Split(keystr, ".")
	return &store.Event{Type: store.Delete, Key: store.Key{Name: keySegments[0], Kind: keySegments[1], Namespace: keySegments[2]}}
}

func unmarshalTestData(data string) *types.Struct {
	jsonData, err := yaml.YAMLToJSON([]byte(data))
	if err != nil {
		panic(fmt.Errorf("unmarshalTestData: YAMLToJSON error: %v", err))
	}

	result := &types.Struct{}

	if err = jsonpb.Unmarshal(bytes.NewReader(jsonData), result); err != nil {
		panic(fmt.Errorf("unmarshalTestData: json.Unmarshal error: %v", err))
	}

	return result
}
