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

package stdioLogger

import (
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"istio.io/mixer/adapter/stdioLogger/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

func TestAdapterInvariants(t *testing.T) {
	test.AdapterInvariants(Register, t)
}

func TestBuilder_NewLogger(t *testing.T) {
	tests := []newAspectTests{
		{&config.Params{}, defaultAspectImpl},
		{defaultParams, defaultAspectImpl},
		{overridesParams, overridesAspectImpl},
	}

	e := testEnv{}
	a := builder{}
	for _, v := range tests {
		asp, err := a.NewApplicationLogsAspect(e, v.config)
		if err != nil {
			t.Errorf("NewApplicationLogsAspect(env, %s) => unexpected error: %v", v.config, err)
		}
		got := asp.(*logger)
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewApplicationLogsAspect(env, %s) => %v, want %v", v.config, got, v.want)
		}
	}
}

func TestBuilder_NewAccessLogger(t *testing.T) {
	tests := []newAspectTests{
		{&config.Params{}, defaultAspectImpl},
		{defaultParams, defaultAspectImpl},
		{overridesParams, overridesAspectImpl},
	}

	e := testEnv{}
	a := builder{}
	for _, v := range tests {
		asp, err := a.NewAccessLogsAspect(e, v.config)
		if err != nil {
			t.Errorf("NewAccessLogsAspect(env, %s) => unexpected error: %v", v.config, err)
		}
		got := asp.(*logger)
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewAccessLogsAspect(env, %s) => %v, want %v", v.config, got, v.want)
		}
	}
}

func TestLogger_Close(t *testing.T) {
	a := &logger{}
	if err := a.Close(); err != nil {
		t.Errorf("Close() => unexpected error: %v", err)
	}
}

func TestLogger_Log(t *testing.T) {

	tw := &testWriter{lines: make([]string, 0)}

	structPayload := map[string]interface{}{"val": 42, "obj": map[string]interface{}{"val": false}}

	noPayloadEntry := adapter.LogEntry{LogName: "istio_log", Labels: map[string]interface{}{}, Timestamp: "2017-Jan-09", Severity: adapter.Info}
	textPayloadEntry := adapter.LogEntry{LogName: "istio_log", TextPayload: "text payload", Timestamp: "2017-Jan-09", Severity: adapter.Info}
	jsonPayloadEntry := adapter.LogEntry{LogName: "istio_log", StructPayload: structPayload, Timestamp: "2017-Jan-09", Severity: adapter.Info}
	labelEntry := adapter.LogEntry{LogName: "istio_log", Labels: map[string]interface{}{"label": 42}, Timestamp: "2017-Jan-09", Severity: adapter.Info}

	baseLog := `{"logName":"istio_log","timestamp":"2017-Jan-09","severity":"INFO"}`
	textPayloadLog := `{"logName":"istio_log","timestamp":"2017-Jan-09","severity":"INFO","textPayload":"text payload"}`
	jsonPayloadLog := `{"logName":"istio_log","timestamp":"2017-Jan-09","severity":"INFO","structPayload":{"obj":{"val":false},"val":42}}`
	labelLog := `{"logName":"istio_log","labels":{"label":42},"timestamp":"2017-Jan-09","severity":"INFO"}`

	baseAspectImpl := &logger{tw}

	tests := []struct {
		asp   *logger
		input []adapter.LogEntry
		want  []string
	}{
		{baseAspectImpl, []adapter.LogEntry{}, []string{}},
		{baseAspectImpl, []adapter.LogEntry{noPayloadEntry}, []string{baseLog}},
		{baseAspectImpl, []adapter.LogEntry{textPayloadEntry}, []string{textPayloadLog}},
		{baseAspectImpl, []adapter.LogEntry{jsonPayloadEntry}, []string{jsonPayloadLog}},
		{baseAspectImpl, []adapter.LogEntry{labelEntry}, []string{labelLog}},
	}

	for _, v := range tests {
		if err := v.asp.Log(v.input); err != nil {
			t.Errorf("Log(%v) => unexpected error: %v", v.input, err)
		}
		if !reflect.DeepEqual(tw.lines, v.want) {
			t.Errorf("Log(%v) => %v, want %s", v.input, tw.lines, v.want)
		}
		tw.lines = make([]string, 0)
	}
}

func TestLogger_LogFailure(t *testing.T) {
	tw := &testWriter{errorOnWrite: true}
	textPayloadEntry := adapter.LogEntry{LogName: "istio_log", TextPayload: "text payload", Timestamp: "2017-Jan-09", Severity: adapter.Info}
	baseAspectImpl := &logger{tw}

	if err := baseAspectImpl.Log([]adapter.LogEntry{textPayloadEntry}); err == nil {
		t.Error("Log() should have produced error")
	}
}

func TestLogger_LogAccess(t *testing.T) {
	tw := &testWriter{lines: make([]string, 0)}

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
		input []adapter.LogEntry
		want  []string
	}{
		{[]adapter.LogEntry{}, []string{}},
		{[]adapter.LogEntry{noLabelsEntry}, []string{baseLog}},
		{[]adapter.LogEntry{labelsEntry}, []string{labelsLog}},
		{[]adapter.LogEntry{labelsWithTextEntry}, []string{labelsWithTextLog}},
	}

	for _, v := range tests {
		log := &logger{tw}
		if err := log.LogAccess(v.input); err != nil {
			t.Errorf("LogAccess(%v) => unexpected error: %v", v.input, err)
		}
		if !reflect.DeepEqual(tw.lines, v.want) {
			t.Errorf("LogAccess(%v) => %v, want %s", v.input, tw.lines, v.want)
		}
		tw.lines = make([]string, 0)
	}
}

func TestLogger_LogAccessFailure(t *testing.T) {
	tw := &testWriter{errorOnWrite: true}
	entry := adapter.LogEntry{LogName: "access_log"}
	l := &logger{tw}

	if err := l.LogAccess([]adapter.LogEntry{entry}); err == nil {
		t.Error("LogAccess() should have produced error")
	}
}

type (
	testEnv struct {
		adapter.Env
	}
	newAspectTests struct {
		config *config.Params
		want   *logger
	}
	testWriter struct {
		io.Writer

		count        int
		lines        []string
		errorOnWrite bool
	}
)

var (
	defaultParams     = &config.Params{LogStream: config.STDERR}
	defaultAspectImpl = &logger{os.Stderr}

	overridesParams     = &config.Params{LogStream: config.STDOUT}
	overridesAspectImpl = &logger{os.Stdout}
)

func (t *testWriter) Write(p []byte) (n int, err error) {
	if t.errorOnWrite {
		return 0, errors.New("write error")
	}
	t.count++
	t.lines = append(t.lines, strings.Trim(string(p), "\n"))
	return len(p), nil
}
