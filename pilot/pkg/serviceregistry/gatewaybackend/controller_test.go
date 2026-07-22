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

package gatewaybackend

import (
	"fmt"
	"sync"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test/util/retry"
)

func TestControllerReferenceLifecycle(t *testing.T) {
	bindings := krt.NewStaticCollection[RuntimeBackend](nil, nil)
	c := NewController(bindings, "cluster-0", nil)

	var mu sync.Mutex
	var events []model.Event
	c.AppendServiceHandler(func(_, _ *model.Service, event model.Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	internal := host.Name("payments.team-a.xbackend.internal")
	first := RuntimeBackend{
		EdgeKey: "team-a/gateway-a/payments", InternalName: internal,
		Namespace: "team-a", Port: 443, Protocol: protocol.HTTPS,
		ExternalAddress: "api.example.com",
	}
	second := first
	second.EdgeKey = "team-a/gateway-b/payments"

	bindings.UpdateObject(first)
	waitForServices(t, c, 1)
	svc := c.GetService(internal)
	if svc == nil {
		t.Fatal("opaque service was not registered")
	}
	if svc.Hostname == host.Name(first.ExternalAddress) {
		t.Fatalf("external hostname %q must not be registered as service identity", first.ExternalAddress)
	}
	if svc.Attributes.ServiceRegistry != provider.GatewayBackend || !svc.MeshExternal || svc.Resolution != model.DNSLB {
		t.Fatalf("unexpected runtime service: %+v", svc)
	}
	c.mu.RLock()
	got := c.services[internal].endpoint.Addresses
	c.mu.RUnlock()
	if len(got) != 1 || got[0] != first.ExternalAddress {
		t.Fatalf("unexpected DNS endpoint: %v", got)
	}

	bindings.UpdateObject(second)
	waitForServices(t, c, 1)
	bindings.DeleteObject(first.EdgeKey)
	waitForServices(t, c, 1)
	bindings.DeleteObject(second.EdgeKey)
	waitForServices(t, c, 0)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != model.EventAdd || events[1] != model.EventDelete {
		t.Fatalf("expected one add and one last-reference delete, got %v", events)
	}
}

func TestControllerSameExternalHostnameHasDistinctOpaqueServices(t *testing.T) {
	bindings := krt.NewStaticCollection(nil, []RuntimeBackend{
		{EdgeKey: "gw/a", InternalName: "a.xbackend.internal", Namespace: "ns", Port: 443, Protocol: protocol.HTTPS, ExternalAddress: "shared.example.com"},
		{EdgeKey: "gw/b", InternalName: "b.xbackend.internal", Namespace: "ns", Port: 8443, Protocol: protocol.TCP, ExternalAddress: "shared.example.com"},
	})
	c := NewController(bindings, "cluster-0", nil)
	waitForServices(t, c, 2)
	if c.GetService("shared.example.com") != nil {
		t.Fatal("real external hostname became a frontend")
	}
	if c.GetService("a.xbackend.internal") == nil || c.GetService("b.xbackend.internal") == nil {
		t.Fatalf("expected two independent opaque services: %v", c.Services())
	}
}

func waitForServices(t *testing.T, c *Controller, want int) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		if got := len(c.Services()); got != want {
			return fmt.Errorf("got %d services, want %d", got, want)
		}
		return nil
	})
}
