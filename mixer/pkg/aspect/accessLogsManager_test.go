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
	"errors"
	"fmt"
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
	m := newAccessLogsManager()
	if m.Kind() != AccessLogsKind {
		t.Fatalf("Wrong kind of adapter; got %v, want %v", m.Kind(), AccessLogsKind)
	}
}

func TestAccessLoggerManager_NewAspect(t *testing.T) {
	tl := &test.Logger{}

	dc := accessLogsManager{}.DefaultConfig()
	commonExec := &accessLogsWrapper{
		name:   "access_log",
		aspect: tl,
	}

	combinedExec := &accessLogsWrapper{
		name:   "combined_access_log",
		aspect: tl,
	}

	// TODO: add back tests for custom when we introduce descriptors
	//customExec := &accessLogsWrapper{
	//	name:   "custom_access_log",
	//	aspect: tl,
	//	labels: map[string]string{"test": "test"},
	//}

	combinedStruct := &aconfig.AccessLogsParams{
		LogName: "combined_access_log",
		Log: &aconfig.AccessLogsParams_AccessLog{
			LogFormat: aconfig.COMBINED,
		},
	}

	//customStruct := &aconfig.AccessLogsParams{
	//	LogName: "custom_access_log",
	//	Log: &aconfig.AccessLogsParams_AccessLog{
	//		LogFormat: aconfig.CUSTOM,
	//		Labels:    map[string]string{"test": "test"},
	//	},
	//}

	newAspectShouldSucceed := []struct {
		name   string
		params interface{}
		want   *accessLogsWrapper
	}{
		{"empty", dc, commonExec},
		{"combined", combinedStruct, combinedExec},
		//{"custom", customStruct, customExec},
	}

	m := newAccessLogsManager()

	for idx, v := range newAspectShouldSucceed {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			c := &configpb.Combined{
				Builder: &configpb.Adapter{Params: &ptypes.Empty{}},
				Aspect:  &configpb.Aspect{Params: v.params, Inputs: map[string]string{"template": "{{.test}}"}},
			}
			asp, err := m.NewAspect(c, tl, test.Env{})
			if err != nil {
				t.Fatalf("NewAspect(): should not have received error for %s (%v)", v.name, err)
			}
			got := asp.(*accessLogsWrapper)
			got.template = nil // ignore template values in equality comp
			if !reflect.DeepEqual(got, v.want) {
				t.Fatalf("NewAspect() => [%s]\ngot: %v (%T)\nwant: %v (%T)", v.name, got, got, v.want, v.want)
			}
		})
	}
}

func TestAccessLoggerManager_NewAspectFailures(t *testing.T) {
	defaultCfg := &configpb.Combined{
		Builder: &configpb.Adapter{Params: &ptypes.Empty{}},
		Aspect: &configpb.Aspect{Params: &aconfig.AccessLogsParams{
			Log: &aconfig.AccessLogsParams_AccessLog{
				LogFormat: aconfig.COMMON,
			},
		}},
	}

	// TODO: add back tests for bad templates when we introduce descriptors.
	//badTemplateCfg := &config.Combined{
	//	Builder: &configpb.Adapter{Params: &ptypes.Empty{}},
	//	Aspect: &configpb.Aspect{Params: &aconfig.AccessLogsParams{
	//		LogName: "custom_access_log",
	//		Log: &aconfig.AccessLogsParams_AccessLog{
	//			LogFormat: aconfig.CUSTOM,
	//		},
	//	}, Inputs: map[string]string{"template": "{{{}}"}},
	//}

	errLogger := &test.Logger{DefaultCfg: &ptypes.Struct{}, ErrOnNewAspect: true}
	//okLogger := &test.Logger{DefaultCfg: &ptypes.Struct{}}

	failureCases := []struct {
		name  string
		cfg   *configpb.Combined
		adptr adapter.Builder
	}{
		{"errorLogger", defaultCfg, errLogger},
		//{"badTemplateCfg", badTemplateCfg, okLogger},
	}

	m := newAccessLogsManager()
	for idx, v := range failureCases {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			if _, err := m.NewAspect(v.cfg, v.adptr, test.Env{}); err == nil {
				t.Fatalf("NewAspect()[%s]: expected error for bad adapter (%T)", v.name, v.adptr)
			}
		})
	}
}

func TestAccessLoggerManager_ValidateConfig(t *testing.T) {
	configs := []config.AspectParams{
		&aconfig.AccessLogsParams{
			LogName: "test",
			Log: &aconfig.AccessLogsParams_AccessLog{
				Labels:    map[string]string{"test": "good"},
				LogFormat: aconfig.COMMON,
			},
		},
		&aconfig.AccessLogsParams{Log: &aconfig.AccessLogsParams_AccessLog{LogFormat: aconfig.COMBINED}},
	}

	m := newAccessLogsManager()
	for idx, v := range configs {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			if err := m.ValidateConfig(v); err != nil {
				t.Fatalf("ValidateConfig(%v) => unexpected error: %v", v, err)
			}
		})
	}
}

func TestAccessLoggerManager_ValidateConfigFailures(t *testing.T) {
	configs := []config.AspectParams{
		&aconfig.AccessLogsParams{},
		&aconfig.AccessLogsParams{Log: &aconfig.AccessLogsParams_AccessLog{LogFormat: aconfig.ACCESS_LOG_FORMAT_UNSPECIFIED}},
	}

	m := newAccessLogsManager()
	for idx, v := range configs {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			if err := m.ValidateConfig(v); err == nil {
				t.Fatalf("ValidateConfig(%v) expected err", v)
			}
		})
	}
}

func TestAccessLoggerWrapper_Execute(t *testing.T) {
	tmpl, _ := template.New("test").Parse("{{.test}}")

	noLabels := &accessLogsWrapper{
		name:     "access_log",
		labels:   map[string]string{},
		template: tmpl,
	}

	labelsInBag := &accessLogsWrapper{
		name: "access_log",
		labels: map[string]string{
			"test": "foo",
		},
		template: tmpl,
	}

	emptyEntry := adapter.LogEntry{LogName: "access_log", TextPayload: "<no value>", Labels: map[string]interface{}{}}
	sourceEntry := adapter.LogEntry{LogName: "access_log", TextPayload: "<no value>", Labels: map[string]interface{}{"test": "foo"}}

	tests := []struct {
		name        string
		exec        *accessLogsWrapper
		bag         attribute.Bag
		mapper      expr.Evaluator
		wantEntries []adapter.LogEntry
	}{
		{"empty bag with defaults", noLabels, test.NewBag(), test.NewIDEval(), []adapter.LogEntry{emptyEntry}},
		{"attrs in bag", labelsInBag, &test.Bag{Strs: map[string]string{"foo": ""}}, test.NewIDEval(), []adapter.LogEntry{sourceEntry}},
	}

	for idx, v := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			l := &test.Logger{}
			v.exec.aspect = l

			if status := v.exec.Execute(v.bag, v.mapper, &ReportMethodArgs{}); !status.IsOK() {
				t.Fatalf("Execute(): should not have received error for %s (%v)", v.name, status)
			}
			if l.EntryCount != len(v.wantEntries) {
				t.Fatalf("Execute(): got %d entries, wanted %d for %s", l.EntryCount, len(v.wantEntries), v.name)
			}

			// don't compare timestamps here (not important to test)
			for _, e := range l.AccessLogs {
				delete(e.Labels, "timestamp")
			}

			if !reflect.DeepEqual(l.AccessLogs, v.wantEntries) {
				t.Fatalf("Execute(): got %v, wanted %v for %s", l.AccessLogs, v.wantEntries, v.name)
			}
		})
	}
}

func TestAccessLoggerWrapper_ExecuteFailures(t *testing.T) {
	timeTmpl, _ := template.New("test").Parse(`{{(.timestamp.Format "02/Jan/2006:15:04:05 -0700")}}`)
	errEval := test.NewFakeEval(func(string, attribute.Bag) (interface{}, error) {
		return nil, errors.New("expected")
	})

	executeErr := &accessLogsWrapper{
		name: "access_log",
		templateExprs: map[string]string{
			"timestamp": "foo",
		},
		template: timeTmpl,
	}

	logErr := &accessLogsWrapper{
		name:     "access_log",
		aspect:   &test.Logger{ErrOnLog: true},
		labels:   map[string]string{},
		template: timeTmpl,
	}

	tests := []struct {
		name   string
		exec   *accessLogsWrapper
		bag    attribute.Bag
		mapper expr.Evaluator
	}{
		{"template.Execute() error", executeErr, test.NewBag(), test.NewIDEval()},
		{"evalAll() error", executeErr, test.NewBag(), errEval},
		{"LogAccess() error", logErr, test.NewBag(), test.NewIDEval()},
	}

	for idx, v := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			if status := v.exec.Execute(v.bag, v.mapper, &ReportMethodArgs{}); status.IsOK() {
				t.Fatalf("Execute(): expected error for %s", v.name)
			}
		})
	}
}

func TestAccessLoggerWrapper_Close(t *testing.T) {
	aw := &accessLogsWrapper{
		aspect: &test.Logger{ErrOnLog: true},
	}
	if err := aw.Close(); err != nil {
		t.Fatalf("Close() should not return error: got %v", err)
	}
}
