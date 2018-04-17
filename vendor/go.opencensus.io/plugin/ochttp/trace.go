// Copyright 2018, OpenCensus Authors
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

package ochttp

import (
	"io"
	"net/http"
	"net/url"

	"go.opencensus.io/plugin/ochttp/propagation/b3"
	"go.opencensus.io/trace"
	"go.opencensus.io/trace/propagation"
)

// TODO(jbd): Add godoc examples.

var defaultFormat propagation.HTTPFormat = &b3.HTTPFormat{}

// Attributes recorded on the span for the requests.
// Only trace exporters will need them.
const (
	HostAttribute       = "http.host"
	MethodAttribute     = "http.method"
	PathAttribute       = "http.path"
	UserAgentAttribute  = "http.user_agent"
	StatusCodeAttribute = "http.status_code"
)

type traceTransport struct {
	base         http.RoundTripper
	startOptions trace.StartOptions
	format       propagation.HTTPFormat
}

// TODO(jbd): Add message events for request and response size.

// RoundTrip creates a trace.Span and inserts it into the outgoing request's headers.
// The created span can follow a parent span, if a parent is presented in
// the request's context.
func (t *traceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	name := spanNameFromURL(req.URL)
	// TODO(jbd): Discuss whether we want to prefix
	// outgoing requests with Sent.
	parent := trace.FromContext(req.Context())
	span := trace.NewSpan(name, parent, t.startOptions)
	req = req.WithContext(trace.WithSpan(req.Context(), span))

	if t.format != nil {
		t.format.SpanContextToRequest(span.SpanContext(), req)
	}

	span.AddAttributes(requestAttrs(req)...)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		span.SetStatus(trace.Status{Code: 2, Message: err.Error()})
		span.End()
		return resp, err
	}

	span.AddAttributes(responseAttrs(resp)...)
	span.SetStatus(status(resp.StatusCode))

	// span.End() will be invoked after
	// a read from resp.Body returns io.EOF or when
	// resp.Body.Close() is invoked.
	resp.Body = &bodyTracker{rc: resp.Body, span: span}
	return resp, err
}

// bodyTracker wraps a response.Body and invokes
// trace.EndSpan on encountering io.EOF on reading
// the body of the original response.
type bodyTracker struct {
	rc   io.ReadCloser
	span *trace.Span
}

var _ io.ReadCloser = (*bodyTracker)(nil)

func (bt *bodyTracker) Read(b []byte) (int, error) {
	n, err := bt.rc.Read(b)

	switch err {
	case nil:
		return n, nil
	case io.EOF:
		bt.span.End()
	default:
		// For all other errors, set the span status
		bt.span.SetStatus(trace.Status{
			// Code 2 is the error code for Internal server error.
			Code:    2,
			Message: err.Error(),
		})
	}
	return n, err
}

func (bt *bodyTracker) Close() error {
	// Invoking endSpan on Close will help catch the cases
	// in which a read returned a non-nil error, we set the
	// span status but didn't end the span.
	bt.span.End()
	return bt.rc.Close()
}

// CancelRequest cancels an in-flight request by closing its connection.
func (t *traceTransport) CancelRequest(req *http.Request) {
	type canceler interface {
		CancelRequest(*http.Request)
	}
	if cr, ok := t.base.(canceler); ok {
		cr.CancelRequest(req)
	}
}

func spanNameFromURL(u *url.URL) string {
	return u.Path
}

func requestAttrs(r *http.Request) []trace.Attribute {
	return []trace.Attribute{
		trace.StringAttribute(PathAttribute, r.URL.Path),
		trace.StringAttribute(HostAttribute, r.URL.Host),
		trace.StringAttribute(MethodAttribute, r.Method),
		trace.StringAttribute(UserAgentAttribute, r.UserAgent()),
	}
}

func responseAttrs(resp *http.Response) []trace.Attribute {
	return []trace.Attribute{
		trace.Int64Attribute(StatusCodeAttribute, int64(resp.StatusCode)),
	}
}

func status(statusCode int) trace.Status {
	var code int32
	if statusCode < 200 || statusCode >= 400 {
		code = codeUnknown
	}
	switch statusCode {
	case 499:
		code = codeCancelled
	case http.StatusBadRequest:
		code = codeInvalidArgument
	case http.StatusGatewayTimeout:
		code = codeDeadlineExceeded
	case http.StatusNotFound:
		code = codeNotFound
	case http.StatusForbidden:
		code = codePermissionDenied
	case http.StatusUnauthorized: // 401 is actually unauthenticated.
		code = codeUnathenticated
	case http.StatusTooManyRequests:
		code = codeResourceExhausted
	case http.StatusNotImplemented:
		code = codeUnimplemented
	case http.StatusServiceUnavailable:
		code = codeUnavailable
	}
	return trace.Status{Code: code, Message: codeToStr[code]}
}

// TODO(jbd): Provide status codes from trace package.
const (
	codeOK                 = 0
	codeCancelled          = 1
	codeUnknown            = 2
	codeInvalidArgument    = 3
	codeDeadlineExceeded   = 4
	codeNotFound           = 5
	codeAlreadyExists      = 6
	codePermissionDenied   = 7
	codeResourceExhausted  = 8
	codeFailedPrecondition = 9
	codeAborted            = 10
	codeOutOfRange         = 11
	codeUnimplemented      = 12
	codeInternal           = 13
	codeUnavailable        = 14
	codeDataLoss           = 15
	codeUnathenticated     = 16
)

var codeToStr = map[int32]string{
	codeOK:                 `"OK"`,
	codeCancelled:          `"CANCELLED"`,
	codeUnknown:            `"UNKNOWN"`,
	codeInvalidArgument:    `"INVALID_ARGUMENT"`,
	codeDeadlineExceeded:   `"DEADLINE_EXCEEDED"`,
	codeNotFound:           `"NOT_FOUND"`,
	codeAlreadyExists:      `"ALREADY_EXISTS"`,
	codePermissionDenied:   `"PERMISSION_DENIED"`,
	codeResourceExhausted:  `"RESOURCE_EXHAUSTED"`,
	codeFailedPrecondition: `"FAILED_PRECONDITION"`,
	codeAborted:            `"ABORTED"`,
	codeOutOfRange:         `"OUT_OF_RANGE"`,
	codeUnimplemented:      `"UNIMPLEMENTED"`,
	codeInternal:           `"INTERNAL"`,
	codeUnavailable:        `"UNAVAILABLE"`,
	codeDataLoss:           `"DATA_LOSS"`,
	codeUnathenticated:     `"UNAUTHENTICATED"`,
}
