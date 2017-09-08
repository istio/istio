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

package stdioLogger

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"istio.io/mixer/adapter/stdioLogger/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

func TestAdapterInvariants(t *testing.T) {
	test.AdapterInvariants(Register, t)
}

func TestBuilder_NewLogger(t *testing.T) {
	tests := []newAspectTests{
		{&config.Params{}, []string{"stderr"}},
		{defaultParams, []string{"stderr"}},
		{overridesParams, []string{"stdout"}},
	}

	e := testEnv{}
	a := builder{}
	for _, v := range tests {
		asp, err := a.NewApplicationLogsAspect(e, v.config)
		if err != nil {
			t.Errorf("NewAccessLogsAspect(env, %s) => unexpected error: %v", v.config, err)
		}
		got := asp.(*logger)
		if !reflect.DeepEqual(got.outputPaths, v.outputPaths) {
			t.Errorf("Bad output path configuration: got %v, want %v", got.outputPaths, v.outputPaths)
		}
	}
}

func TestBuilder_NewAccessLogger(t *testing.T) {
	tests := []newAspectTests{
		{&config.Params{}, []string{"stderr"}},
		{defaultParams, []string{"stderr"}},
		{overridesParams, []string{"stdout"}},
	}

	e := testEnv{}
	a := builder{}
	for _, v := range tests {
		asp, err := a.NewAccessLogsAspect(e, v.config)
		if err != nil {
			t.Errorf("NewAccessLogsAspect(env, %s) => unexpected error: %v", v.config, err)
		}
		got := asp.(*logger)
		if !reflect.DeepEqual(got.outputPaths, v.outputPaths) {
			t.Errorf("Bad output path configuration: got %v, want %v", got.outputPaths, v.outputPaths)
		}
	}
}

func TestNewLogger_Failures(t *testing.T) {
	tests := []struct {
		name       string
		zapBuilder zapBuilderFn
	}{
		{"builder fn failure", func(...string) (*zap.Logger, error) { return nil, errors.New("expected") }},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			if _, err := newLogger(defaultParams, v.zapBuilder); err == nil {
				t.Fatal("newLogger() - Expected error")
			}
		})
	}
}

func TestLogger_Close(t *testing.T) {
	a := &logger{}
	if err := a.Close(); err != nil {
		t.Errorf("Close() => unexpected error: %v", err)
	}
}

func TestLogger_Log(t *testing.T) {
	structPayload := map[string]interface{}{"val": 42, "obj": map[string]interface{}{"val": false}}

	noPayloadEntry := adapter.LogEntry{LogName: "istio_log", Labels: map[string]interface{}{}, Timestamp: "2017-Jan-09", Severity: adapter.InfoLegacy}
	textPayloadEntry := adapter.LogEntry{LogName: "istio_log", TextPayload: "text payload", Timestamp: "2017-Jan-09", Severity: adapter.InfoLegacy}
	jsonPayloadEntry := adapter.LogEntry{LogName: "istio_log", StructPayload: structPayload, Timestamp: "2017-Jan-09", Severity: adapter.InfoLegacy}
	labelEntry := adapter.LogEntry{LogName: "istio_log", Labels: map[string]interface{}{"label": 42}, Timestamp: "2017-Jan-09", Severity: adapter.InfoLegacy}

	omitAll := "{}"
	noOmitAll := `{"logName":"","timestamp":"","severity":"DEFAULT","labels":null,"textPayload":"","structPayload":null}`
	baseLog := `{"logName":"istio_log","timestamp":"2017-Jan-09","severity":"INFO"}`
	textPayloadLog := `{"logName":"istio_log","timestamp":"2017-Jan-09","severity":"INFO","textPayload":"text payload"}`
	jsonPayloadLog := `{"logName":"istio_log","timestamp":"2017-Jan-09","severity":"INFO","structPayload":{"obj":{"val":false},"val":42}}`
	labelLog := `{"logName":"istio_log","timestamp":"2017-Jan-09","severity":"INFO","labels":{"label":42}}`

	tests := []struct {
		name      string
		input     []adapter.LogEntry
		omitEmpty bool
		want      []string
	}{
		{"empty_array", []adapter.LogEntry{}, true, nil},
		{"empty_omit", []adapter.LogEntry{{}}, true, []string{omitAll}},
		{"empty_include", []adapter.LogEntry{{}}, false, []string{noOmitAll}},
		{"no_payload", []adapter.LogEntry{noPayloadEntry}, true, []string{baseLog}},
		{"text_payload", []adapter.LogEntry{textPayloadEntry}, true, []string{textPayloadLog}},
		{"json_payload", []adapter.LogEntry{jsonPayloadEntry}, true, []string{jsonPayloadLog}},
		{"labels", []adapter.LogEntry{labelEntry}, true, []string{labelLog}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			tc := newTestCore(false)
			l := &logger{omitEmpty: v.omitEmpty, impl: newTestLogger(tc)}
			if err := l.Log(v.input); err != nil {
				t.Errorf("Log(%v) => unexpected error: %v", v.input, err)
			}
			if !reflect.DeepEqual(tc.(*testCore).lines, v.want) {
				t.Errorf("Log(%v) => %v, want %s", v.input, tc.(*testCore).lines, v.want)
			}
		})
	}
}

func TestLogger_LogFailure(t *testing.T) {
	textPayloadEntry := adapter.LogEntry{LogName: "istio_log", TextPayload: "text payload", Timestamp: "2017-Jan-09", Severity: adapter.InfoLegacy}
	cases := []struct {
		name string
		core zapcore.Core
	}{
		{"write_error", newTestCore(true)},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			l := &logger{impl: zap.New(v.core)}
			if err := l.Log([]adapter.LogEntry{textPayloadEntry}); err == nil {
				t.Fatal("Log() should have produced error")
			}
		})
	}
}

func TestLogger_LogAccess(t *testing.T) {
	noLabelsEntry := adapter.LogEntry{LogName: "access_log"}
	labelsEntry := adapter.LogEntry{LogName: "access_log", Labels: map[string]interface{}{"test": false, "val": 42}}
	labelsWithTextEntry := adapter.LogEntry{
		LogName:     "access_log",
		Labels:      map[string]interface{}{"test": false, "val": 42},
		TextPayload: "this is a log line",
	}

	baseLog := `{"logName":"access_log"}`
	labelsLog := `{"logName":"access_log","labels":{"test":false,"val":42}}`
	labelsWithTextLog := `{"logName":"access_log","labels":{"test":false,"val":42},"textPayload":"this is a log line"}`

	tests := []struct {
		input     []adapter.LogEntry
		omitEmpty bool
		want      []string
	}{
		{[]adapter.LogEntry{}, true, []string(nil)},
		{[]adapter.LogEntry{noLabelsEntry}, true, []string{baseLog}},
		{[]adapter.LogEntry{labelsEntry}, true, []string{labelsLog}},
		{[]adapter.LogEntry{labelsWithTextEntry}, true, []string{labelsWithTextLog}},
	}

	for _, v := range tests {
		tc := newTestCore(false)
		log := &logger{omitEmpty: v.omitEmpty, impl: newTestLogger(tc)}
		if err := log.LogAccess(v.input); err != nil {
			t.Errorf("LogAccess(%v) => unexpected error: %v", v.input, err)
		}
		if !reflect.DeepEqual(tc.(*testCore).lines, v.want) {
			t.Errorf("LogAccess(%v) => %#v, want %#v", v.input, tc.(*testCore).lines, v.want)
		}
	}
}

func TestLogger_LogAccessFailure(t *testing.T) {
	entry := adapter.LogEntry{LogName: "access_log"}

	cases := []struct {
		name string
		core zapcore.Core
	}{
		{"write_error", newTestCore(true)},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			l := &logger{impl: zap.New(v.core)}
			if err := l.LogAccess([]adapter.LogEntry{entry}); err == nil {
				t.Fatal("LogAccess() should have produced error")
			}
		})
	}
}

func BenchmarkLogger_Log(b *testing.B) {
	entry1 := adapter.LogEntry{
		LogName:       "access_log",
		Labels:        map[string]interface{}{"test": false, "val": 42},
		TextPayload:   "this is a log line",
		StructPayload: map[string]interface{}{"bool": true, "number": 242, "map": map[string]string{"test": "test"}, "array": []string{"t", "v", "x"}},
	}

	entry2 := adapter.LogEntry{
		Labels:        map[string]interface{}{"test": true, "val": 55},
		TextPayload:   "this is a log line",
		StructPayload: map[string]interface{}{"bool": true, "number": 242, "map": map[string]string{"test": "test"}, "array": []string{"t", "v", "x"}},
	}

	l := &logger{omitEmpty: true, impl: zap.NewNop()}

	for i := 0; i < b.N; i++ {
		if err := l.log([]adapter.LogEntry{entry1, entry2}); err != nil {
			b.Logf("error in log: %v", err)
		}
	}
}

type (
	testEnv struct {
		adapter.Env
	}
	newAspectTests struct {
		config      *config.Params
		outputPaths []string
	}
	testCore struct {
		zapcore.Core

		enc          zapcore.Encoder
		count        int
		lines        []string
		errorOnWrite bool
	}
)

var (
	defaultParams   = &config.Params{LogStream: config.STDERR}
	overridesParams = &config.Params{LogStream: config.STDOUT}
)

func newTestLogger(t zapcore.Core) *zap.Logger {
	return zap.New(t)
}

func newTestCore(errOnWrite bool) zapcore.Core {
	return &testCore{
		errorOnWrite: errOnWrite,
		enc:          zapcore.NewJSONEncoder(zapConfig),
	}
}

func (t *testCore) Write(e zapcore.Entry, f []zapcore.Field) error {
	if t.errorOnWrite {
		return errors.New("write error")
	}

	buf, err := t.enc.EncodeEntry(e, f)
	if err != nil {
		return err
	}

	t.count++
	t.lines = append(t.lines, strings.Trim(buf.String(), "\n"))
	return nil
}
