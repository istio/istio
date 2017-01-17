// Copyright 2017 Google Inc.
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

	xctx "golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/golang/glog"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

// TODO: Keep track of per-stream state, e.g. the Tracer impl to use in traces for this stream. This will enable things
// like different tracers per stream (client). Currently this package is built on the assumption that only the global
// tracer is used, which isn't great (it makes testing harder, for example). This state could also be used to keep
// track of per stream config like metadata propagation format.
//
// TODO: investigate wrapping the server stream in one with a mutable context, so that we can use an interceptor or TAP
// handler to set up the root span rather than doing it in the server's stream loop.
//
// TODO: investigate merging some of this code with https://github.com/grpc-ecosystem/grpc-opentracing/

var (
	noopSpan = ot.NoopTracer{}.StartSpan("")

	gRPCComponentTag = ot.Tag{string(ext.Component), "gRPC"}
)

// ClientInterceptor establishes a span that lives for the entire lifetime of the server-client stream and propagates
// it via gRPC request metadata to the client.
func ClientInterceptor(tracer ot.Tracer) grpc.StreamClientInterceptor {
	return func(
		ctx xctx.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption) (grpc.ClientStream, error) {

		var span ot.Span
		if p := ot.SpanFromContext(ctx); p != nil {
			span = tracer.StartSpan(method, ext.SpanKindRPCClient, ot.ChildOf(p.Context()))
		} else {
			span = tracer.StartSpan(method, ext.SpanKindRPCClient)
		}
		defer span.Finish()

		ctx = ot.ContextWithSpan(ctx, span)
		ctx = propagateSpan(ctx, tracer, span)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

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

// StartRootSpan creates a span that is the root of all Istio spans in the current request context. This span will be a
// child of any spans propagated to the server in the request's metadata. The returned span is retrievable from the
// context via tracing.RootSpan.
func StartRootSpan(ctx context.Context, operationName string) (ot.Span, context.Context) {
	md := extractMetadata(ctx)
	spanContext, err := ot.GlobalTracer().Extract(ot.TextMap, metadataReaderWriter{md})
	if err != nil {
		glog.Warningf("Failed to extract opentracing metadata with tracer %v from ctx %v with err %v", ot.GlobalTracer(), ctx, err)

		// We set the spancontext to nil if there's an error so we don't get funky values from ext.RPCServerOption
		// (in particular the mock tracer impl doesn't handle a non nil empty span context gracefully).
		spanContext = nil
	}
	span, ctx := ot.StartSpanFromContext(ctx, operationName, ext.RPCServerOption(spanContext), gRPCComponentTag)
	ctx = context.WithValue(ctx, rootSpanKey{}, span)
	return span, ctx
}

// propagateSpan inserts metadata about the span into the context's metadata so that the span is propagated to the receiver.
// This should be used to prepare the context for outgoing calls.
//
// TODO: consider creating a public version of this method for use at individual call sites if the interceptor isn't
// sufficient for some reason.
func propagateSpan(ctx context.Context, tracer ot.Tracer, span ot.Span) context.Context {
	md := extractMetadata(ctx)
	if err := tracer.Inject(span.Context(), ot.TextMap, metadataReaderWriter{md}); err != nil {
		glog.Warningf("Failed to inject opentracing span state with tracer %v into ctx %v with err %v", ot.GlobalTracer(), ctx, err)
	}
	return metadata.NewContext(ctx, md)
}

// extractMetadata pulls the GRPC metadata out of the context or returns a new empty metadata object if there isn't one
// in the context.
func extractMetadata(ctx context.Context) metadata.MD {
	if md, ok := metadata.FromContext(ctx); ok {
		return md
	}
	return metadata.New(nil)
}
