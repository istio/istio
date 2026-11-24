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

package xds_test

import (
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	xdsfake "istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

// An in-flight full push takes allowed SANs (CDS) from its PushContext snapshot,
// but builds EDS from the live EndpointIndex. If a Service's endpoints switch to
// a workload with a different identity during that push, Envoy would receive the
// new endpoint with the old SAN list and fail with CERTIFICATE_VERIFY_FAILED/SAN
// matcher until the next push. The in-flight push must not send those endpoints;
// the next push sends both the updated SAN list and endpoints.
func TestInFlightPushSANEndpointMismatch(t *testing.T) {
	const (
		ns               = "default"
		hostname         = "service.default.svc.cluster.local"
		clusterNm        = "outbound|80||" + hostname
		originalIdentity = "spiffe://cluster.local/ns/default/sa/original"
		updatedIdentity  = "spiffe://cluster.local/ns/default/sa/updated"
	)
	for _, tc := range []struct {
		name         string
		nextIdentity string
		wantSkipped  bool
	}{
		// Replacing a Pod with a different service account requires updated SANs.
		{name: "identity changes", nextIdentity: updatedIdentity, wantSkipped: true},
		// Control: replacing a Pod with the same identity is sent immediately.
		{name: "same identity", nextIdentity: originalIdentity, wantSkipped: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := xdsfake.NewFakeDiscoveryServer(t, xdsfake.FakeOptions{})
			svc := &model.Service{
				Hostname:   host.Name(hostname),
				Ports:      model.PortList{{Name: "http", Port: 80, Protocol: protocol.HTTP}},
				Attributes: model.ServiceAttributes{Namespace: ns},
			}
			s.MemRegistry.AddService(svc)
			ep := func(ip, sa string) []*model.IstioEndpoint {
				return []*model.IstioEndpoint{{
					Addresses: []string{ip}, EndpointPort: 8080, ServicePortName: "http",
					ServiceAccount: sa, TLSMode: model.IstioMutualTLSModeLabel, HealthStatus: model.Healthy,
				}}
			}
			s.MemRegistry.SetEndpoints(hostname, ns, ep("10.0.0.1", originalIdentity))
			// Wait until a push context exists that knows the original identity.
			assert.EventuallyEqual(t, func() []string { return s.PushContext().ServiceAccounts(svc.Hostname, ns) }, []string{originalIdentity})

			// A full push begins: its PushContext snapshot is taken now.
			inFlight := s.PushContext()
			inFlightStart := time.Now()

			// The Service's endpoints are replaced mid-push. The registry writes the
			// live EndpointIndex immediately; the corrective push is still debounced.
			s.MemRegistry.SetEndpoints(hostname, ns, ep("10.0.0.2", tc.nextIdentity))

			// The in-flight push now generates this proxy's CDS and EDS, using the
			// same generators and request that a real push uses.
			proxy := s.SetupProxy(nil)
			req := &model.PushRequest{Push: inFlight, Forced: true, Start: inFlightStart}
			watched := &model.WatchedResource{ResourceNames: sets.New(clusterNm)}
			sans, ips := allowedSANs(t, s, proxy, watched, req), endpointIPs(t, s, proxy, watched, req)
			t.Logf("in-flight push sends CDS SANs=%v with EDS endpoints=%v", sans, ips)
			assert.Equal(t, sans, []string{originalIdentity})

			if !tc.wantSkipped {
				assert.Equal(t, ips, []string{"10.0.0.2"})
				return
			}
			// Envoy keeps its previous endpoints, which match the old SANs.
			if len(ips) != 0 {
				t.Fatalf("in-flight push sent endpoints %v that its SANs %v reject", ips, sans)
			}

			// The service account change queued a full push; its context allows the new
			// identity, so CDS and EDS are sent together and consistently.
			assert.EventuallyEqual(t, func() []string { return s.PushContext().ServiceAccounts(svc.Hostname, ns) }, []string{updatedIdentity})
			next := &model.PushRequest{Push: s.PushContext(), Forced: true, Start: time.Now()}
			assert.Equal(t, allowedSANs(t, s, proxy, watched, next), []string{updatedIdentity})
			assert.Equal(t, endpointIPs(t, s, proxy, watched, next), []string{"10.0.0.2"})
			t.Log("next push sends matching SANs and endpoints")

			// The next push cached its endpoints. A stale in-flight push must not reuse them.
			if ips := endpointIPs(t, s, proxy, watched, req); len(ips) != 0 {
				t.Fatalf("stale push reused cached endpoints %v that its SANs reject", ips)
			}
		})
	}
}

func allowedSANs(t *testing.T, s *xdsfake.FakeDiscoveryServer, p *model.Proxy, w *model.WatchedResource, req *model.PushRequest) []string {
	t.Helper()
	res, _, err := s.Discovery.Generators[v3.ClusterType].Generate(p, w, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		c := &cluster.Cluster{}
		if err := r.Resource.UnmarshalTo(c); err != nil {
			t.Fatal(err)
		}
		if c.Name != w.ResourceNames.UnsortedList()[0] {
			continue
		}
		for _, m := range c.GetTransportSocketMatches() {
			ctx := &tls.UpstreamTlsContext{}
			if m.GetTransportSocket().GetTypedConfig().UnmarshalTo(ctx) != nil {
				continue
			}
			var out []string
			//nolint: staticcheck
			for _, san := range ctx.GetCommonTlsContext().GetCombinedValidationContext().GetDefaultValidationContext().GetMatchSubjectAltNames() {
				out = append(out, san.GetExact())
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	t.Fatal("no mTLS SAN list found in CDS")
	return nil
}

func endpointIPs(t *testing.T, s *xdsfake.FakeDiscoveryServer, p *model.Proxy, w *model.WatchedResource, req *model.PushRequest) []string {
	t.Helper()
	res, _, err := s.Discovery.Generators[v3.EndpointType].Generate(p, w, req)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range res {
		cla := &endpoint.ClusterLoadAssignment{}
		if err := r.Resource.UnmarshalTo(cla); err != nil {
			t.Fatal(err)
		}
		for _, l := range cla.GetEndpoints() {
			for _, e := range l.GetLbEndpoints() {
				out = append(out, e.GetEndpoint().GetAddress().GetSocketAddress().GetAddress())
			}
		}
	}
	return out
}
