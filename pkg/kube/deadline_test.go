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

const deadlineFiresName = "kube_client_deadline_fired_total"

// newStallServer returns a server that sends headers at once, then stalls the body
// until the request is canceled or the test ends.
func newStallServer(t *testing.T) *httptest.Server {
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
	return server
}

// newSlowBodyServer returns a server that sends headers at once and the body after delay.
func newSlowBodyServer(t *testing.T, delay time.Duration) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("done"))
	}))
	t.Cleanup(server.Close)
	return server
}

// assertNoFires verifies no deadline was recorded for requests to server. The fire is
// recorded asynchronously, so it first waits past any deadline the test set.
func assertNoFires(t *testing.T, mt *monitortest.MetricsTest, server *httptest.Server) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
	host := strings.TrimPrefix(server.URL, "http://")
	for _, m := range mt.Metrics() {
		if m.Name == deadlineFiresName && m.Labels["host"] == host {
			t.Fatalf("unexpected deadline fire: %v", m)
		}
	}
}

func deadlineClient(total time.Duration) *http.Client {
	return &http.Client{Transport: &deadlineRoundTripper{next: http.DefaultTransport, total: total}}
}

func doGet(ctx context.Context, t *testing.T, c *http.Client, url string) (string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	assert.NoError(t, err)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// TestDeadlineDefaultApplied verifies a request without a deadline gets the default one.
func TestDeadlineDefaultApplied(t *testing.T) {
	mt := monitortest.New(t)
	server := newStallServer(t)

	start := time.Now()
	_, err := doGet(context.Background(), t, deadlineClient(50*time.Millisecond), server.URL+"/api/v1/pods")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("expected the default deadline to fire within a couple seconds, took %v", elapsed)
	}
	// The transport surfaces the ctx's cause, which names our deadline.
	var deadline *deadlineError
	if !errors.As(err, &deadline) {
		t.Fatalf("expected a deadlineError, got %v", err)
	}
	mt.Assert(deadlineFiresName, map[string]string{
		"window": "total",
		"host":   strings.TrimPrefix(server.URL, "http://"),
	}, monitortest.Exactly(1))
}

// TestDeadlineErrorBeforeHeaders verifies a default deadline that fires before headers
// arrive is named in the error.
func TestDeadlineErrorBeforeHeaders(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(done) })

	_, err := doGet(context.Background(), t, deadlineClient(50*time.Millisecond), server.URL)
	var deadline *deadlineError
	if !errors.As(err, &deadline) {
		t.Fatalf("expected a deadlineError, got %v", err)
	}
}

// TestDeadlineCallerDeadlineWins verifies the wrapper adds nothing to a request that
// has its own deadline, whether shorter or longer than the default.
func TestDeadlineCallerDeadlineWins(t *testing.T) {
	t.Run("longer", func(t *testing.T) {
		server := newSlowBodyServer(t, 300*time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		body, err := doGet(ctx, t, deadlineClient(50*time.Millisecond), server.URL)
		assert.NoError(t, err)
		assert.Equal(t, body, "done")
	})
	t.Run("shorter", func(t *testing.T) {
		mt := monitortest.New(t)
		server := newStallServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := doGet(ctx, t, deadlineClient(time.Hour), server.URL)
		assert.Error(t, err)
		var deadline *deadlineError
		if errors.As(err, &deadline) {
			t.Fatalf("caller's deadline was reported as ours: %v", err)
		}
		assertNoFires(t, mt, server)
	})
}

// TestDeadlineDisabled verifies a zero default passes requests through.
func TestDeadlineDisabled(t *testing.T) {
	server := newSlowBodyServer(t, 300*time.Millisecond)
	body, err := doGet(context.Background(), t, deadlineClient(0), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, body, "done")
}

// TestDeadlineUpgrade verifies a 101 is returned untouched and its deadline released, so
// the upgraded session is not bounded. Calls RoundTrip directly: http.Client eats a 101.
func TestDeadlineUpgrade(t *testing.T) {
	next := &switchingProtocolsRT{}
	rt := &deadlineRoundTripper{next: next, total: time.Hour}
	req, err := http.NewRequest(http.MethodPost, "http://example.com/api/v1/namespaces/default/pods/foo/portforward", nil)
	assert.NoError(t, err)
	resp, err := rt.RoundTrip(req)
	assert.NoError(t, err)
	if _, ok := resp.Body.(io.ReadWriteCloser); !ok {
		t.Fatalf("upgraded body was wrapped, got %T", resp.Body)
	}
	if next.ctx.Err() == nil {
		t.Fatal("upgrade kept its deadline")
	}
}

// switchingProtocolsRT returns a 101 whose body is the connection itself, the shape
// the SPDY upgrader returns. net.Pipe stands in for the hijacked socket.
type switchingProtocolsRT struct {
	ctx context.Context
}

func (s *switchingProtocolsRT) RoundTrip(req *http.Request) (*http.Response, error) {
	s.ctx = req.Context()
	conn, _ := net.Pipe()
	return &http.Response{StatusCode: http.StatusSwitchingProtocols, Body: conn}, nil
}

// TestDeadlineNoMetricOnClose verifies a request that finishes normally records nothing.
func TestDeadlineNoMetricOnClose(t *testing.T) {
	mt := monitortest.New(t)
	server := newSlowBodyServer(t, 10*time.Millisecond)
	body, err := doGet(context.Background(), t, deadlineClient(100*time.Millisecond), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, body, "done")
	assertNoFires(t, mt, server)
}

// TestDeadlineHeaderTimeoutMetric verifies a transport header timeout is counted, for
// requests with and without their own deadline.
func TestDeadlineHeaderTimeoutMetric(t *testing.T) {
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

	base := http.DefaultTransport.(*http.Transport).Clone()
	base.ResponseHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(base.CloseIdleConnections)
	client := &http.Client{Transport: &deadlineRoundTripper{next: base, total: time.Hour}}

	_, err := doGet(context.Background(), t, client, server.URL)
	assert.Error(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err = doGet(ctx, t, client, server.URL)
	assert.Error(t, err)

	mt.Assert(deadlineFiresName, map[string]string{
		"window": "header",
		"host":   strings.TrimPrefix(server.URL, "http://"),
	}, monitortest.Exactly(2))
}

func TestDeadlineWrappedRoundTripper(t *testing.T) {
	rt := WrapTransportWithDeadlines(http.DefaultTransport)
	if rt.(*deadlineRoundTripper).WrappedRoundTripper() != http.DefaultTransport {
		t.Fatal("expected WrappedRoundTripper to return the wrapped transport")
	}
}

func TestDeadlineWrapIdempotent(t *testing.T) {
	rt := WrapTransportWithDeadlines(http.DefaultTransport)
	if WrapTransportWithDeadlines(rt) != rt {
		t.Fatal("expected WrapTransportWithDeadlines to be idempotent")
	}
}
