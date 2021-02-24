//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Package stackdriver provides utilities for querying data from stackdriver.
package stackdriverasm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/google/go-cmp/cmp"
	cloudtrace "google.golang.org/api/cloudtrace/v1"
	"google.golang.org/api/googleapi"
	logging "google.golang.org/api/logging/v2"
	monitoring "google.golang.org/api/monitoring/v3"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const (
	ContainerResourceType = "k8s_container"
	VMResourceType        = "gce_instance"

	VMOwnerPrefix = "//compute.googleapis.com/projects/"
)

// Instance represents a staging stackdriver service that test will talk to.
type Instance struct {
	ms             *monitoring.Service
	loggingService *logging.Service
	traceService   *cloudtrace.Service
}

// ResourceFilterParam defines param needed to generate stackdriver filters
type ResourceFilterParam struct {
	// FilterFor defaults to "metric" although it can filter logs.
	FilterFor string

	Zone          string
	ClusterName   string
	Namespace     string
	WorkloadName  string
	ContainerName string
	ResourceType  string
}

var containerFilters = map[string]string{
	"zone":      "resource.labels.location",
	"namespace": "resource.labels.namespace_name",
	"workload":  "resource.labels.pod_name",
	"cluster":   "resource.labels.cluster_name",
	"container": "resource.labels.container_name",
}

var filters = map[string]map[string]map[string]string{
	ContainerResourceType: {
		"log":    containerFilters,
		"metric": containerFilters,
	},
	VMResourceType: {
		"log": {
			"zone":      "resource.labels.location",
			"namespace": "labels.source_namespace",
			"workload":  "labels.source_workload",
		},
		"metric": {
			"zone":      "resource.labels.location",
			"namespace": "metric.labels.source_workload_namespace",
			"workload":  "metric.labels.source_workload_name",
		},
	},
}

// String implements the Stringer interface.
func (param ResourceFilterParam) String() string {
	filterFor := param.FilterFor
	if filterFor == "" {
		filterFor = "metric"
	}

	var s strings.Builder
	if filters[param.ResourceType] == nil {
		log.Warnf("invalid resource type for filter")
		return ""
	}
	fmt.Fprintf(&s, "resource.type = %q", param.ResourceType)
	for filterKey, value := range map[string]string{
		"zone":      param.Zone,
		"namespace": param.Namespace,
		"workload":  param.WorkloadName,
		"container": param.ContainerName,
		"cluster":   param.ClusterName,
	} {
		filter := filters[param.ResourceType][filterFor][filterKey]
		if value == "" || filter == "" {
			continue
		}
		fmt.Fprintf(&s, " AND %s = %q", filter, value)
	}

	return s.String()
}

// NewOrFail creates a new stackdriver instance.
func NewOrFail(ctx context.Context, t framework.TestContext) *Instance {
	monitoringService, err := monitoring.NewService(ctx)
	if err != nil {
		t.Fatalf("failed to get monitoring service: %v", err)
	}
	loggingService, err := logging.NewService(ctx)
	if err != nil {
		t.Fatalf("failed to get logging service: %v", err)
	}
	traceService, err := cloudtrace.NewService(ctx)
	if err != nil {
		t.Fatalf("failed to get tracing service: %v", err)
	}

	return &Instance{
		ms:             monitoringService,
		loggingService: loggingService,
		traceService:   traceService,
	}
}

// fetchTimeSeries fetches first time series within a project that match the labels provided.
func fetchTimeSeries(ctx context.Context, t framework.TestContext, ms *monitoring.Service, project, filter, aligner, startTime,
	endTime string) ([]*monitoring.TimeSeries, error) {
	lr := ms.Projects.TimeSeries.List(fmt.Sprintf("projects/%s", project)).
		IntervalStartTime(startTime).
		IntervalEndTime(endTime).
		AggregationCrossSeriesReducer("REDUCE_NONE").
		AggregationAlignmentPeriod("60s").
		AggregationPerSeriesAligner(aligner).
		Filter(filter).
		Context(ctx)

	resp, err := lr.Do()
	if err != nil {
		t.Fatalf("failed to call monitoring api to list time series: %v", err)
	}
	if resp.HTTPStatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get expected status code from monitoring service, got: %d", resp.HTTPStatusCode)
	}

	return resp.TimeSeries, nil
}

func execute(t framework.TestContext, tpl string, data interface{}) []byte {
	t.Helper()
	temp, err := template.New("test template").Parse(tpl)
	if err != nil {
		t.Fatalf("failed to parse template for: %v", err)
	}
	var b bytes.Buffer
	if err := temp.Execute(&b, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}
	return b.Bytes()
}

// GetTimeSeries gets timeseries from stackdriver based on the filters and duration provided.
// If multiple filters are provided, timeseries retrieved with any one of them could be returned.
func (d *Instance) GetAndValidateTimeSeries(ctx context.Context, t framework.TestContext, filters []string, aligner, startTime,
	endTime, projectID string, expLabel []byte, templlabels map[string]interface{}) ([]*monitoring.TimeSeries, error) {
	var timeSeries []*monitoring.TimeSeries
	t.Log("fetching metrics using filters:")
	for i, f := range filters {
		t.Logf("filter %d: %s", i, f)
	}

	t.Logf("fetching for metrics using project: %s; startTime %s; endTime %s", projectID, startTime, endTime)
	err := retry.UntilSuccess(func() error {
		var err error
		for _, filter := range filters {
			timeSeries, err = fetchTimeSeries(ctx, t, d.ms, projectID, filter, aligner, startTime, endTime)
			if err == nil {
				t.Logf("succeeded getting metrics response with %d time series items", len(timeSeries))
				err = d.ValidateMetrics(t, timeSeries, expLabel, templlabels)
				if err == nil {
					return nil
				}
			}
			t.Logf("query with filter %s failed due to error: %v", filter, err)
		}
		t.Log("retry due to no time series returned and validated with the given filters")
		return err
	}, retry.Delay(5*time.Second), retry.Timeout(5*time.Minute))

	return timeSeries, err
}

// CheckForLogEntry validates logs entry from stackdriver.
func (d *Instance) CheckForLogEntry(ctx context.Context, t framework.TestContext, filter, projectID string, want map[string]string) {
	t.Logf("fetching logs using filter: %s", filter)
	t.Logf("fetching logs using project: %s", projectID)
	retry.UntilSuccessOrFail(t, func() error {
		var err error
		logEntries, err := fetchLog(ctx, t, d.loggingService, projectID, filter)
		if err != nil {
			t.Logf("retry due to error: %v", err)
			return err
		}
		found := false
		for _, logEntry := range logEntries {
			got := make(map[string]string)
			for label := range want {
				got[label] = logEntry.Labels[label]
			}

			if diff := cmp.Diff(want, got); diff != "" {
				msg := fmt.Sprintf("Retry due to unexpected logging labels, (-want +got):\n%s", diff)
				t.Log(msg)
			}

			found = true
		}
		if !found {
			return fmt.Errorf("log Entry Not Found")
		}
		t.Log("succeeded CheckForLogEntry")
		return nil
	}, retry.Delay(5*time.Second), retry.Timeout(5*time.Minute))
}

// fetchLog fetches log from a project that match the filter provided.
func fetchLog(ctx context.Context, t framework.TestContext, loggingService *logging.Service, projectID, filter string) ([]*logging.LogEntry, error) {
	t.Logf("fetching log with projectID: %s", projectID)
	t.Logf("fetching log with filter: %s", filter)

	resp, err := loggingService.Entries.List(&logging.ListLogEntriesRequest{
		ResourceNames: []string{"projects/" + projectID},
		PageSize:      10,
		Filter:        filter,
	}).Context(ctx).Do()
	if err != nil {
		if gErr, ok := err.(*googleapi.Error); ok {
			if sc := gErr.Code; sc == http.StatusInternalServerError || sc == http.StatusServiceUnavailable {
				return nil, err
			}
		}
		t.Fatalf("unexpected error from the logging backend: %v", err)
	}
	if resp.HTTPStatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from logging service, got: %d", resp.HTTPStatusCode)
	}

	t.Logf("total log entries: %d", len(resp.Entries))

	if len(resp.Entries) == 0 {
		return nil, errors.New("no log entries match the query provided")
	}

	t.Logf("Found log entry: %s", string(resp.Entries[0].JsonPayload))

	return resp.Entries, err
}

// ValidateMetrics validate metrics from stackdriver.
func (d *Instance) ValidateMetrics(t framework.TestContext, ts []*monitoring.TimeSeries, expLabel []byte,
	templlabels map[string]interface{}) error {
	expLabel = execute(t, string(expLabel), templlabels)
	expm := make(map[string]string)
	if err := json.Unmarshal(expLabel, &expm); err != nil {
		t.Fatalf("failed to unmarshal expected json labels: %v", err)
	}
	if len(ts) == 0 {
		return fmt.Errorf("validating metrics: got empty time series")
	}
	return d.ValidateMetricsWithLabels(t, ts, expm)
}

// Cleanup Source and Destination Principal.
// TODO(gargnupur): This should not be needed. Need to investigate for multicluster scenario.
func cleanupLabels(labels map[string]string) {
	_, ok := labels["source_principal"]
	if ok {
		delete(labels, "source_principal")
	}
	_, ok = labels["destination_principal"]
	if ok {
		delete(labels, "destination_principal")
	}

	// TODO hack since we don't have an easy way to know the instance id
	for _, k := range []string{"source_owner", "destination_owner"} {
		if v, ok := labels[k]; ok && strings.HasPrefix(v, VMOwnerPrefix) {
			labels[k] = VMOwnerPrefix
		}
	}
}

// ValidateMetricsWithLabels validate metrics from stackdriver based on the given labels.
func (d *Instance) ValidateMetricsWithLabels(t framework.TestContext, tss []*monitoring.TimeSeries, expLabels map[string]string) error {
	found := false
	for _, ts := range tss {
		metrics := ts.Metric
		if metrics == nil {
			t.Log("got empty metric from returned time series")
			continue
		}
		labels := metrics.Labels
		if labels == nil {
			t.Log("got empty label from returned metrics")
			continue
		}
		cleanupLabels(labels)
		if diff := cmp.Diff(expLabels, labels); diff != "" {
			t.Log("comparing got labels and expect labels")
			msg := fmt.Sprintf("Retry due to unexpected logging labels, (-want +got):\n %s difference is", diff)
			t.Log(msg)
			continue
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("could not find metrics with matching labels")
	}
	return nil
}

func fetchTrace(ctx context.Context, t framework.TestContext, traceService *cloudtrace.Service, projectID, filter string,
	functionStartTime string) (*cloudtrace.Trace, error) {
	listTracesResponse, err := traceService.Projects.Traces.List(projectID).
		StartTime(functionStartTime).
		Filter(filter).
		View("COMPLETE").
		Context(ctx).
		Do()
	t.Logf("fetching traces using:Traces.List(%q).StartTime(%q).Filter(%q).View(COMPLETE).Context(ctx).Do()", projectID, functionStartTime, filter)
	if err != nil {
		return nil, fmt.Errorf("traces.List(%q).StartTime(%q).Filter(%q).View(COMPLETE).Context(ctx).Do() got"+
			" error %w, want no error", projectID, functionStartTime, filter, err)
	}
	if len(listTracesResponse.Traces) == 0 {
		return nil, errors.New("got empty traces")
	}
	// Return the first trace. Ignoring rest of the traces as they should all be similar and we want to validate just one for the test.
	return listTracesResponse.Traces[0], nil
}

func sanitizeSpans(spans []*cloudtrace.TraceSpan, want cloudtrace.TraceSpan) {
	for _, span := range spans {
		span.EndTime = ""
		span.Kind = ""
		span.ParentSpanId = 0
		span.SpanId = 0
		span.StartTime = ""
		span.ForceSendFields = nil
		span.NullFields = nil
		for key := range span.Labels {
			// If a key is present in the wanted tracespan, keep it.
			if len(want.Labels[key]) != 0 {
				continue
			}
			delete(span.Labels, key)
		}
	}
}

// ValidateTraces validates traces received from stackdriver.
func (d *Instance) ValidateTraces(ctx context.Context, t framework.TestContext, filter string, functionStartTime string, want []*cloudtrace.TraceSpan) {
	if len(want) == 0 {
		return
	}
	var trace *cloudtrace.Trace
	projectID := os.Getenv("GCR_PROJECT_ID")
	t.Logf("fetching traces using filter: %s for project %s", filter, projectID)
	var lastErr error
	retry.UntilSuccessOrFail(t, func() error {
		var err error
		trace, err = fetchTrace(ctx, t, d.traceService, projectID, filter, functionStartTime)
		if err != nil {
			if lastErr == nil || err.Error() != lastErr.Error() {
				lastErr = err
				t.Logf("retry due to error: %v", err)
			}
			return err
		}

		// We assume that label map has same set of keys that needs to be verified in all tracespans.
		sanitizeSpans(trace.Spans, *want[0])

		if diff := cmp.Diff(trace.Spans, want); diff != "" {
			return fmt.Errorf("got unexpected labels (-want, +got):\n%s", diff)
		}

		return nil
	}, retry.Delay(20*time.Second), retry.Timeout(5*time.Minute))
}
