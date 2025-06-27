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

package client

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/atomic"

	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

func TestDNSForwardParallel(t *testing.T) {
	d := initDNS(t, true)
	testDNS(t, d)
}

func TestDNS(t *testing.T) {
	d := initDNS(t, false)
	testDNS(t, d)
}

func TestBuildAlternateHosts(t *testing.T) {
	// Create the server instance without starting it, as it's unnecessary for this test
	d, err := NewLocalDNSServer("ns1", "ns1.svc.cluster.local", "localhost:0", false)
	if err != nil {
		t.Fatal(err)
	}
	// Fill the DNS table with test data
	fillTable(d)

	testCases := []struct {
		startsWith string
		expected   sets.Set[string]
	}{
		{
			startsWith: "www.google.com",
			expected:   sets.New("www.google.com", "www.google.com."),
		},
		{
			startsWith: "example",
			expected:   sets.New("example.ns2.", "example.ns2.svc.", "example.localhost.", "example.ns2.svc.cluster.local.", "example.ns2.svc.cluster.local"),
		},
		{
			startsWith: "details",
			expected:   sets.New("details.ns2.svc.cluster.remote", "details.ns2.svc.cluster.remote."),
		},
		{
			startsWith: "productpage",
			expected: sets.New(
				"productpage.ns1.svc.cluster.local.", "productpage.", "productpage.ns1.svc.cluster.local", "productpage.ns1.", "productpage.ns1.svc.",
			),
		},
		{
			startsWith: "svc-with-alt",
			expected: sets.New(
				"svc-with-alt.",
				"svc-with-alt.ns1.",
				"svc-with-alt.ns1.svc.",
				"svc-with-alt.ns1.svc.clusterset.local.",
				"svc-with-alt.ns1.svc.cluster.local.",
				"svc-with-alt.ns1.svc.cluster.local",
			),
		},
		{
			startsWith: "*.wildcard",
			expected:   sets.New("*.wildcard", "*.wildcard."),
		},
		{
			startsWith: "example.localhost.",
			expected:   sets.New("example.localhost."),
		},
	}

	nt := d.NameTable()
	nt = protomarshal.Clone(nt)
	d.BuildAlternateHosts(nt, func(althosts map[string]struct{}, ipv4 []netip.Addr, ipv6 []netip.Addr, _ []string) {
		for host := range althosts {
			if _, exists := nt.Table[host]; !exists {
				addresses := make([]string, 0, len(ipv4)+len(ipv6))
				for _, addr := range ipv4 {
					addresses = append(addresses, addr.String())
				}
				for _, addr := range ipv6 {
					addresses = append(addresses, addr.String())
				}
				nt.Table[host] = &dnsProto.NameTable_NameInfo{
					Ips:      addresses,
					Registry: "Kubernetes",
				}
			}
		}
	})
	for _, tc := range testCases {
		matched := sets.New[string]()
		for key := range nt.Table {
			if strings.HasPrefix(key, tc.startsWith) {
				matched.Insert(key)
			}
		}
		if !matched.Equals(tc.expected) {
			t.Errorf("expected records %v, got: %v", tc.expected, matched)
		}
	}
}

func testDNS(t *testing.T, d *LocalDNSServer) {
	testCases := []struct {
		name                     string
		host                     string
		id                       int
		queryAAAA                bool
		expected                 []dns.RR
		expectResolutionFailure  int
		expectExternalResolution bool
		modifyReq                func(msg *dns.Msg)
	}{
		{
			name:     "success: non k8s host in local cache",
			host:     "www.google.com.",
			expected: a("www.google.com.", []netip.Addr{netip.MustParseAddr("1.1.1.1")}),
		},
		{
			name: "success: non k8s host with search namespace yields cname+A record",
			host: "www.google.com.ns1.svc.cluster.local.",
			expected: append(cname("www.google.com.ns1.svc.cluster.local.", "www.google.com."),
				a("www.google.com.", []netip.Addr{netip.MustParseAddr("1.1.1.1")})...),
		},
		{
			name:                     "success: non k8s host not in local cache",
			host:                     "www.bing.com.",
			expectExternalResolution: true,
		},
		{
			name:     "success: k8s host - fqdn",
			host:     "productpage.ns1.svc.cluster.local.",
			expected: a("productpage.ns1.svc.cluster.local.", []netip.Addr{netip.MustParseAddr("9.9.9.9")}),
		},
		{
			name: "not found: k8s host, fqdn with search namespace[0]",
			host: "productpage.ns1.svc.cluster.local.ns1.svc.cluster.local.", // This should not exist in the table.
		},
		{
			name:     "success: k8s host - name.namespace",
			host:     "productpage.ns1.",
			expected: a("productpage.ns1.", []netip.Addr{netip.MustParseAddr("9.9.9.9")}),
		},
		{
			name:     "success: k8s host - shortname",
			host:     "productpage.",
			expected: a("productpage.", []netip.Addr{netip.MustParseAddr("9.9.9.9")}),
		},
		{
			name: "success: k8s host (name.namespace) with search namespace yields cname+A record",
			host: "productpage.ns1.ns1.svc.cluster.local.",
			expected: append(cname("productpage.ns1.ns1.svc.cluster.local.", "productpage.ns1."),
				a("productpage.ns1.", []netip.Addr{netip.MustParseAddr("9.9.9.9")})...),
		},
		{
			name:      "success: AAAA query for IPv4 k8s host (name.namespace) with search namespace",
			host:      "productpage.ns1.ns1.svc.cluster.local.",
			queryAAAA: true,
		},
		{
			name:     "success: k8s host - non local namespace - name.namespace",
			host:     "example.ns2.",
			expected: a("example.ns2.", []netip.Addr{netip.MustParseAddr("10.10.10.10")}),
		},
		{
			name:     "success: k8s host - non local namespace - fqdn",
			host:     "example.ns2.svc.cluster.local.",
			expected: a("example.ns2.svc.cluster.local.", []netip.Addr{netip.MustParseAddr("10.10.10.10")}),
		},
		{
			name:     "success: k8s host - non local namespace - name.namespace.svc",
			host:     "example.ns2.svc.",
			expected: a("example.ns2.svc.", []netip.Addr{netip.MustParseAddr("10.10.10.10")}),
		},
		{
			name:                    "failure: k8s host - non local namespace - shortname",
			host:                    "example.",
			expectResolutionFailure: dns.RcodeNameError,
		},
		{
			name:     "success: alt host - name",
			host:     "svc-with-alt.",
			expected: a("svc-with-alt.", []netip.Addr{netip.MustParseAddr("15.15.15.15")}),
		},
		{
			name:     "success: alt host - name.namespace",
			host:     "svc-with-alt.ns1.",
			expected: a("svc-with-alt.ns1.", []netip.Addr{netip.MustParseAddr("15.15.15.15")}),
		},
		{
			name:     "success: alt host - name.namespace.svc",
			host:     "svc-with-alt.ns1.svc.",
			expected: a("svc-with-alt.ns1.svc.", []netip.Addr{netip.MustParseAddr("15.15.15.15")}),
		},
		{
			name:     "success: alt host - name.namespace.svc.cluster.local",
			host:     "svc-with-alt.ns1.svc.cluster.local.",
			expected: a("svc-with-alt.ns1.svc.cluster.local.", []netip.Addr{netip.MustParseAddr("15.15.15.15")}),
		},
		{
			name:     "success: alt host - name.namespace.svc.clusterset.local",
			host:     "svc-with-alt.ns1.svc.clusterset.local.",
			expected: a("svc-with-alt.ns1.svc.clusterset.local.", []netip.Addr{netip.MustParseAddr("15.15.15.15")}),
		},
		{
			name: "success: remote cluster k8s svc - same ns and different domain - fqdn",
			host: "details.ns2.svc.cluster.remote.",
			id:   2,
			expected: a("details.ns2.svc.cluster.remote.",
				[]netip.Addr{
					netip.MustParseAddr("13.13.13.13"),
					netip.MustParseAddr("14.14.14.14"),
					netip.MustParseAddr("12.12.12.12"),
					netip.MustParseAddr("11.11.11.11"),
				}),
		},
		{
			name: "success: remote cluster k8s svc round robin",
			host: "details.ns2.svc.cluster.remote.",
			id:   1,
			expected: a("details.ns2.svc.cluster.remote.",
				[]netip.Addr{
					netip.MustParseAddr("13.13.13.13"),
					netip.MustParseAddr("14.14.14.14"),
					netip.MustParseAddr("11.11.11.11"),
					netip.MustParseAddr("12.12.12.12"),
				}),
		},
		{
			name:                    "failure: remote cluster k8s svc - same ns and different domain - name.namespace",
			host:                    "details.ns2.",
			expectResolutionFailure: dns.RcodeNameError, // on home machines, the ISP may resolve to some generic webpage. So this test may fail on laptops
		},
		{
			name:     "success: TypeA query returns A records only",
			host:     "dual.localhost.",
			expected: a("dual.localhost.", []netip.Addr{netip.MustParseAddr("2.2.2.2")}),
		},
		{
			name:     "success: wild card returns A record correctly",
			host:     "foo.wildcard.",
			expected: a("foo.wildcard.", []netip.Addr{netip.MustParseAddr("10.10.10.10")}),
		},
		{
			name:     "success: specific wild card returns A record correctly",
			host:     "a.b.wildcard.",
			expected: a("a.b.wildcard.", []netip.Addr{netip.MustParseAddr("11.11.11.11")}),
		},
		{
			name: "success: wild card with with search namespace chained pointer correctly",
			host: "foo.wildcard.ns1.svc.cluster.local.",
			expected: append(cname("foo.wildcard.ns1.svc.cluster.local.", "*.wildcard."),
				a("*.wildcard.", []netip.Addr{netip.MustParseAddr("10.10.10.10")})...),
		},
		{
			name:     "success: wild card with domain returns A record correctly",
			host:     "foo.svc.mesh.company.net.",
			expected: a("foo.svc.mesh.company.net.", []netip.Addr{netip.MustParseAddr("10.1.2.3")}),
		},
		{
			name:     "success: wild card with namespace with domain returns A record correctly",
			host:     "foo.foons.svc.mesh.company.net.",
			expected: a("foo.foons.svc.mesh.company.net.", []netip.Addr{netip.MustParseAddr("10.1.2.3")}),
		},
		{
			name: "success: wild card with search domain returns A record correctly",
			host: "foo.svc.mesh.company.net.ns1.svc.cluster.local.",
			expected: append(cname("foo.svc.mesh.company.net.ns1.svc.cluster.local.", "*.svc.mesh.company.net."),
				a("*.svc.mesh.company.net.", []netip.Addr{netip.MustParseAddr("10.1.2.3")})...),
		},
		{
			name:      "success: TypeAAAA query returns AAAA records only",
			host:      "dual.localhost.",
			queryAAAA: true,
			expected:  aaaa("dual.localhost.", []netip.Addr{netip.MustParseAddr("2001:db8:0:0:0:ff00:42:8329")}),
		},
		{
			// This is not a NXDOMAIN, but empty response
			name: "success: Error response if only AAAA records exist for typeA",
			host: "ipv6.localhost.",
		},
		{
			// This is not a NXDOMAIN, but empty response
			name:      "success: Error response if only A records exist for typeAAAA",
			host:      "ipv4.localhost.",
			queryAAAA: true,
		},
		{
			name: "udp: large request",
			host: "giant.",
			// Upstream UDP server returns big response, we cannot serve it. Compliant server would truncate it.
			expectResolutionFailure: dns.RcodeServerFailure,
		},
		{
			name:     "tcp: large request",
			host:     "giant.",
			expected: giantResponse,
		},
		{
			name:                    "large request edns",
			host:                    "giant.",
			expectResolutionFailure: dns.RcodeSuccess,
			expected:                giantResponse,
			modifyReq: func(msg *dns.Msg) {
				msg.SetEdns0(dns.MaxMsgSize, false)
			},
		},
		{
			name:                    "large request truncated",
			host:                    "giant-tc.",
			expectResolutionFailure: dns.RcodeSuccess,
			expected:                giantResponse[:29],
		},
		{
			name:     "success: hostname with a period",
			host:     "example.localhost.",
			expected: a("example.localhost.", []netip.Addr{netip.MustParseAddr("3.3.3.3")}),
		},
	}

	clients := []dns.Client{
		{
			Timeout: 3 * time.Second,
			Net:     "udp",
			UDPSize: 65535,
		},
		{
			Timeout: 3 * time.Second,
			Net:     "tcp",
		},
	}
	addresses := []string{
		d.dnsProxies[0].Address(),
		d.dnsProxies[1].Address(),
	}
	currentID := atomic.NewInt32(0)
	oldID := dns.Id
	dns.Id = func() uint16 {
		return uint16(currentID.Inc())
	}
	defer func() { dns.Id = oldID }()
	for i := range clients {
		addr := addresses[i]
		for _, tt := range testCases {
			// Test is for explicit network
			if (strings.HasPrefix(tt.name, "udp") || strings.HasPrefix(tt.name, "tcp")) && !strings.HasPrefix(tt.name, clients[i].Net) {
				continue
			}
			t.Run(clients[i].Net+"-"+tt.name, func(t *testing.T) {
				m := new(dns.Msg)
				q := dns.TypeA
				if tt.queryAAAA {
					q = dns.TypeAAAA
				}
				m.SetQuestion(tt.host, q)
				if tt.modifyReq != nil {
					tt.modifyReq(m)
				}
				if tt.id != 0 {
					currentID.Store(int32(tt.id))
					defer func() { currentID.Store(0) }()
				}
				res, _, err := clients[i].Exchange(m, addr)
				if res != nil {
					t.Log("size: ", len(res.Answer))
				}
				if err != nil {
					t.Errorf("Failed to resolve query for %s: %v", tt.host, err)
				} else {
					for _, answer := range res.Answer {
						if answer.Header().Class != dns.ClassINET {
							t.Errorf("expected class INET for all responses, got %+v for host %s", answer.Header(), tt.host)
						}
					}
					if tt.expectExternalResolution {
						// just make sure that the response has a valid DNS response from upstream resolvers
						if res.Rcode != dns.RcodeSuccess {
							t.Errorf("upstream dns resolution for %s failed: %v", tt.host, res)
						}
					} else {
						if tt.expectResolutionFailure > 0 && tt.expectResolutionFailure != res.Rcode {
							t.Errorf("expected resolution failure does not match with response code for %s: expected: %v, got: %v",
								tt.host, tt.expectResolutionFailure, res.Rcode)
						}
						if !equalsDNSrecords(res.Answer, tt.expected) {
							t.Log(res)
							t.Errorf("dns responses for %s do not match. \n got %v\nwant %v", tt.host, res.Answer, tt.expected)
						}
					}
				}
			})
		}
	}
}

// Baseline:
//   - ~150us via agent if cached for A/AAAA
//   - ~300us via agent when doing the cname redirect
//   - 5-6ms to upstream resolver directly
//   - 6-7ms via agent to upstream resolver (cache miss)
//
// Also useful for load testing is using dnsperf. This can be run with:
//
//	docker run -v $PWD:$PWD -w $PWD --network host quay.io/ssro/dnsperf dnsperf -p 15053 -d input -c 100 -l 30
//
// where `input` contains dns queries to run, such as `echo.default. A`
func BenchmarkDNS(t *testing.B) {
	s := initDNS(t, false)
	t.Run("via-agent-cache-miss", func(b *testing.B) {
		bench(b, s.dnsProxies[0].Address(), "www.bing.com.")
	})
	t.Run("via-agent-cache-hit-fqdn", func(b *testing.B) {
		bench(b, s.dnsProxies[0].Address(), "www.google.com.")
	})
	t.Run("via-agent-cache-hit-cname", func(b *testing.B) {
		bench(b, s.dnsProxies[0].Address(), "www.google.com.ns1.svc.cluster.local.")
	})
}

func bench(t *testing.B, nameserver string, hostname string) {
	errs := 0
	nrs := 0
	nxdomain := 0
	cnames := 0
	c := dns.Client{
		Timeout: 1 * time.Second,
	}
	for i := 0; i < t.N; i++ {
		toResolve := hostname
	redirect:
		m := new(dns.Msg)
		m.SetQuestion(toResolve, dns.TypeA)
		res, _, err := c.Exchange(m, nameserver)

		if err != nil {
			errs++
		} else if len(res.Answer) == 0 {
			nrs++
		} else {
			for _, a := range res.Answer {
				if arec, ok := a.(*dns.A); !ok {
					// check if this is a cname redirect. If so, repeat the resolution
					// assuming the client does not see/respect the inlined A record in the response.
					if crec, ok := a.(*dns.CNAME); !ok {
						errs++
					} else {
						cnames++
						toResolve = crec.Target
						goto redirect
					}
				} else {
					if arec.Hdr.Rrtype != dns.RcodeSuccess {
						nxdomain++
					}
				}
			}
		}
	}

	if errs+nrs > 0 {
		t.Log("Sent", t.N, "err", errs, "no response", nrs, "nxdomain", nxdomain, "cname redirect", cnames)
	}
}

var giantResponse = func() []dns.RR {
	ips := make([]netip.Addr, 0)
	for i := 0; i < 64; i++ {
		ips = append(ips, netip.MustParseAddr(fmt.Sprintf("240.0.0.%d", i)))
	}
	return a("aaaaaaaaaaaa.aaaaaa.", ips)
}()

func makeUpstream(t test.Failer, responses map[string]string) string {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(resp dns.ResponseWriter, msg *dns.Msg) {
		answer := new(dns.Msg)
		answer.SetReply(msg)
		answer.Rcode = dns.RcodeNameError
		if err := resp.WriteMsg(answer); err != nil {
			t.Fatalf("err: %s", err)
		}
	})
	for hn, desiredResp := range responses {
		mux.HandleFunc(hn, func(resp dns.ResponseWriter, msg *dns.Msg) {
			answer := dns.Msg{
				Answer: a(hn, []netip.Addr{netip.MustParseAddr(desiredResp)}),
			}
			answer.SetReply(msg)
			answer.Rcode = dns.RcodeSuccess
			if err := resp.WriteMsg(&answer); err != nil {
				t.Fatalf("err: %s", err)
			}
		})
	}
	mux.HandleFunc("giant.", func(resp dns.ResponseWriter, msg *dns.Msg) {
		answer := &dns.Msg{
			Answer: giantResponse,
		}
		answer.SetReply(msg)
		answer.Rcode = dns.RcodeSuccess
		if err := resp.WriteMsg(answer); err != nil {
			t.Fatalf("err: %s", err)
		}
	})
	mux.HandleFunc("giant-tc.", func(resp dns.ResponseWriter, msg *dns.Msg) {
		answer := &dns.Msg{
			Answer: giantResponse,
		}
		answer.SetReply(msg)
		answer.Rcode = dns.RcodeSuccess
		answer.Truncate(size("udp", msg))
		if err := resp.WriteMsg(answer); err != nil {
			t.Fatalf("err: %s", err)
		}
	})
	up := make(chan struct{})

	tcp := &dns.Server{
		Addr:              "127.0.0.1:0",
		Net:               "tcp",
		Handler:           mux,
		NotifyStartedFunc: func() { close(up) },
	}
	go func() {
		if err := tcp.ListenAndServe(); err != nil {
			log.Warnf("listen error: %v", err)
		}
	}()
	select {
	case <-time.After(time.Second * 10):
		t.Fatal("setup timeout")
	case <-up:
	}

	// Setup UDP server on same port
	up = make(chan struct{})
	server := &dns.Server{
		Addr:              tcp.Listener.Addr().String(),
		Net:               "udp",
		Handler:           mux,
		NotifyStartedFunc: func() { close(up) },
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Warnf("listen error: %v", err)
		}
	}()
	select {
	case <-time.After(time.Second * 10):
		t.Fatal("setup timeout")
	case <-up:
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	server.Addr = server.PacketConn.LocalAddr().String()

	select {
	case <-time.After(time.Second * 10):
		t.Fatal("setup timeout")
	case <-up:
	}
	t.Cleanup(func() { _ = tcp.Shutdown() })
	t.Cleanup(func() { _ = server.Shutdown() })
	tcp.Addr = server.PacketConn.LocalAddr().String()
	return server.Addr
}

func initDNS(t test.Failer, forwardToUpstreamParallel bool) *LocalDNSServer {
	srv := makeUpstream(t, map[string]string{"www.bing.com.": "1.1.1.1"})
	testAgentDNS, err := NewLocalDNSServer("ns1", "ns1.svc.cluster.local", "localhost:0", forwardToUpstreamParallel)
	if err != nil {
		t.Fatal(err)
	}
	testAgentDNS.resolvConfServers = []string{srv}
	testAgentDNS.StartDNS()
	fillTable(testAgentDNS)
	t.Cleanup(testAgentDNS.Close)
	return testAgentDNS
}

func fillTable(server *LocalDNSServer) {
	server.searchNamespaces = []string{"ns1.svc.cluster.local", "svc.cluster.local", "cluster.local"}
	server.UpdateLookupTable(&dnsProto.NameTable{
		Table: map[string]*dnsProto.NameTable_NameInfo{
			"www.google.com": {
				Ips:      []string{"1.1.1.1"},
				Registry: "External",
			},
			"productpage.ns1.svc.cluster.local": {
				Ips:       []string{"9.9.9.9"},
				Registry:  "Kubernetes",
				Namespace: "ns1",
				Shortname: "productpage",
			},
			"example.ns2.svc.cluster.local": {
				Ips:       []string{"10.10.10.10"},
				Registry:  "Kubernetes",
				Namespace: "ns2",
				Shortname: "example",
			},
			"details.ns2.svc.cluster.remote": {
				Ips:       []string{"11.11.11.11", "12.12.12.12", "13.13.13.13", "14.14.14.14"},
				Registry:  "Kubernetes",
				Namespace: "ns2",
				Shortname: "details",
			},
			"svc-with-alt.ns1.svc.cluster.local": {
				Ips:       []string{"15.15.15.15"},
				Registry:  "Kubernetes",
				Namespace: "ns1",
				Shortname: "svc-with-alt",
				AltHosts: []string{
					"svc-with-alt.ns1.svc.clusterset.local",
				},
			},
			"ipv6.localhost": {
				Ips:      []string{"2001:db8:0:0:0:ff00:42:8329"},
				Registry: "External",
			},
			"dual.localhost": {
				Ips:      []string{"2.2.2.2", "2001:db8:0:0:0:ff00:42:8329"},
				Registry: "External",
			},
			"ipv4.localhost": {
				Ips:      []string{"2.2.2.2"},
				Registry: "External",
			},
			"*.b.wildcard": {
				Ips:      []string{"11.11.11.11"},
				Registry: "External",
			},
			"*.wildcard": {
				Ips:      []string{"10.10.10.10"},
				Registry: "External",
			},
			"*.svc.mesh.company.net": {
				Ips:      []string{"10.1.2.3"},
				Registry: "External",
			},
			"example.localhost.": {
				Ips:      []string{"3.3.3.3"},
				Registry: "External",
			},
		},
	})
}

// reflect.DeepEqual doesn't seem to work well for dns.RR
// as the Rdlength field is not updated in the a(), or aaaa() calls.
// so zero them out before doing reflect.Deepequal
func equalsDNSrecords(got []dns.RR, want []dns.RR) bool {
	for i := range got {
		got[i].Header().Rdlength = 0
	}
	return reflect.DeepEqual(got, want)
}
