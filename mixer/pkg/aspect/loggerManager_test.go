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

package aspect

import (
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/struct"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"

	configpb "istio.io/api/mixer/v1/config"
	dpb "istio.io/api/mixer/v1/config/descriptor"
)

func TestNewLoggerManager(t *testing.T) {
	m := NewLoggerManager()
	if m.Kind() != "istio/logger" {
		t.Error("Wrong kind of adapter")
	}
}

func TestLoggerManager_NewLogger(t *testing.T) {
	tl := &test.Logger{}

	defaultExec := &loggerWrapper{
		logName:      "istio_log",
		timestampFmt: time.RFC3339,
		aspect:       tl,
		inputs:       map[string]string{},
		descriptors:  []dpb.LogEntryDescriptor{},
	}

	overrideExec := &loggerWrapper{
		logName:      "istio_log",
		timestampFmt: "2006-Jan-02",
		aspect:       tl,
		inputs:       map[string]string{},
		descriptors:  []dpb.LogEntryDescriptor{},
	}

	overrideStruct := test.NewStruct(test.StructMap{"timestamp_format": test.NewStringVal("2006-Jan-02")})

	newAspectShouldSucceed := []struct {
		name       string
		defaultCfg adapter.AspectConfig
		params     *structpb.Struct
		want       *loggerWrapper
	}{
		{"empty", &config.LoggerParams{}, test.NewStruct(nil), defaultExec},
		{"nil", &config.LoggerParams{}, nil, defaultExec},
		{"override", &config.LoggerParams{}, overrideStruct, overrideExec},
	}

	m := NewLoggerManager()

	for _, v := range newAspectShouldSucceed {
		c := CombinedConfig{
			Builder: &configpb.Adapter{},
			Aspect:  &configpb.Aspect{Params: v.params, Inputs: map[string]string{}},
		}
		asp, err := m.NewAspect(&c, tl, test.Env{})
		if err != nil {
			t.Errorf("NewAspect(): should not have received error for %s (%v)", v.name, err)
		}
		got := asp.(*loggerWrapper)
		got.defaultTimeFn = nil // ignore time fns in equality comp
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewAspect() => %v (%T), want %v (%T)", got, got, v.want, v.want)
		}
	}
}

func TestLoggerManager_NewLoggerFailures(t *testing.T) {

	defaultCfg := &CombinedConfig{
		Builder: &configpb.Adapter{},
		Aspect:  &configpb.Aspect{},
	}

	var generic adapter.Builder
	errLogger := &test.Logger{DefaultCfg: &structpb.Struct{}, ErrOnNewAspect: true}

	failureCases := []struct {
		cfg   *CombinedConfig
		adptr adapter.Builder
	}{
		{defaultCfg, generic},
		{defaultCfg, errLogger},
	}

	m := NewLoggerManager()
	for _, v := range failureCases {
		if _, err := m.NewAspect(v.cfg, v.adptr, test.Env{}); err == nil {
			t.Errorf("NewAspect(): expected error for bad adapter (%T)", v.adptr)
		}
	}
}

func TestLoggerManager_Execute(t *testing.T) {
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

	noDescriptorExec := &loggerWrapper{"istio_log", []dpb.LogEntryDescriptor{}, map[string]string{}, "severity", "ts", "", nil, time.Now}
	noPayloadExec := &loggerWrapper{"istio_log", []dpb.LogEntryDescriptor{noPayloadDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	payloadExec := &loggerWrapper{"istio_log", []dpb.LogEntryDescriptor{payloadDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	jsonPayloadExec := &loggerWrapper{"istio_log", []dpb.LogEntryDescriptor{jsonPayloadDesc},
		map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	withInputsExec := &loggerWrapper{"istio_log", []dpb.LogEntryDescriptor{withInputsDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}
	withTimestampFormatExec := &loggerWrapper{
		"istio_log",
		[]dpb.LogEntryDescriptor{noPayloadDesc},
		map[string]string{"attr": "val"},
		"severity",
		"ts",
		"2006-Jan-02",
		nil,
		time.Now,
	}

	jsonBag := &test.Bag{Strs: map[string]string{"key": "value", "payload": `{"obj":{"val":54},"question":true}`}}
	tsBag := &test.Bag{Times: map[string]time.Time{"ts": testTime}}

	structPayload := map[string]interface{}{"obj": map[string]interface{}{"val": float64(54)}, "question": true}

	defaultEntry := test.NewLogEntry("istio_log", map[string]interface{}{"attr": "val"}, "", adapter.Default, "", nil)
	infoEntry := test.NewLogEntry("istio_log", map[string]interface{}{"attr": "val"}, "", adapter.Info, "", nil)
	textPayloadEntry := test.NewLogEntry("istio_log", map[string]interface{}{"attr": "val"}, "", adapter.Default, "test", nil)
	jsonPayloadEntry := test.NewLogEntry("istio_log", map[string]interface{}{"key": "value"}, "", adapter.Default, "", structPayload)
	inputsEntry := test.NewLogEntry("istio_log", map[string]interface{}{"key": "value"}, "", adapter.Default, "val", nil)
	timeEntry := test.NewLogEntry("istio_log", map[string]interface{}{"attr": "val"}, "2011-Aug-14", adapter.Default, "", nil)

	executeShouldSucceed := []struct {
		name        string
		exec        *loggerWrapper
		bag         attribute.Bag
		mapper      expr.Evaluator
		wantEntries []adapter.LogEntry
	}{
		{"no descriptors", noDescriptorExec, &test.Bag{}, &test.Evaluator{}, nil},
		{"no payload", noPayloadExec, &test.Bag{Strs: map[string]string{"key": "value"}}, &test.Evaluator{}, []adapter.LogEntry{defaultEntry}},
		{"severity", noPayloadExec, &test.Bag{Strs: map[string]string{"key": "value", "severity": "info"}}, &test.Evaluator{}, []adapter.LogEntry{infoEntry}},
		{"bad severity", noPayloadExec, &test.Bag{Strs: map[string]string{"key": "value", "severity": "500"}}, &test.Evaluator{}, []adapter.LogEntry{defaultEntry}},
		{"payload not found", payloadExec, &test.Bag{Strs: map[string]string{"key": "value"}}, &test.Evaluator{}, []adapter.LogEntry{defaultEntry}},
		{"with payload", payloadExec, &test.Bag{Strs: map[string]string{"key": "value", "payload": "test"}}, &test.Evaluator{}, []adapter.LogEntry{textPayloadEntry}},
		{"with json payload", jsonPayloadExec, jsonBag, &test.Evaluator{}, []adapter.LogEntry{jsonPayloadEntry}},
		{"with payload from inputs", withInputsExec, &test.Bag{Strs: map[string]string{"key": "value"}}, &test.Evaluator{}, []adapter.LogEntry{inputsEntry}},
		{"with non-default time", payloadExec, &test.Bag{Times: map[string]time.Time{"ts": time.Now()}}, &test.Evaluator{}, []adapter.LogEntry{defaultEntry}},
		{"with non-default time and format", withTimestampFormatExec, tsBag, &test.Evaluator{}, []adapter.LogEntry{timeEntry}},
		{"with inputs", withInputsExec, &test.Bag{Strs: map[string]string{"key": "value", "payload": "test"}}, &test.Evaluator{}, []adapter.LogEntry{inputsEntry}},
	}

	for _, v := range executeShouldSucceed {
		l := &test.Logger{}
		v.exec.aspect = l

		if _, err := v.exec.Execute(v.bag, v.mapper); err != nil {
			t.Errorf("Execute(): should not have received error for %s (%v)", v.name, err)
		}
		if l.EntryCount != len(v.wantEntries) {
			t.Errorf("Execute(): got %d entries, wanted %d for %s", l.EntryCount, len(v.wantEntries), v.name)
		}
		if !reflect.DeepEqual(l.Logs, v.wantEntries) {
			t.Errorf("Execute(): got %v, wanted %v for %s", l.Logs, v.wantEntries, v.name)
		}
	}
}

func TestLoggerManager_ExecuteFailures(t *testing.T) {

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

	errorExec := &loggerWrapper{"istio_log", []dpb.LogEntryDescriptor{desc}, map[string]string{}, "severity", "ts", "", &test.Logger{ErrOnLog: true}, time.Now}
	jsonErrorExec := &loggerWrapper{"istio_log", []dpb.LogEntryDescriptor{jsonPayloadDesc}, map[string]string{"attr": "val"}, "severity", "ts", "", nil, time.Now}

	jsonPayloadBag := &test.Bag{Strs: map[string]string{"key": "value", "payload": `{"obj":{"val":54},"question":`}}

	executeShouldFail := []struct {
		name   string
		exec   *loggerWrapper
		bag    attribute.Bag
		mapper expr.Evaluator
	}{
		{"log failure", errorExec, &test.Bag{Strs: map[string]string{"key": "value"}}, &test.Evaluator{}},
		{"json payload failure", jsonErrorExec, jsonPayloadBag, &test.Evaluator{}},
	}

	for _, v := range executeShouldFail {
		if _, err := v.exec.Execute(v.bag, v.mapper); err == nil {
			t.Errorf("Execute(): should have received error for %s", v.name)
		}
	}
}

func TestLoggerManager_DefaultConfig(t *testing.T) {
	m := NewLoggerManager()
	got := m.DefaultConfig()
	want := &config.LoggerParams{LogName: "istio_log", TimestampFormat: time.RFC3339}
	if !proto.Equal(got, want) {
		t.Errorf("DefaultConfig(): got %v, wanted %v", got, want)
	}
}

func TestLoggerManager_ValidateConfig(t *testing.T) {
	m := NewLoggerManager()
	if err := m.ValidateConfig(&empty.Empty{}); err != nil {
		t.Errorf("ValidateConfig(): unexpected error: %v", err)
	}
}
