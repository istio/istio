// Copyright 2018 the Istio Authors.
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

// Package opencensus contains support code for writing adapters that use OpenCensus.
package opencensus

import (
	"context"
	"fmt"
	"sync"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"

	"istio.io/istio/mixer/template/tracespan"
)

const attrHTTPStatusCode = "http.status_code"

// Handler implements tracespan.Handler using an OpenCensus TraceExporter.
type Handler struct {
	exporters sync.Map
	// NewExporter creates OpenCensus exporters where generated spans will be exported.
	NewExporter NewExporterFunc
	// Sampler to be applied to each span.
	Sampler trace.Sampler
	// CloseFunc will be called when this handler is closed. Optional.
	CloseFunc func() error
}

type exporterKey struct {
	name     string
	endpoint string
}

var _ tracespan.Handler = (*Handler)(nil) // type assertion

// NewExporterFunc is a callback to create new OpenCensus exporters for a given
// workload name and endpoint.
//
// The format of endpoint will be IP_ADDRESS:PORT. name and endpoint refer to
// the logical owning workload of this span. For server spans, the values are the
// destinationName and destinationIp, for clients the sourceName and sourceIp.
type NewExporterFunc func(name string, endpoint string) trace.Exporter

// NewTraceHandler returns a new tracespan adapter that sends spans to the provided
// exporter.
func NewTraceHandler(newExporter NewExporterFunc, sampler trace.Sampler) *Handler {
	return &Handler{
		NewExporter: newExporter,
		Sampler:     sampler,
	}
}

// HandleTraceSpan transforms tracespan template instances into OpenCensus spans
// and sends them to the configured OpenCensus exporter.
func (h *Handler) HandleTraceSpan(_ context.Context, values []*tracespan.Instance) (retErr error) {
	if h.Sampler == nil {
		// Tracing is not configured.
		return nil
	}

	for _, val := range values {
		parentContext, ok := ExtractParentContext(val.TraceId, val.ParentSpanId)
		if !ok {
			continue
		}
		spanContext, ok := ExtractSpanContext(val.SpanId, parentContext)
		if !ok {
			continue
		}

		decision := h.Sampler(trace.SamplingParameters{
			ParentContext:   parentContext,
			TraceID:         spanContext.TraceID,
			SpanID:          spanContext.SpanID,
			Name:            val.SpanName,
			HasRemoteParent: true,
		})

		if !decision.Sample {
			continue
		}
		spanContext.TraceOptions = trace.TraceOptions(1 /*sampled*/)

		span := buildSpanData(val, parentContext, spanContext)
		h.exporter(val).ExportSpan(span)
	}

	return
}

func (h *Handler) exporter(val *tracespan.Instance) trace.Exporter {
	key := exporterKey{
		name:     "default",
		endpoint: "0.0.0.0:0",
	}
	if val.ClientSpan {
		if val.SourceName != "" {
			key.name = val.SourceName
		}
		if val.SourceIp != nil {
			key.endpoint = fmt.Sprintf("%s:0", val.SourceIp)
		}
	} else {
		if val.DestinationName != "" {
			key.name = val.DestinationName
		}
		if val.DestinationIp != nil {
			key.endpoint = fmt.Sprintf("%s:0", val.DestinationIp)
		}
	}
	e, ok := h.exporters.Load(key)
	if !ok {
		newVal := h.NewExporter(key.name, key.endpoint)
		e, _ = h.exporters.LoadOrStore(key, newVal)
	}
	return e.(trace.Exporter)
}

func buildSpanData(val *tracespan.Instance, parentContext trace.SpanContext, spanContext trace.SpanContext) *trace.SpanData {
	attributes := make(map[string]interface{})

	var status trace.Status
	if val.HttpStatusCode > 0 {
		attributes[attrHTTPStatusCode] = val.HttpStatusCode
		status = ochttp.TraceStatus(int(val.HttpStatusCode), "")
	}

	for k, v := range val.SpanTags {
		switch x := v.(type) {
		case string, int64, float64:
			attributes[k] = x
		default:
			attributes[k] = fmt.Sprintf("%v", x)
		}
	}

	var spanKind int
	var hasRemoteParent bool
	parentSpanID := parentContext.SpanID
	spanID := spanContext.SpanID
	if val.ClientSpan {
		spanKind = trace.SpanKindClient
		// If this is a client span and rewriteClientSpanId is true, deterministically create a new span
		// ID and rewrite span id to that one. This id should also be used as server span's parent span
		// id.
		if val.RewriteClientSpanId {
			spanID = rewriteSpanID(spanID)
		}
	} else {
		spanKind = trace.SpanKindServer
		hasRemoteParent = true
		// If this is a server span and rewriteClientSpanId is true, deterministically create a new span
		// ID and rewrite parent id to that one, which makes this span attached to the client span as a
		// child span.
		if val.RewriteClientSpanId {
			parentSpanID = rewriteSpanID(spanID)
		}
	}

	span := &trace.SpanData{
		SpanKind:     spanKind,
		Name:         val.SpanName,
		StartTime:    val.StartTime,
		EndTime:      val.EndTime,
		ParentSpanID: parentSpanID,
		SpanContext: trace.SpanContext{
			TraceOptions: spanContext.TraceOptions,
			TraceID:      spanContext.TraceID,
			SpanID:       spanID,
		},
		HasRemoteParent: hasRemoteParent,
		Status:          status,
		Attributes:      attributes,
		MessageEvents:   extractMessageEvents(val),
	}

	return span
}

var pad = [8]byte{0x3f, 0x6a, 0x2e, 0xc3, 0xc8, 0x10, 0xc2, 0xab}

// rewriteSpanID deterministically creates a new span id base on the given span id by XOR with a pad.
func rewriteSpanID(spanID trace.SpanID) trace.SpanID {
	var newID trace.SpanID
	for i, b := range spanID {
		newID[i] = b ^ pad[i]
	}
	return newID
}

func extractMessageEvents(ts *tracespan.Instance) []trace.MessageEvent {
	var requestMessageType, responseMessageType trace.MessageEventType
	if ts.ClientSpan {
		requestMessageType, responseMessageType = trace.MessageEventTypeSent, trace.MessageEventTypeRecv
	} else {
		requestMessageType, responseMessageType = trace.MessageEventTypeRecv, trace.MessageEventTypeSent
	}
	return []trace.MessageEvent{
		{
			EventType:            requestMessageType,
			UncompressedByteSize: ts.RequestTotalSize,
			CompressedByteSize:   ts.RequestTotalSize,
			Time:                 ts.StartTime,
		},
		{
			EventType:            responseMessageType,
			UncompressedByteSize: ts.ResponseTotalSize,
			CompressedByteSize:   ts.ResponseTotalSize,
			Time:                 ts.EndTime,
		},
	}
}

// Close calls the CloseFunc (if any) that was provided when this Handler was
// created.
func (h *Handler) Close() error {
	if h.CloseFunc != nil {
		return h.CloseFunc()
	}
	return nil
}
