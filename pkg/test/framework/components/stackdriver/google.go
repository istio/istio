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
	ltype "google.golang.org/genproto/googleapis/logging/type"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"

	md "istio.io/istio/pkg/bootstrap/platform"
	edgespb "istio.io/istio/pkg/test/framework/components/stackdriver/edges"
	"istio.io/istio/pkg/test/framework/resource"
)

type realStackdriver struct {
	monitoringService *monitoring.Service
	loggingService    *logging.Service
	traceService      *cloudtrace.Service
	gcpEnv            md.Environment
	projectID         string
}

type timeseriesQuery struct {
	metricName   string
	resourceType string
}

var (
	_                 Instance = &realStackdriver{}
	timeseriesQueries          = []timeseriesQuery{
		{
			metricName:   "istio.io/service/server/request_count",
			resourceType: "k8s_container",
		},
		{
			metricName:   "istio.io/service/client/request_count",
			resourceType: "k8s_pod",
		},
		{
			metricName:   "istio.io/service/server/connection_open_count",
			resourceType: "k8s_container",
		},
		{
			metricName:   "istio.io/service/client/connection_open_count",
			resourceType: "k8s_pod",
		},
	}
)

func newRealStackdriver(_ resource.Context, _ Config) (Instance, error) {
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
	rsd := &realStackdriver{
		monitoringService: monitoringService,
		loggingService:    loggingService,
		traceService:      traceService,
		gcpEnv:            md.NewGCP(),
	}
	rsd.projectID = rsd.gcpEnv.Metadata()[md.GCPProject]
	return rsd, nil
}

func (s *realStackdriver) ListTimeSeries(namespace string) ([]*monitoringpb.TimeSeries, error) {
	endTime := time.Now()
	startTime := endTime.Add(-5 * time.Minute)
	ret := &monitoringpb.ListTimeSeriesResponse{}
	for _, q := range timeseriesQueries {
		lr := s.monitoringService.Projects.TimeSeries.List(fmt.Sprintf("projects/%v", s.projectID)).
			IntervalStartTime(startTime.Format(time.RFC3339)).
			IntervalEndTime(endTime.Format(time.RFC3339)).
			AggregationCrossSeriesReducer("REDUCE_NONE").
			AggregationAlignmentPeriod("60s").
			AggregationPerSeriesAligner("ALIGN_RATE").
			Filter(fmt.Sprintf("metric.type = %q AND resource.type = %q AND resource.labels.namespace_name = %q", q.metricName, q.resourceType, namespace)).
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
		ret.TimeSeries = append(ret.TimeSeries, resppb.TimeSeries...)
	}

	return trimMetricLabels(ret), nil
}

func (s *realStackdriver) ListLogEntries(filter LogType, namespace string) ([]*loggingpb.LogEntry, error) {
	logName := logNameSuffix(filter)
	resp, err := s.loggingService.Entries.List(&logging.ListLogEntriesRequest{
		ResourceNames: []string{fmt.Sprintf("projects/%v", s.projectID)},
		PageSize:      200,
		Filter: fmt.Sprintf("timestamp > %q AND logName:%q AND resource.labels.namespace_name=%q",
			time.Now().Add(-5*time.Minute).Format(time.RFC3339), logName, namespace),
	}).Context(context.Background()).Do()
	if err != nil {
		return nil, fmt.Errorf("unexpected error from the logging backend: %v", err)
	}
	if resp.HTTPStatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from logging service, got: %d", resp.HTTPStatusCode)
	}

	resppb := loggingpb.ListLogEntriesResponse{
		Entries: make([]*loggingpb.LogEntry, len(resp.Entries)),
	}
	for i, le := range resp.Entries {
		resppb.Entries[i] = &loggingpb.LogEntry{}
		resppb.Entries[i].LogName = le.LogName
		resppb.Entries[i].HttpRequest = &ltype.HttpRequest{}
		if le.HttpRequest != nil {
			resppb.Entries[i].HttpRequest.RequestMethod = le.HttpRequest.RequestMethod
			resppb.Entries[i].HttpRequest.RequestUrl = le.HttpRequest.RequestUrl
			resppb.Entries[i].HttpRequest.Status = int32(le.HttpRequest.Status)
			resppb.Entries[i].HttpRequest.Protocol = le.HttpRequest.Protocol
		}
		resppb.Entries[i].Labels = le.Labels
		resppb.Entries[i].TraceSampled = le.TraceSampled
	}
	return trimLogLabels(&resppb, filter), nil
}

func (s *realStackdriver) ListTrafficAssertions() ([]*edgespb.TrafficAssertion, error) {
	return nil, nil
}

func (s *realStackdriver) ListTraces(namespace string) ([]*cloudtracepb.Trace, error) {
	startTime := time.Now().Add(-5 * time.Minute)
	listTracesResponse, err := s.traceService.Projects.Traces.List(s.projectID).
		StartTime(startTime.Format(time.RFC3339)).
		View("COMPLETE").
		Filter(fmt.Sprintf("istio.namespace:%q", namespace)).
		Context(context.Background()).
		PageSize(200).
		Do()
	if err != nil {
		return nil, fmt.Errorf("unexpected error from the tracing backend: %v", err)
	}

	ret := make([]*cloudtracepb.Trace, len(listTracesResponse.Traces))
	for i, t := range listTracesResponse.Traces {
		ret[i] = &cloudtracepb.Trace{}
		ret[i].ProjectId = t.ProjectId
		ret[i].TraceId = t.TraceId
		ret[i].Spans = make([]*cloudtracepb.TraceSpan, len(t.Spans))
		for j, s := range t.Spans {
			ret[i].Spans[j] = &cloudtracepb.TraceSpan{}
			ret[i].Spans[j].SpanId = s.SpanId
			ret[i].Spans[j].Name = s.Name
			ret[i].Spans[j].Labels = s.Labels
		}
	}
	return ret, nil
}

func (s *realStackdriver) GetStackdriverNamespace() string {
	return ""
}

func (s *realStackdriver) Address() string {
	return ""
}
