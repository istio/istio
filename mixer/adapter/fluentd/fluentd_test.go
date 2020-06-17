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

package fluentd

import (
	"bytes"
	"context"
	"net"
	"sync"
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

	handler, err := b.build(context.Background(), test.NewEnv(t), func(fluent.Config) (*fluent.Fluent, error) { return &fluent.Fluent{}, nil })
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
		return
	}

	if err = handler.Close(); err == nil {
		t.Errorf("Close(): expected error on second attempt; got none")
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
		{
			"Overrides",
			config.Params{
				Address:              ":0",
				IntegerDuration:      true,
				MaxBatchSizeBytes:    40000,
				InstanceBufferSize:   3,
				PushIntervalDuration: 3 * time.Minute,
				PushTimeoutDuration:  10 * time.Second,
			},
			true,
			true,
		},
		{
			"Bad Batch Size",
			config.Params{
				Address:           ":0",
				IntegerDuration:   true,
				MaxBatchSizeBytes: -14,
			},
			false,
			false,
		},
		{
			"Bad Instance Buffer Size",
			config.Params{
				Address:            ":0",
				IntegerDuration:    true,
				InstanceBufferSize: -5,
			},
			false,
			false,
		},
		{
			"Bad Push Interval",
			config.Params{
				Address:              ":0",
				IntegerDuration:      true,
				PushIntervalDuration: -14 * time.Millisecond,
			},
			false,
			false,
		},
		{
			"Bad Push Timeout",
			config.Params{
				Address:             ":0",
				IntegerDuration:     true,
				PushTimeoutDuration: -18 * time.Millisecond,
			},
			false,
			false,
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

			if ce != nil {
				return
			}

			var h adapter.Handler
			var err error
			if c.inject {
				h, err = b.build(context.Background(), test.NewEnv(t), func(fluent.Config) (*fluent.Fluent, error) { return &fluent.Fluent{}, nil })
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
			if h != nil {
				h.Close()
			}
		})
	}
}

func TestHandleLogEntry(t *testing.T) {
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

	tm := time.Date(2017, time.August, 21, 10, 4, 0, 0, time.UTC)

	cases := []struct {
		name       string
		instances  []*logentry.Instance
		expected   int
		maxBytes   int64
		failWrites bool
		intDur     bool
	}{
		{
			name: "Basic Logs",
			instances: []*logentry.Instance{
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
			expected: 2,
			maxBytes: 11,
		},
		{
			name: "Complex Log",
			instances: []*logentry.Instance{
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
			expected: 1,
			maxBytes: 11,
		},
		{
			name: "Integer Duration",
			instances: []*logentry.Instance{
				{
					Name:     "Foo",
					Severity: "WARNING",
					Variables: map[string]interface{}{
						"String":   "a string",
						"Duration": 10 * time.Millisecond,
					},
				},
			},
			expected: 1,
			intDur:   true,
			maxBytes: 11,
		},
		{
			name: "Too Large Log",
			instances: []*logentry.Instance{
				{
					Name:      "Foo",
					Severity:  "WARNING",
					Timestamp: tm,
					Variables: map[string]interface{}{
						"String":      "a string",
						"OtherString": "too large",
						"Int64":       int64(123),
						"Double":      1.23,
						"Bool":        true,
						"OtherBool":   true,
						"Time":        tm,
						"Duration":    1 * time.Second,
						"StringMap":   map[string]string{"A": "B", "C": "D"},
						"IPAddress":   net.IPv6loopback,
						"Bytes":       []byte{'b', 'a', 'd', 't', 'o', 'o', 'l', 'a', 'r', 'g', 'e'},
					},
				},
			},
			maxBytes: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			han := &handler{
				logger:        mf,
				types:         types,
				env:           test.NewEnv(t),
				intDur:        false,
				dataBuffer:    make(chan dataToEncode, 5),
				pushInterval:  1 * time.Millisecond,
				maxBatchBytes: c.maxBytes,
				stopCh:        make(chan bool),
			}
			go han.postData()
			mf.Reset()

			han.intDur = c.intDur
			err := han.HandleLogEntry(context.Background(), c.instances)
			if err != nil && !c.failWrites {
				t.Errorf("HandleLogEntry(): Got %v, expecting success", err)
			} else if err == nil && c.failWrites {
				t.Errorf("HandleLogEntry(): Got success, expected failure")
			}

			time.Sleep(100 * time.Millisecond)

			mf.mutex.RLock()
			defer mf.mutex.RUnlock()
			if got, want := mf.Batches, c.expected; got != want {
				t.Errorf("Got %d batches; want %d", got, want)
			}

			if err := han.Close(); err != nil {
				t.Errorf("Close(): Got error %v, expecting success", err)
			}
		})
	}
}

func TestHandleLogEntry_Errors(t *testing.T) {
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

	cases := []struct {
		name      string
		instances []*logentry.Instance
	}{
		{
			"Drop Instances",
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
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mf.Reset()

			han := &handler{
				logger:        mf,
				types:         types,
				env:           test.NewEnv(t),
				dataBuffer:    make(chan dataToEncode, 1),
				maxBatchBytes: 3,
				stopCh:        make(chan bool),
			}

			if err := han.HandleLogEntry(context.Background(), c.instances); err == nil {
				t.Fatalf("HandleLogEntry() did not produce expected error!")
			}

			if err := han.Close(); err != nil {
				t.Errorf("Close(): Got error %v, expecting success", err)
			}
		})
	}

}

type mockFluentd struct {
	bytes   bytes.Buffer
	Batches int
	mutex   sync.RWMutex
}

func (l *mockFluentd) Close() error {
	return nil
}

func (l *mockFluentd) EncodeData(tag string, ts time.Time, msg interface{}) ([]byte, error) {
	return []byte(tag), nil
}

func (l *mockFluentd) PostRawData(data []byte) {
	l.bytes.Write(data)
	l.mutex.Lock()
	l.Batches++
	l.mutex.Unlock()
}

func (l *mockFluentd) Reset() {
	l.bytes.Reset()
	l.Batches = 0
}
