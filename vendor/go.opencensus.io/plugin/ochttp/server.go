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

package ochttp

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"go.opencensus.io/trace/propagation"
)

// Handler is a http.Handler that is aware of the incoming request's span.
//
// The extracted span can be accessed from the incoming request's
// context.
//
//    span := trace.FromContext(r.Context())
//
// The server span will be automatically ended at the end of ServeHTTP.
//
// Incoming propagation mechanism is determined by the given HTTP propagators.
type Handler struct {
	// Propagation defines how traces are propagated. If unspecified,
	// B3 propagation will be used.
	Propagation propagation.HTTPFormat

	// Handler is the handler used to handle the incoming request.
	Handler http.Handler

	// StartOptions are applied to the span started by this Handler around each
	// request.
	//
	// StartOptions.SpanKind will always be set to trace.SpanKindServer
	// for spans started by this transport.
	StartOptions trace.StartOptions

	// IsPublicEndpoint should be set to true for publicly accessible HTTP(S)
	// servers. If true, any trace metadata set on the incoming request will
	// be added as a linked trace instead of being added as a parent of the
	// current trace.
	IsPublicEndpoint bool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var traceEnd, statsEnd func()
	r, traceEnd = h.startTrace(w, r)
	defer traceEnd()
	w, statsEnd = h.startStats(w, r)
	defer statsEnd()

	handler := h.Handler
	if handler == nil {
		handler = http.DefaultServeMux
	}
	handler.ServeHTTP(w, r)
}

func (h *Handler) startTrace(w http.ResponseWriter, r *http.Request) (*http.Request, func()) {
	opts := trace.StartOptions{
		Sampler:  h.StartOptions.Sampler,
		SpanKind: trace.SpanKindServer,
	}

	name := spanNameFromURL(r.URL)
	ctx := r.Context()
	var span *trace.Span
	sc, ok := h.extractSpanContext(r)
	if ok && !h.IsPublicEndpoint {
		span = trace.NewSpanWithRemoteParent(name, sc, opts)
		ctx = trace.WithSpan(ctx, span)
	} else {
		span = trace.NewSpan(name, nil, opts)
		if ok {
			span.AddLink(trace.Link{
				TraceID:    sc.TraceID,
				SpanID:     sc.SpanID,
				Type:       trace.LinkTypeChild,
				Attributes: nil,
			})
		}
	}
	ctx = trace.WithSpan(ctx, span)
	span.AddAttributes(requestAttrs(r)...)
	return r.WithContext(trace.WithSpan(r.Context(), span)), span.End
}

func (h *Handler) extractSpanContext(r *http.Request) (trace.SpanContext, bool) {
	if h.Propagation == nil {
		return defaultFormat.SpanContextFromRequest(r)
	}
	return h.Propagation.SpanContextFromRequest(r)
}

func (h *Handler) startStats(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	ctx, _ := tag.New(r.Context(),
		tag.Upsert(Host, r.URL.Host),
		tag.Upsert(Path, r.URL.Path),
		tag.Upsert(Method, r.Method))
	track := &trackingResponseWriter{
		start:  time.Now(),
		ctx:    ctx,
		writer: w,
	}
	if r.Body == nil {
		// TODO: Handle cases where ContentLength is not set.
		track.reqSize = -1
	} else if r.ContentLength > 0 {
		track.reqSize = r.ContentLength
	}
	stats.Record(ctx, ServerRequestCount.M(1))
	return track, track.end
}

type trackingResponseWriter struct {
	ctx        context.Context
	reqSize    int64
	respSize   int64
	start      time.Time
	statusCode int
	endOnce    sync.Once
	writer     http.ResponseWriter
}

var _ http.ResponseWriter = (*trackingResponseWriter)(nil)

func (t *trackingResponseWriter) end() {
	t.endOnce.Do(func() {
		if t.statusCode == 0 {
			t.statusCode = 200
		}
		m := []stats.Measurement{
			ServerLatency.M(float64(time.Since(t.start)) / float64(time.Millisecond)),
			ServerResponseBytes.M(t.respSize),
		}
		if t.reqSize >= 0 {
			m = append(m, ServerRequestBytes.M(t.reqSize))
		}
		ctx, _ := tag.New(t.ctx, tag.Upsert(StatusCode, strconv.Itoa(t.statusCode)))
		stats.Record(ctx, m...)
	})
}

func (t *trackingResponseWriter) Header() http.Header {
	return t.writer.Header()
}

func (t *trackingResponseWriter) Write(data []byte) (int, error) {
	n, err := t.writer.Write(data)
	t.respSize += int64(n)
	return n, err
}

func (t *trackingResponseWriter) WriteHeader(statusCode int) {
	t.writer.WriteHeader(statusCode)
	t.statusCode = statusCode
}

func (t *trackingResponseWriter) Flush() {
	if flusher, ok := t.writer.(http.Flusher); ok {
		flusher.Flush()
	}
}
