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
	"fmt"
	"reflect"
	"testing"
	"text/template"
	"time"

	ptypes "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	atest "istio.io/mixer/pkg/adapter/test"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	configpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

type (
	aspectTestCase struct {
		name   string
		params config.AspectParams
		want   *applicationLogsWrapper
	}
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
	if m.Kind() != ApplicationLogsKind {
		t.Error("Wrong kind of manager")
	}
}

func TestLoggerManager_NewLogger(t *testing.T) {
	tl := &test.Logger{}
	defaultExec := &applicationLogsWrapper{
		name:   "istio_log",
		aspect: tl,
		metadata: map[string]*logInfo{
			"default": {
				timeFormat: time.RFC3339,
			},
		},
	}

	overrideExec := &applicationLogsWrapper{
		name:   "istio_log",
		aspect: tl,
		metadata: map[string]*logInfo{
			"default": {
				timeFormat: "2006-Jan-02",
			},
		},
	}

	emptyExec := &applicationLogsWrapper{
		name:     "istio_log",
		aspect:   tl,
		metadata: make(map[string]*logInfo),
	}

	newAspectShouldSucceed := []aspectTestCase{
		{"empty", &aconfig.ApplicationLogsParams{
			LogName: "istio_log",
			Logs: []*aconfig.ApplicationLogsParams_ApplicationLog{
				{
					DescriptorName: "default",
					TimeFormat:     time.RFC3339,
				},
			}}, defaultExec},
		{"override", &aconfig.ApplicationLogsParams{
			LogName: "istio_log",
			Logs: []*aconfig.ApplicationLogsParams_ApplicationLog{
				{
					DescriptorName: "default",
					TimeFormat:     "2006-Jan-02",
				},
			}}, overrideExec},
		// TODO: should this be an error? An aspect with no logInfo objects will never do any work.
		{"no descriptors", &aconfig.ApplicationLogsParams{
			LogName: "istio_log",
			Logs: []*aconfig.ApplicationLogsParams_ApplicationLog{
				{
					DescriptorName: "no descriptor with this name",
				},
			}}, emptyExec},
	}

	m := newApplicationLogsManager()

	for idx, v := range newAspectShouldSucceed {
		t.Run(fmt.Sprintf("[%d] %s", idx, v.name), func(t *testing.T) {
			c := configpb.Combined{
				Builder: &configpb.Adapter{Params: &ptypes.Empty{}},
				Aspect:  &configpb.Aspect{Params: v.params, Inputs: map[string]string{}},
			}
			asp, err := m.NewAspect(&c, tl, atest.NewEnv(t))
			if err != nil {
				t.Fatalf("NewAspect(): should not have received error for %s (%v)", v.name, err)
			}
			got := asp.(*applicationLogsWrapper)
			// We ignore templates because reflect.DeepEqual doesn't seem to work with them.
			for key := range got.metadata {
				got.metadata[key].tmpl = nil
			}
			if !reflect.DeepEqual(got, v.want) {
				t.Errorf("NewAspect() = %#+v, wanted %#+v", got, v.want)
			}
		})
	}
}

func TestLoggerManager_NewLoggerFailures(t *testing.T) {

	defaultCfg := &aconfig.ApplicationLogsParams{
		LogName: "istio_log",
		Logs: []*aconfig.ApplicationLogsParams_ApplicationLog{
			{
				DescriptorName: "default",
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
			cfg := &configpb.Combined{
				Builder: &configpb.Adapter{
					Params: &ptypes.Empty{},
				},
				Aspect: &configpb.Aspect{
					Params: v.cfg,
				},
			}

			if _, err := m.NewAspect(cfg, v.adptr, atest.NewEnv(t)); err == nil {
				t.Fatalf("NewAspect(): expected error for bad adapter (%T)", v.adptr)
			}
		})
	}
}

func TestLogWrapper_Execute(t *testing.T) {
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

	noDescriptors := &applicationLogsWrapper{
		name:     "name",
		metadata: make(map[string]*logInfo),
	}

	textPayload := &applicationLogsWrapper{
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

	jsonPayload := &applicationLogsWrapper{
		name: "name",
		metadata: map[string]*logInfo{
			"json": &jsonLogInfo,
		},
	}
	jsonPayloadEntry := textPayloadEntry
	jsonPayloadEntry.TextPayload = ""
	jsonPayloadEntry.StructPayload = map[string]interface{}{"value": "value"}

	multipleLogs := &applicationLogsWrapper{
		name: "name",
		metadata: map[string]*logInfo{
			"json": jsonPayload.metadata["json"],
			"text": textPayload.metadata["text"],
		},
	}

	tests := []struct {
		name        string
		exec        *applicationLogsWrapper
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

			if out := tt.exec.Execute(tt.bag, tt.mapper, &ReportMethodArgs{}); !out.IsOK() {
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

func TestLogWrapper_ExecuteFailures(t *testing.T) {
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

	textPayload := applicationLogsWrapper{
		name:   "name",
		aspect: &test.Logger{},
		metadata: map[string]*logInfo{
			"text": &textLogInfo,
		},
	}
	jsonPayload := applicationLogsWrapper{
		name:   "name",
		aspect: &test.Logger{},
		metadata: map[string]*logInfo{
			"json": &jsonLogInfo,
		},
	}
	logError := applicationLogsWrapper{
		name:   "name",
		aspect: &test.Logger{ErrOnLog: true},
		metadata: map[string]*logInfo{
			"don't care": &jsonLogInfo,
		},
	}

	timeTmpl, _ := template.New("test").Parse(`{{ .foo "-" .bar }}`)
	errorLogInfo := textLogInfo
	errorLogInfo.tmpl = timeTmpl
	errorPayload := applicationLogsWrapper{
		name:   "name",
		aspect: &test.Logger{},
		metadata: map[string]*logInfo{
			"error": &errorLogInfo,
		},
	}

	tests := []struct {
		name   string
		exec   *applicationLogsWrapper
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
			if out := tt.exec.Execute(tt.bag, tt.mapper, &ReportMethodArgs{}); out.IsOK() {
				t.Fatalf("Execute(): should have received error for %s", tt.name)
			}
		})
	}
}

func TestLogWrapper_Close(t *testing.T) {
	l := &test.Logger{}
	wrapper := applicationLogsWrapper{
		name:     "name",
		aspect:   l,
		metadata: make(map[string]*logInfo),
	}
	if err := wrapper.Close(); err != nil {
		t.Fatalf("wrapper.Close() = %s, wanted no err.", err)
	}
	if !l.Closed {
		t.Fatal("wrapper.Close() didn't call aspect.Close()")
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
	m := newApplicationLogsManager()
	if err := m.ValidateConfig(&ptypes.Empty{}, nil); err != nil {
		t.Errorf("ValidateConfig(): unexpected error: %v", err)
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
