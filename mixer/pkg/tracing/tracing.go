// Copyright 2017 Istio Authors
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

// Package tracing provides wrappers around opentracing's golang library to make them easier to use in Istio.
package tracing

import (
	"context"

	"github.com/golang/glog"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"google.golang.org/grpc/metadata"
)

// TODO: investigate wrapping the server stream in one with a mutable context, so that we can use an interceptor or TAP
// handler to set up the root span rather than doing it in the server's stream loop.
//
// TODO: investigate merging some of this code with https://github.com/grpc-ecosystem/grpc-opentracing/

var (
	noopSpan = ot.NoopTracer{}.StartSpan("")

	gRPCComponentTag = ot.Tag{Key: string(ext.Component), Value: "gRPC"}
)

// CurrentSpan extracts the current span from the context, or returns a no-op span if no span exists in the context.
// This avoids boilerplate nil checking when calling opentracing.SpanFromContext.
func CurrentSpan(ctx context.Context) ot.Span {
	// TODO: figure out how to handle a span not existing in the context: this shouldn't happen because a root trace
	// is always created for the life of a stream. (Possible cause would be someone creating a new context, or not
	// propagating the context correctly.)
	if current := ot.SpanFromContext(ctx); current != nil {
		return current
	}
	return noopSpan
}

type rootSpanKey struct{}

// RootSpan retrieves the root span from the context. This may be different than the current context returned by
// opentracing.SpanFromContext(ctx)
func RootSpan(ctx context.Context) ot.Span {
	val := ctx.Value(rootSpanKey{})
	if root, ok := val.(ot.Span); ok {
		return root
	}
	return noopSpan
}

// Tracer wraps an opentracing.Tracer with a flag that's enabled when tracing is enabled. This flag is used to short-circuit
// methods that may be costly (intercepting client calls, propagating span metadata, etc).
// The tracer's methods will always return objects that are safe to work with, e.g. if tracing is disabled and StartRootSpan
// is called, a no-op span will be returned, not nil.
type Tracer struct {
	ot.Tracer

	enabled bool

	// this indirection exists strictly to enable error injection
	inject func(sm ot.SpanContext, format interface{}, carrier interface{}) error
}

// NewTracer wraps the provided tracer and enables tracing.
func NewTracer(tracer ot.Tracer) Tracer {
	return Tracer{
		Tracer:  tracer,
		enabled: true,
		inject:  tracer.Inject,
	}
}

// DisabledTracer disables tracing, letting methods in tracing.go short-circuit.
func DisabledTracer() Tracer {
	return Tracer{
		Tracer:  ot.NoopTracer{},
		enabled: false,
		inject:  nil,
	}
}

// StartRootSpan creates a span that is the root of all Istio spans in the current request context. This span will be a
// child of any spans propagated to the server in the request's metadata. The returned span is retrievable from the
// context via tracing.RootSpan.
func (t *Tracer) StartRootSpan(ctx context.Context, operationName string, opts ...ot.StartSpanOption) (ot.Span, context.Context) {
	if !t.enabled {
		return noopSpan, ctx
	}
	spanContext := t.extractSpanContext(ctx, extractMetadata(ctx))
	opts = append(opts, ext.RPCServerOption(spanContext), gRPCComponentTag)
	span, ctx := t.StartSpanFromContext(ctx, operationName, opts...)
	ctx = context.WithValue(ctx, rootSpanKey{}, span)
	return span, ctx
}

// StartSpanFromContext starts a new span and propagates it in ctx. If there exists a span in ctx it will be the parent of
// the returned span.
//
// We provide this method, which should be used instead of opentracing.StartSpanFromContext because opentracing's version
// always uses the global tracer, while we use the opentracing.Tracer stored in t.
func (t *Tracer) StartSpanFromContext(ctx context.Context, operationName string, opts ...ot.StartSpanOption) (ot.Span, context.Context) {
	if !t.enabled {
		return noopSpan, ctx
	}

	if parentSpan := ot.SpanFromContext(ctx); parentSpan != nil {
		opts = append(opts, ot.ChildOf(parentSpan.Context()))
	}
	span := t.StartSpan(operationName, opts...)
	return span, ot.ContextWithSpan(ctx, span)
}

// PropagateSpan inserts metadata about the span into the context's metadata so that the span is propagated to the receiver.
// This should be used to prepare the context for outgoing calls.
func (t *Tracer) PropagateSpan(ctx context.Context, span ot.Span) (metadata.MD, context.Context) {
	md := extractMetadata(ctx)
	if !t.enabled {
		return md, ctx
	}

	if err := t.inject(span.Context(), ot.TextMap, metadataReaderWriter{md}); err != nil {
		glog.Warningf("Failed to inject opentracing span state with tracer %v into ctx %v with err %v", t.Tracer, ctx, err)
	}
	return md, metadata.NewContext(ctx, md)
}

func (t *Tracer) extractSpanContext(ctx context.Context, md metadata.MD) ot.SpanContext {
	if !t.enabled {
		return nil
	}

	spanContext, err := t.Extract(ot.TextMap, metadataReaderWriter{md})
	if err != nil {
		glog.Warningf("Failed to extract opentracing metadata with tracer %v from ctx %v with err %v", t.Tracer, ctx, err)
		// We set the spancontext to nil if there's an error so we don't get funky values from ext.RPCServerOption
		// (in particular the mock tracer impl doesn't handle a non nil empty span context gracefully).
		spanContext = nil
	}
	return spanContext
}

// extractMetadata pulls the GRPC metadata out of the context or returns a new empty metadata object if there isn't one
// in the context.
func extractMetadata(ctx context.Context) metadata.MD {
	if md, ok := metadata.FromContext(ctx); ok {
		return md
	}
	return metadata.New(nil)
}
