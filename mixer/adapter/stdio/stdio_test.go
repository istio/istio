// Copyright Istio Authors
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
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	descriptor "istio.io/api/policy/v1beta1"
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

	tf, _ := ioutil.TempFile("", "TestBuilder")
	name := tf.Name()

	cases := []struct {
		config      config.Params
		metricLevel zapcore.Level
		induceError bool
		success     bool
	}{
		{config.Params{
			LogStream: config.STDOUT,
		}, zapcore.InfoLevel, false, true},

		{config.Params{
			LogStream: config.STDERR,
		}, zapcore.InfoLevel, false, true},

		{config.Params{
			LogStream:  config.FILE,
			OutputPath: name,
		}, zapcore.InfoLevel, false, true},

		{config.Params{
			LogStream:  config.FILE,
			OutputPath: "/",
		}, zapcore.InfoLevel, false, false},

		{config.Params{
			LogStream:  config.ROTATED_FILE,
			OutputPath: name,
		}, zapcore.InfoLevel, false, true},

		{config.Params{
			MetricLevel: config.INFO,
		}, zapcore.InfoLevel, false, true},

		{config.Params{
			MetricLevel: config.WARNING,
		}, zapcore.WarnLevel, false, true},

		{config.Params{
			MetricLevel: config.ERROR,
		}, zapcore.ErrorLevel, false, true},

		{config.Params{
			MetricLevel: config.ERROR,
		}, zapcore.ErrorLevel, true, false},

		{config.Params{
			SeverityLevels: map[string]config.Params_Level{"WARNING": config.WARNING},
			OutputAsJson:   true,
		}, zapcore.InfoLevel, false, true},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			zb := func(options *config.Params) (zapcore.Core, func(), error) {
				if c.induceError {
					return nil, func() {}, errors.New("expected")
				}

				return newZapCore(options)
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

				if err = h.Close(); err != nil {
					t.Errorf("Got %v, expected success", err)
				}
			}
		})
	}

	_ = tf.Close()
	_ = os.Remove(name)
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
				"DNSName":   descriptor.DNS_NAME,
				"URL":       descriptor.STRING,
				"EmailAddr": descriptor.EMAIL_ADDRESS,
			},
		},
	}

	tm := time.Date(2017, time.August, 21, 10, 4, 0, 0, time.UTC)

	cases := []struct {
		instances []*logentry.Instance
		json      bool
		expected  []string
	}{
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
						"Bytes":     []byte{'b'},
						"IPAddress": []byte(net.ParseIP("1.0.0.127")),
						"DNSName":   "foo.bar.com",
						"URL":       "http://foo.com",
						"EmailAddr": "foo@bar.com",
					},
				},
			},
			true,
			[]string{
				`{"level":"warn",` +
					`"time":"0001-01-01T00:00:00.000000Z",` +
					`"instance":"Foo",` +
					`"Bool":true,` +
					`"Bytes":"Yg==",` +
					`"DNSName":"foo.bar.com",` +
					`"Double":1.23,` +
					`"Duration":"1s",` +
					`"EmailAddr":"foo@bar.com",` +
					`"IPAddress":"1.0.0.127",` +
					`"Int64":123,` +
					`"String":"a string",` +
					`"StringMap":{"A":"B","C":"D"},` +
					`"Time":"2017-08-21T10:04:00.000000Z",` +
					`"URL":"http://foo.com"}`,
			},
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
						"DNSName":   "foo.bar.com",
						"URL":       "http://foo.com",
						"EmailAddr": "foo@bar.com",
					},
				},
			},
			false,
			[]string{
				"0001-01-01T00:00:00.000000Z\twarn\tFoo\t" +
					`{"Bool": true, ` +
					`"Bytes": "Yg==", ` +
					`"DNSName": "foo.bar.com", ` +
					`"Double": 1.23, ` +
					`"Duration": "1s", ` +
					`"EmailAddr": "foo@bar.com", ` +
					`"IPAddress": "0.0.0.0", ` +
					`"Int64": 123, ` +
					`"String": "a string", ` +
					`"StringMap": {"A":"B","C":"D"}, ` +
					`"Time": "2017-08-21T10:04:00.000000Z", ` +
					`"URL": "http://foo.com"}`,
			},
		},

		{
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "INFO",
					Variables: map[string]interface{}{
						"Double": 1.23,
					},
				},
			},
			true,
			[]string{
				`{"level":"info",` +
					`"time":"0001-01-01T00:00:00.000000Z",` +
					`"instance":"Foo",` +
					`"Double":1.23}`,
			},
		},

		{
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "informational",
					Variables: map[string]interface{}{
						"Double": 1.23,
					},
				},
			},
			true,
			[]string{
				`{"level":"info",` +
					`"time":"0001-01-01T00:00:00.000000Z",` +
					`"instance":"Foo",` +
					`"Double":1.23}`,
			},
		},

		{
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "ERROR",
					Variables: map[string]interface{}{
						"Double": 1.23,
					},
				},
			},
			true,
			[]string{
				`{"level":"error",` +
					`"time":"0001-01-01T00:00:00.000000Z",` +
					`"instance":"Foo",` +
					`"Double":1.23}`,
			},
		},

		{
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "error",
					Variables: map[string]interface{}{
						"Double": 1.23,
					},
				},
			},
			true,
			[]string{
				`{"level":"error",` +
					`"time":"0001-01-01T00:00:00.000000Z",` +
					`"instance":"Foo",` +
					`"Double":1.23}`,
			},
		},

		{
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "BOGUS",
					Variables: map[string]interface{}{
						"Double": 1.23,
					},
				},
			},
			true,
			[]string{
				`{"level":"info",` +
					`"time":"0001-01-01T00:00:00.000000Z",` +
					`"instance":"Foo",` +
					`"Double":1.23}`,
			},
		},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {

			lines, _ := captureStdout(func() {

				info := GetInfo()
				cfg := *info.DefaultConfig.(*config.Params)
				cfg.OutputAsJson = c.json
				b := info.NewBuilder().(*builder)
				b.SetAdapterConfig(&cfg)
				env := test.NewEnv(t)
				b.SetLogEntryTypes(types)
				h, _ := b.Build(context.Background(), env)
				handler := h.(*handler)
				handler.getTime = func() time.Time { return time.Time{} }

				if err := handler.HandleLogEntry(context.Background(), c.instances); err != nil {
					t.Errorf("Got %v, expecting success", err)
				}

				if err := handler.Close(); err != nil {
					t.Errorf("Got error %v, expecting success", err)
				}
			})

			if len(lines) != len(c.expected) {
				t.Errorf("Got %d lines of output, expected %d", len(lines), len(c.expected))
			}

			for i, l := range c.expected {
				if lines[i] != l {
					t.Errorf("Output line %d\nGot      : %s\nExpecting: %s", i, lines[i], l)
				}
			}
		})
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
				"DNSName":   descriptor.DNS_NAME,
				"URL":       descriptor.STRING,
				"EmailAddr": descriptor.EMAIL_ADDRESS,
			},
		},
	}

	tm := time.Date(2017, time.August, 21, 10, 4, 0, 0, time.UTC)

	cases := []struct {
		instances   []*metric.Instance
		metricLevel config.Params_Level
		json        bool
		expected    []string
	}{
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
						"DNSName":   "foo.bar.com",
						"URL":       "http://foo.com",
						"EmailAddr": "foo@bar.com",
					},
				},
			},
			config.INFO,
			true,
			[]string{
				`{"level":"info",` +
					`"time":"0001-01-01T00:00:00.000000Z",` +
					`"instance":"Foo",` +
					`"value":123,` +
					`"Bool":true,` +
					`"Bytes":"Yg==",` +
					`"DNSName":"foo.bar.com",` +
					`"Double":1.23,` +
					`"Duration":"1s",` +
					`"EmailAddr":"foo@bar.com",` +
					`"IPAddress":"0.0.0.0",` +
					`"Int64":123,` +
					`"String":"a string",` +
					`"StringMap":{"A":"B","C":"D"},` +
					`"Time":"2017-08-21T10:04:00.000000Z",` +
					`"URL":"http://foo.com"}`,
			},
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
						"DNSName":   "foo.bar.com",
						"Bytes":     []byte{'b'},
						"URL":       "http://foo.com",
						"EmailAddr": "foo@bar.com",
					},
				},
			},
			config.INFO,
			false,
			[]string{
				"0001-01-01T00:00:00.000000Z\tinfo\tFoo\t" +
					`{"value": 123, ` +
					`"Bool": true, ` +
					`"Bytes": "Yg==", ` +
					`"DNSName": "foo.bar.com", ` +
					`"Double": 1.23, ` +
					`"Duration": "1s", ` +
					`"EmailAddr": "foo@bar.com", ` +
					`"IPAddress": "0.0.0.0", ` +
					`"Int64": 123, ` +
					`"String": "a string", ` +
					`"StringMap": {"A":"B","C":"D"}, ` +
					`"Time": "2017-08-21T10:04:00.000000Z", ` +
					`"URL": "http://foo.com"}`,
			},
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
						"DNSName":   "foo.bar.com",
						"Bytes":     []byte{'b'},
						"URL":       "http://foo.com",
						"EmailAddr": "foo@bar.com",
					},
				},
			},
			config.WARNING,
			false,
			[]string{
				"0001-01-01T00:00:00.000000Z\twarn\tFoo\t" +
					`{"value": 123, ` +
					`"Bool": true, ` +
					`"Bytes": "Yg==", ` +
					`"DNSName": "foo.bar.com", ` +
					`"Double": 1.23, ` +
					`"Duration": "1s", ` +
					`"EmailAddr": "foo@bar.com", ` +
					`"IPAddress": "0.0.0.0", ` +
					`"Int64": 123, ` +
					`"String": "a string", ` +
					`"StringMap": {"A":"B","C":"D"}, ` +
					`"Time": "2017-08-21T10:04:00.000000Z", ` +
					`"URL": "http://foo.com"}`,
			},
		},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {

			lines, _ := captureStdout(func() {

				info := GetInfo()
				cfg := *info.DefaultConfig.(*config.Params)
				cfg.OutputAsJson = c.json
				cfg.MetricLevel = c.metricLevel
				b := info.NewBuilder().(*builder)
				b.SetAdapterConfig(&cfg)
				env := test.NewEnv(t)
				b.SetMetricTypes(types)
				h, _ := b.Build(context.Background(), env)
				handler := h.(*handler)
				handler.getTime = func() time.Time { return time.Time{} }

				if err := handler.HandleMetric(context.Background(), c.instances); err != nil {
					t.Errorf("Got %v, expecting success", err)
				}

				if err := handler.Close(); err != nil {
					t.Errorf("Got error %v, expecting success", err)
				}
			})

			if len(lines) != len(c.expected) {
				t.Errorf("Got %d lines of output, expected %d", len(lines), len(c.expected))
			}

			for i, l := range c.expected {
				if lines[i] != l {
					t.Errorf("Output line %d\nGot      : %s\nExpecting: %s", i, lines[i], l)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	info := GetInfo()
	cfg := *info.DefaultConfig.(*config.Params)
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(&cfg)

	cases := []struct {
		outputPath string
		stream     config.Params_Stream
		result     bool
	}{
		{"123", config.STDOUT, false},
		{"123", config.STDERR, false},
		{"", config.FILE, false},
		{"", config.ROTATED_FILE, false},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {

			cfg.OutputPath = c.outputPath
			cfg.LogStream = c.stream
			err := b.Validate()
			if err != nil && c.result {
				t.Errorf("Got %v, expected success", err)
			} else if err == nil && !c.result {
				t.Error("Got success, expected failure")
			}
		})
	}
}

func TestWriteErrors(t *testing.T) {
	logEntryTypes := map[string]*logentry.Type{
		"Foo": {
			Variables: map[string]descriptor.ValueType{
				"String": descriptor.STRING,
			},
		},
	}

	metricTypes := map[string]*metric.Type{
		"Foo": {
			Dimensions: map[string]descriptor.ValueType{
				"String": descriptor.STRING,
			},
		},
	}

	logEntries := []*logentry.Instance{
		{
			Name:     "Foo",
			Severity: "WARNING",
			Variables: map[string]interface{}{
				"String": "a string",
			},
		},
	}

	metrics := []*metric.Instance{
		{
			Name:  "Foo",
			Value: 123,
			Dimensions: map[string]interface{}{
				"String": "a string",
			},
		},
	}

	info := GetInfo()
	cfg := info.DefaultConfig
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(cfg)
	env := test.NewEnv(t)
	b.SetLogEntryTypes(logEntryTypes)
	b.SetMetricTypes(metricTypes)
	h, _ := b.Build(context.Background(), env)
	handler := h.(*handler)

	// force I/O errors
	handler.write = func(entry zapcore.Entry, field []zapcore.Field) error { return errors.New("BAD") }

	if err := handler.HandleLogEntry(context.Background(), logEntries); err == nil {
		t.Errorf("Got success, expecting failure")
	}

	if err := handler.HandleMetric(context.Background(), metrics); err == nil {
		t.Errorf("Got success, expecting failure")
	}

	if err := handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

// Runs the given function while capturing everything sent to stdout
func captureStdout(f func()) ([]string, error) {
	tf, err := ioutil.TempFile("", "tracing_test")
	if err != nil {
		return nil, err
	}

	old := os.Stdout
	os.Stdout = tf

	f()

	os.Stdout = old
	path := tf.Name()
	_ = tf.Sync()
	_ = tf.Close()

	content, err := ioutil.ReadFile(path)
	_ = os.Remove(path)

	if err != nil {
		return nil, err
	}

	s := strings.Trim(string(content), "\n")
	return strings.Split(s, "\n"), nil
}
