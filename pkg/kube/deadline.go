// Copyright Istio Authors
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

package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/streaming/pkg/httpstream"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/util/sets"
)

// A request can stall before response headers arrive, or while the body is read.
// The two windows are bounded separately.
var (
	// Must stay above the API server's own --request-timeout (the kube API server has a
	// 60s request timeout by default).
	headerDeadline = env.Register("ISTIO_KUBE_HEADER_TIMEOUT", 90*time.Second,
		"Client-side deadline on how long any Kubernetes API request may wait for its first response header. "+
			"Must exceed the API server's --request-timeout. 0 disables the deadline.").Get()
	// Must stay above headerDeadline.
	unaryBackstop = env.Register("ISTIO_KUBE_REQUEST_TIMEOUT", 120*time.Second,
		"Client-side total deadline applied to non-streaming Kubernetes API requests that do not carry "+
			"their own context deadline. Must exceed ISTIO_KUBE_HEADER_TIMEOUT. 0 disables the deadline.").Get()
	// Fallback for streams that do not set timeoutSeconds.
	streamBackstop = env.Register("ISTIO_KUBE_STREAM_TIMEOUT", 15*time.Minute,
		"Client-side total deadline applied to streaming Kubernetes API requests (watches, log follows) "+
			"that do not carry their own deadline. 0 disables the deadline.").Get()
)

// streamGrace is the grace period before closing a stream after its timeoutSeconds is reached.
const streamGrace = time.Minute

var (
	windowLabel = monitoring.CreateLabel("window")
	classLabel  = monitoring.CreateLabel("class")
	hostLabel   = monitoring.CreateLabel("host")

	deadlineFiresName = "kube_client_deadline_fired_total"

	deadlineFires = monitoring.NewSum(
		deadlineFiresName,
		"Number of Kubernetes API requests canceled by a client-side deadline. The window label states "+
			"which bound fired: header means no response headers arrived, total means the response did not finish. "+
			"The class label separates streaming requests, such as watches, from normal ones. The host "+
			"label is the API server the request was sent to.",
	)
)

// WrapTransportWithDeadlines bounds every request sent through rt. It is idempotent.
func WrapTransportWithDeadlines(rt http.RoundTripper) http.RoundTripper {
	if _, ok := rt.(*deadlineRoundTripper); ok {
		return rt
	}
	return &deadlineRoundTripper{
		next:   rt,
		header: headerDeadline,
		unary:  unaryBackstop,
		stream: streamBackstop,
	}
}

type deadlineRoundTripper struct {
	next http.RoundTripper

	header time.Duration
	unary  time.Duration
	stream time.Duration
}

type deadlineError struct {
	window string
	class  string
	bound  time.Duration
}

func (e *deadlineError) Error() string {
	return fmt.Sprintf("kube client %s deadline (%v, %s request) exceeded", e.window, e.bound, e.class)
}

func (e *deadlineError) Timeout() bool { return true }

// isStreamRequest reports whether req is a long-lived streaming request: a watch, a
// log follow, or a protocol upgrade.
func isStreamRequest(req *http.Request) bool {
	q := req.URL.Query()
	return q.Get("watch") == "true" ||
		q.Get("follow") == "true" ||
		isUpgradeRequest(req)
}

// upgradeSubresources are the pod subresources that hijack the connection instead
// of returning a body.
var upgradeSubresources = sets.New("exec", "attach", "portforward")

// isUpgradeRequest reports whether req initiates a protocol upgrade (exec, attach,
// port-forward). The SPDY and websocket round trippers set the Connection header
// below this wrapper, so the URL subresource is checked as well.
func isUpgradeRequest(req *http.Request) bool {
	if httpstream.IsUpgradeRequest(req) {
		return true
	}
	// Match .../pods/{name}/{subresource}, so a pod named "exec" is not mistaken for one.
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) < 3 {
		return false
	}
	return parts[len(parts)-3] == "pods" && upgradeSubresources.Contains(parts[len(parts)-1])
}

// addedTimeout returns the total deadline the wrapper adds to req, or <= 0 for none.
// Upgrades and requests whose context already has a deadline get none. Streams that
// set timeoutSeconds get it plus streamGrace; other requests get the unary or stream backstop.
func (d *deadlineRoundTripper) addedTimeout(req *http.Request, stream bool) time.Duration {
	if isUpgradeRequest(req) {
		return 0
	}
	if _, ok := req.Context().Deadline(); ok {
		return 0
	}
	if !stream {
		return d.unary
	}
	if d.stream <= 0 {
		return 0
	}
	if timeout := requestedTimeout(req); timeout > 0 {
		return timeout + streamGrace
	}
	return d.stream
}

// requestedTimeout returns the server-side timeout req asks for with timeoutSeconds,
// or 0 if it does not ask for a valid one.
func requestedTimeout(req *http.Request) time.Duration {
	n, err := strconv.ParseInt(req.URL.Query().Get("timeoutSeconds"), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func (d *deadlineRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	stream := isStreamRequest(req)
	total := d.addedTimeout(req, stream)
	hasTotal := total > 0
	if d.header <= 0 && !hasTotal {
		return d.next.RoundTrip(req)
	}

	class := "unary"
	if stream {
		class = "stream"
	}

	// The total window is a context deadline.
	var totalCause *deadlineError
	if hasTotal {
		totalCause = &deadlineError{window: "total", class: class, bound: total}
	}
	req, ctx, cancel := boundedRequest(req, total, totalCause)

	// The header window is a timer, because it stops when we get headers
	// and we need the context to continue.
	var headerTimer *time.Timer
	if d.header > 0 {
		err := &deadlineError{window: "header", class: class, bound: d.header}
		headerTimer = time.AfterFunc(d.header, func() { cancel(err) })
	}
	stopHeaderTimer := func() {
		if headerTimer != nil {
			headerTimer.Stop()
		}
	}

	resp, err := d.next.RoundTrip(req)
	// Headers have landed (or the request failed); the header window closes here.
	stopHeaderTimer()
	if err != nil {
		cause := context.Cause(ctx)
		recordDeadlineFire(ctx, req)
		cancel(nil)
		if deadline, ok := cause.(*deadlineError); ok {
			return nil, fmt.Errorf("%w: %w", deadline, err)
		}
		return nil, err
	}
	// The timer can fire just after the transport produced this response, leaving a
	// canceled ctx behind it. Fail cleanly rather than return a body that cannot read.
	if cause := context.Cause(ctx); cause != nil {
		recordDeadlineFire(ctx, req)
		_ = resp.Body.Close()
		return nil, cause
	}
	if resp.StatusCode == http.StatusSwitchingProtocols {
		// The connection now belongs to the caller, who reads and writes it
		// directly, so return the body untouched.
		return resp, nil
	}
	// A watch or a large list is still streaming long after RoundTrip returns, so the
	// ctx is released on body close rather than here.
	resp.Body = &cancelBody{
		body: resp.Body,
		ctx:  ctx,
		req:  req,
		done: func() { cancel(nil) },
	}
	return resp, nil
}

// boundedRequest derives the request context the deadlines act on, carrying the
// total deadline when there is one. The returned cancel is not called on every path:
// it fires from the header timer, from body close, or for upgrades not at all.
func boundedRequest(req *http.Request, total time.Duration, cause *deadlineError) (
	*http.Request, context.Context, context.CancelCauseFunc,
) {
	if cause == nil {
		ctx, cancel := context.WithCancelCause(req.Context())
		return req.WithContext(ctx), ctx, cancel
	}
	ctx, cancelTimeout := context.WithTimeoutCause(req.Context(), total, cause)
	// WithTimeoutCause returns a plain CancelFunc, so layer a cause-carrying one on
	// top for the header timer.
	ctx, cancelCause := context.WithCancelCause(ctx)
	cancel := func(err error) {
		cancelCause(err)
		cancelTimeout()
	}
	return req.WithContext(ctx), ctx, cancel
}

// recordDeadlineFire counts and logs a deadline only where one canceled the request.
func recordDeadlineFire(ctx context.Context, req *http.Request) {
	e, ok := context.Cause(ctx).(*deadlineError)
	if !ok {
		return
	}
	deadlineFires.With(
		windowLabel.Value(e.window),
		classLabel.Value(e.class),
		// The API server endpoint, which separates clusters in a multicluster mesh.
		// It cannot separate replicas behind one address.
		hostLabel.Value(req.URL.Host),
	).Increment()
	log.Warnf("%v; canceled %s %s%s", e, req.Method, req.URL.Host, req.URL.Path)
}

type cancelBody struct {
	body       io.ReadCloser
	once       sync.Once
	recordOnce sync.Once
	done       func()
	ctx        context.Context
	req        *http.Request
}

func (b *cancelBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		// Report before done cancels the ctx, so only a real deadline is counted.
		b.recordOnce.Do(func() { recordDeadlineFire(b.ctx, b.req) })
		// Surface the deadline rather than the transport's context error.
		if deadline, ok := context.Cause(b.ctx).(*deadlineError); ok {
			err = fmt.Errorf("%w: %w", deadline, err)
		}
		b.once.Do(b.done)
	} else if err != nil {
		b.once.Do(b.done)
	}
	return n, err
}

func (b *cancelBody) Close() error {
	err := b.body.Close()
	b.once.Do(b.done)
	return err
}
