package zipkin

import (
	"time"

	"github.com/openzipkin/zipkin-go/model"
)

// SpanOption allows for functional options to adjust behavior and payload of
// the Span to be created with tracer.StartSpan().
type SpanOption func(t *Tracer, s *spanImpl)

// Kind sets the kind of the span being created..
func Kind(kind model.Kind) SpanOption {
	return func(t *Tracer, s *spanImpl) {
		s.Kind = kind
	}
}

// Parent will use provided SpanContext as parent to the span being created.
func Parent(sc model.SpanContext) SpanOption {
	return func(t *Tracer, s *spanImpl) {
		if sc.Err != nil {
			// encountered an extraction error
			switch t.extractFailurePolicy {
			case ExtractFailurePolicyRestart:
			case ExtractFailurePolicyError:
				panic(s.SpanContext.Err)
			case ExtractFailurePolicyTagAndRestart:
				s.Tags["error.extract"] = sc.Err.Error()
			default:
				panic(ErrInvalidExtractFailurePolicy)
			}
			/* don't use provided SpanContext, but restart trace */
			return
		}
		s.SpanContext = sc
	}
}

// StartTime uses a given start time for the span being created.
func StartTime(start time.Time) SpanOption {
	return func(t *Tracer, s *spanImpl) {
		s.Timestamp = start
	}
}

// RemoteEndpoint sets the remote endpoint of the span being created.
func RemoteEndpoint(e *model.Endpoint) SpanOption {
	return func(t *Tracer, s *spanImpl) {
		s.RemoteEndpoint = e
	}
}

// Tags sets initial tags for the span being created. If default tracer tags
// are present they will be overwritten on key collisions.
func Tags(tags map[string]string) SpanOption {
	return func(t *Tracer, s *spanImpl) {
		for k, v := range tags {
			s.Tags[k] = v
		}
	}
}

// FlushOnFinish when set to false will disable span.Finish() to send the Span
// to the Reporter automatically (which is the default behavior). If set to
// false, having the Span be reported becomes the responsibility of the user.
// This is available if late tag data is expected to be only available after the
// required finish time of the Span.
func FlushOnFinish(b bool) SpanOption {
	return func(t *Tracer, s *spanImpl) {
		s.flushOnFinish = b
	}
}
