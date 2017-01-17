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

package tracing

import (
	"context"
	"testing"

	xctx "golang.org/x/net/context"

	"google.golang.org/grpc/metadata"

	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
	"google.golang.org/grpc"
)

func init() {
	ot.SetGlobalTracer(mocktracer.New())
}

func TestCurrentSpan(t *testing.T) {
	ctx := context.Background()
	if span := CurrentSpan(ctx); span != noopSpan {
		t.Errorf("Calling CurrentSpan on background ctx expected noop span, actual: %v; ctx: %v", span, ctx)
	}

	span, ctx := ot.StartSpanFromContext(ctx, "first")

	if currentSpan := CurrentSpan(ctx); currentSpan != span {
		t.Errorf("Failed to extract the current span from the context, expected '%v' actual '%v'; context: '%v'", span, currentSpan, ctx)
	}
}

func TestStartRootSpan(t *testing.T) {
	ctx := context.Background()

	s, ctx := StartRootSpan(ctx, "first")
	if root := RootSpan(ctx); root != s {
		t.Errorf("No root span in context, expected span '%v'; context: %v", s, ctx)
	}

	first, _ := s.(*mocktracer.MockSpan)
	// We had no metadata in the context being propagated, so we expect no parent
	if first.ParentID != 0 {
		t.Errorf("Expected no parent for root span with no request metadata, actual '%d'; context: %v", first.ParentID, ctx)
	}

	// Shove the first span into the context's grpc metadata, so that StartRootSpan will think it got a propagated span
	// We use a new context so we're guarantee we're not relying on a span being in the context (i.e. so ot.SpanFromContext() == nil)
	newCtx := propagateSpan(context.Background(), ot.GlobalTracer(), s)

	ss, newCtx := StartRootSpan(newCtx, "second")
	if root := RootSpan(newCtx); root != ss {
		t.Errorf("No root span in context, expected span '%v'; context: %v", s, ctx)
	}

	second, _ := ss.(*mocktracer.MockSpan)
	if second.ParentID != first.SpanContext.SpanID {
		t.Errorf("Expected second to have parentID '%d', actual '%d'; context: %v", first.SpanContext.SpanID, second.ParentID, ctx)
	}
}

func TestStartRootSpan_NoRootReturnsNoopSpan(t *testing.T) {
	ctx := context.Background()
	if span := RootSpan(ctx); span != noopSpan {
		t.Errorf("Extracting root span from ctx without calling StartRootSpan expected no-op span, actual: %v; ctx: %v", span, ctx)
	}
}

func TestPropagateSpan(t *testing.T) {
	span := ot.StartSpan("first")
	ctx := propagateSpan(context.Background(), ot.GlobalTracer(), span)

	md, ok := metadata.FromContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}

	sCtx, err := ot.GlobalTracer().Extract(ot.HTTPHeaders, metadataReaderWriter{md})
	if err != nil {
		t.Errorf("Failed to extract metadata with err: %s", err)
	}

	mockSpan, _ := span.(*mocktracer.MockSpan)
	mockCtx, _ := sCtx.(mocktracer.MockSpanContext)

	if mockCtx.SpanID != mockSpan.SpanContext.SpanID {
		t.Errorf(
			"Extracted spancontext doesn't match propagated span: expected spanID '%d', actual '%d'; context: %v",
			mockSpan.SpanContext.SpanID,
			mockCtx.SpanID,
			ctx)
	}
}

func TestClientInterceptor(t *testing.T) {
	tracer := mocktracer.New()
	interceptor := ClientInterceptor(tracer)
	ctx := context.Background()

	_, err := interceptor(ctx, nil, nil, "interceptor test",
		func(ctx xctx.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			ctxSpan := CurrentSpan(ctx)
			if ctxSpan == noopSpan {
				t.Errorf("No span found inside intercepted handler; ctx: %v", ctx)
			}

			mockCtxSpan := ctxSpan.(*mocktracer.MockSpan)
			root, ctx := StartRootSpan(ctx, "root")
			mockRoot := root.(*mocktracer.MockSpan)
			if mockRoot.ParentID != mockCtxSpan.SpanContext.SpanID {
				t.Errorf("Couldn't construct root span out of metadata from client interceptor; ctx: %v", ctx)
			}
			return nil, nil
		})
	if err != nil {
		t.Errorf("Got error from interceptor: %v", err)
	}
}

func TestClientInterceptor_SetsCtxParent(t *testing.T) {
	tracer := mocktracer.New()
	interceptor := ClientInterceptor(tracer)
	ctx := context.Background()

	ctx = ot.ContextWithSpan(ctx, ot.StartSpan(""))
	_, err := interceptor(ctx, nil, nil, "interceptor test",
		func(ctx xctx.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			ctxSpan := CurrentSpan(ctx)
			if ctxSpan == noopSpan {
				t.Errorf("No span found inside intercepted handler; ctx: %v", ctx)
			}

			mockSpan := ctxSpan.(*mocktracer.MockSpan)
			if mockSpan.ParentID == 0 {
				t.Errorf("Mock span didn't respect parent in context, actual: %v; ctx: %v", mockSpan, ctx)
			}
			return nil, nil
		})
	if err != nil {
		t.Errorf("Got error from interceptor: %v", err)
	}
}
