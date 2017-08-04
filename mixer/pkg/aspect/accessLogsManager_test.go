// Copyright 2017 Istio Authors.
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
	"strings"
	"testing"
	"text/template"

	ptypes "github.com/gogo/protobuf/types"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cfgpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

var (
	validAccessLogDesc = dpb.LogEntryDescriptor{
		Name:        "common",
		LogTemplate: "{{.foo}}",
		Labels: map[string]dpb.ValueType{
			"label": dpb.STRING,
		},
	}

	accesslogsDF = test.NewDescriptorFinder(map[string]interface{}{
		validAccessLogDesc.Name: &validAccessLogDesc,
		// our attributes
		"string": &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.STRING},
		"int64":  &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.INT64},
	})
)

func TestNewAccessLoggerManager(t *testing.T) {
	m := newAccessLogsManager()
	if m.Kind() != config.AccessLogsKind {
		t.Fatalf("Wrong kind of adapter; got %v, want %v", m.Kind(), config.AccessLogsKind)
	}
}

func TestAccessLoggerManager_NewAspect(t *testing.T) {
	tl := &test.Logger{}

	dc := accessLogsManager{}.DefaultConfig()
	commonExec := &accessLogsExecutor{
		name:   "access_log",
		aspect: tl,
	}

	combinedExec := &accessLogsExecutor{
		name:   "combined_access_log",
		aspect: tl,
	}

	combinedStruct := &aconfig.AccessLogsParams{
		LogName: "combined_access_log",
		Log: aconfig.AccessLogsParams_AccessLog{
			DescriptorName: "common",
		},
	}

	newAspectShouldSucceed := []struct {
		name   string
		params interface{}
		want   *accessLogsExecutor
	}{
		{"empty", dc, commonExec},
		{"combined", combinedStruct, combinedExec},
	}

	m := newAccessLogsManager()

	for idx, v := range newAspectShouldSucceed {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			c := &cfgpb.Combined{
				Builder: &cfgpb.Adapter{Params: &ptypes.Empty{}},
				Aspect:  &cfgpb.Aspect{Params: v.params},
			}
			f, _ := FromBuilder(tl, config.AccessLogsKind)
			asp, err := m.NewReportExecutor(c, f, test.Env{}, accesslogsDF, "")
			if err != nil {
				t.Fatalf("NewExecutor(): should not have received error for %s (%v)", v.name, err)
			}
			got := asp.(*accessLogsExecutor)
			got.template = nil // ignore template values in equality comp
			if !reflect.DeepEqual(got, v.want) {
				t.Fatalf("NewExecutor() => [%s]\ngot: %v (%T)\nwant: %v (%T)", v.name, got, got, v.want, v.want)
			}
		})
	}
}

func TestAccessLoggerManager_NewAspectFailures(t *testing.T) {
	defaultCfg := &cfgpb.Combined{
		Builder: &cfgpb.Adapter{Params: &ptypes.Empty{}},
		Aspect: &cfgpb.Aspect{Params: &aconfig.AccessLogsParams{
			Log: aconfig.AccessLogsParams_AccessLog{
				DescriptorName: "common",
			},
		}},
	}

	errLogger := &test.Logger{DefaultCfg: &ptypes.Struct{}, ErrOnNewAspect: true}

	failureCases := []struct {
		name  string
		cfg   *cfgpb.Combined
		adptr adapter.Builder
	}{
		{"errorLogger", defaultCfg, errLogger},
	}

	m := newAccessLogsManager()
	for idx, v := range failureCases {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			f, _ := FromBuilder(v.adptr, config.AccessLogsKind)
			if _, err := m.NewReportExecutor(v.cfg, f, test.Env{}, accesslogsDF, ""); err == nil {
				t.Fatalf("NewExecutor()[%s]: expected error for bad adapter (%T)", v.name, v.adptr)
			}
		})
	}
}

func TestAccessLoggerManager_ValidateConfig(t *testing.T) {
	wrap := func(name string, log aconfig.AccessLogsParams_AccessLog) *aconfig.AccessLogsParams {
		return &aconfig.AccessLogsParams{
			LogName: name,
			Log:     log,
		}
	}

	validDesc := dpb.LogEntryDescriptor{
		Name:        "logentry",
		LogTemplate: "{{.foo}}",
		Labels: map[string]dpb.ValueType{
			"label": dpb.STRING,
		},
	}
	invalidDesc := validDesc
	invalidDesc.Name = "invalid"
	invalidDesc.LogTemplate = "{{.foo"

	df := test.NewDescriptorFinder(map[string]interface{}{
		validDesc.Name:   &validDesc,
		invalidDesc.Name: &invalidDesc,
		// our attributes
		"string": &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.STRING},
		"int64":  &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.INT64},
	})

	validLog := aconfig.AccessLogsParams_AccessLog{
		DescriptorName:      validDesc.Name,
		Labels:              map[string]string{"label": "string"},
		TemplateExpressions: map[string]string{"foo": "int64"},
	}

	missingDesc := validLog
	missingDesc.DescriptorName = "not in the df"

	invalidLabels := validLog
	invalidLabels.Labels = map[string]string{
		"not a label": "string", // correct type, but doesn't match desc's label name
	}

	invalidDescLog := validLog
	invalidDescLog.DescriptorName = invalidDesc.Name

	missingTmplExprs := validLog
	missingTmplExprs.TemplateExpressions = map[string]string{"foo": "not an attribute"}

	tests := []struct {
		name string
		cfg  *aconfig.AccessLogsParams
		df   descriptor.Finder
		err  string
	}{
		{"valid", wrap("valid", validLog), df, ""},
		{"empty config", &aconfig.AccessLogsParams{}, df, "logName"},
		{"no log name", wrap("", validLog), df, "logName"}, // name is ""
		{"missing desc", wrap("missing desc", missingDesc), df, "could not find a descriptor"},
		{"invalid labels", wrap("labels", invalidLabels), df, "labels"},
		{"invalid logtemplate", wrap("tmpl", invalidDescLog), df, "logDescriptor"},
		{"template expr attr missing", wrap("missing attr", missingTmplExprs), df, "templateExpressions"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			eval, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
			if err := (&accessLogsManager{}).ValidateConfig(tt.cfg, eval, tt.df); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("Foo = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestAccessLoggerExecutor_Execute(t *testing.T) {
	tmpl, _ := template.New("test").Parse("{{.test}}")

	noLabels := &accessLogsExecutor{
		name:     "access_log",
		labels:   map[string]string{},
		template: tmpl,
	}

	labelsInBag := &accessLogsExecutor{
		name: "access_log",
		labels: map[string]string{
			"test": "foo",
		},
		template: tmpl,
	}

	errExec := &accessLogsExecutor{
		name: "access_log",
		labels: map[string]string{
			"timestamp": "foo",
			"notFound":  "missing",
		},
		template: tmpl,
		templateExprs: map[string]string{
			"timestamp": "foo",
			"notFound":  "missing",
		},
	}

	errEval := test.NewFakeEval(func(string, attribute.Bag) (interface{}, error) {
		return nil, errors.New("expected")
	})

	emptyEntry := adapter.LogEntry{LogName: "access_log", TextPayload: "<no value>", Labels: map[string]interface{}{}}
	sourceEntry := adapter.LogEntry{LogName: "access_log", TextPayload: "<no value>", Labels: map[string]interface{}{"test": "foo"}}

	tests := []struct {
		name        string
		exec        *accessLogsExecutor
		bag         attribute.Bag
		mapper      expr.Evaluator
		wantEntries []adapter.LogEntry
	}{
		{"empty bag with defaults", noLabels, test.NewBag(), test.NewIDEval(), []adapter.LogEntry{emptyEntry}},
		{"attrs in bag", labelsInBag, &test.Bag{Strs: map[string]string{"foo": ""}}, test.NewIDEval(), []adapter.LogEntry{sourceEntry}},
		{"failed evals", errExec, test.NewBag(), errEval, []adapter.LogEntry{emptyEntry}},
	}

	for idx, v := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			l := &test.Logger{}
			v.exec.aspect = l

			if out := v.exec.Execute(v.bag, v.mapper); !status.IsOK(out) {
				t.Fatalf("Execute(): should not have received error for %s (%v)", v.name, out)
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

func TestAccessLoggerExecutor_ExecuteFailures(t *testing.T) {
	timeTmpl, _ := template.New("test").Parse(`{{(.timestamp.Format "02/Jan/2006:15:04:05 -0700")}}`)

	executeErr := &accessLogsExecutor{
		name: "access_log",
		templateExprs: map[string]string{
			"timestamp": "foo",
		},
		template: timeTmpl,
	}

	logErr := &accessLogsExecutor{
		name:     "access_log",
		aspect:   &test.Logger{ErrOnLog: true},
		labels:   map[string]string{},
		template: timeTmpl,
	}

	tests := []struct {
		name   string
		exec   *accessLogsExecutor
		bag    attribute.Bag
		mapper expr.Evaluator
	}{
		{"template.Execute() error", executeErr, test.NewBag(), test.NewIDEval()},
		{"LogAccess() error", logErr, test.NewBag(), test.NewIDEval()},
	}

	for idx, v := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			if out := v.exec.Execute(v.bag, v.mapper); status.IsOK(out) {
				t.Fatalf("Execute(): expected error for %s", v.name)
			}
		})
	}
}

func TestAccessLoggerExecutor_Close(t *testing.T) {
	aw := &accessLogsExecutor{
		aspect: &test.Logger{ErrOnLog: true},
	}
	if err := aw.Close(); err != nil {
		t.Fatalf("Close() should not return error: got %v", err)
	}
}
