// Copyright 2017 the Istio Authors.
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
	"text/template"

	ptypes "github.com/gogo/protobuf/types"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	configpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

func TestNewAccessLoggerManager(t *testing.T) {
	m := NewAccessLogsManager()
	if m.Kind() != AccessLogsKind {
		t.Errorf("Wrong kind of adapter; got %v, want %v", m.Kind(), AccessLogsKind)
	}
}

func TestAccessLoggerManager_NewAspect(t *testing.T) {
	tl := &test.Logger{}

	dc := accessLogsManager{}.DefaultConfig()
	commonExec := &accessLogsWrapper{
		logName:   "access_log",
		aspect:    tl,
		inputs:    map[string]string{},
		attrNames: commonLogAttributes,
	}

	combinedExec := &accessLogsWrapper{
		logName:   "combined_access_log",
		aspect:    tl,
		inputs:    map[string]string{},
		attrNames: combinedLogAttributes,
	}

	customExec := &accessLogsWrapper{
		logName: "custom_access_log",
		aspect:  tl,
		inputs:  map[string]string{},
		// TODO: cannot test overriding of attributes at the moment.
		//attrNames: []string{"test", "other"},
	}

	combinedStruct := &aconfig.AccessLogsParams{
		LogName:   "combined_access_log",
		LogFormat: aconfig.COMBINED,
	}

	customStruct := &aconfig.AccessLogsParams{
		LogName:           "custom_access_log",
		LogFormat:         aconfig.CUSTOM,
		CustomLogTemplate: "{{.test}}",
	}

	newAspectShouldSucceed := []struct {
		name       string
		defaultCfg adapter.AspectConfig
		params     interface{}
		want       *accessLogsWrapper
	}{
		{"empty", &aconfig.AccessLogsParams{}, dc, commonExec},
		{"combined", &aconfig.AccessLogsParams{}, combinedStruct, combinedExec},
		{"custom", &aconfig.AccessLogsParams{}, customStruct, customExec},
	}

	m := NewAccessLogsManager()

	for _, v := range newAspectShouldSucceed {
		c := config.Combined{
			Builder: &configpb.Adapter{Params: &ptypes.Empty{}},
			Aspect:  &configpb.Aspect{Params: v.params, Inputs: map[string]string{}},
		}
		asp, err := m.NewAspect(&c, tl, test.Env{})
		if err != nil {
			t.Errorf("NewAspect(): should not have received error for %s (%v)", v.name, err)
		}
		got := asp.(*accessLogsWrapper)
		got.template = nil // ignore template values in equality comp
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewAspect() => [%s]\ngot: %v (%T)\nwant: %v (%T)", v.name, got, got, v.want, v.want)
		}
	}
}

func TestAccessLoggerManager_NewAspectFailures(t *testing.T) {
	defaultCfg := &config.Combined{
		Builder: &configpb.Adapter{Params: &ptypes.Empty{}},
		Aspect:  &configpb.Aspect{Params: &aconfig.AccessLogsParams{}},
	}

	badTemplate := "{{{{}}"
	badTemplateCfg := &config.Combined{
		Builder: &configpb.Adapter{Params: &ptypes.Empty{}},
		Aspect: &configpb.Aspect{Params: &aconfig.AccessLogsParams{
			LogName:           "custom_access_log",
			LogFormat:         aconfig.CUSTOM,
			CustomLogTemplate: badTemplate,
		}},
	}

	errLogger := &test.Logger{DefaultCfg: &ptypes.Struct{}, ErrOnNewAspect: true}
	okLogger := &test.Logger{DefaultCfg: &ptypes.Struct{}}

	failureCases := []struct {
		name  string
		cfg   *config.Combined
		adptr adapter.Builder
	}{
		{"errorLogger", defaultCfg, errLogger},
		{"badTemplateCfg", badTemplateCfg, okLogger},
	}

	m := NewAccessLogsManager()
	for _, v := range failureCases {
		if _, err := m.NewAspect(v.cfg, v.adptr, test.Env{}); err == nil {
			t.Errorf("NewAspect()[%s]: expected error for bad adapter (%T)", v.name, v.adptr)
		}
	}
}

func TestAccessLoggerManager_ValidateConfig(t *testing.T) {
	configs := []adapter.AspectConfig{
		&aconfig.AccessLogsParams{},
		&aconfig.AccessLogsParams{LogName: "test"},
		&aconfig.AccessLogsParams{LogName: "test", Attributes: []string{"test", "good"}},
		&aconfig.AccessLogsParams{LogFormat: aconfig.COMBINED},
		&aconfig.AccessLogsParams{LogFormat: aconfig.CUSTOM, CustomLogTemplate: "{{.test}}"},
	}

	m := NewAccessLogsManager()
	for _, v := range configs {
		if err := m.ValidateConfig(v); err != nil {
			t.Errorf("ValidateConfig(%v) => unexpected error: %v", v, err)
		}
	}
}

func TestAccessLoggerManager_ValidateConfigFailures(t *testing.T) {
	configs := []adapter.AspectConfig{
		&aconfig.AccessLogsParams{LogFormat: aconfig.CUSTOM, CustomLogTemplate: "{{.test"},
	}

	m := NewAccessLogsManager()
	for _, v := range configs {
		if err := m.ValidateConfig(v); err == nil {
			t.Errorf("ValidateConfig(%v): expected error", v)
		}
	}
}

func TestAccessLoggerWrapper_Execute(t *testing.T) {
	tmpl, _ := template.New("test").Parse("{{.test}}")

	commonExec := &accessLogsWrapper{
		logName:   "access_log",
		inputs:    map[string]string{},
		attrNames: commonLogAttributes,
		template:  tmpl,
	}

	commonExecWithInputs := &accessLogsWrapper{
		logName: "access_log",
		inputs: map[string]string{
			"test":     "testExpr",
			"originIp": "127.0.0.1",
		},
		attrNames: commonLogAttributes,
		template:  tmpl,
	}

	customEmpty := &accessLogsWrapper{
		logName:   "empty_log",
		inputs:    map[string]string{},
		attrNames: []string{},
		template:  tmpl,
	}

	emptyEntry := adapter.LogEntry{LogName: "access_log", TextPayload: "<no value>", Labels: map[string]interface{}{}}
	sourceEntry := adapter.LogEntry{LogName: "access_log", TextPayload: "<no value>", Labels: map[string]interface{}{"originIp": "127.0.0.1"}}

	tests := []struct {
		name        string
		exec        *accessLogsWrapper
		bag         attribute.Bag
		mapper      expr.Evaluator
		wantEntries []adapter.LogEntry
	}{
		{"empty bag with defaults", commonExec, test.NewBag(), test.NewIDEval(), []adapter.LogEntry{emptyEntry}},
		{"attrs in bag", commonExec, &test.Bag{Strs: map[string]string{"originIp": "127.0.0.1"}}, test.NewIDEval(), []adapter.LogEntry{sourceEntry}},
		{"attrs from inputs", commonExecWithInputs, test.NewBag(), test.NewIDEval(), []adapter.LogEntry{sourceEntry}},
		{"custom - no attrs", customEmpty, test.NewBag(), test.NewIDEval(), nil},
	}

	for _, v := range tests {
		l := &test.Logger{}
		v.exec.aspect = l

		if _, err := v.exec.Execute(v.bag, v.mapper); err != nil {
			t.Errorf("Execute(): should not have received error for %s (%v)", v.name, err)
		}
		if l.EntryCount != len(v.wantEntries) {
			t.Errorf("Execute(): got %d entries, wanted %d for %s", l.EntryCount, len(v.wantEntries), v.name)
		}

		// don't compare timestamps here (not important to test)
		for _, e := range l.AccessLogs {
			delete(e.Labels, "timestamp")
		}

		if !reflect.DeepEqual(l.AccessLogs, v.wantEntries) {
			t.Errorf("Execute(): got %v, wanted %v for %s", l.AccessLogs, v.wantEntries, v.name)
		}
	}
}

func TestAccessLoggerWrapper_ExecuteFailures(t *testing.T) {
	tmpl, _ := template.New("test").Parse("{{.test}}")

	logErrExec := &accessLogsWrapper{
		logName:   "access_log",
		inputs:    map[string]string{},
		attrNames: commonLogAttributes,
		template:  tmpl,
		aspect:    &test.Logger{ErrOnLog: true},
	}

	tests := []struct {
		name   string
		exec   *accessLogsWrapper
		bag    attribute.Bag
		mapper expr.Evaluator
	}{
		{"LogAccess() error", logErrExec, test.NewBag(), test.NewIDEval()},
	}

	for _, v := range tests {
		if _, err := v.exec.Execute(v.bag, v.mapper); err == nil {
			t.Errorf("Execute(): expected error for %s", v.name)
		}
	}
}

func TestAccessLoggerWrapper_Close(t *testing.T) {
	aw := &accessLogsWrapper{
		aspect: &test.Logger{ErrOnLog: true},
	}
	if err := aw.Close(); err != nil {
		t.Errorf("Close() should not return error: got %v", err)
	}
}
