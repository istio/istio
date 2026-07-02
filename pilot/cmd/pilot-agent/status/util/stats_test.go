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

package util

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"istio.io/istio/pkg/test"
)

const (
	readyStatsBody = `server.state: 0
listener_manager.workers_started: 1
`
	updateStatsBody = `cluster_manager.cds.update_success: 1
cluster_manager.cds.update_rejected: 0
listener_manager.lds.update_success: 1
listener_manager.lds.update_rejected: 0
`
)

// newStatsServer starts a test server that serves the given body after an optional delay,
// and returns its host and admin port for use with the stats helpers.
func newStatsServer(t test.Failer, body string, delay time.Duration) (string, uint16) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL %q: %v", srv.URL, err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("failed to split host/port from %q: %v", u.Host, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", portStr, err)
	}
	return host, uint16(port)
}

func TestGetReadinessStats(t *testing.T) {
	host, port := newStatsServer(t, readyStatsBody, 0)

	state, workersStarted, err := GetReadinessStats(host, port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil || *state != 0 {
		t.Fatalf("expected server state 0 (LIVE), got %v", state)
	}
	if !workersStarted {
		t.Fatal("expected workers to be started")
	}
}

func TestGetUpdateStatusStats(t *testing.T) {
	host, port := newStatsServer(t, updateStatsBody, 0)

	s, err := GetUpdateStatusStats(host, port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.CDSUpdatesSuccess != 1 || s.LDSUpdatesSuccess != 1 {
		t.Fatalf("unexpected update stats: %s", s.String())
	}
}

// TestReadinessTimeout verifies that both readiness helpers honor the configurable
// readinessTimeout, which is overridable via the PROXY_READINESS_PROBE_TIMEOUT env var.
func TestReadinessTimeout(t *testing.T) {
	// Shrink the timeout well below the server delay so the request is guaranteed to time out.
	test.SetForTest(t, &readinessTimeout, 50*time.Millisecond)

	t.Run("GetReadinessStats", func(t *testing.T) {
		host, port := newStatsServer(t, readyStatsBody, 500*time.Millisecond)
		if _, _, err := GetReadinessStats(host, port); err == nil {
			t.Fatal("expected timeout error, got nil")
		}
	})

	t.Run("GetUpdateStatusStats", func(t *testing.T) {
		host, port := newStatsServer(t, updateStatsBody, 500*time.Millisecond)
		if _, err := GetUpdateStatusStats(host, port); err == nil {
			t.Fatal("expected timeout error, got nil")
		}
	})
}
