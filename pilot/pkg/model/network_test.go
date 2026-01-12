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

package model_test

import (
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

func TestGatewayHostnames(t *testing.T) {
	test.SetForTest(t, &model.MinGatewayTTL, 30*time.Millisecond)
	ttl := uint32(0) // second

	gwHost := "test.gw.istio.io"
	workingDNSServer := newFakeDNSServer(ttl, sets.New(gwHost))
	failingDNSServer := newFakeDNSServer(ttl, sets.NewWithLength[string](0))
	failingDNSServer.setFailure(true)
	model.NetworkGatewayTestDNSServers = []string{
		// try resolving with the failing server first to make sure the next upstream is retried
		failingDNSServer.Server.PacketConn.LocalAddr().String(),
		workingDNSServer.Server.PacketConn.LocalAddr().String(),
	}
	t.Cleanup(func() {
		errW := workingDNSServer.Shutdown()
		errF := failingDNSServer.Shutdown()
		if errW != nil || errF != nil {
			t.Logf("failed shutting down fake dns servers")
		}
	})

	meshNetworks := meshwatcher.NewFixedNetworksWatcher(nil)
	xdsUpdater := xdsfake.NewFakeXDS()
	env := &model.Environment{NetworksWatcher: meshNetworks, ServiceDiscovery: memory.NewServiceDiscovery()}
	if err := env.InitNetworksManager(xdsUpdater); err != nil {
		t.Fatal(err)
	}

	var gateways []model.NetworkGateway
	t.Run("initial resolution", func(t *testing.T) {
		meshNetworks.SetNetworks(&meshconfig.MeshNetworks{Networks: map[string]*meshconfig.Network{
			"nw0": {Gateways: []*meshconfig.Network_IstioNetworkGateway{{
				Gw: &meshconfig.Network_IstioNetworkGateway_Address{
					Address: gwHost,
				},
				Port: 15443,
			}}},
		}})
		xdsUpdater.WaitOrFail(t, "xds full")
		gateways = env.NetworkManager.AllGateways()
		// A and AAAA
		if len(gateways) != 2 {
			t.Fatal("expected 2 IPs")
		}
		if gateways[0].Network != "nw0" || gateways[1].Network != "nw0" {
			t.Fatalf("unexpected network: %v", gateways)
		}
	})

	t.Run("re-resolve after TTL", func(t *testing.T) {
		// after the update, we should see the next gateway. Since TTL is low we don't know the exact IP, but we know it should change from
		// the original
		assert.EventuallyEqual(t, env.NetworkManager.AllGateways, gateways)
		xdsUpdater.WaitOrFail(t, "xds full")
	})

	workingDNSServer.setFailure(true)
	gateways = env.NetworkManager.AllGateways()
	t.Run("resolution failed", func(t *testing.T) {
		xdsUpdater.AssertEmpty(t, 10*model.MinGatewayTTL)
		// the gateways should remain
		currentGateways := env.NetworkManager.AllGateways()
		if len(currentGateways) == 0 || !reflect.DeepEqual(currentGateways, gateways) {
			t.Fatalf("unexpected network: %v", currentGateways)
		}
		if !env.NetworkManager.IsMultiNetworkEnabled() {
			t.Fatal("multi network is not enabled")
		}
	})

	workingDNSServer.setFailure(false)
	t.Run("resolution recovered", func(t *testing.T) {
		// addresses should be updated
		retry.UntilOrFail(t, func() bool {
			return !reflect.DeepEqual(env.NetworkManager.AllGateways(), gateways)
		}, retry.Timeout(10*model.MinGatewayTTL), retry.Delay(time.Millisecond*10))
		xdsUpdater.WaitOrFail(t, "xds full")
	})

	workingDNSServer.setHosts(make(sets.String))
	t.Run("no answer", func(t *testing.T) {
		assert.EventuallyEqual(t, func() int {
			return len(env.NetworkManager.AllGateways())
		}, 0, retry.Timeout(10*model.MinGatewayTTL))
		xdsUpdater.WaitOrFail(t, "xds full")
		if env.NetworkManager.IsMultiNetworkEnabled() {
			t.Fatal("multi network should not be enabled when there are no gateways")
		}
	})

	workingDNSServer.setHosts(sets.New(gwHost))
	t.Run("new answer", func(t *testing.T) {
		retry.UntilOrFail(t, func() bool {
			return len(env.NetworkManager.AllGateways()) != 0 &&
				!reflect.DeepEqual(env.NetworkManager.AllGateways(), gateways)
		}, retry.Timeout(10*model.MinGatewayTTL), retry.Delay(time.Millisecond*10))
		xdsUpdater.WaitOrFail(t, "xds full")
	})

	t.Run("forget", func(t *testing.T) {
		meshNetworks.SetNetworks(nil)
		xdsUpdater.WaitOrFail(t, "xds full")
		if len(env.NetworkManager.AllGateways()) > 0 {
			t.Fatal("expected no gateways")
		}
	})
}

type fakeDNSServer struct {
	*dns.Server
	ttl     uint32
	failure bool

	mu sync.Mutex
	// map fqdn hostname -> successful query count
	hosts map[string]int
}

func newFakeDNSServer(ttl uint32, hosts sets.String) *fakeDNSServer {
	var wg sync.WaitGroup
	wg.Add(1)
	s := &fakeDNSServer{
		Server: &dns.Server{Addr: ":0", Net: "udp", NotifyStartedFunc: wg.Done},
		ttl:    ttl,
		hosts:  make(map[string]int, len(hosts)),
	}
	s.Handler = s

	for k := range hosts {
		s.hosts[dns.Fqdn(k)] = 0
	}

	go func() {
		if err := s.ListenAndServe(); err != nil {
			scopes.Framework.Errorf("fake dns server error: %v", err)
		}
	}()
	wg.Wait()
	return s
}

func (s *fakeDNSServer) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg := (&dns.Msg{}).SetReply(r)
	if s.failure {
		msg.Rcode = dns.RcodeServerFailure
	} else {
		domain := msg.Question[0].Name
		c, ok := s.hosts[domain]
		if ok {
			s.hosts[domain]++
			switch r.Question[0].Qtype {
			case dns.TypeA:
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: s.ttl},
					A:   net.ParseIP(fmt.Sprintf("10.0.0.%d", c)),
				})
			case dns.TypeAAAA:
				// set a long TTL for AAAA
				msg.Answer = append(msg.Answer, &dns.AAAA{
					Hdr:  dns.RR_Header{Name: domain, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: s.ttl * 10},
					AAAA: net.ParseIP(fmt.Sprintf("fd00::%x", c)),
				})
			// simulate behavior of some public/cloud DNS like Cloudflare or DigitalOcean
			case dns.TypeANY:
				msg.Rcode = dns.RcodeRefused
			default:
				msg.Rcode = dns.RcodeNotImplemented
			}
		} else {
			msg.Rcode = dns.RcodeNameError
		}
	}
	if err := w.WriteMsg(msg); err != nil {
		scopes.Framework.Errorf("failed writing fake DNS response: %v", err)
	}
}

func (s *fakeDNSServer) setHosts(hosts sets.String) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts = make(map[string]int, len(hosts))
	for k := range hosts {
		s.hosts[dns.Fqdn(k)] = 0
	}
}

func (s *fakeDNSServer) setFailure(failure bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = failure
}
