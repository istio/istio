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

package fluentd

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/fluentd/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
)

func TestBasic(t *testing.T) {
	info := GetInfo()

	if !contains(info.SupportedTemplates, logentry.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	cfg := info.DefaultConfig
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(cfg)

	if err := b.Validate(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, err := b.injectBuild(context.Background(), test.NewEnv(t), &fluent.Fluent{})
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	logEntryHandler := handler.(logentry.Handler)
	err = logEntryHandler.HandleLogEntry(context.Background(), nil)
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
		name    string
		config  config.Params
		success bool
		inject  bool
	}{
		{
			"Empty Address",
			config.Params{
				Address:         "",
				IntegerDuration: false,
			},
			false,
			false,
		},
		{
			"Bad Address",
			config.Params{
				Address:         "dummy",
				IntegerDuration: false,
			},
			false,
			false,
		},
		{
			"Good Address",
			config.Params{
				Address:         "1.2.3.4:1234",
				IntegerDuration: false,
			},
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := GetInfo()
			b := info.NewBuilder().(*builder)
			b.SetAdapterConfig(&c.config)

			ce := b.Validate()
			if (ce != nil) && c.success {
				t.Errorf("Got %v, expecting success", ce)
			} else if (ce == nil) && !c.success {
				t.Errorf("Got success, expecting failure")
			}

			var h adapter.Handler
			var err error
			if c.inject {
				h, err = b.injectBuild(context.Background(), env, &fluent.Fluent{})
			} else {
				h, err = b.Build(context.Background(), env)
			}
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
		})
	}
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

	mf := &mockFluentd{}

	han := &handler{
		logger: mf,
		types:  types,
		env:    test.NewEnv(t),
		intDur: false,
	}

	tm := time.Date(2017, time.August, 21, 10, 4, 00, 0, time.UTC)

	cases := []struct {
		name         string
		instances    []*logentry.Instance
		failWrites   bool
		expectedTags []string
		expected     []map[string]interface{}
		intDur       bool
	}{
		{
			"Basic Logs",
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "WARNING",
					Variables: map[string]interface{}{
						"String": "a string",
					},
				},
				{
					Name:     "Foo",
					Severity: "WARNING",
					Variables: map[string]interface{}{
						"tag": "fluent-tag",
					},
				},
			},
			false,
			[]string{
				"Foo",
				"fluent-tag",
			},
			[]map[string]interface{}{
				{
					"severity": "WARNING",
					"String":   "a string",
				},
				{
					"name":     "Foo",
					"severity": "WARNING",
				},
			},
			false,
		},

		{
			"Complex Log",
			[]*logentry.Instance{
				{
					Name:      "Foo",
					Severity:  "WARNING",
					Timestamp: tm,
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
				"Foo",
			},
			[]map[string]interface{}{
				{
					"severity": "WARNING",
					"String":   "a string",
					"Duration": "1s",
				},
			},
			false,
		},
		{
			"Integer Duration",
			[]*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "WARNING",
					Variables: map[string]interface{}{
						"String":   "a string",
						"Duration": 10 * time.Millisecond,
					},
				},
			},
			false,
			[]string{
				"Foo",
			},
			[]map[string]interface{}{
				{
					"severity": "WARNING",
					"String":   "a string",
					"Duration": int64(10),
				},
			},
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mf.Reset()

			han.intDur = c.intDur
			err := han.HandleLogEntry(context.Background(), c.instances)
			if err != nil && !c.failWrites {
				t.Errorf("Got %v, expecting success", err)
			} else if err == nil && c.failWrites {
				t.Errorf("Got success, expected failure")
			}

			if len(mf.Messages) != len(c.expected) {
				t.Errorf("Got %d messages, expected %d", len(mf.Messages), len(c.expected))
			}

			for i, mp := range c.expected {
				if mf.Messages[i].Tag != c.expectedTags[i] {
					t.Errorf("Got %v for Tag, expected %v", mf.Messages[i].Tag, c.expectedTags[i])
				}
				for k, v := range mp {
					got := mf.Messages[i].Msg.(map[string]interface{})
					if got[k] != v {
						t.Errorf("Got %v for key %v, expected %v", got[k], k, v)
					}
				}
			}
		})
	}

	if err := han.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

type message struct {
	Tag string
	TS  time.Time
	Msg interface{}
}

type mockFluentd struct {
	Messages []message
}

func (l *mockFluentd) Close() error {
	return nil
}

func (l *mockFluentd) PostWithTime(tag string, ts time.Time, msg interface{}) error {
	l.Messages = append(l.Messages, message{Tag: tag, TS: ts, Msg: msg})
	return nil
}

func (l *mockFluentd) Reset() {
	l.Messages = make([]message, 0)
}
