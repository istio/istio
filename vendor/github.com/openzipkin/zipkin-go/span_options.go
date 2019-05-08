// Copyright 2019 The OpenZipkin Authors
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
