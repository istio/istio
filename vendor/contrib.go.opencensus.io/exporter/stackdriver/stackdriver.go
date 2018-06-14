// Copyright 2018, OpenCensus Authors
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

// Package stackdriver contains the OpenCensus exporters for
// Stackdriver Monitoring and Stackdriver Tracing.
//
// Please note that the Stackdriver exporter is currently experimental.
//
// The package uses Application Default Credentials to authenticate.  See
// https://developers.google.com/identity/protocols/application-default-credentials
package stackdriver // import "contrib.go.opencensus.io/exporter/stackdriver"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	traceapi "cloud.google.com/go/trace/apiv2"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	monitoredrespb "google.golang.org/genproto/googleapis/api/monitoredres"
)

// Options contains options for configuring the exporter.
type Options struct {
	// ProjectID is the identifier of the Stackdriver
	// project the user is uploading the stats data to.
	// If not set, this will default to your "Application Default Credentials".
	// For details see: https://developers.google.com/accounts/docs/application-default-credentials
	ProjectID string

	// OnError is the hook to be called when there is
	// an error uploading the stats or tracing data.
	// If no custom hook is set, errors are logged.
	// Optional.
	OnError func(err error)

	// MonitoringClientOptions are additional options to be passed
	// to the underlying Stackdriver Monitoring API client.
	// Optional.
	MonitoringClientOptions []option.ClientOption

	// TraceClientOptions are additional options to be passed
	// to the underlying Stackdriver Trace API client.
	// Optional.
	TraceClientOptions []option.ClientOption

	// BundleDelayThreshold determines the max amount of time
	// the exporter can wait before uploading view data to
	// the backend.
	// Optional.
	BundleDelayThreshold time.Duration

	// BundleCountThreshold determines how many view data events
	// can be buffered before batch uploading them to the backend.
	// Optional.
	BundleCountThreshold int

	// Resource is an optional field that represents the Stackdriver
	// MonitoredResource, a resource that can be used for monitoring.
	// If no custom ResourceDescriptor is set, a default MonitoredResource
	// with type global and no resource labels will be used.
	// Optional.
	Resource *monitoredrespb.MonitoredResource

	// MetricPrefix overrides the OpenCensus prefix of a stackdriver metric.
	// Optional.
	MetricPrefix string

	// DefaultTraceAttributes will be appended to every span that is exported.
	DefaultTraceAttributes map[string]interface{}
}

// Exporter is a stats.Exporter and trace.Exporter
// implementation that uploads data to Stackdriver.
type Exporter struct {
	traceExporter *traceExporter
	statsExporter *statsExporter
}

// NewExporter creates a new Exporter that implements both stats.Exporter and
// trace.Exporter.
func NewExporter(o Options) (*Exporter, error) {
	if o.ProjectID == "" {
		creds, err := google.FindDefaultCredentials(context.Background(), traceapi.DefaultAuthScopes()...)
		if err != nil {
			return nil, fmt.Errorf("stackdriver: %v", err)
		}
		if creds.ProjectID == "" {
			return nil, errors.New("stackdriver: no project found with application default credentials")
		}
		o.ProjectID = creds.ProjectID
	}
	se, err := newStatsExporter(o)
	if err != nil {
		return nil, err
	}
	te, err := newTraceExporter(o)
	if err != nil {
		return nil, err
	}
	return &Exporter{
		statsExporter: se,
		traceExporter: te,
	}, nil
}

// ExportView exports to the Stackdriver Monitoring if view data
// has one or more rows.
func (e *Exporter) ExportView(vd *view.Data) {
	e.statsExporter.ExportView(vd)
}

// ExportSpan exports a SpanData to Stackdriver Trace.
func (e *Exporter) ExportSpan(sd *trace.SpanData) {
	if len(e.traceExporter.o.DefaultTraceAttributes) > 0 {
		sd = e.sdWithDefaultTraceAttributes(sd)
	}
	e.traceExporter.ExportSpan(sd)
}

func (e *Exporter) sdWithDefaultTraceAttributes(sd *trace.SpanData) *trace.SpanData {
	newSD := *sd
	newSD.Attributes = make(map[string]interface{})
	for k, v := range e.traceExporter.o.DefaultTraceAttributes {
		newSD.Attributes[k] = v
	}
	for k, v := range sd.Attributes {
		newSD.Attributes[k] = v
	}
	return &newSD
}

// Flush waits for exported data to be uploaded.
//
// This is useful if your program is ending and you do not
// want to lose recent stats or spans.
func (e *Exporter) Flush() {
	e.statsExporter.Flush()
	e.traceExporter.Flush()
}

func (o Options) handleError(err error) {
	if o.OnError != nil {
		o.OnError(err)
		return
	}
	log.Printf("Error exporting to Stackdriver: %v", err)
}
