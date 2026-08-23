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

package wasm

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/features"
)

func TestWasmBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"169.254.169.254", true}, // cloud metadata endpoint
		{"fe80::1", true},
		{"100.100.100.200", true}, // Alibaba Cloud metadata endpoint
		{"fd00:ec2::230", true},   // AWS IMDS IPv6 endpoint
		{"10.0.0.1", false},       // legitimately used for in-cluster Wasm module hosting
		{"127.0.0.1", false},      // not blocked by default so unit tests using httptest keep working
		{"8.8.8.8", false},
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %s", c.ip)
			}
			err := wasmBlockedIP(ip)
			if c.blocked && err == nil {
				t.Errorf("wasmBlockedIP(%s) = nil, want blocked", c.ip)
			}
			if !c.blocked && err != nil {
				t.Errorf("wasmBlockedIP(%s) = %v, want allowed", c.ip, err)
			}
		})
	}
}

func TestWasmBlockedIPHonorsConfiguredCIDRs(t *testing.T) {
	_, cidr, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	orig := features.BlockedCIDRsInWasmFetch
	features.BlockedCIDRsInWasmFetch = []*net.IPNet{cidr}
	defer func() { features.BlockedCIDRsInWasmFetch = orig }()

	if err := wasmBlockedIP(net.ParseIP("10.1.2.3")); err == nil {
		t.Error("expected 10.1.2.3 to be blocked once configured, got nil error")
	}
	if err := wasmBlockedIP(net.ParseIP("192.168.1.1")); err != nil {
		t.Errorf("expected 192.168.1.1 to remain allowed, got %v", err)
	}
}

// TestWasmFetchBlocksRedirectToBlockedHost proves the dial-level block also applies to a
// redirect target, not just the original request URL -- the exact gap left open by
// go-containerregistry's own checkRedirectSSRF (which only inspects IP-literal redirect
// targets) and by header/URL string checks in general. It uses the operator-configurable
// BlockedCIDRsInWasmFetch list (rather than a real link-local address) as the test vehicle,
// since real link-local addresses aren't reliably bindable in a test sandbox.
//
// The redirector and the redirect target are bound to distinct loopback addresses
// (127.0.0.1 and 127.0.0.2) so only the target -- not the redirector itself -- is blocked;
// this isolates "the redirect hop was blocked" from "the initial request was blocked".
func TestWasmFetchBlocksRedirectToBlockedHost(t *testing.T) {
	targetLis, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("could not bind to 127.0.0.2, skipping: %v", err)
	}

	_, cidr, err := net.ParseCIDR("127.0.0.2/32")
	if err != nil {
		t.Fatal(err)
	}
	orig := features.BlockedCIDRsInWasmFetch
	features.BlockedCIDRsInWasmFetch = []*net.IPNet{cidr}
	defer func() { features.BlockedCIDRsInWasmFetch = orig }()

	blocked := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("should never be reached"))
	}))
	blocked.Listener.Close()
	blocked.Listener = targetLis
	blocked.Start()
	defer blocked.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, blocked.URL, http.StatusFound)
	}))
	defer redirector.Close()

	fetcher := NewHTTPFetcher(DefaultHTTPRequestTimeout, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := fetcher.Fetch(ctx, redirector.URL, false); err == nil {
		t.Fatal("expected fetch following a redirect to a blocked host to fail, got nil error")
	}
}
