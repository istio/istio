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
	at "istio.io/mixer/pkg/adapter/testing"
)

func TestAdapterInvariants(t *testing.T) {
	at.TestAdapterInvariants(Register, t)
}

func TestAdapter_NewAspect(t *testing.T) {
	tests := []newAspectTests{
		{&config.Params{}, defaultAspectImpl},
		{defaultParams, defaultAspectImpl},
		{overridesParams, overridesAspectImpl},
	}

	e := testEnv{}
	a := builderState{}
	for _, v := range tests {
		asp, err := a.NewLogger(e, v.config)
		if err != nil {
			t.Errorf("NewLogger(env, %s) => unexpected error: %v", v.config, err)
		}
		got := asp.(*aspectImpl)
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewLogger(env, %s) => %v, want %v", v.config, got, v.want)
		}
	}
}

func TestAspectImpl_Close(t *testing.T) {
	a := &aspectImpl{}
	if err := a.Close(); err != nil {
		t.Errorf("Close() => unexpected error: %v", err)
	}
}

func TestAspectImpl_Log(t *testing.T) {

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

	baseAspectImpl := &aspectImpl{tw}

	tests := []logTests{
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

func TestAspectImpl_LogFailure(t *testing.T) {
	tw := &testWriter{errorOnWrite: true}
	textPayloadEntry := adapter.LogEntry{LogName: "istio_log", TextPayload: "text payload", Timestamp: "2017-Jan-09", Severity: adapter.Info}
	baseAspectImpl := &aspectImpl{tw}

	if err := baseAspectImpl.Log([]adapter.LogEntry{textPayloadEntry}); err == nil {
		t.Error("Log() should have produced error")
	}
}

type (
	testEnv struct {
		adapter.Env
	}
	newAspectTests struct {
		config *config.Params
		want   *aspectImpl
	}
	logTests struct {
		asp   *aspectImpl
		input []adapter.LogEntry
		want  []string
	}
	testWriter struct {
		io.Writer

		count        int
		lines        []string
		errorOnWrite bool
	}
)

var (
	defaultParams     = &config.Params{LogStream: config.Params_STDERR}
	defaultAspectImpl = &aspectImpl{os.Stderr}

	overridesParams     = &config.Params{LogStream: config.Params_STDOUT}
	overridesAspectImpl = &aspectImpl{os.Stdout}
)

func (t *testWriter) Write(p []byte) (n int, err error) {
	if t.errorOnWrite {
		return 0, errors.New("write error")
	}
	t.count++
	t.lines = append(t.lines, strings.Trim(string(p), "\n"))
	return len(p), nil
}
