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

package stdio

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	descriptor "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/adapter/stdio/config"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
)

func TestBasic(t *testing.T) {
	info := GetInfo()

	if !contains(info.SupportedTemplates, logentry.TemplateName) ||
		!contains(info.SupportedTemplates, metric.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	cfg := info.DefaultConfig
	b := info.NewBuilder()
	b.SetAdapterConfig(cfg)

	if err := b.Validate(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	logEntryHandler := handler.(logentry.Handler)
	err = logEntryHandler.HandleLogEntry(context.Background(), nil)
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	metricHandler := handler.(metric.Handler)
	err = metricHandler.HandleMetric(context.Background(), nil)
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if err = handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func TestBuilder(t *testing.T) {
	env := test.NewEnv(t)

	cases := []struct {
		config      config.Params
		outputPath  string
		encoding    string
		metricLevel zapcore.Level
		induceError bool
		success     bool
	}{
		{config.Params{
			LogStream: config.STDOUT,
		}, "stdout", "console", zapcore.InfoLevel, false, true},

		{config.Params{
			LogStream: config.STDERR,
		}, "stderr", "console", zapcore.InfoLevel, false, true},

		{config.Params{
			MetricLevel: config.INFO,
		}, "stdout", "console", zapcore.InfoLevel, false, true},

		{config.Params{
			MetricLevel: config.WARNING,
		}, "stdout", "console", zapcore.WarnLevel, false, true},

		{config.Params{
			MetricLevel: config.ERROR,
		}, "stdout", "console", zapcore.ErrorLevel, false, true},

		{config.Params{
			MetricLevel: config.ERROR,
		}, "stdout", "console", zapcore.ErrorLevel, true, false},

		{config.Params{
			SeverityLevels: map[string]config.Params_Level{"WARNING": config.WARNING},
			OutputAsJson:   true,
		}, "stdout", "json", zapcore.InfoLevel, false, true},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			zb := func(outputPath string, encoding string) (*zap.Logger, error) {
				if outputPath != c.outputPath {
					t.Errorf("Got output path %s, expecting %s", outputPath, c.outputPath)
				}

				if encoding != c.encoding {
					t.Errorf("Got encoding %s, expecting %s", encoding, c.encoding)
				}

				if c.induceError {
					return nil, errors.New("expected")
				}

				return newZapLogger(outputPath, encoding)
			}

			info := GetInfo()
			b := info.NewBuilder().(*builder)
			b.SetAdapterConfig(&c.config)
			h, err := b.buildWithZapBuilder(context.Background(), env, zb)

			if (err != nil) && c.success {
				t.Errorf("Got %v, expecting success", err)
			} else if (err == nil) && !c.success {
				t.Errorf("Got success, expecting failure")
			}

			if (h == nil) && c.success {
				t.Errorf("Got nil, expecting valid handler")
			} else if (h != nil) && !c.success {
				t.Errorf("Got a handler, expecting nil")
			}

			if h != nil {
				handler := h.(*handler)
				if handler.metricLevel != c.metricLevel {
					t.Errorf("Got metric level %v, expecting %v", handler.metricLevel, c.metricLevel)
				}
			}
		})
	}
}

type testZap struct {
	zapcore.Core
	enc          zapcore.Encoder
	lines        []string
	errorOnWrite bool
}

func newTestZap() *testZap {
	return &testZap{
		enc: zapcore.NewJSONEncoder(newZapEncoderConfig()),
	}
}

func (tz *testZap) Write(e zapcore.Entry, f []zapcore.Field) error {
	if tz.errorOnWrite {
		return errors.New("write error")
	}

	buf, err := tz.enc.EncodeEntry(e, f)
	if err != nil {
		return err
	}

	tz.lines = append(tz.lines, strings.Trim(buf.String(), "\n"))
	return nil
}

func (tz *testZap) reset(errorOnWrite bool) {
	tz.errorOnWrite = errorOnWrite
	tz.lines = tz.lines[:0]
}

func TestLogEntry(t *testing.T) {
	types := map[string]*logentry.Type{
		"Foo": {
			Variables: map[string]descriptor.ValueType{
				"String":    descriptor.STRING,
				"Int64":     descriptor.INT64,
				"Double":    descriptor.DOUBLE,
				"Bool":      descriptor.BOOL,
				"Duration":  descriptor.DURATION,
				"Time":      descriptor.TIMESTAMP,
				"StringMap": descriptor.STRING_MAP,
				"IPAddress": descriptor.IP_ADDRESS,
				"Bytes":     descriptor.VALUE_TYPE_UNSPECIFIED,
			},
		},
	}

	info := GetInfo()
	cfg := info.DefaultConfig
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(cfg)
	env := test.NewEnv(t)
	b.SetLogEntryTypes(types)
	h, _ := b.Build(context.Background(), env)
	handler := h.(*handler)
	tz := newTestZap()
	handler.logger = zap.New(tz)

	tm := time.Date(2017, time.August, 21, 10, 4, 00, 0, time.UTC)

	cases := []struct {
		instances  []*logentry.Instance
		failWrites bool
		expected   []string
	}{
		{
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "WARNING",
					Variables: map[string]interface{}{
						"String": "a string",
					},
				},
			},
			true,
			[]string{},
		},

		{
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "WARNING",
					Variables: map[string]interface{}{
						"String":    "a string",
						"Int64":     int64(123),
						"Double":    1.23,
						"Bool":      true,
						"Time":      tm,
						"Duration":  1 * time.Second,
						"StringMap": map[string]string{"A": "B", "C": "D"},
						"IPAddress": net.IPv4zero,
						"Bytes":     []byte{'b'},
					},
				},
			},
			false,
			[]string{
				`{"level":"info","ts":"0001-01-01T00:00:00.000Z","instance":"Foo","Bool":true,"Bytes":"Yg==","Double":1.23,"Duration":"1s",` +
					`"IPAddress":"0.0.0.0","Int64":123,"String":"a string","StringMap":{"A":"B","C":"D"},"Time":"2017-08-21T10:04:00.000Z"}`,
			},
		},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			tz.reset(c.failWrites)

			err := handler.HandleLogEntry(context.Background(), c.instances)
			if err != nil && !c.failWrites {
				t.Errorf("Got %v, expecting success", err)
			} else if err == nil && c.failWrites {
				t.Errorf("Got success, expected failure")
			}

			if len(tz.lines) != len(c.expected) {
				t.Errorf("Got %d lines of output, expected %d", len(tz.lines), len(c.expected))
			}

			for i, l := range c.expected {
				if tz.lines[i] != l {
					t.Errorf("Got %s for output line %d, expected %s", tz.lines[i], i, l)
				}
			}
		})
	}

	if err := handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

func TestMetricEntry(t *testing.T) {
	types := map[string]*metric.Type{
		"Foo": {
			Dimensions: map[string]descriptor.ValueType{
				"String":    descriptor.STRING,
				"Int64":     descriptor.INT64,
				"Double":    descriptor.DOUBLE,
				"Bool":      descriptor.BOOL,
				"Duration":  descriptor.DURATION,
				"Time":      descriptor.TIMESTAMP,
				"StringMap": descriptor.STRING_MAP,
				"IPAddress": descriptor.IP_ADDRESS,
				"Bytes":     descriptor.VALUE_TYPE_UNSPECIFIED,
			},
		},
	}

	info := GetInfo()
	cfg := info.DefaultConfig
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(cfg)
	env := test.NewEnv(t)
	b.SetMetricTypes(types)
	h, _ := b.Build(context.Background(), env)
	handler := h.(*handler)
	tz := newTestZap()
	handler.logger = zap.New(tz)
	handler.getTime = func() time.Time { return time.Time{} }

	tm := time.Date(2017, time.August, 21, 10, 4, 00, 0, time.UTC)

	cases := []struct {
		instances  []*metric.Instance
		failWrites bool
		expected   []string
	}{
		{
			[]*metric.Instance{
				{
					Name: "Foo",
					Dimensions: map[string]interface{}{
						"String": "a string",
					},
				},
			},
			true,
			[]string{},
		},

		{
			[]*metric.Instance{
				{
					Name:  "Foo",
					Value: int64(123),
					Dimensions: map[string]interface{}{
						"String":    "a string",
						"Int64":     int64(123),
						"Double":    1.23,
						"Bool":      true,
						"Time":      tm,
						"Duration":  1 * time.Second,
						"StringMap": map[string]string{"A": "B", "C": "D"},
						"IPAddress": net.IPv4zero,
						"Bytes":     []byte{'b'},
					},
				},
			},
			false,
			[]string{
				`{"level":"info","ts":"0001-01-01T00:00:00.000Z","instance":"Foo","value":123,"Bool":true,"Bytes":"Yg==","Double":1.23,` +
					`"Duration":"1s","IPAddress":"0.0.0.0","Int64":123,"String":"a string","StringMap":{"A":"B","C":"D"},"Time":"2017-08-21T10:04:00.000Z"}`,
			},
		},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			tz.reset(c.failWrites)

			err := handler.HandleMetric(context.Background(), c.instances)
			if err != nil && !c.failWrites {
				t.Errorf("Got %v, expecting success", err)
			} else if err == nil && c.failWrites {
				t.Errorf("Got success, expected failure")
			}

			if len(tz.lines) != len(c.expected) {
				t.Errorf("Got %d lines of output, expected %d", len(tz.lines), len(c.expected))
			}

			for i, l := range c.expected {
				if tz.lines[i] != l {
					t.Errorf("Got %s for output line %d, expected %s", tz.lines[i], i, l)
				}
			}
		})
	}

	if err := handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}
