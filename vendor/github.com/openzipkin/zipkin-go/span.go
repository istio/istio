package zipkin

import (
	"time"

	"github.com/openzipkin/zipkin-go/model"
)

// Span interface as returned by Tracer.StartSpan()
type Span interface {
	// Context returns the Span's SpanContext.
	Context() model.SpanContext

	// SetName updates the Span's name.
	SetName(string)

	// SetRemoteEndpoint updates the Span's Remote Endpoint.
	SetRemoteEndpoint(*model.Endpoint)

	// Annotate adds a timed event to the Span.
	Annotate(time.Time, string)

	// Tag sets Tag with given key and value to the Span. If key already exists in
	// the Span the value will be overridden except for error tags where the first
	// value is persisted.
	Tag(string, string)

	// Finish the Span and send to Reporter. If DelaySend option was used at
	// Span creation time, Finish will not send the Span to the Reporter. It then
	// becomes the user's responsibility to get the Span reported (by using
	// span.Flush).
	Finish()

	// Flush the Span to the Reporter (regardless of being finished or not).
	// This can be used if the DelaySend SpanOption was set or when dealing with
	// one-way RPC tracing where duration might not be measured.
	Flush()
}
