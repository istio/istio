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

package stackdriver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	cloudtrace "google.golang.org/api/cloudtrace/v1"
	logging "google.golang.org/api/logging/v2"
	monitoring "google.golang.org/api/monitoring/v3"
	cloudtracepb "google.golang.org/genproto/googleapis/devtools/cloudtrace/v1"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"

	edgespb "istio.io/istio/pkg/test/framework/components/stackdriver/edges"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	_ Instance = &realStackdriver{}
)

type realStackdriver struct {
	monitoringService *monitoring.Service
	loggingService    *logging.Service
	traceService      *cloudtrace.Service
}

func newRealStackdriver(ctx resource.Context, cfg Config) (Instance, error) {
	monitoringService, err := monitoring.NewService(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get monitoring service: %v", err)
	}
	loggingService, err := logging.NewService(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get logging service: %v", err)
	}
	traceService, err := cloudtrace.NewService(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get tracing service: %v", err)
	}
	return &realStackdriver{
		monitoringService: monitoringService,
		loggingService:    loggingService,
		traceService:      traceService,
	}, nil
}

func (s *realStackdriver) ListTimeSeries(metricName string) ([]*monitoringpb.TimeSeries, error) {
	endTime := time.Now()
	startTime := endTime.Add(-5 * time.Minute)
	// TODO!!: get project id somewhere
	lr := s.monitoringService.Projects.TimeSeries.List(fmt.Sprintf("projects/istio-prow-build")).
		IntervalStartTime(startTime.Format(time.RFC3339)).
		IntervalEndTime(endTime.Format(time.RFC3339)).
		AggregationCrossSeriesReducer("REDUCE_NONE").
		AggregationAlignmentPeriod("60s").
		AggregationPerSeriesAligner("ALIGN_RATE").
		Filter(fmt.Sprintf("metric.type = %q AND (resource.type = k8s_container OR resource.type = k8s_pod)", metricName)).
		Context(context.Background())
	resp, err := lr.Do()
	if err != nil {
		return nil, err
	}
	if resp.HTTPStatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get expected status code from monitoring service, got: %d", resp.HTTPStatusCode)
	}
	b, _ := resp.MarshalJSON()
	r := bytes.NewReader(b)
	resppb := monitoringpb.ListTimeSeriesResponse{}
	_ = jsonpb.Unmarshal(r, &resppb)
	return trimMetricLabels(&resppb), nil
}

func (s *realStackdriver) ListLogEntries(filter LogType) ([]*loggingpb.LogEntry, error) {
	resp, err := s.loggingService.Entries.List(&logging.ListLogEntriesRequest{
		ResourceNames: []string{"projects/istio-prow-build"},
		PageSize:      10,
		Filter: fmt.Sprintf("timestamp > %q AND logName=\"projects/istio-prow-build/logs/server-accesslog-stackdriver\"",
			time.Now().Add(-5*time.Minute).Format(time.RFC3339)),
	}).Context(context.Background()).Do()
	if err != nil {
		return nil, fmt.Errorf("unexpected error from the logging backend: %v", err)
	}
	if resp.HTTPStatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from logging service, got: %d", resp.HTTPStatusCode)
	}

	b, _ := resp.MarshalJSON()
	r := bytes.NewReader(b)
	resppb := loggingpb.ListLogEntriesResponse{}
	_ = jsonpb.Unmarshal(r, &resppb)
	return trimLogLabels(&resppb, filter), nil
}

func (c *realStackdriver) ListTrafficAssertions() ([]*edgespb.TrafficAssertion, error) {
	return nil, nil
}

func (c *realStackdriver) ListTraces() ([]*cloudtracepb.Trace, error) {
	return nil, nil
}

func (c *realStackdriver) GetStackdriverNamespace() string {
	return ""
}

func (c *realStackdriver) Address() string {
	return ""
}
