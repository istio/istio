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

	"google.golang.org/grpc/metadata"

	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
)

func TestCurrentSpan(t *testing.T) {
	tracer := NewTracer(mocktracer.New())
	ctx := context.Background()

	if span := CurrentSpan(ctx); span != noopSpan {
		t.Errorf("Calling CurrentSpan on background ctx expected noop span, actual: %v; ctx: %v", span, ctx)
	}

	span, ctx := tracer.StartSpanFromContext(ctx, "first")

	if currentSpan := CurrentSpan(ctx); currentSpan != span {
		t.Errorf("Failed to extract the current span from the context, expected '%v' actual '%v'; context: '%v'", span, currentSpan, ctx)
	}
}

func TestStartSpanFromContext(t *testing.T) {
	tracer := NewTracer(mocktracer.New())

	span := tracer.StartSpan("parent")
	ctx := ot.ContextWithSpan(context.Background(), span)

	childSpan, ctx := tracer.StartSpanFromContext(ctx, "child")

	mockSpan, _ := span.(*mocktracer.MockSpan)
	mockChild, _ := childSpan.(*mocktracer.MockSpan)
	if mockChild.ParentID != mockSpan.SpanContext.SpanID {
		t.Errorf("Failed to extract parent from ctx %+v; expected parent %d, actual %d", ctx, mockSpan.SpanContext.SpanID, mockChild.ParentID)
	}
}

func TestStartRootSpan(t *testing.T) {
	tracer := NewTracer(mocktracer.New())
	ctx := context.Background()

	s, ctx := tracer.StartRootSpan(ctx, "first")
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
	_, newCtx := tracer.PropagateSpan(context.Background(), s)

	ss, newCtx := tracer.StartRootSpan(newCtx, "second")
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
	tracer := NewTracer(mocktracer.New())
	span := tracer.StartSpan("first")
	_, ctx := tracer.PropagateSpan(context.Background(), span)

	md, ok := metadata.FromContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}

	sCtx, err := tracer.Extract(ot.HTTPHeaders, metadataReaderWriter{md})
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

func TestDisabledTracer(t *testing.T) {
	tracer := DisabledTracer()

	if span, _ := tracer.StartRootSpan(context.Background(), ""); span != noopSpan {
		t.Errorf("Expected disabled tracer to return noop span, actual: %+v", span)
	}

	if span, _ := tracer.StartSpanFromContext(context.Background(), ""); span != noopSpan {
		t.Errorf("Expected disabled tracer to return noop span, actual: %+v", span)
	}
}
