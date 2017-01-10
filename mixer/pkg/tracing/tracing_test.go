package tracing

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"

	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
)

func init() {
	ot.SetGlobalTracer(mocktracer.New())
}

func TestCurrentSpan(t *testing.T) {
	span, ctx := ot.StartSpanFromContext(context.Background(), "first")

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
	newCtx := propagateSpan(context.Background(), s)

	ss, newCtx := StartRootSpan(newCtx, "second")
	if root := RootSpan(newCtx); root != ss {
		t.Errorf("No root span in context, expected span '%v'; context: %v", s, ctx)
	}

	second, _ := ss.(*mocktracer.MockSpan)
	if second.ParentID != first.SpanContext.SpanID {
		t.Errorf("Expected second to have parentID '%d', actual '%d'; context: %v", first.SpanContext.SpanID, second.ParentID, ctx)
	}
}

func TestPropagateSpan(t *testing.T) {
	span := ot.StartSpan("first")
	ctx := propagateSpan(context.Background(), span)

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
