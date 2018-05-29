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

// nolint
//go:generate protoc testdata/tmpl1.proto -otestdata/tmpl1.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/tmpl2.proto -otestdata/tmpl2.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/adptCfg.proto -otestdata/adptCfg.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate protoc testdata/adptCfg2.proto -otestdata/adptCfg2.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.

package validator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	multi "github.com/hashicorp/go-multierror"

	"istio.io/api/mixer/adapter/model/v1beta1"
	cpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/lang/checker"
	"istio.io/istio/mixer/pkg/template"
)

var tmpl1Base64Str = getFileDescSetBase64("testdata/tmpl1.descriptor")
var tmpl2Base64Str = getFileDescSetBase64("testdata/tmpl2.descriptor")
var adpt1DescBase64 = getFileDescSetBase64("testdata/adptCfg.descriptor")
var adpt2DescBase64 = getFileDescSetBase64("testdata/adptCfg2.descriptor")

// testAdapterConfig is a types.Struct instance which represents the
// adapter config used in the mixer/testdata/config/listentry.yaml.
// This is commonly used in the test cases and expectations in this file.
var testAdapterConfig = &types.Struct{
	Fields: map[string]*types.Value{
		"overrides": {
			Kind: &types.Value_ListValue{ListValue: &types.ListValue{
				Values: []*types.Value{
					{Kind: &types.Value_StringValue{StringValue: "v1"}},
					{Kind: &types.Value_StringValue{StringValue: "v2"}},
				},
			}},
		},
		"blacklist": {Kind: &types.Value_BoolValue{BoolValue: false}},
	},
}

type dummyHandlerBuilder struct {
	want proto.Message
	got  proto.Message
}

func (d *dummyHandlerBuilder) SetAdapterConfig(cfg adapter.Config) {
	d.got = cfg
}

func (d *dummyHandlerBuilder) Validate() *adapter.ConfigErrors {
	var err *adapter.ConfigErrors
	if !reflect.DeepEqual(d.want, d.got) {
		err = err.Appendf("", "Got %v, Want %v", d.got, d.want)
	}
	return err
}

func (d *dummyHandlerBuilder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	return nil, errors.New("dummy can't build")
}

func getValidatorForTest() (*Validator, error) {
	path, err := filepath.Abs("../../../testdata/config")
	if err != nil {
		return nil, err
	}
	s, err := store.NewRegistry(config.StoreInventory()...).NewStore("fs://" + path)
	if err != nil {
		return nil, err
	}
	tc := checker.NewTypeChecker()
	adapterInfo := map[string]*adapter.Info{
		"listchecker": {
			DefaultConfig: &types.Struct{},
			NewBuilder: func() adapter.HandlerBuilder {
				return &dummyHandlerBuilder{want: testAdapterConfig}
			},
		},
	}
	templateInfo := map[string]*template.Info{
		"listentry": {
			CtrCfg: &types.Struct{},
			InferType: func(msg proto.Message, fn template.TypeEvalFn) (proto.Message, error) {
				st := msg.(*types.Struct)
				v, ok := st.Fields["value"]
				if !ok {
					return nil, errors.New("no value field")
				}
				value := v.GetStringValue()
				if value == "" {
					return nil, errors.New("not string value")
				}
				_, ierr := fn(value)
				return nil, ierr
			},
		},
	}
	v, err := New(tc, "destination.service", s, adapterInfo, templateInfo)
	if err != nil {
		return nil, err
	}
	return v.(*Validator), nil
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

func TestValidator(t *testing.T) {
	adpt1Bytes, _ := yaml.YAMLToJSON([]byte(`
abc: "abcstring"
`))
	var adapter1Params map[string]interface{}
	_ = json.Unmarshal(adpt1Bytes, &adapter1Params)

	adpt2Bytes, _ := yaml.YAMLToJSON([]byte(`
pqr: "abcstring"
`))
	var adapter2Params map[string]interface{}
	_ = json.Unmarshal(adpt2Bytes, &adapter2Params)

	invalidBytes, _ := yaml.YAMLToJSON([]byte(`
fildNotFound: "abcstring"
`))
	var invalidHandlerParams map[string]interface{}
	_ = json.Unmarshal(invalidBytes, &invalidHandlerParams)

	tmpl1Instance, _ := yaml.YAMLToJSON([]byte(`
s1: source.name | "yoursrc"
`))
	var tmpl1InstanceParam map[string]interface{}
	_ = json.Unmarshal(tmpl1Instance, &tmpl1InstanceParam)

	tmpl2Instance, _ := yaml.YAMLToJSON([]byte(`
s2: source.name | "yoursrc"
`))
	var tmpl2InstanceParam map[string]interface{}
	_ = json.Unmarshal(tmpl2Instance, &tmpl2InstanceParam)

	badInstance, _ := yaml.YAMLToJSON([]byte(`
badFld: "s1stringVal"
`))
	var badInstanceParamIn map[string]interface{}
	_ = json.Unmarshal(badInstance, &badInstanceParamIn)

	validCfg := []*store.Event{
		updateEvent("t1.template.default", &v1beta1.Template{
			Descriptor_: tmpl1Base64Str,
		}),
		updateEvent("a1.adapter.default", &v1beta1.Info{
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       adpt1DescBase64,
			Templates:    []string{"t1.default"},
		}),
		updateEvent("h1.handler.default", &cpb.Handler{
			Adapter: "a1.default",
			Params:  adapter1Params,
		}),
		updateEvent("i1.instance.default", &cpb.Instance{
			Template: "t1.default",
			Params:   tmpl1InstanceParam,
		}),
		updateEvent("r1.rule.default", &cpb.Rule{
			Match: "true",
			Actions: []*cpb.Action{
				{
					Handler: "h1.default",
					Instances: []string{
						"i1.default",
					},
				},
			},
		}),
	}

	validCfgMixShortLongName := []*store.Event{
		updateEvent("t1.template.default", &v1beta1.Template{
			Descriptor_: tmpl1Base64Str,
		}),
		updateEvent("t2.template.default", &v1beta1.Template{
			Descriptor_: tmpl2Base64Str,
		}),
		updateEvent("a1.adapter.default", &v1beta1.Info{
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       adpt1DescBase64,
			Templates:    []string{"t1", "t2.default"},
		}),
		updateEvent("a2.adapter.default", &v1beta1.Info{
			Description:  "testAdapter description",
			SessionBased: true,
			Config:       adpt2DescBase64,
			Templates:    []string{"t2.default"},
		}),
		updateEvent("h1.handler.default", &cpb.Handler{
			Adapter: "a1.default",
			Params:  adapter1Params,
		}),
		updateEvent("h2.handler.default", &cpb.Handler{
			Adapter: "a2",
			Params:  adapter2Params,
		}),
		updateEvent("i1.instance.default", &cpb.Instance{
			Template: "t1.default",
			Params:   tmpl1InstanceParam,
		}),
		updateEvent("i2.instance.default", &cpb.Instance{
			Template: "t2",
			Params:   tmpl2InstanceParam,
		}),

		updateEvent("r1.rule.default", &cpb.Rule{
			Match: "true",
			Actions: []*cpb.Action{
				{
					Handler: "h1.default",
					Instances: []string{
						"i1.default",
						"i2",
					},
				},
			},
		}),
		updateEvent("r2.rule.default", &cpb.Rule{
			Match: "true",
			Actions: []*cpb.Action{
				{
					Handler: "h2.default",
					Instances: []string{
						"i2",
					},
				},
			},
		}),
	}

	for _, cc := range []struct {
		title   string
		evs     []*store.Event
		ok      bool
		wantErr string
	}{
		{
			"new rule",
			[]*store.Event{updateEvent("test.rule.default", &cpb.Rule{
				Actions: []*cpb.Action{
					{Handler: "staticversion.listchecker.istio-system", Instances: []string{"appversion.listentry.istio-system"}},
				}})},
			true,
			"",
		},
		{
			"update rule",
			[]*store.Event{updateEvent("checkwl.rule.istio-system", &cpb.Rule{
				Actions: []*cpb.Action{
					{Handler: "staticversion.listchecker", Instances: []string{"appversion.listentry"}},
				}})},
			true,
			"",
		},
		{
			"delete rule",
			[]*store.Event{deleteEvent("checkwl.rule.istio-system")},
			true,
			"",
		},
		{
			"invalid updating rule: match syntax error",
			[]*store.Event{updateEvent("test.rule.default", &cpb.Rule{Match: "foo"})},
			false,
			"",
		},
		{
			"invalid updating rule: match type error",
			[]*store.Event{updateEvent("test.rule.default", &cpb.Rule{Match: "1"})},
			false,
			"",
		},
		{
			"invalid updating rule: reference not found",
			[]*store.Event{updateEvent("test.rule.default",
				&cpb.Rule{Actions: []*cpb.Action{{Handler: "nonexistent.listchecker.istio-system"}}})},
			false,
			"",
		},
		{
			"adding adapter",
			[]*store.Event{updateEvent("test.listchecker.default", testAdapterConfig)},
			true,
			"",
		},
		{
			"adding instance",
			[]*store.Event{updateEvent("test.listentry.default", &types.Struct{Fields: map[string]*types.Value{
				"value": {Kind: &types.Value_StringValue{StringValue: "0"}},
			}})},
			true,
			"",
		},
		{
			"adapter validation failure",
			[]*store.Event{updateEvent("test.listchecker.default", &types.Struct{})},
			false,
			"",
		},
		{
			"invalid instance",
			[]*store.Event{updateEvent("test.listentry.default", &types.Struct{})},
			false,
			"",
		},
		{
			"invalid instance syntax",
			[]*store.Event{updateEvent("test.listentry.default", &types.Struct{Fields: map[string]*types.Value{
				"value": {Kind: &types.Value_StringValue{StringValue: ""}},
			}})},
			false,
			"",
		},
		{
			"invalid delete handler",
			[]*store.Event{deleteEvent("staticversion.listchecker.istio-system")},
			false,
			"",
		},
		{
			"invalid delete instance",
			[]*store.Event{deleteEvent("appversion.listentry.istio-system")},
			false,
			"",
		},
		{
			"invalid removal of attributemanifest",
			[]*store.Event{deleteEvent("kubernetes.attributemanifest.istio-system")},
			false,
			"",
		},

		// Templates
		{
			"add template",
			[]*store.Event{updateEvent("metric.template.default", &v1beta1.Template{
				Descriptor_: tmpl1Base64Str,
			})},
			true,
			"",
		},
		{
			"update template",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					Templates:    []string{"t1.default"},
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   tmpl1InstanceParam,
				}),
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
			},
			true,
			"",
		},
		{
			"add template - bad descriptor",
			[]*store.Event{updateEvent("metric.template.default", &v1beta1.Template{
				Descriptor_: "bad base64 string",
			})},
			false,
			"template[metric.template.default].descriptor: illegal base64 data at input byte 3",
		},
		{
			"update template - break instance",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   tmpl1InstanceParam,
				}),
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl2Base64Str,
				}),
			},
			false,
			"instance[i1.instance.default].Params: fieldEncoder 's1' not found in message 'Template'",
		},
		{
			"delete template",
			[]*store.Event{
				updateEvent("mymetric.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("testCR1.adapter.default", &v1beta1.Info{
					Name:         "testAdapter",
					Description:  "testAdapter description",
					SessionBased: true,
					Templates:    []string{"mymetric.default"},
				}),
				deleteEvent("testCR1.adapter.default"),
				deleteEvent("mymetric.template.default"),
			},
			true,
			"",
		},
		{
			"delete template - break adapter",
			[]*store.Event{
				updateEvent("mymetric.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("testCR1.adapter.default", &v1beta1.Info{
					Name:         "testAdapter",
					Description:  "testAdapter description",
					SessionBased: true,
					Templates:    []string{"mymetric.default"},
				}),
				deleteEvent("mymetric.template.default"),
			},
			false,
			"adapter[testCR1.adapter.default]/templates[0]: references to be deleted template mymetric.template.default",
		},
		{
			"delete template - break instance",
			[]*store.Event{
				updateEvent("mymetric.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("testCR1.instance.default", &cpb.Instance{
					Template: "mymetric.default",
				}),
				deleteEvent("mymetric.template.default"),
			},
			false,
			"instance[testCR1.instance.default].template: references to be deleted template mymetric.template.default",
		},

		// Adapters
		{
			"add adapter",
			[]*store.Event{updateEvent("testCR1.adapter.default", &v1beta1.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       "",
			})},
			true,
			"",
		},
		{
			"add adapter - bad tmpls",
			[]*store.Event{updateEvent("testCR1.adapter.default", &v1beta1.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       "",
				Templates:    []string{"a.b.c"},
			})},
			false,
			"emplates[0]: a.b.c is not a template",
		},
		{
			"add adapter - bad cfg",
			[]*store.Event{updateEvent("testCR1.adapter.default", &v1beta1.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       "bad cfg descriptor",
			})},
			false,
			"",
		},
		{
			"add adapter - valid template reference",
			[]*store.Event{
				updateEvent("mymetric.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("testCR1.adapter.default", &v1beta1.Info{
					Name:         "testAdapter",
					Description:  "testAdapter description",
					SessionBased: true,
					Templates:    []string{"mymetric.default"},
				})},
			true,
			"",
		},
		{
			"add adapter, valid template reference, short tmpl name",
			[]*store.Event{
				updateEvent("mymetric.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("testCR1.adapter.default", &v1beta1.Info{
					Name:         "testAdapter",
					Description:  "testAdapter description",
					SessionBased: true,
					Templates:    []string{"mymetric.default"},
				})},
			true,
			"",
		},
		{
			"add adapter - bad template kind",
			[]*store.Event{updateEvent("testCR1.adapter.default", &v1beta1.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Templates:    []string{"notATemplate.rule"},
			})},
			false,
			"notATemplate.rule not found",
		},
		{
			"add adapter - bad template reference",
			[]*store.Event{updateEvent("testCR1.adapter.default", &v1beta1.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Templates:    []string{"notexists.default"},
			})},
			false,
			"notexists.default not found",
		},
		{
			"update adapter",
			append(validCfg,
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					Templates:    []string{"t1.default"},
				})),
			true,
			"",
		},
		{
			"update adapter supported template- break routed instances",
			append(validCfg,
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description 22",
					SessionBased: true,
					Config:       adpt1DescBase64,
					// stop supporting previously supported template "t1.template.default"
					Templates: []string{},
				})),
			false,
			"adapter a1.adapter.default does not support template t1.template.default of the " +
				"instance i1.instance.default",
		},
		{
			"update adapter cfg - break handler",
			append(validCfg,
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt2DescBase64,
					Templates:    []string{"t1.default"},
				}),
			),
			false,
			"field 'abc' not found in message 'Params'",
		},
		{
			"delete adapter",
			[]*store.Event{
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
				}),
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a1.default",
					Params:  adapter1Params,
				}),
				deleteEvent("h1.handler.default"),
				deleteEvent("a1.adapter.default"),
			},
			true,
			"",
		},
		{
			"delete adapter - break handler",
			[]*store.Event{
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					Templates:    []string{},
				}),
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a1.default",
					Params:  adapter1Params,
				}),
				deleteEvent("a1.adapter.default"),
			},
			false,
			"handler[h1.handler.default].adapter: references to be deleted adapter a1.adapter.default",
		},

		// handler
		{
			"add handler",
			[]*store.Event{updateEvent("a1.adapter.default", &v1beta1.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
			}), updateEvent("h1.handler.default", &cpb.Handler{
				Adapter: "a1.default",
				Params:  adapter1Params,
			})},
			true,
			"",
		},
		{
			"add handler - bad adapter",
			[]*store.Event{updateEvent("h1.handler.default", &cpb.Handler{
				Adapter: "not.an.adapter",
				Params:  adapter1Params,
			})},
			false,
			"handler[h1.handler.default]: adapter not.an.adapter not found",
		},
		{
			"add handler - bad adapter reference",
			[]*store.Event{updateEvent("h1.handler.default", &cpb.Handler{
				Adapter: "broken.default",
				Params:  adapter1Params,
			})},
			false,
			"handler[h1.handler.default]: adapter broken.default not found",
		},
		{
			"add handler - broken adapter reference",
			[]*store.Event{updateEvent("h1.handler.default", &cpb.Handler{
				Adapter: "broken.default.extra3.extra4",
				Params:  adapter1Params,
			})},
			false,
			"handler[h1.handler.default]: invalid adapter value broken.default.extra3.extra4",
		},
		{
			"add handler - bad param content",
			[]*store.Event{updateEvent("a1.adapter.default", &v1beta1.Info{
				Name:         "testAdapter",
				Description:  "testAdapter description",
				SessionBased: true,
				Config:       adpt1DescBase64,
			}), updateEvent("h1.handler.default", &cpb.Handler{
				Adapter: "a1.default",
				Params:  invalidHandlerParams,
			})},
			false,
			"handler[h1.handler.default]: field 'fildNotFound' not found in message 'Params'",
		},
		{
			"update handler",
			append(validCfg,
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a1.default",
					Params:  nil,
				})),
			true,
			"",
		},
		{
			"update handler's adapter - break supported tmpls",
			append(validCfg,
				updateEvent("a2.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					// Does not support any template
					Templates: []string{},
				}),
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a2.default",
					Params:  adapter1Params,
				})),
			false,
			"adapter a2.adapter.default does not support template t1.template.default of the " +
				"instance i1.instance.default",
		},
		{
			"delete handler",
			append(validCfg,
				deleteEvent("r1.rule.default"),
				deleteEvent("h1.handler.default"),
			),
			true,
			"",
		},

		// instances
		{
			"add instance",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   tmpl1InstanceParam,
				}),
			},
			true,
			"",
		},
		{
			"add instance - bad template",
			[]*store.Event{
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "not.a.template",
					Params:   tmpl1InstanceParam,
				}),
			},
			false,
			"instance[i1.instance.default]: template not.a.template not found",
		},
		{
			"add instance - bad template reference",
			[]*store.Event{
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "notExist.default",
					Params:   tmpl1InstanceParam,
				}),
			},
			false,
			"instance[i1.instance.default]: template notExist.default not found",
		},
		{
			"add instance - bad template string",
			[]*store.Event{
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "oneWordNotValidRef",
					Params:   tmpl1InstanceParam,
				}),
			},
			false,
			"instance[i1.instance.default]: template oneWordNotValidRef not found",
		},
		{
			"add instance - bad content",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   badInstanceParamIn,
				}),
			},
			false,
			"instance[i1.instance.default]: fieldEncoder 'badFld' not found in message 'Template'",
		},
		{
			"update instance",
			append(validCfg,
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   tmpl1InstanceParam,
				})),
			true,
			"",
		},
		{
			"update instance - bad template",
			append(validCfg,
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default.extra1.extra2",
					Params:   tmpl1InstanceParam,
				})),
			false,
			"instance[i1.instance.default]: invalid template value t1.default.extra1.extra2",
		},
		{
			"update instance ( change template ) - break rule",
			append(validCfg,
				updateEvent("t2.template.default", &v1beta1.Template{
					Descriptor_: tmpl2Base64Str,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t2.default",
					Params:   tmpl2InstanceParam,
				}),
			),
			false,
			"rule[r1.rule.default].actions[0].handler.adapter 'a1.adapter.default': " +
				"instance[i1.instance.default].Template 't2.default' is not supported by adapter of the handler " +
				"to which the instance is routed",
		},
		{
			"delete handler - break rule",
			append(validCfg,
				deleteEvent("h1.handler.default"),
			),
			false,
			"h1.handler.default is referred by r1.rule.default/actions[0].handler",
		},
		{
			"delete instance",
			append(validCfg,
				deleteEvent("r1.rule.default"),
				deleteEvent("i1.instance.default"),
			),
			true,
			"",
		},

		{
			"delete instance - break rule",
			append(validCfg,
				deleteEvent("i1.instance.default"),
			),
			false,
			"i1.instance.default is referred by r1.rule.default/actions[0].instances",
		},

		// rule
		{
			"add rule",
			validCfg,
			true,
			"",
		},
		{
			"add rule mix short and long reference names",
			validCfgMixShortLongName,
			true,
			"",
		},
		{
			"add rule - bad handler reference",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   tmpl1InstanceParam,
				}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "doesNotExist.default",
							Instances: []string{
								"i1.default",
							},
						},
					},
				}),
			},
			false,
			"actions[0].handler 'doesNotExist.default': handler doesNotExist.handler.default not found",
		},
		{
			"add rule - bad handler kind",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   tmpl1InstanceParam,
				}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "not.handler",
							Instances: []string{
								"i1.default",
							},
						},
					},
				}),
			},
			false,
			"actions[0].handler 'not.handler': handler not.handler.handler not found",
		},
		{
			"add rule - bad handler string",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t1.default",
					Params:   tmpl1InstanceParam,
				}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "oneWordNotAValidRef",
							Instances: []string{
								"i1.default",
							},
						},
					},
				}),
			},
			false,
			"actions[0].handler 'oneWordNotAValidRef': handler oneWordNotAValidRef.handler.default not found",
		},
		{
			"add rule - bad instance reference",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					Templates:    []string{"t1.default"},
				}),
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a1.default",
					Params:  adapter1Params,
				}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "h1.default",
							Instances: []string{
								"doesnotexist.default",
							},
						},
					},
				}),
			},
			false,
			"actions[0].instances[0] 'doesnotexist.default': instance doesnotexist.default not found",
		},
		{
			"add rule - bad instance string",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					Templates:    []string{"t1.default"},
				}),
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a1.default",
					Params:  adapter1Params,
				}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "h1.default",
							Instances: []string{
								"onewordNotAValidInstRef",
							},
						},
					},
				}),
			},
			false,
			"actions[0].instances[0] 'onewordNotAValidInstRef': instance onewordNotAValidInstRef not found",
		},
		{
			"add rule - bad instance kind",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					Templates:    []string{"t1.default"},
				}),
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a1.default",
					Params:  adapter1Params,
				}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "h1.default",
							Instances: []string{
								"not.a.instance",
							},
						},
					},
				}),
			},
			false,
			"actions[0].instances[0] 'not.a.instance': instance not.a.instance not found",
		},

		{
			"add rule - instances template not supported by handler",
			[]*store.Event{
				updateEvent("t1.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("t2.template.default", &v1beta1.Template{
					Descriptor_: tmpl1Base64Str,
				}),
				updateEvent("a1.adapter.default", &v1beta1.Info{
					Description:  "testAdapter description",
					SessionBased: true,
					Config:       adpt1DescBase64,
					Templates:    []string{"t1.default"},
				}),
				updateEvent("h1.handler.default", &cpb.Handler{
					Adapter: "a1.default",
					Params:  adapter1Params,
				}),
				updateEvent("i1.instance.default", &cpb.Instance{
					Template: "t2.default",
					Params:   tmpl1InstanceParam,
				}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
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
			false,
			"actions[0].instances[0].Template 't2.default': actions[0].handler.adapter 'a1.adapter.default' " +
				"does not support template 't2.default'. Only supported templates are t1.default",
		},
		{
			"update rule",
			append(validCfg,
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "h1.default",
							Instances: []string{
								"i1.default",
							},
						},
					},
				}),
			),
			true,
			"",
		},
		{
			"update rule - break handler",
			append(validCfg,
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "hnotexists.default",
							Instances: []string{
								"i1.default",
							},
						},
					},
				}),
			),
			false,
			"actions[0].handler 'hnotexists.default': handler hnotexists.handler.default not found",
		},
		{
			"update rule - break instance",
			append(validCfg,
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "h1.default",
							Instances: []string{
								"inotexist.default",
							},
						},
					},
				}),
			),
			false,
			" actions[0].instances[0] 'inotexist.default': instance inotexist.default not found",
		},
		{
			"update rule - bad instance",
			append(validCfg,
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "h1.default",
							Instances: []string{
								"i1.default.extra3.extra4",
							},
						},
					},
				}),
			),
			false,
			"actions[0].instances[0] 'i1.default.extra3.extra4': invalid instance value i1.default.extra3.extra4",
		},
		{
			"add rule - bad handler",
			append(validCfg,
				updateEvent("r1.rule.default", &cpb.Rule{
					Match: "true",
					Actions: []*cpb.Action{
						{
							Handler: "h1.h2.h3.h4.h5",
							Instances: []string{
								"i1.instance.default",
							},
						},
					},
				}),
			),
			false,
			"actions[0].handler: illformed h1.h2.h3.h4.h5",
		},
	} {
		t.Run(cc.title, func(tt *testing.T) {
			v, err := getValidatorForTest()
			v.refreshTypeChecker()
			if err != nil {
				tt.Fatal(err)
			}
			defer v.Stop()
			var result *multi.Error
			for _, ev := range cc.evs {
				e := v.Validate(ev)
				v.refreshTypeChecker()
				result = multi.Append(result, e)
			}
			ok := result.ErrorOrNil() == nil
			if cc.ok != ok {
				tt.Errorf("Got %v, Want %v", result.ErrorOrNil(), cc.ok)
			}
			if cc.wantErr != "" {
				if !strings.Contains(result.Error(), cc.wantErr) {
					tt.Errorf("Got error %s, Want err %s", result.Error(), cc.wantErr)
				}
			}
		})
	}
}

func TestValidatorToRememberValidation(t *testing.T) {
	t.SkipNow()
	for _, c := range []struct {
		title string
		ev1   *store.Event
		evs   []*store.Event
	}{
		{
			"reference",
			updateEvent("test.rule.default", &cpb.Rule{
				Actions: []*cpb.Action{
					{Handler: "test.listchecker", Instances: []string{"appversion.listentry.istio-system"}},
				},
			}),
			[]*store.Event{updateEvent("test.listchecker.default", testAdapterConfig)},
		},
		{
			"deletion order",
			deleteEvent("checkwl.rule.istio-system"),
			[]*store.Event{
				deleteEvent("staticversion.listchecker.istio-system"),
				deleteEvent("appversion.listentry.istio-system"),
			},
		},
		{
			"deleting attribute manifests",
			deleteEvent("kubernetes.attributemanifest.istio-system"),
			[]*store.Event{
				deleteEvent("staticversion.listchecker.istio-system"),
				deleteEvent("appversion.listentry.istio-system"),
				deleteEvent("checkwl.rule.istio-system"),
			},
		},
	} {
		t.Run(c.title, func(tt *testing.T) {
			v, err := getValidatorForTest()
			if err != nil {
				tt.Fatal(err)
			}
			defer v.Stop()

			if err = v.Validate(c.ev1); err == nil {
				tt.Error("Got nil, Want error")
			}
			for _, ev := range c.evs {
				if err = v.Validate(ev); err != nil {
					tt.Errorf("Got %v, Want nil", err)
				}
			}
			if err = v.Validate(c.ev1); err != nil {
				tt.Errorf("Got %v, Want nil", err)
			}
		})
	}
}

func getFileDescSetBase64(path string) string {
	byts, _ := ioutil.ReadFile(path)
	var b bytes.Buffer
	encoder := base64.NewEncoder(base64.StdEncoding, &b)
	_, _ = encoder.Write(byts)
	_ = encoder.Close()
	return b.String()
}
