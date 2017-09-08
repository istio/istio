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

package serviceControl

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/proto"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/mixer/adapter/serviceControl/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

const testServiceName = "gcp-service"

var (
	defaultParams = &config.Params{ServiceName: testServiceName}
	fakeService   = new(sc.Service)
)

func fakeCreateClient(logger adapter.Logger) (*sc.Service, error) {
	return fakeService, nil
}

func TestAdapterInvariants(t *testing.T) {
	test.AdapterInvariants(Register, t)
}

func TestBuilder_NewAspect(t *testing.T) {
	e := test.NewEnv(t)
	b := builder{adapter.DefaultBuilder{}, fakeCreateClient}
	a, err := b.newAspect(e, defaultParams)
	if err != nil {
		t.Errorf("NewApplicationLogsAspect(env, %s) => unexpected error: %v", defaultParams, err)
	}
	if x := a.serviceName; x != testServiceName {
		t.Errorf("NewApplicationLogsAspect(env, %s) => service name actual: %s", defaultParams, x)
	}

	if a.service != fakeService {
		t.Errorf("NewApplicationLogsAspect(env, %s) => create service control client fail", defaultParams)
	}

	if x := a.logger; x != e.Logger() {
		t.Errorf("NewApplicationLogsAspect(env, %s) mismatching logger actual: %v", defaultParams, x)
	}
}

func TestAspect_Close(t *testing.T) {
	a := new(aspect)
	if err := a.Close(); err != nil {
		t.Errorf("Close() => unexpected error: %v", err)
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		conf      proto.Message
		errString string
	}{
		{&config.Params{}, "serviceName"},
		{&config.Params{ServiceName: testServiceName}, ""},
	}
	for idx, c := range cases {
		b := &builder{}
		errString := ""
		if err := b.ValidateConfig(c.conf); err != nil {
			errString = err.Error()
		}
		if !strings.Contains(errString, c.errString) {
			t.Errorf("[%d] b.ValidateConfig(c.conf) = '%s'; want errString containing '%s'", idx, errString, c.errString)
		}
	}
}

func TestAspect_Log(t *testing.T) {
	a := &aspect{
		testServiceName,
		new(sc.Service),
		test.NewEnv(t).Logger(),
	}

	l := adapter.LogEntry{LogName: "istio_log", TextPayload: "text payload", Timestamp: "2017-Jan-09", Severity: adapter.InfoLegacy}

	tests := []struct {
		input []adapter.LogEntry
		want  sc.ReportRequest
	}{
		{[]adapter.LogEntry{l},
			sc.ReportRequest{
				Operations: []*sc.Operation{
					{
						OperationId:   "test_operation",
						OperationName: "reportLogs",
						StartTime:     "2017-07-01T10:10:05.000000002Z",
						EndTime:       "2017-07-01T10:10:05.000000002Z",
						LogEntries: []*sc.LogEntry{
							{
								Name:        "istio_log",
								Severity:    "INFO",
								TextPayload: "text payload",
								Timestamp:   "2017-Jan-09",
							},
						},
						Labels: map[string]string{"cloud.googleapis.com/location": "global"},
					},
				},
			},
		},
	}

	for _, c := range tests {
		r := a.log(c.input, time.Date(2017, time.July, 1, 10, 10, 5, 2, time.UTC), "test_operation")
		if !reflect.DeepEqual(*r, c.want) {
			t.Errorf("log(%+v) => %v, want %v", c.input, spew.Sdump(*r), spew.Sdump(c.want))
		}
	}
}

func TestAspect_Record(t *testing.T) {
	a := &aspect{
		testServiceName,
		new(sc.Service),
		test.NewEnv(t).Logger(),
	}

	c := int64(123)
	v := adapter.Value{
		Definition: &adapter.MetricDefinition{
			Name: "request_count",
			Kind: adapter.Counter,
		},
		Labels:      make(map[string]interface{}),
		StartTime:   time.Date(2017, time.June, 30, 18, 10, 5, 2, time.UTC),
		EndTime:     time.Date(2017, time.June, 30, 18, 10, 30, 2, time.UTC),
		MetricValue: c,
	}

	v.Labels["response_code"] = 500

	tests := []struct {
		input []adapter.Value
		want  sc.ReportRequest
	}{
		{[]adapter.Value{v},
			sc.ReportRequest{
				Operations: []*sc.Operation{
					{
						OperationId:   "test_operation",
						OperationName: "reportMetrics",
						StartTime:     "2017-07-01T10:10:05.000000002Z",
						EndTime:       "2017-07-01T10:10:05.000000002Z",
						MetricValueSets: []*sc.MetricValueSet{
							{
								MetricName: "serviceruntime.googleapis.com/api/producer/request_count",
								MetricValues: []*sc.MetricValue{
									{
										Labels: map[string]string{
											"/response_code": "500",
										},
										StartTime:  "2017-06-30T18:10:05.000000002Z",
										EndTime:    "2017-06-30T18:10:30.000000002Z",
										Int64Value: &c,
									},
								},
							},
						},
						Labels: map[string]string{"cloud.googleapis.com/location": "global"},
					},
				},
			},
		},
	}

	for _, c := range tests {
		r := a.record(c.input, time.Date(2017, time.July, 1, 10, 10, 5, 2, time.UTC), "test_operation")
		if !reflect.DeepEqual(*r, c.want) {
			t.Errorf("record(%+v) => %v, want %v", c.input, spew.Sdump(*r), spew.Sdump(c.want))
		}
	}
}
