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

package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		annotations, metadata map[string]string
		want                  Transport
		fail                  bool
	}{
		{name: "default", want: TCP},
		{name: "metadata", metadata: map[string]string{Env: "UDS"}, want: UDS},
		{name: "opt out", metadata: map[string]string{Env: "UDS"}, annotations: map[string]string{Annotation: "TCP"}, want: TCP},
		{name: "opt in", metadata: map[string]string{Env: "TCP"}, annotations: map[string]string{Annotation: "UDS"}, want: UDS},
		{name: "empty metadata", metadata: map[string]string{Env: ""}, fail: true},
		{name: "empty annotation", annotations: map[string]string{Annotation: ""}, fail: true},
		{name: "invalid", annotations: map[string]string{Annotation: "uds"}, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.annotations, tt.metadata)
			if (err != nil) != tt.fail || got != tt.want {
				t.Fatalf("got %q, %v; want %q, fail %v", got, err, tt.want, tt.fail)
			}
		})
	}
}

func socketDir(t *testing.T) string {
	t.Helper()
	// macOS Unix socket paths have a short length limit.
	dir, err := os.MkdirTemp("/tmp", "admin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestClient(t *testing.T) {
	for _, transport := range []Transport{TCP, UDS} {
		t.Run(string(transport), func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.RequestURI() != "/drain_listeners?inboundonly&graceful&skip_exit" || string(body) != "body" {
					t.Errorf("unexpected request: %s %s %s", r.Method, r.URL, body)
				}
				w.Write([]byte("response"))
			})
			s := httptest.NewUnstartedServer(handler)
			socket := filepath.Join(socketDir(t), "admin.sock")
			if transport == UDS {
				s.Listener.Close()
				l, err := net.Listen("unix", socket)
				if err != nil {
					t.Fatal(err)
				}
				s.Listener = l
			}
			s.Start()
			defer s.Close()
			client, err := NewClient(transport, socket, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			address := s.URL
			if transport == UDS {
				address = "http://localhost"
			}
			req, _ := http.NewRequest("POST", address+"/drain_listeners?inboundonly&graceful&skip_exit", strings.NewReader("body"))
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || string(body) != "response" {
				t.Fatalf("response: %s, %v", body, err)
			}
		})
	}
}

func TestNoFallback(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer s.Close()
	c, _ := NewClient(UDS, filepath.Join(socketDir(t), "missing.sock"), time.Second)
	defer c.CloseIdleConnections()
	if _, err := c.Get(s.URL); err == nil {
		t.Fatal("missing socket succeeded")
	}
	if hits.Load() != 0 {
		t.Fatal("fell back to TCP")
	}
	t.Setenv(Env, "invalid")
	if _, err := Do(context.Background(), "GET", s.URL, "", time.Second); err == nil {
		t.Fatal("invalid transport succeeded")
	}
}

func TestCancellationAndTimeout(t *testing.T) {
	for _, transport := range []Transport{TCP, UDS} {
		t.Run(string(transport), func(t *testing.T) {
			entered := make(chan struct{}, 2)
			s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }))
			socket := filepath.Join(socketDir(t), "admin.sock")
			if transport == UDS {
				s.Listener.Close()
				l, err := net.Listen("unix", socket)
				if err != nil {
					t.Fatal(err)
				}
				s.Listener = l
			}
			s.Start()
			defer s.Close()
			address := s.URL
			if transport == UDS {
				address = "http://localhost"
			}
			c, _ := NewClient(transport, socket, time.Second)
			defer c.CloseIdleConnections()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
			done := make(chan error, 1)
			go func() { _, err := c.Do(req); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request not received")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancellation did not return")
			}
			c.Timeout = 20 * time.Millisecond
			if _, err := c.Get(address); err == nil {
				t.Fatal("expected timeout")
			}
		})
	}
}

func TestAdminResponseErrors(t *testing.T) {
	t.Setenv(Env, "TCP")
	for _, code := range []int{200, 404, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code); w.Write([]byte("admin body")) }))
			defer server.Close()
			body, err := Do(context.Background(), "GET", server.URL, "", time.Second)
			if code == 200 {
				if err != nil || body.String() != "admin body" {
					t.Fatalf("got %v, %v", body, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "admin body") {
				t.Fatalf("missing admin failure: %v", err)
			}
		})
	}
}
