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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test/util/assert"
)

// newDeadlineClient builds an *http.Client whose Transport is rt.
func newDeadlineClient(rt *deadlineRoundTripper) *http.Client {
	return &http.Client{Transport: rt}
}

// TestDeadlineHeaderWindow verifies a slow-to-respond server is aborted by the header deadline.
func TestDeadlineHeaderWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newDeadlineClient(&deadlineRoundTripper{
		next:   http.DefaultTransport,
		header: 50 * time.Millisecond,
		unary:  time.Hour,
	})

	start := time.Now()
	_, err := client.Get(server.URL)
	elapsed := time.Since(start)

	assert.Error(t, err)
	if elapsed > time.Second {
		t.Fatalf("expected header deadline to fire well under 2s, took %v", elapsed)
	}
	if !strings.Contains(err.Error(), "header deadline") {
		t.Fatalf("expected error to mention header deadline, got: %v", err)
	}
}

// TestDeadlineHeaderWindowClosesOnHeaders verifies the header timer stops once headers land.
func TestDeadlineHeaderWindowClosesOnHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(server.Close)

	client := newDeadlineClient(&deadlineRoundTripper{
		next:   http.DefaultTransport,
		header: 50 * time.Millisecond,
		unary:  time.Hour,
	})

	resp, err := client.Get(server.URL)
	assert.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	if string(body) != "done" {
		t.Fatalf("expected body %q, got %q", "done", string(body))
	}
}

// TestDeadlineUnaryBackstop verifies a request whose body never completes is aborted.
func TestDeadlineUnaryBackstop(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(done) })

	client := newDeadlineClient(&deadlineRoundTripper{
		next:   http.DefaultTransport,
		header: 0,
		unary:  50 * time.Millisecond,
	})

	resp, err := client.Get(server.URL)
	assert.NoError(t, err)
	defer resp.Body.Close()

	start := time.Now()
	_, err = io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	assert.Error(t, err)
	if elapsed > 2*time.Second {
		t.Fatalf("expected unary backstop to fire within a couple seconds, took %v", elapsed)
	}
}

// TestDeadlineCallerDeadlineWins verifies a caller-supplied deadline suppresses the backstop.
func TestDeadlineCallerDeadlineWins(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(server.Close)

	client := newDeadlineClient(&deadlineRoundTripper{
		next:   http.DefaultTransport,
		header: 0,
		unary:  50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	assert.NoError(t, err)

	resp, err := client.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	if string(body) != "done" {
		t.Fatalf("expected body %q, got %q", "done", string(body))
	}
}

// TestDeadlineStreamClassification is a table test on isStreamRequest.
func TestDeadlineStreamClassification(t *testing.T) {
	cases := []struct {
		name  string
		build func() *http.Request
		want  bool
	}{
		{
			name: "watch=true",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/pods?watch=true", nil)
			},
			want: true,
		},
		{
			name: "follow=true",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/pods/foo/log?follow=true", nil)
			},
			want: true,
		},
		{
			name: "connection upgrade",
			build: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/pods/foo/exec", nil)
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Upgrade", "SPDY/3.1")
				return req
			},
			want: true,
		},
		{
			name: "portforward subresource, no upgrade header",
			build: func() *http.Request {
				// What the wrapper sees: the upgrade header is added below this layer.
				return httptest.NewRequest(http.MethodPost,
					"http://example.com/api/v1/namespaces/istio-system/pods/istiod-0/portforward", nil)
			},
			want: true,
		},
		{
			name: "exec subresource, no upgrade header",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodPost,
					"http://example.com/api/v1/namespaces/default/pods/foo/exec?command=ls", nil)
			},
			want: true,
		},
		{
			name: "attach subresource, no upgrade header",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodPost,
					"http://example.com/api/v1/namespaces/default/pods/foo/attach", nil)
			},
			want: true,
		},
		{
			name: "pod named exec is not an upgrade",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet,
					"http://example.com/api/v1/namespaces/default/pods/exec", nil)
			},
			want: false,
		},
		{
			name: "pod log subresource is not an upgrade",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet,
					"http://example.com/api/v1/namespaces/default/pods/foo/log", nil)
			},
			want: false,
		},
		{
			name: "plain GET",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/pods", nil)
			},
			want: false,
		},
		{
			name: "watch=false",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/pods?watch=false", nil)
			},
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isStreamRequest(c.build())
			if got != c.want {
				t.Fatalf("isStreamRequest() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestDeadlineStreamBackstop verifies a watch is bounded by the stream backstop, not the unary one.
func TestDeadlineStreamBackstop(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(done) })

	client := newDeadlineClient(&deadlineRoundTripper{
		next:   http.DefaultTransport,
		header: 0,
		unary:  time.Hour,
		stream: 50 * time.Millisecond,
	})

	req, err := http.NewRequest(http.MethodGet, server.URL+"/?watch=true", nil)
	assert.NoError(t, err)

	resp, err := client.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()

	start := time.Now()
	_, err = io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	assert.Error(t, err)
	if elapsed > 2*time.Second {
		t.Fatalf("expected stream backstop to fire within a couple seconds, took %v", elapsed)
	}
}

// TestDeadlineUpgradeExemptFromTotal verifies upgrades get no total deadline, only the
// header one. The request carries no upgrade header, as the wrapper sees it in production.
func TestDeadlineUpgradeExemptFromTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(server.Close)

	client := newDeadlineClient(&deadlineRoundTripper{
		next:   http.DefaultTransport,
		header: time.Hour,
		unary:  50 * time.Millisecond,
		stream: 50 * time.Millisecond,
	})

	req, err := http.NewRequest(http.MethodPost,
		server.URL+"/api/v1/namespaces/default/pods/foo/portforward", nil)
	assert.NoError(t, err)

	resp, err := client.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	if string(body) != "done" {
		t.Fatalf("expected body %q, got %q", "done", string(body))
	}
}

// TestDeadlineUpgradeAtUnknownPathStopsTimers verifies a 101 detaches the deadline even when
// the path was not recognized as an upgrade. Calls RoundTrip directly: http.Client eats a 101.
func TestDeadlineUpgradeAtUnknownPathStopsTimers(t *testing.T) {
	rt := &deadlineRoundTripper{
		next:   switchingProtocolsRT{},
		header: time.Hour,
		unary:  50 * time.Millisecond,
		stream: 50 * time.Millisecond,
	}

	req, err := http.NewRequest(http.MethodPost, "http://example.com/some/hijacking/endpoint", nil)
	assert.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	assert.NoError(t, err)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
	// The stream machinery writes to the upgraded body, so it must not have been
	// wrapped on the way out.
	if _, ok := resp.Body.(io.ReadWriteCloser); !ok {
		t.Fatalf("upgraded body was wrapped, got %T", resp.Body)
	}
}

// switchingProtocolsRT returns a 101 whose body is the connection itself, the shape
// the SPDY upgrader returns. net.Pipe stands in for the hijacked socket.
type switchingProtocolsRT struct{}

func (switchingProtocolsRT) RoundTrip(*http.Request) (*http.Response, error) {
	conn, _ := net.Pipe()
	return &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Header:     http.Header{},
		Body:       conn,
	}, nil
}

// TestDeadlineWindowOrdering asserts the header deadline stays below both totals; a smaller
// total fires first and makes the header window unreachable.
func TestDeadlineWindowOrdering(t *testing.T) {
	if headerDeadline <= 0 {
		t.Skip("header deadline disabled")
	}
	if unaryBackstop > 0 && unaryBackstop <= headerDeadline {
		t.Errorf("unaryBackstop (%v) must exceed headerDeadline (%v), or the header window can never fire",
			unaryBackstop, headerDeadline)
	}
	if streamBackstop > 0 && streamBackstop <= headerDeadline {
		t.Errorf("streamBackstop (%v) must exceed headerDeadline (%v), or the header window can never fire",
			streamBackstop, headerDeadline)
	}
}

// TestDeadlineRaceFailsCleanly drives the window where a deadline and the response land
// together. Failing is legitimate; every failure must name the deadline that caused it.
func TestDeadlineRaceFailsCleanly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(server.Close)

	var deadline *deadlineError
	// Tiny header deadlines against an immediate response, to interleave both ways.
	for i := 0; i < 300; i++ {
		client := newDeadlineClient(&deadlineRoundTripper{
			next:   http.DefaultTransport,
			header: time.Duration(i%50) * time.Microsecond,
			unary:  time.Hour,
		})

		resp, err := client.Get(server.URL)
		if err != nil {
			if !errors.As(err, &deadline) {
				t.Fatalf("request failed without naming a deadline: %v", err)
			}
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if !errors.As(readErr, &deadline) {
				t.Fatalf("body read failed without naming a deadline: %v", readErr)
			}
			continue
		}
		if string(body) != "done" {
			t.Fatalf("reported success with an incomplete body: got %q, want %q", body, "done")
		}
	}
}

// errReadCloser yields a fixed error, standing in for a body whose read was ended.
type errReadCloser struct{ err error }

func (e errReadCloser) Read([]byte) (int, error) { return 0, e.err }
func (e errReadCloser) Close() error             { return nil }

// TestCancelBodyNamesDeadline verifies a deadline landing mid-read surfaces as the deadline,
// not the bare context error, and that a clean EOF is untouched.
func TestCancelBodyNamesDeadline(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/pods", nil)

	t.Run("deadline ends the read", func(t *testing.T) {
		want := &deadlineError{window: "total", class: "unary", bound: time.Second}
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(want)

		b := &cancelBody{
			body: errReadCloser{err: context.Canceled},
			ctx:  ctx,
			req:  req,
			done: func() {},
		}
		_, err := b.Read(make([]byte, 8))

		var got *deadlineError
		if !errors.As(err, &got) {
			t.Fatalf("read error did not name the deadline: %v", err)
		}
		if got != want {
			t.Fatalf("named the wrong deadline: got %v, want %v", got, want)
		}
	})

	t.Run("clean EOF is not a deadline", func(t *testing.T) {
		b := &cancelBody{
			body: errReadCloser{err: io.EOF},
			ctx:  context.Background(),
			req:  req,
			done: func() {},
		}
		_, err := b.Read(make([]byte, 8))

		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected io.EOF to pass through unchanged, got %v", err)
		}
		var unwanted *deadlineError
		if errors.As(err, &unwanted) {
			t.Fatalf("EOF was reported as a deadline: %v", err)
		}
	})
}

// TestDeadlineMetricLabels verifies a fired deadline is counted against the window,
// class, and API server it came from.
func TestDeadlineMetricLabels(t *testing.T) {
	mt := monitortest.New(t)

	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(done) })

	client := newDeadlineClient(&deadlineRoundTripper{
		next:   http.DefaultTransport,
		header: 50 * time.Millisecond,
		unary:  time.Hour,
	})

	_, err := client.Get(server.URL + "/api/v1/pods")
	assert.Error(t, err)

	host := strings.TrimPrefix(server.URL, "http://")
	mt.Assert(deadlineFiresName, map[string]string{
		"window": "header",
		"class":  "unary",
		"host":   host,
	}, monitortest.AtLeast(1))
}

// TestDeadlineWrapIdempotent verifies that wrapping an already-wrapped transport is a no-op.
func TestDeadlineWrapIdempotent(t *testing.T) {
	rt := WrapTransportWithDeadlines(http.DefaultTransport)
	rewrapped := WrapTransportWithDeadlines(rt)
	if rewrapped != rt {
		t.Fatalf("expected WrapTransportWithDeadlines to be idempotent, got a different value")
	}
}

// TestStreamBackstopClearsWatchCeiling asserts streamBackstop stays above client-go's reflector
// watch ceiling. The ceiling holds because istio never overrides MinWatchTimeout, which
// SharedIndexInformerOptions does not expose. On failure, re-derive it from the defaults in
// tools/cache/reflector.go.
func TestStreamBackstopClearsWatchCeiling(t *testing.T) {
	const watchCeiling = 2 * 5 * time.Minute // 2 * defaultMinWatchTimeout
	const requiredMargin = 5 * time.Minute

	if streamBackstop < watchCeiling+requiredMargin {
		t.Fatalf("streamBackstop (%v) must be at least %v above the client-go watch ceiling (%v)",
			streamBackstop, requiredMargin, watchCeiling)
	}
}
