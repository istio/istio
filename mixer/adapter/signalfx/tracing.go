// Copyright 2018 Istio Authors
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

package signalfx

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/signalfx/golib/errors"
	"github.com/signalfx/golib/sfxclient"
	"github.com/signalfx/golib/trace"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/tracespan"
)

type tracinghandler struct {
	ctx        context.Context
	env        adapter.Env
	sink       *sfxclient.HTTPSink
	tracesChan chan []*tracespan.Instance
}

func (th *tracinghandler) InitTracing(bufferLen int) error {
	th.tracesChan = make(chan []*tracespan.Instance, bufferLen)

	th.env.ScheduleDaemon(th.sendTraces)
	return nil
}

// Pull spans out of tracesChan as they come in and send them to SignalFx.
func (th *tracinghandler) sendTraces() {
	for {
		select {
		case <-th.ctx.Done():
			return
		case insts := <-th.tracesChan:
			allInsts := th.drainChannel(insts)

			spans := make([]*trace.Span, 0, len(allInsts))
			for i := range allInsts {
				span := convertInstance(allInsts[i])
				if span.ID == "" {
					continue
				}
				spans = append(spans, span)
			}

			ctx, cancel := context.WithTimeout(th.ctx, 8*time.Second)
			err := th.sink.AddSpans(ctx, spans)
			cancel()
			if err != nil {
				_ = th.env.Logger().Errorf("Could not send spans to SignalFx: %s", err.Error())
			}
		}
	}
}

// Pull spans out of the tracesChan until it is empty.  This helps cut down on
// the number of requests to ingest by batching things when traces come in
// fast.
func (th *tracinghandler) drainChannel(initial []*tracespan.Instance) (out []*tracespan.Instance) {
	out = append(out, initial...)
	for {
		select {
		case insts := <-th.tracesChan:
			out = append(out, insts...)
		default:
			return out
		}
	}
}

// metric.Handler#HandleTraceSpan
func (th *tracinghandler) HandleTraceSpan(ctx context.Context, values []*tracespan.Instance) error {
	if th == nil {
		return nil
	}

	select {
	case th.tracesChan <- values:
		return nil
	default:
		return errors.New("dropping span because trace buffer is full -- you should increase the capacity of it")
	}
}

var (
	clientKind = "CLIENT"
	serverKind = "SERVER"
)

// Converts an istio span to a SignalFx span, which is currently equivalent to
// a Zipkin V2 span.
func convertInstance(istioSpan *tracespan.Instance) *trace.Span {
	startTime := float64(istioSpan.StartTime.UnixNano()) / 1000
	endTime := float64(istioSpan.EndTime.UnixNano()) / 1000
	duration := endTime - startTime

	kind := &serverKind
	// ClientSpan doesn't seem to populate reliably yet so fall back on a tag
	if isLocal, ok := istioSpan.SpanTags["context.reporter.local"].(bool); istioSpan.ClientSpan || (ok && !isLocal) {
		kind = &clientKind
	}

	tags := map[string]string{}

	if labels, ok := istioSpan.SpanTags["destination.labels"].(map[string]string); ok {
		for k, v := range labels {
			if k != "" && v != "" {
				tags["destination.labels."+k] = v
			}
		}
		delete(istioSpan.SpanTags, "destination.labels")
	}

	if labels, ok := istioSpan.SpanTags["source.labels"].(map[string]string); ok {
		for k, v := range labels {
			if k != "" && v != "" {
				tags["source.labels."+k] = v
			}
		}
		delete(istioSpan.SpanTags, "source.labels")
	}

	for k, v := range istioSpan.SpanTags {
		if s := adapter.Stringify(v); s != "" && k != "context.reporter.local" {
			tags[k] = s
		}
	}

	if istioSpan.HttpStatusCode != 0 {
		tags["httpStatusCode"] = strconv.FormatInt(istioSpan.HttpStatusCode, 10)
		if istioSpan.HttpStatusCode >= 500 {
			tags["error"] = "server error"
		}
	}

	span := &trace.Span{
		ID:             istioSpan.SpanId,
		Name:           &istioSpan.SpanName,
		TraceID:        istioSpan.TraceId,
		Kind:           kind,
		Timestamp:      &startTime,
		Duration:       &duration,
		Tags:           tags,
		LocalEndpoint:  &trace.Endpoint{},
		RemoteEndpoint: &trace.Endpoint{},
	}

	if n, ok := istioSpan.SpanTags["source.name"].(string); ok && n != "" {
		span.LocalEndpoint.ServiceName = &n
	}

	if ip, ok := istioSpan.SpanTags["source.ip"].(net.IP); ok {
		ips := ip.String()
		span.LocalEndpoint.Ipv4 = &ips
	}

	if n, ok := istioSpan.SpanTags["destination.name"].(string); ok && n != "" {
		span.RemoteEndpoint.ServiceName = &n
	}

	if ip, ok := istioSpan.SpanTags["destination.ip"].(net.IP); ok {
		ips := ip.String()
		span.RemoteEndpoint.Ipv4 = &ips
	}

	if istioSpan.ParentSpanId != "" {
		span.ParentID = &istioSpan.ParentSpanId
	}

	return span
}
