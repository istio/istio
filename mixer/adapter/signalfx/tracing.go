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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/signalfx/golib/errors"
	"github.com/signalfx/golib/sfxclient"
	"github.com/signalfx/golib/trace"
	octrace "go.opencensus.io/trace"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/tracespan"
)

// How long to wait for a response from ingest when sending spans.  New spans will
// buffer during the round trip so we shouldn't wait too long.
const spanSendTimeout = 8 * time.Second

type tracinghandler struct {
	ctx      context.Context
	env      adapter.Env
	sink     *sfxclient.HTTPSink
	spanChan chan *tracespan.Instance
	sampler  octrace.Sampler
}

func (th *tracinghandler) InitTracing(bufferLen int, sampleProbability float64) error {
	th.spanChan = make(chan *tracespan.Instance, bufferLen)

	// Use the OpenCensus span sampler even though we don't use its wire
	// format.
	th.sampler = octrace.ProbabilitySampler(sampleProbability)

	th.env.ScheduleDaemon(th.sendTraces)
	return nil
}

// Pull spans out of spanChan as they come in and send them to SignalFx.
func (th *tracinghandler) sendTraces() {
	for {
		select {
		case <-th.ctx.Done():
			return
		case inst := <-th.spanChan:
			allInsts := th.drainChannel(inst)

			spans := make([]*trace.Span, 0, len(allInsts))
			for i := range allInsts {
				span := convertInstance(allInsts[i])
				if span.ID == "" {
					continue
				}
				spans = append(spans, span)
			}

			ctx, cancel := context.WithTimeout(th.ctx, spanSendTimeout)
			err := th.sink.AddSpans(ctx, spans)
			cancel()
			if err != nil {
				_ = th.env.Logger().Errorf("Could not send spans: %s", err.Error())
			}
		}
	}
}

// Pull spans out of the spanChan until it is empty.  This helps cut down on
// the number of requests to ingest by batching things when traces come in
// fast.
func (th *tracinghandler) drainChannel(initial *tracespan.Instance) (out []*tracespan.Instance) {
	out = []*tracespan.Instance{initial}
	for {
		select {
		case inst := <-th.spanChan:
			out = append(out, inst)
		default:
			return out
		}
	}
}

func (th *tracinghandler) HandleTraceSpan(ctx context.Context, values []*tracespan.Instance) error {
	if th == nil {
		return nil
	}

	for i := range values {
		span := values[i]
		if !th.shouldSend(span) {
			continue
		}

		select {
		case th.spanChan <- span:
			return nil
		default:
			// Just abandon any remaining spans in `values` at this point to
			// help relieve pressure on the buffer
			return errors.New("dropping span because trace buffer is full -- you should increase the capacity of it")
		}
	}
	return nil
}

func (th *tracinghandler) shouldSend(span *tracespan.Instance) bool {
	parentContext, ok := adapter.ExtractParentContext(span.TraceId, span.ParentSpanId)
	if !ok {
		return false
	}
	spanContext, ok := adapter.ExtractSpanContext(span.SpanId, parentContext)
	if !ok {
		return false
	}

	params := octrace.SamplingParameters{
		ParentContext:   parentContext,
		TraceID:         spanContext.TraceID,
		SpanID:          spanContext.SpanID,
		Name:            span.SpanName,
		HasRemoteParent: true,
	}
	return th.sampler(params).Sample
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
	if istioSpan.ClientSpan {
		kind = &clientKind
	}

	tags := map[string]string{}

	if labels, ok := istioSpan.SpanTags["destination.labels"].(map[string]string); ok {
		for k, v := range labels {
			if k != "" && v != "" {
				tags["destination.labels."+k] = v
			}
		}
	}

	if labels, ok := istioSpan.SpanTags["source.labels"].(map[string]string); ok {
		for k, v := range labels {
			if k != "" && v != "" {
				tags["source.labels."+k] = v
			}
		}
	}

	// Special handling for the span name since the Istio attribute that is the
	// most suited for the span name is request.path which can contain query
	// params which could cause high cardinality of the span name.
	spanName := istioSpan.SpanName
	if strings.Contains(spanName, "?") {
		idx := strings.LastIndex(spanName, "?")
		qs := spanName[idx+1:]
		if vals, err := url.ParseQuery(qs); err == nil {
			for k, v := range vals {
				tags[k] = strings.Join(v, "; ")
			}
		}

		spanName = spanName[:idx]
	}

	for k, v := range istioSpan.SpanTags {
		shouldSet := k != "context.reporter.type" &&
			k != "source.labels" &&
			k != "destination.labels"

		if s := adapter.Stringify(v); s != "" && shouldSet {
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
		Name:           &spanName,
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
