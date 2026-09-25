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
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
)

var defaultRequestTimeout = env.Register("ISTIO_KUBE_REQUEST_TIMEOUT", 15*time.Minute,
	"Default time limit for a Kubernetes API request that does not set its own deadline. "+
		"0 disables the limit.").Get()

var (
	windowLabel = monitoring.CreateLabel("window")
	hostLabel   = monitoring.CreateLabel("host")

	deadlineFires = monitoring.NewSum(
		"kube_client_deadline_fired_total",
		"Number of Kubernetes API requests canceled by a client-side timeout. The window label is header "+
			"(no response headers arrived) or total (the request did not finish). The host label is the API server.",
	)
)

// headerTimeoutMessage is the error text net/http and http2 use when ResponseHeaderTimeout fires.
const headerTimeoutMessage = "timeout awaiting response headers"

// WrapTransportWithDeadlines gives every request sent through rt a default deadline,
// unless the request already has one. It is idempotent.
func WrapTransportWithDeadlines(rt http.RoundTripper) http.RoundTripper {
	if _, ok := rt.(*deadlineRoundTripper); ok {
		return rt
	}
	return &deadlineRoundTripper{next: rt, total: defaultRequestTimeout}
}

type deadlineRoundTripper struct {
	next  http.RoundTripper
	total time.Duration
}

// WrappedRoundTripper lets client-go reach the underlying transport, for example to
// close idle connections.
func (d *deadlineRoundTripper) WrappedRoundTripper() http.RoundTripper { return d.next }

type deadlineError struct {
	bound time.Duration
}

func (e *deadlineError) Error() string {
	return fmt.Sprintf("kube client default request deadline (%v) exceeded", e.bound)
}

func (e *deadlineError) Timeout() bool { return true }

func (d *deadlineRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, ok := req.Context().Deadline(); ok || d.total <= 0 {
		// The caller's deadline wins.
		return d.roundTrip(req)
	}

	cause := &deadlineError{bound: d.total}
	ctx, cancel := context.WithTimeoutCause(req.Context(), d.total, cause)
	stop := context.AfterFunc(ctx, func() {
		if context.Cause(ctx) == cause {
			recordDeadlineFire("total", req)
		}
	})
	resp, err := d.roundTrip(req.WithContext(ctx))
	if err != nil {
		cancel()
		if context.Cause(ctx) == cause {
			return nil, fmt.Errorf("%w: %w", cause, err)
		}
		return nil, err
	}
	if resp.StatusCode == http.StatusSwitchingProtocols {
		// An upgrade (exec, attach, port-forward): the connection now belongs to the
		// caller, so return the body untouched.
		stop()
		cancel()
		return resp, nil
	}
	// A watch or a large list is still streaming long after RoundTrip returns, so the
	// ctx is released on body close rather than here.
	resp.Body = &closeBody{ReadCloser: resp.Body, release: func() {
		stop()
		cancel()
	}}
	return resp, nil
}

// roundTrip sends req and records a header timeout from the transport.
func (d *deadlineRoundTripper) roundTrip(req *http.Request) (*http.Response, error) {
	resp, err := d.next.RoundTrip(req)
	if err != nil && strings.Contains(err.Error(), headerTimeoutMessage) {
		recordDeadlineFire("header", req)
	}
	return resp, err
}

// recordDeadlineFire counts and logs a timeout that canceled req.
func recordDeadlineFire(window string, req *http.Request) {
	deadlineFires.With(
		windowLabel.Value(window),
		// The API server endpoint, which separates clusters in a multicluster mesh.
		// It cannot separate replicas behind one address.
		hostLabel.Value(req.URL.Host),
	).Increment()
	log.Warnf("kube client %s timeout; canceled %s %s%s", window, req.Method, req.URL.Host, req.URL.Path)
}

// closeBody releases the request's deadline when the body is closed.
type closeBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *closeBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}
