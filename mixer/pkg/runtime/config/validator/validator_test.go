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

package validator

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/config/crd"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	cpb "istio.io/api/policy/v1beta1"
	adapter2 "istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/lang/checker"
	"istio.io/istio/mixer/pkg/template"
	template2 "istio.io/istio/mixer/template"
)

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

type dummyHandlerBldr struct {
	want proto.Message
	got  proto.Message
}

func (d *dummyHandlerBldr) SetAdapterConfig(cfg adapter.Config) {
	d.got = cfg
}

func (d *dummyHandlerBldr) Validate() *adapter.ConfigErrors {
	var err *adapter.ConfigErrors
	if !reflect.DeepEqual(d.want, d.got) {
		err = err.Appendf("", "Got %v, Want %v", d.got, d.want)
	}
	return err
}

func (d *dummyHandlerBldr) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	return nil, errors.New("dummy can't build")
}

func getValidatorForTest() (*Validator, error) {
	path, err := filepath.Abs("../../../../testdata/config")
	if err != nil {
		return nil, err
	}
	groupVersion := &schema.GroupVersion{Group: crd.ConfigAPIGroup, Version: crd.ConfigAPIVersion}
	s, err := store.NewRegistry(config.StoreInventory()...).NewStore("fs://"+path, groupVersion, "")
	if err != nil {
		return nil, err
	}
	tc := checker.NewTypeChecker()
	adapterInfo := make(map[string]*adapter.Info)
	for _, y := range adapter2.Inventory() {
		i := y()
		adapterInfo[i.Name] = &i
	}

	adapterInfo["listchecker"] = &adapter.Info{
		DefaultConfig: &types.Struct{},
		NewBuilder: func() adapter.HandlerBuilder {
			return &dummyHandlerBldr{want: testAdapterConfig}
		},
		SupportedTemplates: []string{
			"listentry",
		},
	}

	templateInfo := make(map[string]*template.Info)
	for x, y := range template2.SupportedTmplInfo {
		tmp := y
		templateInfo[x] = &tmp
	}

	templateInfo["listentry"] = &template.Info{
		Name:   "listentry",
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
		BuilderSupportsTemplate: func(builder adapter.HandlerBuilder) bool {
			return true
		},
	}

	v, err := NewValidator(tc, s, adapterInfo, templateInfo)
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
			"rule='test.rule.default'.Match: unknown attribute foo",
		},
		{
			"invalid updating rule: match type error",
			[]*store.Event{updateEvent("test.rule.default", &cpb.Rule{Match: "1"})},
			false,
			"rule='test.rule.default'.Match: expression '1' evaluated to type INT64, expected type BOOL",
		},
		{
			"invalid updating rule: reference not found",
			[]*store.Event{updateEvent("test.rule.default", &cpb.Rule{Actions: []*cpb.Action{{Handler: "nonexistent.listchecker.istio-system"}}})},
			false,
			"action='test.rule.default[0]': Handler not found: handler='nonexistent.listchecker.istio-system'",
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
			[]*store.Event{
				updateEvent("test.listchecker.default", &types.Struct{}),
				updateEvent("testInst.listentry.default", &types.Struct{Fields: map[string]*types.Value{
					"value": {Kind: &types.Value_StringValue{StringValue: "0"}},
				}}),
				updateEvent("r1.rule.default", &cpb.Rule{
					Actions: []*cpb.Action{
						{Handler: "test.listchecker.default", Instances: []string{"testInst.listentry.default"}},
					}}),
			},
			false,
			"handler[test.listchecker.default]/instances[testInst.listentry.default]: adapter validation failed",
		},
		{
			"invalid instance",
			[]*store.Event{updateEvent("test.listentry.default", &types.Struct{})},
			false,
			"instance='test.listentry.default': no value field",
		},
		{
			"invalid instance syntax",
			[]*store.Event{updateEvent("test.listentry.default", &types.Struct{Fields: map[string]*types.Value{
				"value": {Kind: &types.Value_StringValue{StringValue: ""}},
			}})},
			false,
			"instance='test.listentry.default': not string value",
		},
		{
			"invalid delete handler",
			[]*store.Event{deleteEvent("staticversion.listchecker.istio-system")},
			false,
			"action='checkwl.rule.istio-system[0]': Handler not found: handler='staticversion.listchecker'",
		},
		{
			"invalid delete instance",
			[]*store.Event{deleteEvent("appversion.listentry.istio-system")},
			false,
			"action='checkwl.rule.istio-system[0]': Instance not found: instance='appversion.listentry'",
		},
		{
			"invalid removal of attributemanifest",
			[]*store.Event{deleteEvent("kubernetes.attributemanifest.istio-system")},
			false,
			"",
		},
	} {
		t.Run(cc.title, func(tt *testing.T) {
			v, err := getValidatorForTest()
			if err != nil {
				tt.Fatal(err)
			}
			defer v.Stop()
			var result *multierror.Error
			for _, ev := range cc.evs {
				e := v.Validate(ev)
				result = multierror.Append(result, e)
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
	t.Skip("https://github.com/istio/istio/issues/7696")
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
