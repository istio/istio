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
	"text/template"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/struct"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	configpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

func TestNewAccessLoggerManager(t *testing.T) {
	m := NewAccessLoggerManager()
	if m.Kind() != "istio/accessLogger" {
		t.Errorf("Wrong kind of adapter; got %s, want %s", m.Kind(), "istio/accessLogger")
	}
}

func TestAccessLoggerManager_NewAspect(t *testing.T) {
	tl := &test.Logger{}

	dc := accessLoggerManager{}.DefaultConfig()
	commonExec := &accessLoggerWrapper{
		logName:   "access_log",
		aspect:    tl,
		inputs:    map[string]string{},
		attrNames: commonLogAttributes,
	}

	combinedExec := &accessLoggerWrapper{
		logName:   "combined_access_log",
		aspect:    tl,
		inputs:    map[string]string{},
		attrNames: combinedLogAttributes,
	}

	customExec := &accessLoggerWrapper{
		logName: "custom_access_log",
		aspect:  tl,
		inputs:  map[string]string{},
		// TODO: cannot test overriding of attributes at the moment.
		//attrNames: []string{"test", "other"},
	}

	combinedStruct := &aconfig.AccessLoggerParams{
		LogName:   "combined_access_log",
		LogFormat: aconfig.AccessLoggerParams_COMBINED,
	}

	customStruct := &aconfig.AccessLoggerParams{
		LogName:           "custom_access_log",
		LogFormat:         aconfig.AccessLoggerParams_CUSTOM,
		CustomLogTemplate: "{{.test}}",
	}

	newAspectShouldSucceed := []struct {
		name       string
		defaultCfg adapter.AspectConfig
		params     interface{}
		want       *accessLoggerWrapper
	}{
		{"empty", &aconfig.AccessLoggerParams{}, dc, commonExec},
		{"combined", &aconfig.AccessLoggerParams{}, combinedStruct, combinedExec},
		{"custom", &aconfig.AccessLoggerParams{}, customStruct, customExec},
	}

	m := NewAccessLoggerManager()

	for _, v := range newAspectShouldSucceed {
		c := config.Combined{
			Builder: &configpb.Adapter{Params: &empty.Empty{}},
			Aspect:  &configpb.Aspect{Params: v.params, Inputs: map[string]string{}},
		}
		asp, err := m.NewAspect(&c, tl, test.Env{})
		if err != nil {
			t.Errorf("NewAspect(): should not have received error for %s (%v)", v.name, err)
		}
		got := asp.(*accessLoggerWrapper)
		got.template = nil // ignore template values in equality comp
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewAspect() => [%s]\ngot: %v (%T)\nwant: %v (%T)", v.name, got, got, v.want, v.want)
		}
	}
}

func TestAccessLoggerManager_NewAspectFailures(t *testing.T) {
	defaultCfg := &config.Combined{
		Builder: &configpb.Adapter{Params: &empty.Empty{}},
		Aspect:  &configpb.Aspect{Params: &aconfig.AccessLoggerParams{}},
	}

	badTemplate := "{{{{}}"
	badTemplateCfg := &config.Combined{
		Builder: &configpb.Adapter{Params: &empty.Empty{}},
		Aspect: &configpb.Aspect{Params: &aconfig.AccessLoggerParams{
			LogName:           "custom_access_log",
			LogFormat:         aconfig.AccessLoggerParams_CUSTOM,
			CustomLogTemplate: badTemplate,
		}},
	}

	errLogger := &test.Logger{DefaultCfg: &structpb.Struct{}, ErrOnNewAspect: true}
	okLogger := &test.Logger{DefaultCfg: &structpb.Struct{}}

	failureCases := []struct {
		name  string
		cfg   *config.Combined
		adptr adapter.Builder
	}{
		{"errorLogger", defaultCfg, errLogger},
		{"badTemplateCfg", badTemplateCfg, okLogger},
	}

	m := NewAccessLoggerManager()
	for _, v := range failureCases {
		if _, err := m.NewAspect(v.cfg, v.adptr, test.Env{}); err == nil {
			t.Errorf("NewAspect()[%s]: expected error for bad adapter (%T)", v.name, v.adptr)
		}
	}
}

func TestAccessLoggerManager_ValidateConfig(t *testing.T) {
	configs := []adapter.AspectConfig{
		&aconfig.AccessLoggerParams{},
		&aconfig.AccessLoggerParams{LogName: "test"},
		&aconfig.AccessLoggerParams{LogName: "test", Attributes: []string{"test", "good"}},
		&aconfig.AccessLoggerParams{LogFormat: aconfig.AccessLoggerParams_COMBINED},
		&aconfig.AccessLoggerParams{LogFormat: aconfig.AccessLoggerParams_CUSTOM, CustomLogTemplate: "{{.test}}"},
	}

	m := NewAccessLoggerManager()
	for _, v := range configs {
		if err := m.ValidateConfig(v); err != nil {
			t.Errorf("ValidateConfig(%v) => unexpected error: %v", v, err)
		}
	}
}

func TestAccessLoggerManager_ValidateConfigFailures(t *testing.T) {
	configs := []adapter.AspectConfig{
		&aconfig.AccessLoggerParams{LogFormat: aconfig.AccessLoggerParams_CUSTOM, CustomLogTemplate: "{{.test"},
	}

	m := NewAccessLoggerManager()
	for _, v := range configs {
		if err := m.ValidateConfig(v); err == nil {
			t.Errorf("ValidateConfig(%v): expected error", v)
		}
	}
}

func TestAccessLoggerWrapper_Execute(t *testing.T) {
	tmpl, _ := template.New("test").Parse("{{.test}}")

	commonExec := &accessLoggerWrapper{
		logName:   "access_log",
		inputs:    map[string]string{},
		attrNames: commonLogAttributes,
		template:  tmpl,
	}

	commonExecWithInputs := &accessLoggerWrapper{
		logName: "access_log",
		inputs: map[string]string{
			"test":      "testExpr",
			"source_ip": "127.0.0.1",
		},
		attrNames: commonLogAttributes,
		template:  tmpl,
	}

	emptyEntry := adapter.AccessLogEntry{"access_log", "<no value>", map[string]interface{}{}}
	sourceEntry := adapter.AccessLogEntry{"access_log", "<no value>", map[string]interface{}{"source_ip": "127.0.0.1"}}

	tests := []struct {
		name        string
		exec        *accessLoggerWrapper
		bag         attribute.Bag
		mapper      expr.Evaluator
		wantEntries []adapter.AccessLogEntry
	}{
		{"empty bag with defaults", commonExec, &test.Bag{}, &test.Evaluator{}, []adapter.AccessLogEntry{emptyEntry}},
		{"attrs in bag", commonExec, &test.Bag{Strs: map[string]string{"source_ip": "127.0.0.1"}}, &test.Evaluator{}, []adapter.AccessLogEntry{sourceEntry}},
		{"attrs from inputs", commonExecWithInputs, &test.Bag{}, &test.Evaluator{}, []adapter.AccessLogEntry{sourceEntry}},
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
		if !reflect.DeepEqual(l.AccessLogs, v.wantEntries) {
			t.Errorf("Execute(): got %v, wanted %v for %s", l.AccessLogs, v.wantEntries, v.name)
		}
	}
}

func TestAccessLoggerWrapper_ExecuteFailures(t *testing.T) {
	tmpl, _ := template.New("test").Parse("{{.test}}")

	logErrExec := &accessLoggerWrapper{
		logName:   "access_log",
		inputs:    map[string]string{},
		attrNames: commonLogAttributes,
		template:  tmpl,
		aspect:    &test.Logger{ErrOnLog: true},
	}

	tests := []struct {
		name   string
		exec   *accessLoggerWrapper
		bag    attribute.Bag
		mapper expr.Evaluator
	}{
		{"LogAccess() error", logErrExec, &test.Bag{}, &test.Evaluator{}},
	}

	for _, v := range tests {
		if _, err := v.exec.Execute(v.bag, v.mapper); err == nil {
			t.Errorf("Execute(): expected error for %s", v.name)
		}
	}
}
