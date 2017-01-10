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
	"time"

	"istio.io/mixer/adapter/stdioLogger/config"
	"istio.io/mixer/pkg/adaptertesting"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/aspect/logger"
)

func TestAdapterInvariants(t *testing.T) {
	adaptertesting.TestAdapterInvariants(&adapter{}, Register, t)
}

func TestAdapter_NewAspect(t *testing.T) {
	tests := []newAspectTests{
		{&config.Params{}, defaultAspectImpl},
		{defaultParams, defaultAspectImpl},
		{overridesParams, overridesAspectImpl},
	}

	e := testEnv{}
	a := &adapter{}
	for _, v := range tests {
		asp, err := a.NewAspect(e, v.config)
		if err != nil {
			t.Errorf("NewAspect(env, %s) => unexpected error: %v", v.config, err)
		}
		got := asp.(*aspectImpl)
		// ignore timeFn when handling equality checks here
		got.timeFn = nil
		if !reflect.DeepEqual(got, v.want) {
			t.Errorf("NewAspect(env, %s) => %v, want %v", v.config, got, v.want)
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

	textPayloadEntry := logger.Entry{"payload": "payload value"}
	structPayloadEntry := logger.Entry{"payload": `{"val":"42", "obj":{"val":"false"}}`}

	baseLog := `{"logName":"istio_log","timestamp":"2017-01-09T00:00:00Z","severity":"INFO"}`
	textPayloadLog := `{"logName":"istio_log","timestamp":"2017-01-09T00:00:00Z","severity":"INFO","textPayload":"payload value"}`
	structPayloadLog := `{"logName":"istio_log","timestamp":"2017-01-09T00:00:00Z","severity":"INFO","structPayload":{"obj":{"val":"false"},"val":"42"}}`
	warningLog := `{"logName":"istio_log","timestamp":"2017-01-09T00:00:00Z","severity":"WARNING"}`
	labelLog := `{"logName":"istio_log","timestamp":"2017-01-09T00:00:00Z","labels":{"label":"42"},"severity":"INFO"}`
	timestampLog := `{"logName":"istio_log","timestamp":"2017-01-10T00:00:00Z","labels":{"label":"42"},"severity":"INFO"}`

	baseAspectImpl := &aspectImpl{tw, "istio_log", "", textFmt, "", "", "timefmt", timeFn}
	textPayloadAspectImpl := &aspectImpl{tw, "istio_log", "payload", textFmt, "", "", "timefmt", timeFn}
	structPayloadAspectImpl := &aspectImpl{tw, "istio_log", "payload", structFmt, "", "", "timefmt", timeFn}
	severityAspectImpl := &aspectImpl{tw, "istio_log", "", textFmt, "severity", "", "timefmt", timeFn}
	timestampAspectImpl := &aspectImpl{tw, "istio_log", "", textFmt, "", "timestamp", "2006-Jan-02", timeFn}

	tests := []logTests{
		{baseAspectImpl, []logger.Entry{}, []string{}},
		{baseAspectImpl, []logger.Entry{{}}, []string{baseLog}},
		{textPayloadAspectImpl, []logger.Entry{textPayloadEntry}, []string{textPayloadLog}},
		{structPayloadAspectImpl, []logger.Entry{structPayloadEntry}, []string{structPayloadLog}},
		{severityAspectImpl, []logger.Entry{{"severity": "WARNING"}}, []string{warningLog}},
		{baseAspectImpl, []logger.Entry{{"label": 42}}, []string{labelLog}},
		{timestampAspectImpl, []logger.Entry{{"label": 42, "timestamp": "2017-Jan-10"}}, []string{timestampLog}},
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

func TestAspectImpl_LogBad(t *testing.T) {

	tw := &testWriter{lines: make([]string, 0)}

	structPayloadEntry := logger.Entry{"payload": `{"val":"42", "obj":{"val":`}

	tests := []logTests{
		{&aspectImpl{tw, "istio_log", "", textFmt, "", "timestamp", "2006-Jan-02", timeFn}, []logger.Entry{{"timestamp": "bad timestamp"}}, []string{}},
		{&aspectImpl{tw, "istio_log", "payload", structFmt, "", "", "time-fmt-ignored", timeFn}, []logger.Entry{structPayloadEntry}, []string{}},
		{&aspectImpl{&testWriter{errorOnWrite: true}, "istio_log", "", textFmt, "", "", "", timeFn}, []logger.Entry{{}}, []string{}},
	}

	for _, v := range tests {
		if err := v.asp.Log(v.input); err == nil {
			t.Errorf("Log(%v) => expected error", v.input)
		}
	}
}

type (
	testEnv struct {
		aspect.Env
	}
	newAspectTests struct {
		config *config.Params
		want   *aspectImpl
	}
	logTests struct {
		asp   *aspectImpl
		input []logger.Entry
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
	defaultParams = &config.Params{
		LogStream:         config.Params_STDERR,
		LogName:           "",
		PayloadFormat:     config.Params_TEXT,
		PayloadAttribute:  "",
		SeverityAttribute: "",
	}
	defaultAspectImpl = &aspectImpl{os.Stderr, "istio_log", "", textFmt, "", "", "", nil}

	overridesParams = &config.Params{
		LogStream:         config.Params_STDOUT,
		LogName:           "service_log",
		PayloadAttribute:  "struct_payload",
		PayloadFormat:     config.Params_STRUCTURED,
		SeverityAttribute: "severity",
	}
	overridesAspectImpl = &aspectImpl{os.Stdout, "service_log", "struct_payload", structFmt, "severity", "", "", nil}
	timeFn              = func() time.Time { r, _ := time.Parse("2006-Jan-02", "2017-Jan-09"); return r }
)

func (t *testWriter) Write(p []byte) (n int, err error) {
	if t.errorOnWrite {
		return 0, errors.New("write error")
	}
	t.count++
	t.lines = append(t.lines, strings.Trim(string(p), "\n"))
	return len(p), nil
}
