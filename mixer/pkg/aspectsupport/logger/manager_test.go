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
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/struct"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/aspect/logger"
	"istio.io/mixer/pkg/aspectsupport"
	"istio.io/mixer/pkg/aspectsupport/logger/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"

	configpb "istio.io/api/mixer/v1/config"
	dpb "istio.io/api/mixer/v1/config/descriptor"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m.Kind() != "istio/logger" {
		t.Error("Wrong kind of adapter")
	}
}

func TestManager_NewAspect(t *testing.T) {
	tl := &testLogger{}

	defaultExec := &executor{
		logName:      "istio_log",
		timestampFmt: time.RFC3339,
		aspect:       tl,
		inputs:       map[string]string{},
		descriptors:  []dpb.LogEntryDescriptor{},
	}

	overrideExec := &executor{
		logName:      "istio_log",
		timestampFmt: "2006-Jan-02",
		aspect:       tl,
		inputs:       map[string]string{},
		descriptors:  []dpb.LogEntryDescriptor{},
	}

	overrideStruct := newStruct(structMap{"timestamp_format": newStringVal("2006-Jan-02")})

	newAspectShouldSucceed := []aspectTestCase{
		{"empty", &config.Params{}, newStruct(nil), defaultExec},
		{"nil", &config.Params{}, nil, defaultExec},
		{"override", &config.Params{}, overrideStruct, overrideExec},
	}

	m := NewManager()

	for _, v := range newAspectShouldSucceed {
		c := aspectsupport.CombinedConfig{
			Adapter: &configpb.Adapter{},
			Aspect:  &configpb.Aspect{Params: v.params, Inputs: map[string]string{}},
		}
		asp, err := m.NewAspect(&c, tl, testEnv{})
		if err != nil {
			t.Errorf("NewAspect(): should not have received error for %s (%v)", v.name, err)
		}
		got := asp.(*executor)
		got.defaultTimeFn = nil // ignore time fns in equality comp
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewAspect() => %v (%T), want %v (%T)", got, got, v.want, v.want)
		}
	}
}

func TestManager_NewAspectFailures(t *testing.T) {

	defaultCfg := &aspectsupport.CombinedConfig{
		Adapter: &configpb.Adapter{},
		Aspect:  &configpb.Aspect{},
	}

	var generic aspect.Adapter
	errLogger := &testLogger{defaultCfg: &structpb.Struct{}, errOnNewAspect: true}

	failureCases := []struct {
		cfg   *aspectsupport.CombinedConfig
		adptr aspect.Adapter
	}{
		{defaultCfg, generic},
		{defaultCfg, errLogger},
	}

	m := NewManager()
	for _, v := range failureCases {
		if _, err := m.NewAspect(v.cfg, v.adptr, testEnv{}); err == nil {
			t.Errorf("NewAspect(): expected error for bad adapter (%T)", v.adptr)
		}
	}
}

func TestExecutor_Execute(t *testing.T) {
	testTime, _ := time.Parse("2006-Jan-02", "2011-Aug-14")
	noPayloadDesc := dpb.LogEntryDescriptor{
		Name:       "test",
		Attributes: []string{"attr"},
	}
	payloadDesc := dpb.LogEntryDescriptor{
		Name:             "test",
		Attributes:       []string{"attr", "payload"},
		PayloadAttribute: "payload",
	}
	jsonPayloadDesc := dpb.LogEntryDescriptor{
		Name:             "test",
		Attributes:       []string{"key"},
		PayloadAttribute: "payload",
		PayloadFormat:    dpb.LogEntryDescriptor_JSON,
	}
	withInputsDesc := dpb.LogEntryDescriptor{
		Name:             "test",
		Attributes:       []string{"key"},
		PayloadAttribute: "attr",
	}

	noDescriptorExec := &executor{"istio_log", []dpb.LogEntryDescriptor{}, map[string]string{}, "severity", "ts", "", nil, time.Now}
	noPayloadExec := &executor{"istio_log", []dpb.LogEntryDescriptor{noPayloadDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	payloadExec := &executor{"istio_log", []dpb.LogEntryDescriptor{payloadDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	jsonPayloadExec := &executor{"istio_log", []dpb.LogEntryDescriptor{jsonPayloadDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	withInputsExec := &executor{"istio_log", []dpb.LogEntryDescriptor{withInputsDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	withTimestampFormatExec := &executor{
		"istio_log",
		[]dpb.LogEntryDescriptor{noPayloadDesc},
		map[string]string{"attr": "val"},
		"severity",
		"ts",
		"2006-Jan-02",
		nil,
		time.Now,
	}

	jsonBag := &testBag{strs: map[string]string{"key": "value", "payload": `{"obj":{"val":54},"question":true}`}}
	tsBag := &testBag{times: map[string]time.Time{"ts": testTime}}

	structPayload := map[string]interface{}{"obj": map[string]interface{}{"val": float64(54)}, "question": true}

	defaultEntry := newEntry("istio_log", map[string]interface{}{"attr": "val"}, "", logger.Default, "", nil)
	infoEntry := newEntry("istio_log", map[string]interface{}{"attr": "val"}, "", logger.Info, "", nil)
	textPayloadEntry := newEntry("istio_log", map[string]interface{}{"attr": "val"}, "", logger.Default, "test", nil)
	jsonPayloadEntry := newEntry("istio_log", map[string]interface{}{"key": "value"}, "", logger.Default, "", structPayload)
	inputsEntry := newEntry("istio_log", map[string]interface{}{"key": "value"}, "", logger.Default, "val", nil)
	timeEntry := newEntry("istio_log", map[string]interface{}{"attr": "val"}, "2011-Aug-14", logger.Default, "", nil)

	executeShouldSucceed := []executeTestCase{
		{"no descriptors", noDescriptorExec, &testBag{}, &testEvaluator{}, nil},
		{"no payload", noPayloadExec, &testBag{strs: map[string]string{"key": "value"}}, &testEvaluator{}, []logger.Entry{defaultEntry}},
		{"severity", noPayloadExec, &testBag{strs: map[string]string{"key": "value", "severity": "info"}}, &testEvaluator{}, []logger.Entry{infoEntry}},
		{"bad severity", noPayloadExec, &testBag{strs: map[string]string{"key": "value", "severity": "500"}}, &testEvaluator{}, []logger.Entry{defaultEntry}},
		{"payload not found", payloadExec, &testBag{strs: map[string]string{"key": "value"}}, &testEvaluator{}, []logger.Entry{defaultEntry}},
		{"with payload", payloadExec, &testBag{strs: map[string]string{"key": "value", "payload": "test"}}, &testEvaluator{}, []logger.Entry{textPayloadEntry}},
		{"with json payload", jsonPayloadExec, jsonBag, &testEvaluator{}, []logger.Entry{jsonPayloadEntry}},
		{"with payload from inputs", withInputsExec, &testBag{strs: map[string]string{"key": "value"}}, &testEvaluator{}, []logger.Entry{inputsEntry}},
		{"with non-default time", payloadExec, &testBag{times: map[string]time.Time{"ts": time.Now()}}, &testEvaluator{}, []logger.Entry{defaultEntry}},
		{"with non-default time and format", withTimestampFormatExec, tsBag, &testEvaluator{}, []logger.Entry{timeEntry}},
		{"with inputs", withInputsExec, &testBag{strs: map[string]string{"key": "value", "payload": "test"}}, &testEvaluator{}, []logger.Entry{inputsEntry}},
	}

	for _, v := range executeShouldSucceed {
		l := &testLogger{}
		v.exec.aspect = l

		if _, err := v.exec.Execute(v.bag, v.mapper); err != nil {
			t.Errorf("Execute(): should not have received error for %s (%v)", v.name, err)
		}
		if l.entryCount != len(v.wantEntries) {
			t.Errorf("Execute(): got %d entries, wanted %d for %s", l.entryCount, len(v.wantEntries), v.name)
		}
		if !reflect.DeepEqual(l.entries, v.wantEntries) {
			t.Errorf("Execute(): got %v, wanted %v for %s", l.entries, v.wantEntries, v.name)
		}
	}
}

func TestExecutor_ExecuteFailures(t *testing.T) {

	desc := dpb.LogEntryDescriptor{
		Name:       "test",
		Attributes: []string{"key"},
	}
	jsonPayloadDesc := dpb.LogEntryDescriptor{
		Name:             "test",
		Attributes:       []string{"key"},
		PayloadAttribute: "payload",
		PayloadFormat:    dpb.LogEntryDescriptor_JSON,
	}

	errorExec := &executor{"istio_log", []dpb.LogEntryDescriptor{desc}, map[string]string{}, "severity", "ts", "", &testLogger{errOnLog: true}, time.Now}
	jsonErrorExec := &executor{"istio_log", []dpb.LogEntryDescriptor{jsonPayloadDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}

	jsonPayloadBag := &testBag{strs: map[string]string{"key": "value", "payload": `{"obj":{"val":54},"question":`}}

	executeShouldFail := []executeTestCase{
		{"log failure", errorExec, &testBag{strs: map[string]string{"key": "value"}}, &testEvaluator{}, []logger.Entry{}},
		{"json payload failure", jsonErrorExec, jsonPayloadBag, &testEvaluator{}, []logger.Entry{}},
	}

	for _, v := range executeShouldFail {
		if _, err := v.exec.Execute(v.bag, v.mapper); err == nil {
			t.Errorf("Execute(): should have received error for %s", v.name)
		}
	}
}

func TestManager_DefaultConfig(t *testing.T) {
	m := NewManager()
	got := m.DefaultConfig()
	want := &config.Params{LogName: "istio_log", TimestampFormat: time.RFC3339}
	if !proto.Equal(got, want) {
		t.Errorf("DefaultConfig(): got %v, wanted %v", got, want)
	}
}

func TestManager_ValidateConfig(t *testing.T) {
	m := NewManager()
	if err := m.ValidateConfig(&empty.Empty{}); err != nil {
		t.Errorf("ValidateConfig(): unexpected error: %v", err)
	}
}

type (
	structMap      map[string]*structpb.Value
	aspectTestCase struct {
		name       string
		defaultCfg aspect.Config
		params     *structpb.Struct
		want       *executor
	}
	executeTestCase struct {
		name        string
		exec        *executor
		bag         attribute.Bag
		mapper      expr.Evaluator
		wantEntries []logger.Entry
	}
	testLogger struct {
		logger.Adapter
		logger.Aspect

		defaultCfg     aspect.Config
		entryCount     int
		entries        []logger.Entry
		errOnNewAspect bool
		errOnLog       bool
	}
	testEvaluator struct {
		expr.Evaluator
	}
	testBag struct {
		attribute.Bag

		strs  map[string]string
		times map[string]time.Time
	}
	testEnv struct {
		aspect.Env
	}
)

func (t *testLogger) NewAspect(e aspect.Env, m aspect.Config) (logger.Aspect, error) {
	if t.errOnNewAspect {
		return nil, errors.New("new aspect error")
	}
	return t, nil
}
func (t *testLogger) DefaultConfig() aspect.Config { return t.defaultCfg }
func (t *testLogger) Log(l []logger.Entry) error {
	if t.errOnLog {
		return errors.New("log error")
	}
	t.entryCount++
	t.entries = append(t.entries, l...)
	return nil
}
func (t *testLogger) Close() error { return nil }

func (t *testEvaluator) Eval(e string, bag attribute.Bag) (interface{}, error) {
	return e, nil
}

func (t *testBag) String(name string) (string, bool) {
	v, found := t.strs[name]
	return v, found
}

func (t *testBag) Time(name string) (time.Time, bool) {
	v, found := t.times[name]
	return v, found
}

func (t *testBag) Int64(name string) (int64, bool)     { return 0, false }
func (t *testBag) Float64(name string) (float64, bool) { return 0, false }
func (t *testBag) Bool(name string) (bool, bool)       { return false, false }
func (t *testBag) Bytes(name string) ([]byte, bool)    { return []byte{}, false }

func newStringVal(s string) *structpb.Value {
	return &structpb.Value{Kind: &structpb.Value_StringValue{StringValue: s}}
}

func newStruct(fields map[string]*structpb.Value) *structpb.Struct {
	return &structpb.Struct{Fields: fields}
}

func newEntry(n string, l map[string]interface{}, ts string, s logger.Severity, tp string, sp map[string]interface{}) logger.Entry {
	return logger.Entry{
		LogName:       n,
		Labels:        l,
		Timestamp:     ts,
		Severity:      s,
		TextPayload:   tp,
		StructPayload: sp,
	}
}
