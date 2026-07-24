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

package core

import (
	"sort"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

const testDNSDomain = "testns.svc.cluster.local"

// externalService builds a ClientSideLB ServiceEntry-style service with a single VIP.
func externalService(hostname, vip string) *model.Service {
	return &model.Service{
		Hostname:       host.Name(hostname),
		DefaultAddress: vip,
		Ports:          model.PortList{{Name: "http", Port: 80, Protocol: protocol.HTTP}},
		Resolution:     model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Name:            hostname,
			Namespace:       "testns",
			ServiceRegistry: provider.External,
		},
	}
}

// headlessService builds a Kubernetes headless (Passthrough) service.
func headlessK8sService(hostname string) *model.Service {
	return &model.Service{
		Hostname:       host.Name(hostname),
		DefaultAddress: constants.UnspecifiedIP,
		Ports:          model.PortList{{Name: "tcp", Port: 9000, Protocol: protocol.TCP}},
		Resolution:     model.Passthrough,
		Attributes: model.ServiceAttributes{
			Name:            hostname,
			Namespace:       "testns",
			ServiceRegistry: provider.Kubernetes,
		},
	}
}

func podEndpoints(ip string, svc *model.Service, hostname string) map[int][]*model.IstioEndpoint {
	instances := make(map[int][]*model.IstioEndpoint)
	for _, port := range svc.Ports {
		instances[port.Port] = []*model.IstioEndpoint{{
			Addresses:       []string{ip},
			ServicePortName: port.Name,
			EndpointPort:    uint32(port.Port),
			HealthStatus:    model.Healthy,
			HostName:        hostname,
			SubDomain:       "headless-svc",
		}}
	}
	return instances
}

// pushWith builds a push context containing the given services and their (optional) endpoints.
func pushWith(services []*model.Service, instances map[*model.Service]map[int][]*model.IstioEndpoint) *model.PushContext {
	push := model.NewPushContext()
	push.Mesh = &meshconfig.MeshConfig{RootNamespace: "istio-system"}
	push.AddPublicServices(services)
	for svc, inst := range instances {
		push.AddServiceInstances(svc, inst)
	}
	return push
}

// deltaProxy builds a delta-NDS-capable sidecar proxy whose SidecarScope is derived from newPush
// and whose PrevSidecarScope is derived from prevPush (mirroring the real SetSidecarScope flow).
func deltaProxy(prevPush, newPush *model.PushContext) *model.Proxy {
	proxy := &model.Proxy{
		IPAddresses: []string{"1.1.1.1"},
		Metadata:    &model.NodeMetadata{DeltaNDS: model.StringBool(true)},
		Type:        model.SidecarProxy,
		DNSDomain:   testDNSDomain,
	}
	proxy.SetSidecarScope(prevPush)
	proxy.SetSidecarScope(newPush)
	return proxy
}

func serviceEntryUpdate(hostnames ...string) *model.PushRequest {
	cfgs := sets.New[model.ConfigKey]()
	for _, h := range hostnames {
		cfgs.Insert(model.ConfigKey{Kind: kind.ServiceEntry, Name: h, Namespace: "testns"})
	}
	return &model.PushRequest{ConfigsUpdated: cfgs}
}

func watched(names ...string) *model.WatchedResource {
	return &model.WatchedResource{ResourceNames: sets.New(names...)}
}

func resourceNames(res []*discovery.Resource) []string {
	names := make([]string, 0, len(res))
	for _, r := range res {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	return names
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// TestBuildDeltaNameTableServiceEntryNotOwnedAsPerPod verifies that changing a ServiceEntry
// hostname does not cause an unrelated watched hostname that happens to be a single-label prefix
// of it (e.g. x.foo.bar.com vs foo.bar.com) to be treated as a per-pod record and deleted.
// Only Kubernetes headless services own per-pod records.
func TestBuildDeltaNameTableServiceEntryNotOwnedAsPerPod(t *testing.T) {
	parent := externalService("foo.bar.com", "10.0.0.1")
	// x.foo.bar.com looks like a per-pod record of foo.bar.com under isOwnedBy, but it is a
	// distinct, unchanged ServiceEntry.
	child := externalService("x.foo.bar.com", "10.0.0.2")

	prev := pushWith([]*model.Service{parent, child}, nil)
	next := pushWith([]*model.Service{parent, child}, nil)
	proxy := deltaProxy(prev, next)

	cg := &ConfigGeneratorImpl{}
	// Only the parent ServiceEntry changed.
	res, removed, _, delta := cg.BuildDeltaNameTable(proxy, withPush(serviceEntryUpdate("foo.bar.com"), next),
		watched("foo.bar.com", "x.foo.bar.com"))

	if !delta {
		t.Fatalf("expected a true delta build")
	}
	if len(removed) != 0 {
		t.Errorf("no resource should be removed when a sibling ServiceEntry changes, got removed=%v", removed)
	}
	if got := resourceNames(res); !slicesEqual(got, []string{"foo.bar.com"}) {
		t.Errorf("expected only foo.bar.com to be rebuilt, got %v", got)
	}
}

// TestBuildDeltaNameTableHeadlessDeletion verifies that deleting a Kubernetes headless service
// removes both its own record and all of its per-pod records from the watched set.
func TestBuildDeltaNameTableHeadlessDeletion(t *testing.T) {
	svc := headlessK8sService("headless-svc.testns.svc.cluster.local")
	prev := pushWith([]*model.Service{svc}, map[*model.Service]map[int][]*model.IstioEndpoint{
		svc: podEndpoints("1.2.3.1", svc, "pod1"),
	})
	// pod2 lived in a prior push too; both per-pod records are being watched.
	prev.AddServiceInstances(svc, podEndpoints("1.2.3.2", svc, "pod2"))

	// New push no longer has the headless service.
	next := pushWith([]*model.Service{externalService("other.example.com", "10.0.0.9")}, nil)
	proxy := deltaProxy(prev, next)

	cg := &ConfigGeneratorImpl{}
	_, removed, _, delta := cg.BuildDeltaNameTable(proxy,
		withPush(serviceEntryUpdate("headless-svc.testns.svc.cluster.local"), next),
		watched(
			"headless-svc.testns.svc.cluster.local",
			"pod1.headless-svc.testns.svc.cluster.local",
			"pod2.headless-svc.testns.svc.cluster.local",
		))

	if !delta {
		t.Fatalf("expected a true delta build")
	}
	want := []string{
		"headless-svc.testns.svc.cluster.local",
		"pod1.headless-svc.testns.svc.cluster.local",
		"pod2.headless-svc.testns.svc.cluster.local",
	}
	if got := sorted(removed); !slicesEqual(got, want) {
		t.Errorf("expected the service and all per-pod records removed\n got: %v\nwant: %v", got, want)
	}
}

// TestBuildDeltaNameTableHeadlessScaleDown verifies that when a still-headless service loses a pod,
// the removed pod's per-pod record is cleaned up from the watched set.
func TestBuildDeltaNameTableHeadlessScaleDown(t *testing.T) {
	svc := headlessK8sService("headless-svc.testns.svc.cluster.local")

	prev := pushWith([]*model.Service{svc}, map[*model.Service]map[int][]*model.IstioEndpoint{
		svc: podEndpoints("1.2.3.1", svc, "pod1"),
	})
	prev.AddServiceInstances(svc, podEndpoints("1.2.3.2", svc, "pod2"))
	prev.AddServiceInstances(svc, podEndpoints("1.2.3.3", svc, "pod3"))

	// pod3 is scaled down; the service is still headless.
	next := pushWith([]*model.Service{svc}, map[*model.Service]map[int][]*model.IstioEndpoint{
		svc: podEndpoints("1.2.3.1", svc, "pod1"),
	})
	next.AddServiceInstances(svc, podEndpoints("1.2.3.2", svc, "pod2"))
	proxy := deltaProxy(prev, next)

	cg := &ConfigGeneratorImpl{}
	_, removed, _, delta := cg.BuildDeltaNameTable(proxy,
		withPush(serviceEntryUpdate("headless-svc.testns.svc.cluster.local"), next),
		watched(
			"headless-svc.testns.svc.cluster.local",
			"pod1.headless-svc.testns.svc.cluster.local",
			"pod2.headless-svc.testns.svc.cluster.local",
			"pod3.headless-svc.testns.svc.cluster.local",
		))

	if !delta {
		t.Fatalf("expected a true delta build")
	}
	want := []string{"pod3.headless-svc.testns.svc.cluster.local"}
	if got := sorted(removed); !slicesEqual(got, want) {
		t.Errorf("expected only the scaled-down pod record removed\n got: %v\nwant: %v", got, want)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func withPush(pr *model.PushRequest, push *model.PushContext) *model.PushRequest {
	pr.Push = push
	return pr
}
