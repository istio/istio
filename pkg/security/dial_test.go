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

package security

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLinkLocal(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"169.254.169.254", true}, // cloud metadata endpoint
		{"169.254.0.1", true},
		{"fe80::1", true},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"127.0.0.1", false},
		{"8.8.8.8", false},
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %s", c.ip)
			}
			if got := IsLinkLocal(ip); got != c.blocked {
				t.Errorf("IsLinkLocal(%s) = %v, want %v", c.ip, got, c.blocked)
			}
		})
	}
}

func TestIsKnownCloudMetadataAddress(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"100.100.100.200", true},  // Alibaba Cloud metadata (RFC 6598 CGNAT space)
		{"100.100.100.199", false}, // just outside the /32
		{"fd00:ec2::230", true},    // AWS IMDS IPv6 endpoint
		{"fd00:ec2::1", true},      // still within fd00:ec2::/32
		{"fd12:3456::1", false},    // ULA space, but not AWS's reserved prefix
		{"10.0.0.1", false},
		{"169.254.169.254", false}, // link-local, covered by IsLinkLocal instead
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %s", c.ip)
			}
			if got := IsKnownCloudMetadataAddress(ip); got != c.blocked {
				t.Errorf("IsKnownCloudMetadataAddress(%s) = %v, want %v", c.ip, got, c.blocked)
			}
		})
	}
}

func TestBlockedIPDialContext(t *testing.T) {
	blockLoopback := func(ip net.IP) error {
		if ip.IsLoopback() {
			return errors.New("blocked: loopback")
		}
		return nil
	}

	t.Run("blocks matching predicate", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer ts.Close()

		dialCtx := BlockedIPDialContext(&net.Dialer{}, blockLoopback)
		client := &http.Client{Transport: &http.Transport{DialContext: dialCtx}}

		_, err := client.Get(ts.URL)
		if err == nil {
			t.Fatal("expected request to a blocked address to fail, got nil error")
		}
	})

	t.Run("allows non-matching predicate", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer ts.Close()

		dialCtx := BlockedIPDialContext(&net.Dialer{}, func(ip net.IP) error { return nil })
		client := &http.Client{Transport: &http.Transport{DialContext: dialCtx}}

		resp, err := client.Get(ts.URL)
		if err != nil {
			t.Fatalf("expected request to succeed, got error: %v", err)
		}
		resp.Body.Close()
	})
}
