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

// Package zipkin contains an trace exporter for Zipkin.
package zipkin // import "contrib.go.opencensus.io/exporter/zipkin"

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/reporter"
	"go.opencensus.io/trace"
)

// Exporter is an implementation of trace.Exporter that uploads spans to a
// Zipkin server.
type Exporter struct {
	reporter      reporter.Reporter
	localEndpoint *model.Endpoint
}

// NewExporter returns an implementation of trace.Exporter that uploads spans
// to a Zipkin server.
//
// reporter is a Zipkin Reporter which will be used to send the spans.  These
// can be created with the openzipkin library, using one of the packages under
// github.com/openzipkin/zipkin-go/reporter.
//
// localEndpoint sets the local endpoint of exported spans.  It can be
// constructed with github.com/openzipkin/zipkin-go.NewEndpoint, e.g.:
// 	localEndpoint, err := NewEndpoint("my server", listener.Addr().String())
// localEndpoint can be nil.
func NewExporter(reporter reporter.Reporter, localEndpoint *model.Endpoint) *Exporter {
	return &Exporter{
		reporter:      reporter,
		localEndpoint: localEndpoint,
	}
}

// ExportSpan exports a span to a Zipkin server.
func (e *Exporter) ExportSpan(s *trace.SpanData) {
	e.reporter.Send(zipkinSpan(s, e.localEndpoint))
}

const (
	statusCodeTagKey        = "error"
	statusDescriptionTagKey = "opencensus.status_description"
)

var (
	sampledTrue    = true
	canonicalCodes = [...]string{
		"OK",
		"CANCELLED",
		"UNKNOWN",
		"INVALID_ARGUMENT",
		"DEADLINE_EXCEEDED",
		"NOT_FOUND",
		"ALREADY_EXISTS",
		"PERMISSION_DENIED",
		"RESOURCE_EXHAUSTED",
		"FAILED_PRECONDITION",
		"ABORTED",
		"OUT_OF_RANGE",
		"UNIMPLEMENTED",
		"INTERNAL",
		"UNAVAILABLE",
		"DATA_LOSS",
		"UNAUTHENTICATED",
	}
)

func canonicalCodeString(code int32) string {
	if code < 0 || int(code) >= len(canonicalCodes) {
		return "error code " + strconv.FormatInt(int64(code), 10)
	}
	return canonicalCodes[code]
}

func convertTraceID(t trace.TraceID) model.TraceID {
	return model.TraceID{
		High: binary.BigEndian.Uint64(t[:8]),
		Low:  binary.BigEndian.Uint64(t[8:]),
	}
}

func convertSpanID(s trace.SpanID) model.ID {
	return model.ID(binary.BigEndian.Uint64(s[:]))
}

func spanKind(s *trace.SpanData) model.Kind {
	switch s.SpanKind {
	case trace.SpanKindClient:
		return model.Client
	case trace.SpanKindServer:
		return model.Server
	}
	return model.Undetermined
}

func zipkinSpan(s *trace.SpanData, localEndpoint *model.Endpoint) model.SpanModel {
	sc := s.SpanContext
	z := model.SpanModel{
		SpanContext: model.SpanContext{
			TraceID: convertTraceID(sc.TraceID),
			ID:      convertSpanID(sc.SpanID),
			Sampled: &sampledTrue,
		},
		Kind:          spanKind(s),
		Name:          s.Name,
		Timestamp:     s.StartTime,
		Shared:        false,
		LocalEndpoint: localEndpoint,
	}

	if s.ParentSpanID != (trace.SpanID{}) {
		id := convertSpanID(s.ParentSpanID)
		z.ParentID = &id
	}

	if s, e := s.StartTime, s.EndTime; !s.IsZero() && !e.IsZero() {
		z.Duration = e.Sub(s)
	}

	// construct Tags from s.Attributes and s.Status.
	if len(s.Attributes) != 0 {
		m := make(map[string]string, len(s.Attributes)+2)
		for key, value := range s.Attributes {
			switch v := value.(type) {
			case string:
				m[key] = v
			case bool:
				if v {
					m[key] = "true"
				} else {
					m[key] = "false"
				}
			case int64:
				m[key] = strconv.FormatInt(v, 10)
			case float64:
				m[key] = strconv.FormatFloat(v, 'f', -1, 64)
			}
		}
		z.Tags = m
	}
	if s.Status.Code != 0 || s.Status.Message != "" {
		if z.Tags == nil {
			z.Tags = make(map[string]string, 2)
		}
		if s.Status.Code != 0 {
			z.Tags[statusCodeTagKey] = canonicalCodeString(s.Status.Code)
		}
		if s.Status.Message != "" {
			z.Tags[statusDescriptionTagKey] = s.Status.Message
		}
	}

	// construct Annotations from s.Annotations and s.MessageEvents.
	if len(s.Annotations) != 0 || len(s.MessageEvents) != 0 {
		z.Annotations = make([]model.Annotation, 0, len(s.Annotations)+len(s.MessageEvents))
		for _, a := range s.Annotations {
			z.Annotations = append(z.Annotations, model.Annotation{
				Timestamp: a.Time,
				Value:     a.Message,
			})
		}
		for _, m := range s.MessageEvents {
			a := model.Annotation{
				Timestamp: m.Time,
			}
			switch m.EventType {
			case trace.MessageEventTypeSent:
				a.Value = fmt.Sprintf("Sent %d bytes", m.UncompressedByteSize)
			case trace.MessageEventTypeRecv:
				a.Value = fmt.Sprintf("Received %d bytes", m.UncompressedByteSize)
			default:
				a.Value = "<?>"
			}
			z.Annotations = append(z.Annotations, a)
		}
	}

	return z
}
