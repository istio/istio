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

package log

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"text/template"
	"time"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"golang.org/x/net/context"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/api/monitoredres"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/logentry"
)

var dummyShouldFill = func() bool { return true }
var dummyMetadataFn = func() (string, error) { return "", nil }
var dummyMetadataGenerator = helper.NewMetadataGenerator(dummyShouldFill, dummyMetadataFn, dummyMetadataFn, dummyMetadataFn)

func TestBuild(t *testing.T) {
	b := &builder{makeClient: func(context.Context, string, ...option.ClientOption) (*logging.Client, error) {
		return nil, errors.New("expected")
	}, makeSyncClient: func(context.Context, string, ...option.ClientOption) (*logadmin.Client, error) {
		return nil, errors.New("expected")
	}, mg: dummyMetadataGenerator}
	b.SetLogEntryTypes(map[string]*logentry.Type{})
	b.SetAdapterConfig(&config.Params{})
	if _, err := b.Build(context.Background(), test.NewEnv(t)); err == nil {
		t.Error("Expected error, got none.")
	}

	tests := []struct {
		name  string
		types map[string]*logentry.Type
		cfg   *config.Params
		logs  []string
	}{
		{"empty", map[string]*logentry.Type{}, &config.Params{}, []string{}},
		{"missing",
			map[string]*logentry.Type{},
			&config.Params{LogInfo: map[string]*config.Params_LogInfo{"missing": {}}},
			[]string{"which is not an Istio log"}},
		{"bad tmpl",
			map[string]*logentry.Type{"bad": {}},
			&config.Params{LogInfo: map[string]*config.Params_LogInfo{"bad": {PayloadTemplate: "{{}"}}},
			[]string{"failed to evaluate template"}},
		{"happy",
			map[string]*logentry.Type{"good": {}},
			&config.Params{LogInfo: map[string]*config.Params_LogInfo{"good": {PayloadTemplate: "literal"}}},
			[]string{}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			env := test.NewEnv(t)

			b := &builder{makeClient: func(context.Context, string, ...option.ClientOption) (*logging.Client, error) {
				return &logging.Client{}, nil
			}, makeSyncClient: func(context.Context, string, ...option.ClientOption) (*logadmin.Client, error) {
				return &logadmin.Client{}, nil
			}, mg: dummyMetadataGenerator}
			b.SetLogEntryTypes(tt.types)
			b.SetAdapterConfig(tt.cfg)
			if _, err := b.Build(context.Background(), env); err != nil {
				t.Fatalf("Unexpected error building the handler: %v", err)
			}

			logs := env.GetLogs()
			if len(logs) < len(tt.logs) || (len(tt.logs) == 0 && len(logs) > 0) {
				t.Errorf("Expected at least %d log entries, got %d: %v", len(tt.logs), len(logs), logs)
			}
			for _, expected := range tt.logs {
				found := false
				for _, actual := range logs {
					found = found || strings.Contains(actual, expected)
				}
				if !found {
					t.Errorf("Expected to find log entry '%s', did not: %v", expected, logs)
				}
			}
		})
	}
}

func TestEmptyProjectID(t *testing.T) {
	genMakeClientFn := func(want string) makeClientFn {
		return func(ctx context.Context, projectID string, opts ...option.ClientOption) (*logging.Client, error) {
			if projectID != want {
				return nil, fmt.Errorf("want %v got %v", want, projectID)
			}
			return &logging.Client{}, nil
		}
	}

	tests := []struct {
		name  string
		cfg   *config.Params
		pidFn func() (string, error)
		want  string
	}{
		{"empty", &config.Params{}, func() (string, error) { return "pid", nil }, "pid"},
		{"filled", &config.Params{ProjectId: "projectID"}, func() (string, error) { return "pid", nil }, "projectID"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			b := &builder{
				makeClient: genMakeClientFn(tt.want),
				makeSyncClient: func(context.Context, string, ...option.ClientOption) (*logadmin.Client, error) {
					return &logadmin.Client{}, nil
				},
				mg: helper.NewMetadataGenerator(dummyShouldFill, tt.pidFn, dummyMetadataFn, dummyMetadataFn),
			}
			b.SetAdapterConfig(tt.cfg)
			if _, err := b.Build(context.Background(), test.NewEnv(t)); err != nil {
				t.Errorf("Unexpected project id: %v", err)
			}
		})
	}
}

func TestHandleLogEntry(t *testing.T) {
	now := time.Now()
	log := func(entry logging.Entry) {}

	tests := []struct {
		name     string
		info     map[string]info
		vals     []*logentry.Instance
		expected []logging.Entry
		types    map[string]*logentry.Type
	}{
		{"empty", map[string]info{}, []*logentry.Instance{}, []logging.Entry{}, map[string]*logentry.Type{}},
		{"missing", map[string]info{}, []*logentry.Instance{{Name: "missing"}}, []logging.Entry{}, map[string]*logentry.Type{
			"missing": {},
		}},
		{"happy",
			map[string]info{"happy": {tmpl: template.Must(template.New("").Parse("literal")), log: log}},
			[]*logentry.Instance{{Name: "happy"}},
			[]logging.Entry{
				{
					Timestamp: now,
					Severity:  logging.Default,
					Labels:    map[string]string{},
					Payload:   "literal",
				},
			}, map[string]*logentry.Type{
				"happy": {},
			}},
		{"labels",
			map[string]info{"labels": {tmpl: template.Must(template.New("").Parse("literal")), labels: []string{"foo", "time"}, log: log}},
			[]*logentry.Instance{{Name: "labels", Variables: map[string]interface{}{"foo": "bar", "time": now}}},
			[]logging.Entry{
				{
					Timestamp: now,
					Severity:  logging.Default,
					Labels:    map[string]string{"foo": "bar", "time": fmt.Sprintf("%v", now)},
					Payload:   "literal",
				},
			},
			map[string]*logentry.Type{
				"labels": {
					Variables: map[string]descriptor.ValueType{
						"foo":  descriptor.STRING,
						"time": descriptor.TIMESTAMP,
					},
				},
			}},
		{"labels with bad utf8",
			map[string]info{"labels": {tmpl: template.Must(template.New("").Parse("literal")), labels: []string{"foo", "time"}, log: log}},
			[]*logentry.Instance{{Name: "labels", Variables: map[string]interface{}{"foo": "bar\xed\xa0\x80", "time": now}}},
			[]logging.Entry{
				{
					Timestamp: now,
					Severity:  logging.Default,
					Labels:    map[string]string{"foo": "bar", "time": fmt.Sprintf("%v", now)},
					Payload:   "literal",
				},
			},
			map[string]*logentry.Type{
				"labels": {
					Variables: map[string]descriptor.ValueType{
						"foo":  descriptor.STRING,
						"time": descriptor.TIMESTAMP,
					},
				},
			}},
		{"labels only one",
			map[string]info{"labels": {tmpl: template.Must(template.New("").Parse("literal")), labels: []string{"foo"}, log: log}},
			[]*logentry.Instance{{Name: "labels", Variables: map[string]interface{}{"foo": "bar", "time": fmt.Sprintf("%v", now)}}},
			[]logging.Entry{
				{
					Timestamp: now,
					Severity:  logging.Default,
					Labels:    map[string]string{"foo": "bar"},
					Payload:   "literal",
				},
			},
			map[string]*logentry.Type{
				"labels": {
					Variables: map[string]descriptor.ValueType{
						"foo": descriptor.STRING,
					},
				},
			}},
		{"req map",
			map[string]info{"reqmap": {
				tmpl:   template.Must(template.New("").Parse("literal")),
				labels: []string{"foo", "source_ip"},
				req: &config.Params_LogInfo_HttpRequestMapping{
					Status:       "status",
					LocalIp:      "localip",
					RemoteIp:     "remoteip",
					RequestSize:  "reqsize",
					ResponseSize: "respsize",
					Latency:      "latency",
				},
				log: log,
			}},
			[]*logentry.Instance{{
				Name: "reqmap",
				Variables: map[string]interface{}{
					"foo":       "bar",
					"time":      fmt.Sprintf("%v", now),
					"status":    int64(200),
					"localip":   "127.0.0.1",
					"remoteip":  []byte(net.ParseIP("1.0.0.127")),
					"source_ip": []byte(net.ParseIP("1.0.0.127")),
					"latency":   time.Second,
					"reqsize":   123,
					"respsize":  int64(456),
				},
			}},
			[]logging.Entry{
				{
					Timestamp: now,
					Severity:  logging.Default,
					Labels:    map[string]string{"foo": "bar", "source_ip": "1.0.0.127"},
					Payload:   "literal",
					HTTPRequest: &logging.HTTPRequest{
						Status:       200,
						LocalIP:      "127.0.0.1",
						RemoteIP:     "1.0.0.127",
						Latency:      time.Second,
						RequestSize:  123,
						ResponseSize: 456,
						Request:      &http.Request{URL: &url.URL{}, Method: "", Header: make(http.Header)},
					},
				},
			},
			map[string]*logentry.Type{
				"reqmap": {
					Variables: map[string]descriptor.ValueType{
						"foo":       descriptor.STRING,
						"status":    descriptor.INT64,
						"reqsize":   descriptor.INT64,
						"respsize":  descriptor.INT64,
						"latency":   descriptor.DURATION,
						"time":      descriptor.TIMESTAMP,
						"localip":   descriptor.IP_ADDRESS,
						"remoteip":  descriptor.IP_ADDRESS,
						"source_ip": descriptor.IP_ADDRESS,
					},
				},
			}},
		{"template",
			map[string]info{"template": {tmpl: template.Must(template.New("").Parse("{{.a}}-{{.b}}-{{.c}}")), log: log}},
			[]*logentry.Instance{{Name: "template", Variables: map[string]interface{}{"a": 1, "b": "foo", "c": now}}},
			[]logging.Entry{
				{
					Timestamp: now,
					Severity:  logging.Default,
					Labels:    map[string]string{},
					Payload:   fmt.Sprintf("%d-%s-%v", 1, "foo", now),
				},
			},
			map[string]*logentry.Type{
				"template": {
					Variables: map[string]descriptor.ValueType{
						"b": descriptor.STRING,
						"a": descriptor.INT64,
						"c": descriptor.TIMESTAMP,
					},
				},
			}},
		{"resource",
			map[string]info{"resource": {tmpl: template.Must(template.New("").Parse("{{.a}}-{{.b}}-{{.c}}")), log: log}},
			[]*logentry.Instance{{Name: "resource", Variables: map[string]interface{}{"a": 1, "b": "foo", "c": now}, MonitoredResourceType: "mr-type"}},
			[]logging.Entry{
				{
					Timestamp: now,
					Severity:  logging.Default,
					Labels:    map[string]string{},
					Payload:   fmt.Sprintf("1-foo-%v", now),
					Resource:  &monitoredres.MonitoredResource{Type: "mr-type", Labels: map[string]string{}},
				},
			},
			map[string]*logentry.Type{
				"resource": {
					Variables: map[string]descriptor.ValueType{
						"b": descriptor.STRING,
						"a": descriptor.INT64,
						"c": descriptor.TIMESTAMP,
					},
				},
			}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			actuals := make([]logging.Entry, 0)
			tinfo := make(map[string]info, len(tt.info))
			for name, i := range tt.info {
				tinfo[name] = info{
					labels: i.labels,
					tmpl:   i.tmpl,
					req:    i.req,
					log:    func(entry logging.Entry) { actuals = append(actuals, entry) },
					flush:  func() error { return nil },
				}
			}
			h := &handler{
				info:  tinfo,
				l:     test.NewEnv(t).Logger(),
				now:   func() time.Time { return now },
				types: tt.types,
			}
			if err := h.HandleLogEntry(context.Background(), tt.vals); err != nil {
				t.Fatalf("Got error while logging, should never happen.")
			}

			if len(actuals) != len(tt.expected) {
				t.Errorf("Expected %d entries, got %d: %v", len(tt.expected), len(actuals), actuals)
			}
			for _, expected := range tt.expected {
				found := false
				for _, actual := range actuals {
					found = found || reflect.DeepEqual(actual, expected)
				}
				if !found {
					t.Errorf("Expected entry %v, got: %v", expected, actuals)
				}
			}
		})
	}
}

func TestHandleLogEntry_Errors(t *testing.T) {
	tests := []struct {
		name string
		info map[string]info
		vals []*logentry.Instance
		logs []string
	}{
		{"empty", map[string]info{}, []*logentry.Instance{}, []string{}},
		{"missing", map[string]info{}, []*logentry.Instance{{Name: "missing"}}, []string{"unknown log 'missing'"}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			env := test.NewEnv(t)

			h := &handler{l: env.Logger()}

			if err := h.HandleLogEntry(context.Background(), tt.vals); err != nil {
				t.Fatalf("Got error while logging, should never happen.")
			}

			logs := env.GetLogs()
			if len(logs) < len(tt.logs) || (len(tt.logs) == 0 && len(logs) > 0) {
				t.Errorf("Expected at least %d log entries, got %d: %v", len(tt.logs), len(logs), logs)
			}
			for _, expected := range tt.logs {
				found := false
				for _, actual := range logs {
					found = found || strings.Contains(actual, expected)
				}
				if !found {
					t.Errorf("Expected to find log entry '%s', did not: %v", expected, logs)
				}
			}
		})
	}
}

func TestProjectMetadata(t *testing.T) {
	tests := []struct {
		name string
		vals []*logentry.Instance
		want *monitoredres.MonitoredResource
	}{
		{
			"filled",
			[]*logentry.Instance{
				{
					Name:                  "log",
					MonitoredResourceType: "mr-type",
					MonitoredResourceDimensions: map[string]interface{}{
						"project_id":   "id",
						"location":     "l",
						"cluster_name": "c",
					},
				},
			},
			&monitoredres.MonitoredResource{
				Type: "mr-type",
				Labels: map[string]string{
					"project_id":   "id",
					"location":     "l",
					"cluster_name": "c",
				},
			},
		},
		{
			"empty",
			[]*logentry.Instance{
				{
					Name:                  "log",
					MonitoredResourceType: "mr-type",
					MonitoredResourceDimensions: map[string]interface{}{
						"project_id":   "",
						"location":     "",
						"cluster_name": "",
					},
				},
			},
			&monitoredres.MonitoredResource{
				Type: "mr-type",
				Labels: map[string]string{
					"project_id":   "pid",
					"location":     "location",
					"cluster_name": "cluster",
				},
			},
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			logs := make([]logging.Entry, 0)
			infoMap := map[string]info{
				"log": {
					labels: []string{"foo", "time"},
					tmpl:   template.Must(template.New("").Parse("literal")),
					log:    func(entry logging.Entry) { logs = append(logs, entry) },
					flush:  func() error { return nil },
				},
			}

			h := &handler{
				info: infoMap,
				l:    test.NewEnv(t).Logger(),
				now:  time.Now,
				md:   helper.Metadata{ProjectID: "pid", Location: "location", ClusterName: "cluster"},
			}
			if err := h.HandleLogEntry(context.Background(), tt.vals); err != nil {
				t.Fatalf("Got error while logging, should never happen.")
			}
			if len(logs) != 1 {
				t.Fatalf("Want 1 log entry, got %d.", len(logs))
			}
			got := logs[0].Resource
			if !reflect.DeepEqual(tt.want.Labels, got.Labels) {
				t.Errorf("Want monitored resource %v, got %v", tt.want, got)
			}
		})
	}
}
