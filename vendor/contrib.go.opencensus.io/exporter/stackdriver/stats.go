// Copyright 2017, OpenCensus Authors
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
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	"cloud.google.com/go/monitoring/apiv3"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/api/option"
	"google.golang.org/api/support/bundler"
	distributionpb "google.golang.org/genproto/googleapis/api/distribution"
	labelpb "google.golang.org/genproto/googleapis/api/label"
	"google.golang.org/genproto/googleapis/api/metric"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

const (
	maxTimeSeriesPerUpload    = 200
	opencensusTaskKey         = "opencensus_task"
	opencensusTaskDescription = "Opencensus task identifier"
	defaultDisplayNamePrefix  = "OpenCensus"

	userAgent = "opencensus-go [0.8.0]"
)

// statsExporter exports stats to the Stackdriver Monitoring.
type statsExporter struct {
	bundler *bundler.Bundler
	o       Options

	createdViewsMu sync.Mutex
	createdViews   map[string]*metricpb.MetricDescriptor // Views already created remotely

	c         *monitoring.MetricClient
	taskValue string
}

// Enforces the singleton on NewExporter per projectID per process
// lest there will be races with Stackdriver.
var (
	seenProjectsMu sync.Mutex
	seenProjects   = make(map[string]bool)
)

var (
	errBlankProjectID    = errors.New("expecting a non-blank ProjectID")
	errSingletonExporter = errors.New("only one exporter can be created per unique ProjectID per process")
)

// newStatsExporter returns an exporter that uploads stats data to Stackdriver Monitoring.
// Only one Stackdriver exporter should be created per ProjectID per process, any subsequent
// invocations of NewExporter with the same ProjectID will return an error.
func newStatsExporter(o Options) (*statsExporter, error) {
	if strings.TrimSpace(o.ProjectID) == "" {
		return nil, errBlankProjectID
	}

	seenProjectsMu.Lock()
	defer seenProjectsMu.Unlock()
	_, seen := seenProjects[o.ProjectID]
	if seen {
		return nil, errSingletonExporter
	}

	seenProjects[o.ProjectID] = true

	opts := append(o.MonitoringClientOptions, option.WithUserAgent(userAgent))
	client, err := monitoring.NewMetricClient(context.Background(), opts...)
	if err != nil {
		return nil, err
	}
	e := &statsExporter{
		c:            client,
		o:            o,
		createdViews: make(map[string]*metricpb.MetricDescriptor),
		taskValue:    getTaskValue(),
	}
	e.bundler = bundler.NewBundler((*view.Data)(nil), func(bundle interface{}) {
		vds := bundle.([]*view.Data)
		e.handleUpload(vds...)
	})
	e.bundler.DelayThreshold = e.o.BundleDelayThreshold
	e.bundler.BundleCountThreshold = e.o.BundleCountThreshold
	return e, nil
}

// ExportView exports to the Stackdriver Monitoring if view data
// has one or more rows.
func (e *statsExporter) ExportView(vd *view.Data) {
	if len(vd.Rows) == 0 {
		return
	}
	err := e.bundler.Add(vd, 1)
	switch err {
	case nil:
		return
	case bundler.ErrOversizedItem:
		go e.handleUpload(vd)
	case bundler.ErrOverflow:
		e.o.handleError(errors.New("failed to upload: buffer full"))
	default:
		e.o.handleError(err)
	}
}

// getTaskValue returns a task label value in the format of
// "go-<pid>@<hostname>".
func getTaskValue() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	return "go-" + strconv.Itoa(os.Getpid()) + "@" + hostname
}

// handleUpload handles uploading a slice
// of Data, as well as error handling.
func (e *statsExporter) handleUpload(vds ...*view.Data) {
	if err := e.uploadStats(vds); err != nil {
		e.o.handleError(err)
	}
}

// Flush waits for exported view data to be uploaded.
//
// This is useful if your program is ending and you do not
// want to lose recent spans.
func (e *statsExporter) Flush() {
	e.bundler.Flush()
}

func (e *statsExporter) uploadStats(vds []*view.Data) error {
	ctx, span := trace.StartSpan(
		context.Background(),
		"contrib.go.opencensus.io/exporter/stackdriver.uploadStats",
		trace.WithSampler(trace.NeverSample()),
	)
	defer span.End()

	for _, vd := range vds {
		if err := e.createMeasure(ctx, vd); err != nil {
			span.SetStatus(trace.Status{Code: 2, Message: err.Error()})
			return err
		}
	}
	for _, req := range e.makeReq(vds, maxTimeSeriesPerUpload) {
		if err := e.c.CreateTimeSeries(ctx, req); err != nil {
			span.SetStatus(trace.Status{Code: 2, Message: err.Error()})
			// TODO(jbd): Don't fail fast here, batch errors?
			return err
		}
	}
	return nil
}

func (e *statsExporter) makeReq(vds []*view.Data, limit int) []*monitoringpb.CreateTimeSeriesRequest {
	var reqs []*monitoringpb.CreateTimeSeriesRequest
	var timeSeries []*monitoringpb.TimeSeries

	resource := e.o.Resource
	if resource == nil {
		resource = &monitoredrespb.MonitoredResource{
			Type: "global",
		}
	}

	for _, vd := range vds {
		for _, row := range vd.Rows {
			ts := &monitoringpb.TimeSeries{
				Metric: &metricpb.Metric{
					Type:   namespacedViewName(vd.View.Name),
					Labels: newLabels(row.Tags, e.taskValue),
				},
				Resource: resource,
				Points:   []*monitoringpb.Point{newPoint(vd.View, row, vd.Start, vd.End)},
			}
			timeSeries = append(timeSeries, ts)
			if len(timeSeries) == limit {
				reqs = append(reqs, &monitoringpb.CreateTimeSeriesRequest{
					Name:       monitoring.MetricProjectPath(e.o.ProjectID),
					TimeSeries: timeSeries,
				})
				timeSeries = []*monitoringpb.TimeSeries{}
			}
		}
	}
	if len(timeSeries) > 0 {
		reqs = append(reqs, &monitoringpb.CreateTimeSeriesRequest{
			Name:       monitoring.MetricProjectPath(e.o.ProjectID),
			TimeSeries: timeSeries,
		})
	}
	return reqs
}

// createMeasure creates a MetricDescriptor for the given view data in Stackdriver Monitoring.
// An error will be returned if there is already a metric descriptor created with the same name
// but it has a different aggregation or keys.
func (e *statsExporter) createMeasure(ctx context.Context, vd *view.Data) error {
	e.createdViewsMu.Lock()
	defer e.createdViewsMu.Unlock()

	m := vd.View.Measure
	agg := vd.View.Aggregation
	tagKeys := vd.View.TagKeys
	viewName := vd.View.Name

	if md, ok := e.createdViews[viewName]; ok {
		return equalMeasureAggTagKeys(md, m, agg, tagKeys)
	}

	metricType := namespacedViewName(viewName)
	var valueType metricpb.MetricDescriptor_ValueType
	unit := m.Unit()

	switch agg.Type {
	case view.AggTypeCount:
		valueType = metricpb.MetricDescriptor_INT64
		// If the aggregation type is count, which counts the number of recorded measurements, the unit must be "1",
		// because this view does not apply to the recorded values.
		unit = stats.UnitDimensionless
	case view.AggTypeSum:
		switch m.(type) {
		case *stats.Int64Measure:
			valueType = metricpb.MetricDescriptor_INT64
		case *stats.Float64Measure:
			valueType = metricpb.MetricDescriptor_DOUBLE
		}
	case view.AggTypeDistribution:
		valueType = metricpb.MetricDescriptor_DISTRIBUTION
	case view.AggTypeLastValue:
		switch m.(type) {
		case *stats.Int64Measure:
			valueType = metricpb.MetricDescriptor_INT64
		case *stats.Float64Measure:
			valueType = metricpb.MetricDescriptor_DOUBLE
		}
	default:
		return fmt.Errorf("unsupported aggregation type: %s", agg.Type.String())
	}

	metricKind := metricpb.MetricDescriptor_CUMULATIVE
	displayNamePrefix := defaultDisplayNamePrefix
	if e.o.MetricPrefix != "" {
		displayNamePrefix = e.o.MetricPrefix
	}

	md, err := createMetricDescriptor(ctx, e.c, &monitoringpb.CreateMetricDescriptorRequest{
		Name: fmt.Sprintf("projects/%s", e.o.ProjectID),
		MetricDescriptor: &metricpb.MetricDescriptor{
			Name:        fmt.Sprintf("projects/%s/metricDescriptors/%s", e.o.ProjectID, metricType),
			DisplayName: path.Join(displayNamePrefix, viewName),
			Description: vd.View.Description,
			Unit:        unit,
			Type:        metricType,
			MetricKind:  metricKind,
			ValueType:   valueType,
			Labels:      newLabelDescriptors(vd.View.TagKeys),
		},
	})
	if err != nil {
		return err
	}

	e.createdViews[viewName] = md
	return nil
}

func newPoint(v *view.View, row *view.Row, start, end time.Time) *monitoringpb.Point {
	return &monitoringpb.Point{
		Interval: &monitoringpb.TimeInterval{
			StartTime: &timestamp.Timestamp{
				Seconds: start.Unix(),
				Nanos:   int32(start.Nanosecond()),
			},
			EndTime: &timestamp.Timestamp{
				Seconds: end.Unix(),
				Nanos:   int32(end.Nanosecond()),
			},
		},
		Value: newTypedValue(v, row),
	}
}

func newTypedValue(vd *view.View, r *view.Row) *monitoringpb.TypedValue {
	switch v := r.Data.(type) {
	case *view.CountData:
		return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_Int64Value{
			Int64Value: v.Value,
		}}
	case *view.SumData:
		switch vd.Measure.(type) {
		case *stats.Int64Measure:
			return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_Int64Value{
				Int64Value: int64(v.Value),
			}}
		case *stats.Float64Measure:
			return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DoubleValue{
				DoubleValue: v.Value,
			}}
		}
	case *view.DistributionData:
		return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DistributionValue{
			DistributionValue: &distributionpb.Distribution{
				Count: v.Count,
				Mean:  v.Mean,
				SumOfSquaredDeviation: v.SumOfSquaredDev,
				// TODO(songya): uncomment this once Stackdriver supports min/max.
				// Range: &distributionpb.Distribution_Range{
				// 	Min: v.Min,
				// 	Max: v.Max,
				// },
				BucketOptions: &distributionpb.Distribution_BucketOptions{
					Options: &distributionpb.Distribution_BucketOptions_ExplicitBuckets{
						ExplicitBuckets: &distributionpb.Distribution_BucketOptions_Explicit{
							Bounds: vd.Aggregation.Buckets,
						},
					},
				},
				BucketCounts: v.CountPerBucket,
			},
		}}
	case *view.LastValueData:
		switch vd.Measure.(type) {
		case *stats.Int64Measure:
			return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_Int64Value{
				Int64Value: int64(v.Value),
			}}
		case *stats.Float64Measure:
			return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DoubleValue{
				DoubleValue: v.Value,
			}}
		}
	}
	return nil
}

func namespacedViewName(v string) string {
	return path.Join("custom.googleapis.com", "opencensus", v)
}

func newLabels(tags []tag.Tag, taskValue string) map[string]string {
	labels := make(map[string]string)
	for _, tag := range tags {
		labels[sanitize(tag.Key.Name())] = tag.Value
	}
	labels[opencensusTaskKey] = taskValue
	return labels
}

func newLabelDescriptors(keys []tag.Key) []*labelpb.LabelDescriptor {
	labelDescriptors := make([]*labelpb.LabelDescriptor, len(keys)+1)
	for i, key := range keys {
		labelDescriptors[i] = &labelpb.LabelDescriptor{
			Key:       sanitize(key.Name()),
			ValueType: labelpb.LabelDescriptor_STRING, // We only use string tags
		}
	}
	// Add a specific open census task id label.
	labelDescriptors[len(keys)] = &labelpb.LabelDescriptor{
		Key:         opencensusTaskKey,
		ValueType:   labelpb.LabelDescriptor_STRING,
		Description: opencensusTaskDescription,
	}
	return labelDescriptors
}

func equalMeasureAggTagKeys(md *metricpb.MetricDescriptor, m stats.Measure, agg *view.Aggregation, keys []tag.Key) error {
	var aggTypeMatch bool
	switch md.ValueType {
	case metricpb.MetricDescriptor_INT64:
		if _, ok := m.(*stats.Int64Measure); !(ok || agg.Type == view.AggTypeCount) {
			return fmt.Errorf("stackdriver metric descriptor was not created as int64")
		}
		aggTypeMatch = agg.Type == view.AggTypeCount || agg.Type == view.AggTypeSum || agg.Type == view.AggTypeLastValue
	case metricpb.MetricDescriptor_DOUBLE:
		if _, ok := m.(*stats.Float64Measure); !ok {
			return fmt.Errorf("stackdriver metric descriptor was not created as double")
		}
		aggTypeMatch = agg.Type == view.AggTypeSum || agg.Type == view.AggTypeLastValue
	case metricpb.MetricDescriptor_DISTRIBUTION:
		aggTypeMatch = agg.Type == view.AggTypeDistribution
	}

	if !aggTypeMatch {
		return fmt.Errorf("stackdriver metric descriptor was not created with aggregation type %T", agg.Type)
	}

	if len(md.Labels) != len(keys)+1 {
		return errors.New("stackdriver metric descriptor was not created with the view labels")
	}

	labels := make(map[string]struct{}, len(keys)+1)
	for _, k := range keys {
		labels[sanitize(k.Name())] = struct{}{}
	}
	labels[opencensusTaskKey] = struct{}{}

	for _, k := range md.Labels {
		if _, ok := labels[k.Key]; !ok {
			return fmt.Errorf("stackdriver metric descriptor was not created with label %q", k)
		}
	}

	return nil
}

var createMetricDescriptor = func(ctx context.Context, c *monitoring.MetricClient, mdr *monitoringpb.CreateMetricDescriptorRequest) (*metric.MetricDescriptor, error) {
	return c.CreateMetricDescriptor(ctx, mdr)
}

var getMetricDescriptor = func(ctx context.Context, c *monitoring.MetricClient, mdr *monitoringpb.GetMetricDescriptorRequest) (*metric.MetricDescriptor, error) {
	return c.GetMetricDescriptor(ctx, mdr)
}
