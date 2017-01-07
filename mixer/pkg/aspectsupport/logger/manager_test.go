// Copyright 2017 Google Inc.
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

package logger

import (
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/struct"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"

	jtpb "github.com/golang/protobuf/jsonpb/jsonpb_test_proto"
	"github.com/golang/protobuf/ptypes/empty"
	configpb "istio.io/api/istio/config/v1"
	"istio.io/mixer/pkg/aspect/logger"
	"istio.io/mixer/pkg/aspectsupport"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m.Kind() != "istio/logger" {
		t.Error("Wrong kind of adapter")
	}
}

func TestNewAspectBadConfig(t *testing.T) {

	newAspectShouldError := []aspectTestCase{
		{"mismatched protos", widget, statusStruct},
	}

	m := NewManager()

	for _, v := range newAspectShouldError {
		c := aspectsupport.CombinedConfig{Adapter: &configpb.Adapter{Params: v.params}}
		if _, err := m.NewAspect(&c, &testLogger{defaultCfg: v.defaultCfg}, testEnv{}); err == nil {
			t.Errorf("NewAspect(): expected error for %s", v.name)
		}
	}
}

func TestNewAspectConfig(t *testing.T) {

	newAspectShouldSucceed := []aspectTestCase{
		{"empty", widget, &structpb.Struct{}},
		{"nil", widget, nil},
		{"override", widget, widgetStruct},
	}

	m := NewManager()

	for _, v := range newAspectShouldSucceed {
		c := aspectsupport.CombinedConfig{Adapter: &configpb.Adapter{Params: v.params}, Aspect: &configpb.Aspect{Inputs: map[string]string{}}}
		if _, err := m.NewAspect(&c, &testLogger{defaultCfg: v.defaultCfg}, testEnv{}); err != nil {
			t.Errorf("NewAspect(): should not have received error for %s (%v)", v.name, err)
		}
	}
}

func TestNewAspectBadAdapter(t *testing.T) {
	m := NewManager()

	var generic aspect.Adapter

	if _, err := m.NewAspect(&aspectsupport.CombinedConfig{}, generic, testEnv{}); err == nil {
		t.Error("NewAspect(): expected error for bad adapter")
	}
}

func TestExecute(t *testing.T) {
	executeShouldSucceed := []executeTestCase{
		{"single attribute", map[string]string{"attr": "val"}, &testBag{}, &testEvaluator{}},
	}

	for _, v := range executeShouldSucceed {
		l := &testLogger{}
		e := &executor{v.inputs, l}
		if _, err := e.Execute(v.bag, v.mapper); err != nil {
			t.Errorf("Execute(): should not have received error for %s (%v)", v.name, err)
		}
		if l.entryCount != 1 {
			t.Errorf("Execute(): wanted entry count of 1, got %d", l.entryCount)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	m := NewManager()
	o := m.DefaultConfig()
	if !proto.Equal(o, &empty.Empty{}) {
		t.Errorf("DefaultConfig(): wanted empty proto, got %v", o)
	}
}

func TestValidateConfig(t *testing.T) {
	m := NewManager()
	if err := m.ValidateConfig(&empty.Empty{}); err != nil {
		t.Errorf("ValidateConfig(): unexpected error: %v", err)
	}
}

type (
	structMap      map[string]*structpb.Value
	aspectTestCase struct {
		name       string
		defaultCfg proto.Message
		params     *structpb.Struct
	}
	executeTestCase struct {
		name   string
		inputs map[string]string
		bag    attribute.Bag
		mapper expr.Evaluator
	}
	testLogger struct {
		logger.Adapter
		logger.Aspect

		defaultCfg proto.Message
		entryCount int
	}
	testEvaluator struct {
		expr.Evaluator
	}
	testBag struct {
		attribute.Bag
	}
	testEnv struct {
		aspect.Env
	}
)

var (
	widget       = &jtpb.Widget{Color: jtpb.Widget_RED.Enum(), RColor: []jtpb.Widget_Color{jtpb.Widget_GREEN}}
	statusStruct = newStruct(structMap{"code": newStringVal("Test")})
	widgetStruct = newStruct(structMap{"color": newStringVal("BLUE"), "simple": newStructVal(structMap{"o_bool": newBoolVal(false)})})
)

func (t *testLogger) NewAspect(e aspect.Env, m proto.Message) (logger.Aspect, error) { return t, nil }
func (t *testLogger) DefaultConfig() proto.Message                                   { return t.defaultCfg }
func (t *testLogger) Log([]logger.Entry) error                                       { t.entryCount++; return nil }
func (t *testLogger) Close() error                                                   { return nil }

func (t *testEvaluator) Eval(e string, bag attribute.Bag) (interface{}, error) {
	return e, nil
}

func newStringVal(s string) *structpb.Value {
	return &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: s}}
}

func newBoolVal(b bool) *structpb.Value {
	return &structpb.Value{Kind: &structpb.Value_BoolValue{BoolValue: b}}
}

func newStruct(fields map[string]*structpb.Value) *structpb.Struct {
	return &structpb.Struct{Fields: fields}
}

func newStructVal(fields map[string]*structpb.Value) *structpb.Value {
	return &structpb.Value{Kind: &structpb.Value_StructValue{StructValue: newStruct(fields)}}
}
