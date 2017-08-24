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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/gogo/protobuf/proto"
	ptypes "github.com/gogo/protobuf/types"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	atest "istio.io/mixer/pkg/adapter/test"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cfgpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

type (
	aspectTestCase struct {
		name   string
		params config.AspectParams
		want   *applicationLogsExecutor
	}
)

var (
	validDesc = dpb.LogEntryDescriptor{
		Name:          "logentry",
		PayloadFormat: dpb.TEXT,
		LogTemplate:   "{{.foo}}",
		Labels: map[string]dpb.ValueType{
			"label": dpb.STRING,
		},
	}

	applogsDF = test.NewDescriptorFinder(map[string]interface{}{
		validDesc.Name: &validDesc,
		// our attributes
		"duration":  &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.DURATION},
		"string":    &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.STRING},
		"int64":     &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.INT64},
		"timestamp": &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.TIMESTAMP},
	})
)

func evalErrOnStr(s string) expr.Evaluator {
	return test.NewFakeEval(func(e string, _ attribute.Bag) (interface{}, error) {
		if e == s {
			return "", fmt.Errorf("expected error for %s", s)
		}
		return e, nil
	})
}

func TestNewLoggerManager(t *testing.T) {
	m := newApplicationLogsManager()
	if m.Kind() != config.ApplicationLogsKind {
		t.Error("Wrong kind of manager")
	}
}

func TestLoggerManager_NewLogger(t *testing.T) {
	tl := &test.Logger{}
	defaultExec := &applicationLogsExecutor{
		name:   "istio_log",
		aspect: tl,
		metadata: map[string]*logInfo{
			validDesc.Name: {
				timeFormat: time.RFC3339,
			},
		},
	}

	overrideExec := &applicationLogsExecutor{
		name:   "istio_log",
		aspect: tl,
		metadata: map[string]*logInfo{
			validDesc.Name: {
				timeFormat: "2006-Jan-02",
			},
		},
	}

	newAspectShouldSucceed := []aspectTestCase{
		{"empty", &aconfig.ApplicationLogsParams{
			LogName: "istio_log",
			Logs: []aconfig.ApplicationLogsParams_ApplicationLog{
				{
					DescriptorName: validDesc.Name,
					TimeFormat:     time.RFC3339,
				},
			}}, defaultExec},
		{"override", &aconfig.ApplicationLogsParams{
			LogName: "istio_log",
			Logs: []aconfig.ApplicationLogsParams_ApplicationLog{
				{
					DescriptorName: validDesc.Name,
					TimeFormat:     "2006-Jan-02",
				},
			}}, overrideExec},
	}

	m := newApplicationLogsManager()

	for idx, v := range newAspectShouldSucceed {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			c := cfgpb.Combined{
				Builder: &cfgpb.Adapter{Params: &ptypes.Empty{}},
				Aspect:  &cfgpb.Aspect{Params: v.params},
			}
			f, _ := FromBuilder(tl, config.ApplicationLogsKind)
			asp, err := m.NewReportExecutor(&c, f, atest.NewEnv(t), applogsDF, "")
			if err != nil {
				t.Fatalf("NewExecutor(): should not have received error for %s (%v)", v.name, err)
			}
			got := asp.(*applicationLogsExecutor)
			// We ignore templates because reflect.DeepEqual doesn't seem to work with them.
			for key := range got.metadata {
				got.metadata[key].tmpl = nil
			}
			if !reflect.DeepEqual(got, v.want) {
				t.Errorf("NewExecutor() = %#+v, wanted %#+v", got, v.want)
			}
		})
	}
}

func TestLoggerManager_NewLoggerFailures(t *testing.T) {
	defaultCfg := &aconfig.ApplicationLogsParams{
		LogName: "istio_log",
		Logs: []aconfig.ApplicationLogsParams_ApplicationLog{
			{
				DescriptorName: validDesc.Name,
				TimeFormat:     time.RFC3339,
			},
		},
	}

	errLogger := &test.Logger{DefaultCfg: &ptypes.Empty{}, ErrOnNewAspect: true}

	failureCases := []struct {
		name  string
		cfg   *aconfig.ApplicationLogsParams
		adptr adapter.Builder
	}{
		{"new aspect error", defaultCfg, errLogger},
	}

	m := newApplicationLogsManager()
	for idx, v := range failureCases {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			cfg := &cfgpb.Combined{
				Builder: &cfgpb.Adapter{
					Params: &ptypes.Empty{},
				},
				Aspect: &cfgpb.Aspect{
					Params: v.cfg,
				},
			}
			f, _ := FromBuilder(v.adptr, config.ApplicationLogsKind)
			if _, err := m.NewReportExecutor(cfg, f, atest.NewEnv(t), applogsDF, ""); err == nil {
				t.Fatalf("NewExecutor(): expected error for bad adapter (%T)", v.adptr)
			}
		})
	}
}

func TestLoggerManager_DefaultConfig(t *testing.T) {
	m := newApplicationLogsManager()
	got := m.DefaultConfig()
	want := &aconfig.ApplicationLogsParams{LogName: "istio_log"}
	if !proto.Equal(got, want) {
		t.Errorf("DefaultConfig(): got %v, wanted %v", got, want)
	}
}

func TestLoggerManager_ValidateConfig(t *testing.T) {
	wrap := func(name string, log *aconfig.ApplicationLogsParams_ApplicationLog) *aconfig.ApplicationLogsParams {
		return &aconfig.ApplicationLogsParams{
			LogName: name,
			Logs:    []aconfig.ApplicationLogsParams_ApplicationLog{*log},
		}
	}

	validDesc := dpb.LogEntryDescriptor{
		Name:          "logentry",
		PayloadFormat: dpb.TEXT,
		LogTemplate:   "{{.foo}}",
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
		"duration":  &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.DURATION},
		"string":    &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.STRING},
		"int64":     &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.INT64},
		"timestamp": &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.TIMESTAMP},
	})

	validLog := aconfig.ApplicationLogsParams_ApplicationLog{
		DescriptorName:      validDesc.Name,
		Severity:            "string",
		Timestamp:           "timestamp",
		Labels:              map[string]string{"label": "string"},
		TemplateExpressions: map[string]string{"foo": "int64"},
	}

	noLogName := wrap("", &validLog)

	missingDesc := validLog
	missingDesc.DescriptorName = "not in the df"

	invalidSeverity := validLog
	invalidSeverity.Severity = "int64"

	invalidTimestamp := validLog
	invalidTimestamp.Timestamp = "duration"

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
		cfg  *aconfig.ApplicationLogsParams
		df   descriptor.Finder
		err  string
	}{
		{"valid", wrap("valid", &validLog), df, ""},
		{"empty config", &aconfig.ApplicationLogsParams{}, df, "logName"},
		{"no log name", noLogName, df, "logName"},
		{"missing desc", wrap("missing desc", &missingDesc), df, "could not find a descriptor"},
		{"invalid severity", wrap("sev", &invalidSeverity), df, "severity"},
		{"invalid timestamp", wrap("ts", &invalidTimestamp), df, "timestamp"},
		{"invalid labels", wrap("labels", &invalidLabels), df, "labels"},
		{"invalid logtemplate", wrap("tmpl", &invalidDescLog), df, "logDescriptor"},
		{"template expr attr missing", wrap("missing attr", &missingTmplExprs), df, "templateExpressions"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			eval, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
			if err := (&applicationLogsManager{}).ValidateConfig(tt.cfg, eval, tt.df); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("Foo = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestLogExecutor_Execute(t *testing.T) {
	timeAttrName := "timestamp"
	knownTime := time.Now()
	idEvalWithTime := test.NewFakeEval(func(s string, _ attribute.Bag) (interface{}, error) {
		if s == timeAttrName {
			return knownTime, nil
		}
		return s, nil
	})

	tmpl, _ := template.New("test").Parse("{{.test}}")
	jsontmpl, _ := template.New("test").Parse(`{"value": "{{.test}}"}`)

	textLogInfo := logInfo{
		format:     Text,
		severity:   "DEFAULT",
		timestamp:  "timestamp",
		timeFormat: time.RFC822Z,
		tmpl:       tmpl,
		tmplExprs:  map[string]string{"test": "value"},
		labels:     map[string]string{"label1": "label1val"},
	}

	noDescriptors := &applicationLogsExecutor{
		name:     "name",
		metadata: make(map[string]*logInfo),
	}

	textPayload := &applicationLogsExecutor{
		name: "name",
		metadata: map[string]*logInfo{
			"text": &textLogInfo,
		},
	}
	textPayloadEntry := adapter.LogEntry{
		LogName:     "name",
		Labels:      map[string]interface{}{"label1": "label1val"},
		TextPayload: "value",
		Timestamp:   knownTime.Format(time.RFC822Z),
		Severity:    adapter.Default,
	}

	jsonLogInfo := textLogInfo
	jsonLogInfo.format = JSON
	jsonLogInfo.tmpl = jsontmpl

	jsonPayload := &applicationLogsExecutor{
		name: "name",
		metadata: map[string]*logInfo{
			"json": &jsonLogInfo,
		},
	}
	jsonPayloadEntry := textPayloadEntry
	jsonPayloadEntry.TextPayload = ""
	jsonPayloadEntry.StructPayload = map[string]interface{}{"value": "value"}

	multipleLogs := &applicationLogsExecutor{
		name: "name",
		metadata: map[string]*logInfo{
			"json": jsonPayload.metadata["json"],
			"text": textPayload.metadata["text"],
		},
	}

	tests := []struct {
		name        string
		exec        *applicationLogsExecutor
		bag         attribute.Bag
		mapper      expr.Evaluator
		wantEntries []adapter.LogEntry
	}{
		{"no descriptors", noDescriptors, test.NewBag(), idEvalWithTime, nil},
		{"text payload", textPayload, test.NewBag(), idEvalWithTime, []adapter.LogEntry{textPayloadEntry}},
		{"json payload", jsonPayload, test.NewBag(), idEvalWithTime, []adapter.LogEntry{jsonPayloadEntry}},
		{"multiple logs", multipleLogs, test.NewBag(), idEvalWithTime, []adapter.LogEntry{jsonPayloadEntry, textPayloadEntry}},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			l := &test.Logger{}
			tt.exec.aspect = l

			if out := tt.exec.Execute(tt.bag, tt.mapper); !status.IsOK(out) {
				t.Fatalf("Execute(): should not have received error for %s (%v)", tt.name, out)
			}
			if l.EntryCount != len(tt.wantEntries) {
				t.Fatalf("Execute(): got %d entries, wanted %d for %s", l.EntryCount, len(tt.wantEntries), tt.name)
			}
			if !deepEqualIgnoreOrder(l.Logs, tt.wantEntries) {
				t.Fatalf("Execute(): got %v, wanted %v for %s", l.Logs, tt.wantEntries, tt.name)
			}
		})
	}
}

func TestLogExecutor_ExecuteFailures(t *testing.T) {
	tmpl, _ := template.New("test").Parse("{{.test}}")

	textLogInfo := logInfo{
		format:    Text,
		severity:  "DEFAULT",
		timestamp: "timestamp",
		tmpl:      tmpl,
		tmplExprs: map[string]string{"tmplExprs": "tmplExprs"},
		labels:    map[string]string{"labels": "labels"},
	}
	jsonLogInfo := textLogInfo
	jsonLogInfo.format = JSON

	textPayload := applicationLogsExecutor{
		name:   "name",
		aspect: &test.Logger{},
		metadata: map[string]*logInfo{
			"text": &textLogInfo,
		},
	}
	jsonPayload := applicationLogsExecutor{
		name:   "name",
		aspect: &test.Logger{},
		metadata: map[string]*logInfo{
			"json": &jsonLogInfo,
		},
	}
	logError := applicationLogsExecutor{
		name:   "name",
		aspect: &test.Logger{ErrOnLog: true},
		metadata: map[string]*logInfo{
			"don't care": &jsonLogInfo,
		},
	}

	timeTmpl, _ := template.New("test").Parse(`{{ .foo "-" .bar }}`)
	errorLogInfo := textLogInfo
	errorLogInfo.tmpl = timeTmpl
	errorPayload := applicationLogsExecutor{
		name:   "name",
		aspect: &test.Logger{},
		metadata: map[string]*logInfo{
			"error": &errorLogInfo,
		},
	}

	tests := []struct {
		name   string
		exec   *applicationLogsExecutor
		bag    attribute.Bag
		mapper expr.Evaluator
	}{
		{"invalid json template", &jsonPayload, test.NewBag(), test.NewIDEval()},
		{"err on log", &logError, test.NewBag(), test.NewIDEval()},
		{"err on label eval", &textPayload, test.NewBag(), evalErrOnStr("labels")},
		{"err on template eval", &jsonPayload, test.NewBag(), evalErrOnStr("tmplExprs")},
		{"err on severity", &jsonPayload, test.NewBag(), evalErrOnStr("DEFAULT")},
		{"err on timestamp", &jsonPayload, test.NewBag(), evalErrOnStr("timestamp")},
		{"err on execute", &errorPayload, test.NewBag(), test.NewIDEval()},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if out := tt.exec.Execute(tt.bag, tt.mapper); status.IsOK(out) {
				t.Fatalf("Execute(): should have received error for %s", tt.name)
			}
		})
	}
}

func TestLogExecutor_Close(t *testing.T) {
	l := &test.Logger{}
	executor := applicationLogsExecutor{
		name:     "name",
		aspect:   l,
		metadata: make(map[string]*logInfo),
	}
	if err := executor.Close(); err != nil {
		t.Fatalf("executor.Close() = %s, wanted no err.", err)
	}
	if !l.Closed {
		t.Fatal("executor.Close() didn't call aspect.Close()")
	}
}

func TestPayloadFormatFromProto(t *testing.T) {
	tests := []struct {
		name string
		in   dpb.LogEntryDescriptor_PayloadFormat
		out  PayloadFormat
	}{
		{"json", dpb.JSON, JSON},
		{"text", dpb.TEXT, Text},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if r := payloadFormatFromProto(tt.in); r != tt.out {
				t.Errorf("payloadFormatFromProto(%s) = %v, wanted %v", tt.in, r, tt.out)
			}
		})
	}
}

// We can't actually nail down which entries correspond to each other as there's no unique identifier per entry.
// We'll do the n^2 compare-everything-to-everything method. Keep actual and expected small!
func deepEqualIgnoreOrder(actual, expected []adapter.LogEntry) bool {
	for _, e := range expected {
		result := false
		for _, a := range actual {
			result = result || reflect.DeepEqual(e, a)
		}
		// bail early: we found no match for this e
		if !result {
			return false
		}
	}
	return true
}
