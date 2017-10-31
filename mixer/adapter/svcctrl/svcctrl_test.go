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

package svcctrl

import (
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	sc "google.golang.org/api/servicecontrol/v1"
)

func TestHandleMetric(t *testing.T) {
	timeNow := time.Now().Format(time.RFC3339Nano)
	request, err := handleMetric(timeNow, "")

	if err != nil {
		t.Fatalf("handleMetric() failed with:%v", err)
	}

	metricValue := int64(1)
	expected := &sc.ReportRequest{
		Operations: []*sc.Operation{
			{
				OperationName: "reportMetrics",
				StartTime:     timeNow,
				EndTime:       timeNow,
				Labels: map[string]string{
					"cloud.googleapis.com/location": "global",
				},
				MetricValueSets: []*sc.MetricValueSet{
					{
						MetricName: "serviceruntime.googleapis.com/api/producer/request_count",
						MetricValues: []*sc.MetricValue{
							{
								StartTime:  timeNow,
								EndTime:    timeNow,
								Int64Value: &metricValue,
							},
						},
					},
				},
			},
		},
	}

	cfg := spew.NewDefaultConfig()
	cfg.DisablePointerAddresses = true
	if !reflect.DeepEqual(*expected, *request) {
		t.Errorf("expect op1 == op2, but op1 = %v, op2 = %v", cfg.Sdump(*expected), cfg.Sdump(*request))
	}
}
