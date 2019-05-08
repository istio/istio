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
	"context"
)

var defaultNoopSpan = &noopSpan{}

// SpanFromContext retrieves a Zipkin Span from Go's context propagation
// mechanism if found. If not found, returns nil.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey).(Span); ok {
		return s
	}
	return nil
}

// SpanOrNoopFromContext retrieves a Zipkin Span from Go's context propagation
// mechanism if found. If not found, returns a noopSpan.
// This function typically is used for modules that want to provide existing
// Zipkin spans with additional data, but can't guarantee that spans are
// properly propagated. It is preferred to use SpanFromContext() and test for
// Nil instead of using this function.
func SpanOrNoopFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey).(Span); ok {
		return s
	}
	return defaultNoopSpan
}

// NewContext stores a Zipkin Span into Go's context propagation mechanism.
func NewContext(ctx context.Context, s Span) context.Context {
	return context.WithValue(ctx, spanKey, s)
}

type ctxKey struct{}

var spanKey = ctxKey{}
