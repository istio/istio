package zipkin

import (
	"context"
)

// SpanFromContext retrieves a Zipkin Span from Go's context propagation
// mechanism if found. If not found, returns nil.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey).(Span); ok {
		return s
	}
	return nil
}

// NewContext stores a Zipkin Span into Go's context propagation mechanism.
func NewContext(ctx context.Context, s Span) context.Context {
	return context.WithValue(ctx, spanKey, s)
}

type ctxKey struct{}

var spanKey = ctxKey{}
