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

package serviceentry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func createConfigs(configs []*config.Config, store model.ConfigStore, t testing.TB) {
	t.Helper()
	for _, cfg := range configs {
		_, err := store.Create(*cfg)
		if err != nil && strings.Contains(err.Error(), "item already exists") {
			_, err := store.Update(*cfg)
			if err != nil {
				t.Fatalf("error occurred updating ServiceEntry config: %v", err)
			}
		} else if err != nil {
			t.Fatalf("error occurred creating ServiceEntry config: %v", err)
		}
	}
}

func callInstanceHandlers(instances []*model.WorkloadInstance, sd *Controller, ev model.Event, t testing.TB) {
	t.Helper()
	for _, instance := range instances {
		sd.WorkloadInstanceHandler(instance, ev)
	}
}

func deleteConfigs(configs []*config.Config, store model.ConfigStore, t testing.TB) {
	t.Helper()
	for _, cfg := range configs {
		err := store.Delete(cfg.GroupVersionKind, cfg.Name, cfg.Namespace, nil)
		if err != nil {
			t.Errorf("error occurred crearting ServiceEntry config: %v", err)
		}
	}
}

type Event struct {
	kind      string
	host      string
	namespace string
	proxyIP   string
	endpoints int
	pushReq   *model.PushRequest
}

type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan Event
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

func (fx *FakeXdsUpdater) EDSUpdate(_ model.ShardKey, hostname string, namespace string, entry []*model.IstioEndpoint) {
	fx.Events <- Event{kind: "eds", host: hostname, namespace: namespace, endpoints: len(entry)}
}

func (fx *FakeXdsUpdater) EDSCacheUpdate(_ model.ShardKey, _, _ string, _ []*model.IstioEndpoint) {
}

func (fx *FakeXdsUpdater) ConfigUpdate(req *model.PushRequest) {
	fx.Events <- Event{kind: "xds", pushReq: req}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_ cluster.ID, ip string) {
	fx.Events <- Event{kind: "xds", proxyIP: ip}
}

func (fx *FakeXdsUpdater) SvcUpdate(_ model.ShardKey, hostname string, namespace string, _ model.Event) {
	fx.Events <- Event{kind: "svcupdate", host: hostname, namespace: namespace}
}

func (fx *FakeXdsUpdater) RemoveShard(_ model.ShardKey) {
	fx.Events <- Event{kind: "removeshard"}
}

func waitUntilEvent(t testing.TB, ch chan Event, event Event) {
	t.Helper()
	for {
		select {
		case e := <-ch:
			if e == event {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %v", event)
			return
		}
	}
}

func waitForEvent(t testing.TB, ch chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event")
		return Event{}
	}
}

func initServiceDiscovery() (model.ConfigStore, *Controller, chan Event, func()) {
	return initServiceDiscoveryWithOpts(false)
}

// initServiceDiscoveryWithoutEvents initializes a test setup with no events. This avoids excessive attempts to push
// EDS updates to a full queue
func initServiceDiscoveryWithoutEvents(t test.Failer) (model.ConfigStore, *Controller) {
	store := memory.Make(collections.Pilot)
	configController := memory.NewController(store)

	stop := make(chan struct{})
	go configController.Run(stop)

	eventch := make(chan Event, 100)
	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-eventch: // drain
			}
		}
	}()

	istioStore := model.MakeIstioStore(configController)
	serviceController := NewController(configController, istioStore, xdsUpdater)
	t.Cleanup(func() {
		close(stop)
	})
	return istioStore, serviceController
}

func initServiceDiscoveryWithOpts(workloadOnly bool, opts ...Option) (model.ConfigStore, *Controller, chan Event, func()) {
	store := memory.Make(collections.Pilot)
	configController := memory.NewController(store)

	stop := make(chan struct{})
	go configController.Run(stop)

	eventch := make(chan Event, 100)
	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}

	istioStore := model.MakeIstioStore(configController)
	var controller *Controller
	if !workloadOnly {
		controller = NewController(configController, istioStore, xdsUpdater, opts...)
	} else {
		controller = NewWorkloadEntryController(configController, istioStore, xdsUpdater, opts...)
	}
	go controller.Run(stop)
	return istioStore, controller, eventch, func() {
		close(stop)
	}
}

func TestServiceDiscoveryServices(t *testing.T) {
	store, sd, eventCh, stopFn := initServiceDiscovery()
	defer stopFn()
	expectedServices := []*model.Service{
		makeService("*.istio.io", "httpDNSRR", constants.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSRoundRobinLB),
		makeService("*.google.com", "httpDNS", constants.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
		makeService("tcpstatic.com", "tcpStatic", "172.217.0.1", map[string]int{"tcp-444": 444}, true, model.ClientSideLB),
	}

	createConfigs([]*config.Config{httpDNS, httpDNSRR, tcpStatic}, store, t)

	waitUntilEvent(t, eventCh, Event{
		kind:      "svcupdate",
		host:      "*.google.com",
		namespace: httpDNS.Namespace,
	})

	waitUntilEvent(t, eventCh, Event{
		kind:      "svcupdate",
		host:      "*.istio.io",
		namespace: httpDNSRR.Namespace,
	})

	waitUntilEvent(t, eventCh, Event{
		kind:      "svcupdate",
		host:      "tcpstatic.com",
		namespace: tcpStatic.Namespace,
	})

	services := sd.Services()
	sortServices(services)
	sortServices(expectedServices)
	if err := compare(t, services, expectedServices); err != nil {
		t.Error(err)
	}
}

func TestServiceDiscoveryGetService(t *testing.T) {
	hostname := "*.google.com"
	hostDNE := "does.not.exist.local"

	store, sd, eventCh, stopFn := initServiceDiscovery()
	defer stopFn()

	createConfigs([]*config.Config{httpDNS, tcpStatic}, store, t)
	waitForEvent(t, eventCh)
	waitForEvent(t, eventCh)
	service := sd.GetService(host.Name(hostDNE))
	if service != nil {
		t.Errorf("GetService(%q) => should not exist, got %s", hostDNE, service.Hostname)
	}

	service = sd.GetService(host.Name(hostname))
	if service == nil {
		t.Fatalf("GetService(%q) => should exist", hostname)
	}
	if service.Hostname != host.Name(hostname) {
		t.Errorf("GetService(%q) => %q, want %q", hostname, service.Hostname, hostname)
	}
}

// TestServiceDiscoveryServiceUpdate test various add/update/delete events for ServiceEntry
// nolint: lll
func TestServiceDiscoveryServiceUpdate(t *testing.T) {
	store, sd, events, stopFn := initServiceDiscovery()
	defer stopFn()
	// httpStaticOverlayUpdated is the same as httpStaticOverlay but with an extra endpoint added to test updates
	httpStaticOverlayUpdated := func() *config.Config {
		c := httpStaticOverlay.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		se.Endpoints = append(se.Endpoints, &networking.WorkloadEntry{
			Address: "6.6.6.6",
			Labels:  map[string]string{"other": "bar"},
		})
		return &c
	}()
	// httpStaticOverlayUpdatedInstance is the same as httpStaticOverlayUpdated but with an extra endpoint added that has the same address
	httpStaticOverlayUpdatedInstance := func() *config.Config {
		c := httpStaticOverlayUpdated.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		se.Endpoints = append(se.Endpoints, &networking.WorkloadEntry{
			Address: "6.6.6.6",
			Labels:  map[string]string{"some-new-label": "bar"},
		})
		return &c
	}()

	// httpStaticOverlayUpdatedNop is the same as httpStaticOverlayUpdated but with a NOP change
	httpStaticOverlayUpdatedNop := func() *config.Config {
		c := httpStaticOverlayUpdated.DeepCopy()
		return &c
	}()

	// httpStaticOverlayUpdatedNs is the same as httpStaticOverlay but with an extra endpoint and different namespace added to test updates
	httpStaticOverlayUpdatedNs := func() *config.Config {
		c := httpStaticOverlay.DeepCopy()
		c.Namespace = "other"
		se := c.Spec.(*networking.ServiceEntry)
		se.Endpoints = append(se.Endpoints, &networking.WorkloadEntry{
			Address: "7.7.7.7",
			Labels:  map[string]string{"namespace": "bar"},
		})
		return &c
	}()

	// Setup the expected instances for `httpStatic`. This will be added/removed from as we add various configs
	baseInstances := []*model.ServiceInstance{
		makeInstance(httpStatic, "2.2.2.2", 7080, httpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpStatic, "2.2.2.2", 18080, httpStatic.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
		makeInstance(httpStatic, "3.3.3.3", 1080, httpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpStatic, "3.3.3.3", 8080, httpStatic.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
		makeInstance(httpStatic, "4.4.4.4", 1080, httpStatic.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, PlainText),
		makeInstance(httpStatic, "4.4.4.4", 8080, httpStatic.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"foo": "bar"}, PlainText),
	}

	t.Run("simple entry", func(t *testing.T) {
		// Create a SE, expect the base instances
		createConfigs([]*config.Config{httpStatic}, store, t)
		instances := baseInstances
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStatic.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: httpStatic.Spec.(*networking.ServiceEntry).Hosts[0], Namespace: httpStatic.Namespace}: {}}}})
	})

	t.Run("add entry", func(t *testing.T) {
		// Create another SE for the same host, expect these instances to get added
		createConfigs([]*config.Config{httpStaticOverlay}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: httpStaticOverlay.Spec.(*networking.ServiceEntry).Hosts[0], Namespace: httpStaticOverlay.Namespace}: {}}}})
	})

	t.Run("add endpoint", func(t *testing.T) {
		// Update the SE for the same host, expect these instances to get added
		createConfigs([]*config.Config{httpStaticOverlayUpdated}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})

		// Make a NOP change, expect that there are no changes
		createConfigs([]*config.Config{httpStaticOverlayUpdatedNop}, store, t)
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedNop, 0, instances)
		// TODO this could trigger no changes
		expectEvents(t, events, Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})
	})

	t.Run("overlapping address", func(t *testing.T) {
		// Add another SE with an additional endpoint with a matching address
		createConfigs([]*config.Config{httpStaticOverlayUpdatedInstance}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"some-new-label": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedInstance, 0, instances)
		proxyInstances := []*model.ServiceInstance{
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"some-new-label": "bar"}, PlainText),
		}
		expectProxyInstances(t, sd, proxyInstances, "6.6.6.6")
		// TODO 45 is wrong
		expectEvents(t, events, Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})

		// Remove the additional endpoint
		createConfigs([]*config.Config{httpStaticOverlayUpdated}, store, t)
		instances = append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		proxyInstances = []*model.ServiceInstance{
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText),
		}
		expectProxyInstances(t, sd, proxyInstances, "6.6.6.6")
		expectEvents(t, events, Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})
	})

	t.Run("update removes endpoint", func(t *testing.T) {
		// Update the SE for the same host to remove the endpoint
		createConfigs([]*config.Config{httpStaticOverlay}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStaticOverlay, 0, instances)
		expectEvents(t, events,
			Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})
	})

	t.Run("different namespace", func(t *testing.T) {
		// Update the SE for the same host in a different ns, expect these instances to get added
		createConfigs([]*config.Config{httpStaticOverlayUpdatedNs}, store, t)
		instances := []*model.ServiceInstance{
			makeInstance(httpStaticOverlayUpdatedNs, "5.5.5.5", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlayUpdatedNs, "7.7.7.7", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"namespace": "bar"}, PlainText),
		}
		// This lookup is per-namespace, so we should only see the objects in the same namespace
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedNs, 0, instances)
		// Expect a full push, as the Service has changed
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: "other"},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Hosts[0], Namespace: httpStaticOverlayUpdatedNs.Namespace}: {}}}})
	})

	t.Run("delete entry", func(t *testing.T) {
		// Delete the additional SE in same namespace , expect it to get removed
		deleteConfigs([]*config.Config{httpStaticOverlayUpdated}, store, t)
		expectServiceInstances(t, sd, httpStatic, 0, baseInstances)
		// Check the other namespace is untouched
		instances := []*model.ServiceInstance{
			makeInstance(httpStaticOverlayUpdatedNs, "5.5.5.5", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlayUpdatedNs, "7.7.7.7", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"namespace": "bar"}, PlainText),
		}
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedNs, 0, instances)
		// svcUpdate is not triggered since `httpStatic` is there and has instances, so we should
		// not delete the endpoints shards of "*.google.com". We xpect a full push as the service has changed.
		expectEvents(t, events,
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "*.google.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}},
		)

		// delete httpStatic, no "*.google.com" service exists now.
		deleteConfigs([]*config.Config{httpStatic}, store, t)
		// svcUpdate is triggered since "*.google.com" in same namespace is deleted and
		// we need to delete endpoint shards. We expect a full push as the service has changed.
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStatic.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "*.google.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}},
		)

		// add back httpStatic
		createConfigs([]*config.Config{httpStatic}, store, t)
		instances = baseInstances
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStatic.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: httpStatic.Spec.(*networking.ServiceEntry).Hosts[0], Namespace: httpStatic.Namespace}: {}}}})

		// Add back the ServiceEntry, expect these instances to get added
		createConfigs([]*config.Config{httpStaticOverlayUpdated}, store, t)
		instances = append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		// Service change, so we need a full push
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "*.google.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}})
	})

	t.Run("change host", func(t *testing.T) {
		// same as httpStaticOverlayUpdated but with an additional host
		httpStaticHost := func() *config.Config {
			c := httpStaticOverlayUpdated.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Hosts = append(se.Hosts, "other.com")
			return &c
		}()
		createConfigs([]*config.Config{httpStaticHost}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		// This is not applied, just to make makeInstance pick the right service.
		otherHost := func() *config.Config {
			c := httpStaticOverlayUpdated.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Hosts = []string{"other.com"}
			return &c
		}()
		instances2 := []*model.ServiceInstance{
			makeInstance(otherHost, "5.5.5.5", 4567, httpStaticHost.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(otherHost, "6.6.6.6", 4567, httpStaticHost.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText),
		}
		expectServiceInstances(t, sd, httpStaticHost, 0, instances, instances2)
		// Service change, so we need a full push
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "other.com", namespace: httpStaticOverlayUpdated.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "other.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}}) // service added

		// restore this config and remove the added host.
		createConfigs([]*config.Config{httpStaticOverlayUpdated}, store, t)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "other.com", namespace: httpStatic.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "other.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}}) // service deleted
	})

	t.Run("change dns endpoints", func(t *testing.T) {
		// Setup the expected instances for DNS. This will be added/removed from as we add various configs
		instances1 := []*model.ServiceInstance{
			makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0],
				nil, MTLS),
			makeInstance(tcpDNS, "in.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0],
				nil, MTLS),
		}

		// This is not applied, just to make makeInstance pick the right service.
		tcpDNSUpdated := func() *config.Config {
			c := tcpDNS.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Endpoints = []*networking.WorkloadEntry{
				{
					Address: "lon.google.com",
					Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
				},
			}
			return &c
		}()

		instances2 := []*model.ServiceInstance{
			makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0],
				nil, MTLS),
		}

		createConfigs([]*config.Config{tcpDNS}, store, t)
		expectServiceInstances(t, sd, tcpDNS, 0, instances1)
		// Service change, so we need a full push
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "tcpdns.com", namespace: tcpDNS.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "tcpdns.com", Namespace: tcpDNS.Namespace}: {}}}}) // service added

		// now update the config
		createConfigs([]*config.Config{tcpDNSUpdated}, store, t)
		expectEvents(t, events,
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind: gvk.ServiceEntry, Name: "tcpdns.com",
				Namespace: tcpDNSUpdated.Namespace,
			}: {}}}}) // service deleted
		expectServiceInstances(t, sd, tcpDNS, 0, instances2)
	})

	t.Run("change workload selector", func(t *testing.T) {
		// same as selector but with an additional host
		selector1 := func() *config.Config {
			c := httpStaticOverlay.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Hosts = append(se.Hosts, "selector1.com")
			se.Endpoints = nil
			se.WorkloadSelector = &networking.WorkloadSelector{
				Labels: map[string]string{"app": "wle"},
			}
			return &c
		}()
		createConfigs([]*config.Config{selector1}, store, t)
		// Service change, so we need a full push
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "selector1.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},

			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Kind: gvk.ServiceEntry, Name: "*.google.com", Namespace: selector1.Namespace}:  {},
				{Kind: gvk.ServiceEntry, Name: "selector1.com", Namespace: selector1.Namespace}: {},
			}}}) // service added

		selector1Updated := func() *config.Config {
			c := selector1.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.WorkloadSelector = &networking.WorkloadSelector{
				Labels: map[string]string{"app": "wle1"},
			}
			return &c
		}()
		createConfigs([]*config.Config{selector1Updated}, store, t)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "svcupdate", host: "selector1.com", namespace: httpStaticOverlay.Namespace},

			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{
				{Kind: gvk.ServiceEntry, Name: "*.google.com", Namespace: selector1.Namespace}:  {},
				{Kind: gvk.ServiceEntry, Name: "selector1.com", Namespace: selector1.Namespace}: {},
			}}}) // service updated
	})
}

func TestServiceDiscoveryWorkloadUpdate(t *testing.T) {
	store, sd, events, stopFn := initServiceDiscovery()
	defer stopFn()

	// Setup a couple workload entries for test. These will be selected by the `selector` SE
	wle := createWorkloadEntry("wl", selector.Name,
		&networking.WorkloadEntry{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: "default",
		})
	wle2 := createWorkloadEntry("wl2", selector.Name,
		&networking.WorkloadEntry{
			Address:        "3.3.3.3",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: "default",
		})
	wle3 := createWorkloadEntry("wl3", selector.Name,
		&networking.WorkloadEntry{
			Address:        "abc.def",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: "default",
		})
	dnsWle := createWorkloadEntry("dnswl", dnsSelector.Namespace,
		&networking.WorkloadEntry{
			Address:        "4.4.4.4",
			Labels:         map[string]string{"app": "dns-wle"},
			ServiceAccount: "default",
		})

	t.Run("service entry", func(t *testing.T) {
		// Add just the ServiceEntry with selector. We should see no instances
		createConfigs([]*config.Config{selector}, store, t)
		instances := []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "selector.com", namespace: selector.Namespace},
			Event{kind: "xds"})
	})

	t.Run("add workload", func(t *testing.T) {
		// Add a WLE, we expect this to update
		createConfigs([]*config.Config{wle}, store, t)

		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0],
				map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1],
				map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "wl"
			i.Endpoint.Namespace = selector.Name
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "xds", proxyIP: "2.2.2.2"},
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2},
		)
	})

	t.Run("add dns service entry", func(t *testing.T) {
		// Add just the ServiceEntry with selector. We should see no instances
		createConfigs([]*config.Config{dnsSelector}, store, t)
		instances := []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "4.4.4.4")
		expectServiceInstances(t, sd, dnsSelector, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "dns.selector.com", namespace: dnsSelector.Namespace},
			Event{kind: "xds"})
	})

	t.Run("add dns workload", func(t *testing.T) {
		// Add a WLE, we expect this to update
		createConfigs([]*config.Config{dnsWle}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(dnsSelector, "4.4.4.4", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0],
				map[string]string{"app": "dns-wle"}, "default"),
			makeInstanceWithServiceAccount(dnsSelector, "4.4.4.4", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1],
				map[string]string{"app": "dns-wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "dnswl"
			i.Endpoint.Namespace = dnsSelector.Namespace
		}
		expectProxyInstances(t, sd, instances, "4.4.4.4")
		expectServiceInstances(t, sd, dnsSelector, 0, instances)
		expectEvents(t, events, Event{kind: "xds"})
	})

	t.Run("another workload", func(t *testing.T) {
		// Add a different WLE
		createConfigs([]*config.Config{wle2}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "wl"
			i.Endpoint.Namespace = selector.Name
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		instances = append(instances,
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"))
		for _, i := range instances[2:] {
			i.Endpoint.WorkloadName = "wl2"
			i.Endpoint.Namespace = selector.Name
		}
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "xds", proxyIP: "3.3.3.3"},
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 4},
		)
	})

	t.Run("ignore host workload", func(t *testing.T) {
		// Add a WLE with host address. Should be ignored by static service entry.
		createConfigs([]*config.Config{wle3}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "wl"
			i.Endpoint.Namespace = selector.Name
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		instances = append(instances,
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"))
		for _, i := range instances[2:] {
			i.Endpoint.WorkloadName = "wl2"
			i.Endpoint.Namespace = selector.Name
		}
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "xds", proxyIP: "abc.def"},
		)
	})

	t.Run("deletion", func(t *testing.T) {
		// Delete the configs, it should be gone
		deleteConfigs([]*config.Config{wle2}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "wl"
			i.Endpoint.Namespace = selector.Name
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2})

		// Delete the other config
		deleteConfigs([]*config.Config{wle}, store, t)
		instances = []*model.ServiceInstance{}
		expectServiceInstances(t, sd, selector, 0, instances)
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 0})

		// Add the config back
		createConfigs([]*config.Config{wle}, store, t)
		instances = []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "wl"
			i.Endpoint.Namespace = selector.Name
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "xds", proxyIP: "2.2.2.2"},
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2},
		)
	})
}

func TestServiceDiscoveryWorkloadChangeLabel(t *testing.T) {
	store, sd, events, stopFn := initServiceDiscovery()
	defer stopFn()

	wle := createWorkloadEntry("wl", selector.Name,
		&networking.WorkloadEntry{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: "default",
		})

	wle2 := createWorkloadEntry("wl", selector.Name,
		&networking.WorkloadEntry{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle2"},
			ServiceAccount: "default",
		})
	wle3 := createWorkloadEntry("wl3", selector.Name,
		&networking.WorkloadEntry{
			Address:        "3.3.3.3",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: "default",
		})

	t.Run("service entry", func(t *testing.T) {
		// Add just the ServiceEntry with selector. We should see no instances
		createConfigs([]*config.Config{selector}, store, t)
		instances := []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "selector.com", namespace: selector.Namespace},
			Event{kind: "xds"})
	})

	t.Run("change label removing all", func(t *testing.T) {
		// Add a WLE, we expect this to update
		createConfigs([]*config.Config{wle}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0],
				map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1],
				map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "wl"
			i.Endpoint.Namespace = selector.Name
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "xds", proxyIP: "2.2.2.2"},
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2},
		)

		createConfigs([]*config.Config{wle2}, store, t)
		instances = []*model.ServiceInstance{}
		expectServiceInstances(t, sd, selector, 0, instances)
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 0})
	})

	t.Run("change label removing one", func(t *testing.T) {
		// Add a WLE, we expect this to update
		createConfigs([]*config.Config{wle}, store, t)
		expectEvents(t, events,
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2},
		)
		// add a wle, expect this to be an add
		createConfigs([]*config.Config{wle3}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0],
				map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1],
				map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0],
				map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1],
				map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances[:2] {
			i.Endpoint.WorkloadName = "wl"
			i.Endpoint.Namespace = selector.Name
		}
		for _, i := range instances[2:] {
			i.Endpoint.WorkloadName = "wl3"
			i.Endpoint.Namespace = selector.Name
		}
		expectProxyInstances(t, sd, instances[:2], "2.2.2.2")
		expectProxyInstances(t, sd, instances[2:], "3.3.3.3")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "xds", proxyIP: "3.3.3.3"},
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 4},
		)

		createConfigs([]*config.Config{wle2}, store, t)
		instances = []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0],
				map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1],
				map[string]string{"app": "wle"}, "default"),
		}
		for _, i := range instances {
			i.Endpoint.WorkloadName = "wl3"
			i.Endpoint.Namespace = selector.Name
		}
		expectServiceInstances(t, sd, selector, 0, instances)
		expectProxyInstances(t, sd, instances, "3.3.3.3")
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2})
	})
}

func TestServiceDiscoveryWorkloadInstance(t *testing.T) {
	store, sd, events, stopFn := initServiceDiscovery()
	defer stopFn()

	// Setup a couple of workload instances for test. These will be selected by the `selector` SE
	fi1 := &model.WorkloadInstance{
		Name:      selector.Name,
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	fi2 := &model.WorkloadInstance{
		Name:      "some-other-name",
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "3.3.3.3",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	fi3 := &model.WorkloadInstance{
		Name:      "another-name",
		Namespace: dnsSelector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "dns-wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(dnsSelector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	t.Run("service entry", func(t *testing.T) {
		// Add just the ServiceEntry with selector. We should see no instances
		createConfigs([]*config.Config{selector}, store, t)
		instances := []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "selector.com", namespace: selector.Namespace},
			Event{kind: "xds"})
	})

	t.Run("add another service entry", func(t *testing.T) {
		createConfigs([]*config.Config{dnsSelector}, store, t)
		instances := []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, dnsSelector, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "dns.selector.com", namespace: dnsSelector.Namespace},
			Event{kind: "xds"})
	})

	t.Run("add workload instance", func(t *testing.T) {
		// Add a workload instance, we expect this to update
		callInstanceHandlers([]*model.WorkloadInstance{fi1}, sd, model.EventAdd, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2})
	})

	t.Run("another workload instance", func(t *testing.T) {
		// Add a different instance
		callInstanceHandlers([]*model.WorkloadInstance{fi2}, sd, model.EventAdd, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		instances = append(instances,
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "3.3.3.3", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"))
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 4})
	})

	t.Run("delete workload instance", func(t *testing.T) {
		// Delete the instances, it should be gone
		callInstanceHandlers([]*model.WorkloadInstance{fi2}, sd, model.EventDelete, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2})

		key := instancesKey{namespace: selector.Namespace, hostname: "selector.com"}
		namespacedName := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
		if len(sd.serviceInstances.ip2instance) != 1 {
			t.Fatalf("service instances store `ip2instance` memory leak, expect 1, got %d", len(sd.serviceInstances.ip2instance))
		}
		if len(sd.serviceInstances.instances[key]) != 1 {
			t.Fatalf("service instances store `instances` memory leak, expect 1, got %d", len(sd.serviceInstances.instances[key]))
		}
		if len(sd.serviceInstances.instancesBySE[namespacedName]) != 1 {
			t.Fatalf("service instances store `instancesBySE` memory leak, expect 1, got %d", len(sd.serviceInstances.instancesBySE[namespacedName]))
		}

		// The following sections mimic this scenario:
		// f1 starts terminating, f3 picks up the IP, f3 delete event (pod
		// not ready yet) comes before f1
		//
		// Delete f3 event
		callInstanceHandlers([]*model.WorkloadInstance{fi3}, sd, model.EventDelete, t)
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)

		// Delete f1 event
		callInstanceHandlers([]*model.WorkloadInstance{fi1}, sd, model.EventDelete, t)
		instances = []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 0})

		if len(sd.serviceInstances.ip2instance) != 0 {
			t.Fatalf("service instances store `ip2instance` memory leak, expect 0, got %d", len(sd.serviceInstances.ip2instance))
		}
		if len(sd.serviceInstances.instances[key]) != 0 {
			t.Fatalf("service instances store `instances` memory leak, expect 0, got %d", len(sd.serviceInstances.instances[key]))
		}
		if len(sd.serviceInstances.instancesBySE[namespacedName]) != 0 {
			t.Fatalf("service instances store `instancesBySE` memory leak, expect 0, got %d", len(sd.serviceInstances.instancesBySE[namespacedName]))
		}

		// Add f3 event
		callInstanceHandlers([]*model.WorkloadInstance{fi3}, sd, model.EventAdd, t)
		instances = []*model.ServiceInstance{
			makeInstanceWithServiceAccount(dnsSelector, "2.2.2.2", 444,
				dnsSelector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "dns-wle"}, "default"),
			makeInstanceWithServiceAccount(dnsSelector, "2.2.2.2", 445,
				dnsSelector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "dns-wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, dnsSelector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "dns.selector.com", namespace: dnsSelector.Namespace, endpoints: 2})
	})
}

func expectProxyInstances(t testing.TB, sd *Controller, expected []*model.ServiceInstance, ip string) {
	t.Helper()
	// The system is eventually consistent, so add some retries
	retry.UntilSuccessOrFail(t, func() error {
		instances := sd.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{ip}, Metadata: &model.NodeMetadata{}})
		sortServiceInstances(instances)
		sortServiceInstances(expected)
		if err := compare(t, instances, expected); err != nil {
			return err
		}
		return nil
	}, retry.Converge(2), retry.Timeout(time.Second*5))
}

func expectEvents(t testing.TB, ch chan Event, events ...Event) {
	cmpPushRequest := func(expectReq, gotReq *model.PushRequest) bool {
		var expectConfigs, gotConfigs map[model.ConfigKey]struct{}
		if expectReq != nil {
			expectConfigs = expectReq.ConfigsUpdated
		}
		if gotReq != nil {
			gotConfigs = gotReq.ConfigsUpdated
		}

		return reflect.DeepEqual(expectConfigs, gotConfigs)
	}

	t.Helper()
	for _, event := range events {
		got := waitForEvent(t, ch)
		if event.pushReq != nil {
			if !cmpPushRequest(event.pushReq, got.pushReq) {
				t.Fatalf("expected event %+v %+v, got %+v %+v", event, event.pushReq, got, got.pushReq)
			}
		}

		event.pushReq, got.pushReq = nil, nil
		if event != got {
			t.Fatalf("expected event %+v, got %+v", event, got)
		}
	}
	// Drain events
	for {
		select {
		case e := <-ch:
			t.Logf("ignoring event %+v", e)
			if len(events) == 0 {
				t.Fatalf("got unexpected event %+v", e)
			}
		default:
			return
		}
	}
}

func expectServiceInstances(t testing.TB, sd *Controller, cfg *config.Config, port int, expected ...[]*model.ServiceInstance) {
	t.Helper()
	svcs := convertServices(*cfg)
	if len(svcs) != len(expected) {
		t.Fatalf("got more services than expected: %v vs %v", len(svcs), len(expected))
	}
	// The system is eventually consistent, so add some retries
	retry.UntilSuccessOrFail(t, func() error {
		for i, svc := range svcs {
			instances := sd.InstancesByPort(svc, port, nil)
			sortServiceInstances(instances)
			sortServiceInstances(expected[i])
			if err := compare(t, instances, expected[i]); err != nil {
				return fmt.Errorf("%d: %v", i, err)
			}
		}
		return nil
	}, retry.Converge(2), retry.Timeout(time.Second*1))
}

func TestServiceDiscoveryGetProxyServiceInstances(t *testing.T) {
	store, sd, _, stopFn := initServiceDiscovery()
	defer stopFn()

	createConfigs([]*config.Config{httpStatic, tcpStatic}, store, t)

	expectProxyInstances(t, sd, []*model.ServiceInstance{
		makeInstance(httpStatic, "2.2.2.2", 7080, httpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpStatic, "2.2.2.2", 18080, httpStatic.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
		makeInstance(tcpStatic, "2.2.2.2", 444, tcpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
	}, "2.2.2.2")
}

// Keeping this test for legacy - but it never happens in real life.
func TestServiceDiscoveryInstances(t *testing.T) {
	store, sd, _, stopFn := initServiceDiscovery()
	defer stopFn()

	createConfigs([]*config.Config{httpDNS, tcpStatic}, store, t)

	expectServiceInstances(t, sd, httpDNS, 0, []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "us.google.com", 18080, httpDNS.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "uk.google.com", 8080, httpDNS.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, MTLS),
		makeInstance(httpDNS, "de.google.com", 8080, httpDNS.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"foo": "bar"}, MTLS),
	})
}

// Keeping this test for legacy - but it never happens in real life.
func TestServiceDiscoveryInstances1Port(t *testing.T) {
	store, sd, _, stopFn := initServiceDiscovery()
	defer stopFn()

	createConfigs([]*config.Config{httpDNS, tcpStatic}, store, t)

	expectServiceInstances(t, sd, httpDNS, 80, []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, MTLS),
	})
}

func TestNonServiceConfig(t *testing.T) {
	store, sd, _, stopFn := initServiceDiscovery()
	defer stopFn()

	// Create a non-service configuration element. This should not affect the service registry at all.
	cfg := config.Config{
		Meta: config.Meta{
			GroupVersionKind:  collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:              "fakeDestinationRule",
			Namespace:         "default",
			Domain:            "cluster.local",
			CreationTimestamp: GlobalTime,
		},
		Spec: &networking.DestinationRule{
			Host: "fakehost",
		},
	}
	_, err := store.Create(cfg)
	if err != nil {
		t.Errorf("error occurred crearting ServiceEntry config: %v", err)
	}

	// Now create some service entries and verify that it's added to the registry.
	createConfigs([]*config.Config{httpDNS, tcpStatic}, store, t)
	expectServiceInstances(t, sd, httpDNS, 80, []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, MTLS),
	})
}

// nolint: lll
func TestServicesDiff(t *testing.T) {
	updatedHTTPDNS := &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
			Name:              "httpDNS",
			Namespace:         "httpDNS",
			CreationTimestamp: GlobalTime,
			Labels:            map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.google.com", "*.mail.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
				{Number: 8080, Name: "http-alt-port", Protocol: "http"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{
					Address: "us.google.com",
					Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
					Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "uk.google.com",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "de.google.com",
					Labels:  map[string]string{"foo": "bar", label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DNS,
		},
	}

	updatedHTTPDNSPort := func() *config.Config {
		c := updatedHTTPDNS.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		var ports []*networking.Port
		ports = append(ports, se.Ports...)
		ports = append(ports, &networking.Port{Number: 9090, Name: "http-new-port", Protocol: "http"})
		se.Ports = ports
		return &c
	}()

	updatedEndpoint := func() *config.Config {
		c := updatedHTTPDNS.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		var endpoints []*networking.WorkloadEntry
		endpoints = append(endpoints, se.Endpoints...)
		endpoints = append(endpoints, &networking.WorkloadEntry{
			Address: "in.google.com",
			Labels:  map[string]string{"foo": "bar", label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
		})
		se.Endpoints = endpoints
		return &c
	}()

	stringsToHosts := func(hosts []string) []host.Name {
		ret := make([]host.Name, len(hosts))
		for i, hostname := range hosts {
			ret[i] = host.Name(hostname)
		}
		return ret
	}

	cases := []struct {
		name    string
		current *config.Config
		new     *config.Config

		added     []host.Name
		deleted   []host.Name
		updated   []host.Name
		unchanged []host.Name
	}{
		{
			name:      "same config",
			current:   updatedHTTPDNS,
			new:       updatedHTTPDNS,
			unchanged: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name:    "same config with different name",
			current: updatedHTTPDNS,
			new: func() *config.Config {
				c := updatedHTTPDNS.DeepCopy()
				c.Name = "httpDNS1"
				return &c
			}(),
			unchanged: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name:    "different resolution",
			current: updatedHTTPDNS,
			new: func() *config.Config {
				c := updatedHTTPDNS.DeepCopy()
				c.Spec.(*networking.ServiceEntry).Resolution = networking.ServiceEntry_NONE
				return &c
			}(),
			updated: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name:    "config modified with added/deleted host",
			current: updatedHTTPDNS,
			new: func() *config.Config {
				c := updatedHTTPDNS.DeepCopy()
				se := c.Spec.(*networking.ServiceEntry)
				se.Hosts = []string{"*.google.com", "host.com"}
				return &c
			}(),
			added:     []host.Name{"host.com"},
			deleted:   []host.Name{"*.mail.com"},
			unchanged: []host.Name{"*.google.com"},
		},
		{
			name:    "config modified with additional port",
			current: updatedHTTPDNS,
			new:     updatedHTTPDNSPort,
			updated: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name:      "same config with additional endpoint",
			current:   updatedHTTPDNS,
			new:       updatedEndpoint,
			unchanged: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
	}

	servicesHostnames := func(services []*model.Service) []host.Name {
		if len(services) == 0 {
			return nil
		}
		ret := make([]host.Name, len(services))
		for i, svc := range services {
			ret[i] = svc.Hostname
		}
		return ret
	}

	for _, tt := range cases {
		if tt.name != "same config with additional endpoint" {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			as := convertServices(*tt.current)
			bs := convertServices(*tt.new)
			added, deleted, updated, unchanged := servicesDiff(as, bs)
			for i, item := range []struct {
				hostnames []host.Name
				services  []*model.Service
			}{
				{tt.added, added},
				{tt.deleted, deleted},
				{tt.updated, updated},
				{tt.unchanged, unchanged},
			} {
				if !reflect.DeepEqual(servicesHostnames(item.services), item.hostnames) {
					t.Errorf("ServicesChanged %d got %v, want %v", i, servicesHostnames(item.services), item.hostnames)
				}
			}
		})
	}
}

func sortServices(services []*model.Service) {
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })
	for _, service := range services {
		sortPorts(service.Ports)
	}
}

func sortServiceInstances(instances []*model.ServiceInstance) {
	labelsToSlice := func(labels labels.Instance) []string {
		out := make([]string, 0, len(labels))
		for k, v := range labels {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(out)
		return out
	}

	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Service.Hostname == instances[j].Service.Hostname {
			if instances[i].Endpoint.EndpointPort == instances[j].Endpoint.EndpointPort {
				if instances[i].Endpoint.Address == instances[j].Endpoint.Address {
					if len(instances[i].Endpoint.Labels) == len(instances[j].Endpoint.Labels) {
						iLabels := labelsToSlice(instances[i].Endpoint.Labels)
						jLabels := labelsToSlice(instances[j].Endpoint.Labels)
						for k := range iLabels {
							if iLabels[k] < jLabels[k] {
								return true
							}
						}
					}
					return len(instances[i].Endpoint.Labels) < len(instances[j].Endpoint.Labels)
				}
				return instances[i].Endpoint.Address < instances[j].Endpoint.Address
			}
			return instances[i].Endpoint.EndpointPort < instances[j].Endpoint.EndpointPort
		}
		return instances[i].Service.Hostname < instances[j].Service.Hostname
	})
}

func sortPorts(ports []*model.Port) {
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port == ports[j].Port {
			if ports[i].Name == ports[j].Name {
				return ports[i].Protocol < ports[j].Protocol
			}
			return ports[i].Name < ports[j].Name
		}
		return ports[i].Port < ports[j].Port
	})
}

func Test_autoAllocateIP_conditions(t *testing.T) {
	tests := []struct {
		name         string
		inServices   []*model.Service
		wantServices []*model.Service
	}{
		{
			name: "no allocation for passthrough",
			inServices: []*model.Service{
				{
					Hostname:       "foo.com",
					Resolution:     model.Passthrough,
					DefaultAddress: "0.0.0.0",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:       "foo.com",
					Resolution:     model.Passthrough,
					DefaultAddress: "0.0.0.0",
				},
			},
		},
		{
			name: "no allocation if address exists",
			inServices: []*model.Service{
				{
					Hostname:       "foo.com",
					Resolution:     model.ClientSideLB,
					DefaultAddress: "1.1.1.1",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:       "foo.com",
					Resolution:     model.ClientSideLB,
					DefaultAddress: "1.1.1.1",
				},
			},
		},
		{
			name: "no allocation if hostname is wildcard",
			inServices: []*model.Service{
				{
					Hostname:       "*.foo.com",
					Resolution:     model.ClientSideLB,
					DefaultAddress: "1.1.1.1",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:       "*.foo.com",
					Resolution:     model.ClientSideLB,
					DefaultAddress: "1.1.1.1",
				},
			},
		},
		{
			name: "allocate IP for clientside lb",
			inServices: []*model.Service{
				{
					Hostname:       "foo.com",
					Resolution:     model.ClientSideLB,
					DefaultAddress: "0.0.0.0",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:                 "foo.com",
					Resolution:               model.ClientSideLB,
					DefaultAddress:           "0.0.0.0",
					AutoAllocatedIPv4Address: "240.240.0.1",
					AutoAllocatedIPv6Address: "2001:2::f0f0:1",
				},
			},
		},
		{
			name: "allocate IP for dns lb",
			inServices: []*model.Service{
				{
					Hostname:       "foo.com",
					Resolution:     model.DNSLB,
					DefaultAddress: "0.0.0.0",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:                 "foo.com",
					Resolution:               model.DNSLB,
					DefaultAddress:           "0.0.0.0",
					AutoAllocatedIPv4Address: "240.240.0.1",
					AutoAllocatedIPv6Address: "2001:2::f0f0:1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := autoAllocateIPs(tt.inServices)
			if got[0].AutoAllocatedIPv4Address != tt.wantServices[0].AutoAllocatedIPv4Address {
				t.Errorf("autoAllocateIPs() AutoAllocatedIPv4Address = %v, want %v",
					got[0].AutoAllocatedIPv4Address, tt.wantServices[0].AutoAllocatedIPv4Address)
			}
			if got[0].AutoAllocatedIPv6Address != tt.wantServices[0].AutoAllocatedIPv6Address {
				t.Errorf("autoAllocateIPs() AutoAllocatedIPv4Address = %v, want %v",
					got[0].AutoAllocatedIPv6Address, tt.wantServices[0].AutoAllocatedIPv6Address)
			}
		})
	}
}

func Test_autoAllocateIP_values(t *testing.T) {
	inServices := make([]*model.Service, 512)
	for i := 0; i < 512; i++ {
		temp := model.Service{
			Hostname:       "foo.com",
			Resolution:     model.ClientSideLB,
			DefaultAddress: constants.UnspecifiedIP,
		}
		inServices[i] = &temp
	}
	gotServices := autoAllocateIPs(inServices)

	// out of the 512 IPs, we dont expect the following IPs
	// 240.240.0.0
	// 240.240.0.255
	// 240.240.1.0
	// 240.240.1.255
	// 240.240.2.0
	// 240.240.2.255
	// The last IP should be 240.240.2.4
	doNotWant := map[string]bool{
		"240.240.0.0":   true,
		"240.240.0.255": true,
		"240.240.1.0":   true,
		"240.240.1.255": true,
		"240.240.2.0":   true,
		"240.240.2.255": true,
	}
	expectedLastIP := "240.240.2.4"
	if gotServices[len(gotServices)-1].AutoAllocatedIPv4Address != expectedLastIP {
		t.Errorf("expected last IP address to be %s, got %s", expectedLastIP, gotServices[len(gotServices)-1].AutoAllocatedIPv4Address)
	}

	gotIPMap := make(map[string]bool)
	for _, svc := range gotServices {
		if svc.AutoAllocatedIPv4Address == "" || doNotWant[svc.AutoAllocatedIPv4Address] {
			t.Errorf("unexpected value for auto allocated IP address %s", svc.AutoAllocatedIPv4Address)
		}
		if gotIPMap[svc.AutoAllocatedIPv4Address] {
			t.Errorf("multiple allocations of same IP address to different services: %s", svc.AutoAllocatedIPv4Address)
		}
		gotIPMap[svc.AutoAllocatedIPv4Address] = true
	}
}

func TestWorkloadEntryOnlyMode(t *testing.T) {
	store, registry, _, cleanup := initServiceDiscoveryWithOpts(true)
	defer cleanup()
	createConfigs([]*config.Config{httpStatic}, store, t)
	svcs := registry.Services()
	if len(svcs) > 0 {
		t.Fatalf("expected 0 services, got %d", len(svcs))
	}
	svc := registry.GetService("*.google.com")
	if svc != nil {
		t.Fatalf("expected nil, got %v", svc)
	}
}

func BenchmarkServiceEntryHandler(b *testing.B) {
	_, sd := initServiceDiscoveryWithoutEvents(b)
	stopCh := make(chan struct{})
	go sd.Run(stopCh)
	defer close(stopCh)
	for i := 0; i < b.N; i++ {
		sd.serviceEntryHandler(config.Config{}, *httpDNS, model.EventAdd)
		sd.serviceEntryHandler(config.Config{}, *httpDNSRR, model.EventAdd)
		sd.serviceEntryHandler(config.Config{}, *tcpDNS, model.EventAdd)
		sd.serviceEntryHandler(config.Config{}, *tcpStatic, model.EventAdd)

		sd.serviceEntryHandler(config.Config{}, *httpDNS, model.EventDelete)
		sd.serviceEntryHandler(config.Config{}, *httpDNSRR, model.EventDelete)
		sd.serviceEntryHandler(config.Config{}, *tcpDNS, model.EventDelete)
		sd.serviceEntryHandler(config.Config{}, *tcpStatic, model.EventDelete)
	}
}

func BenchmarkWorkloadInstanceHandler(b *testing.B) {
	store, sd := initServiceDiscoveryWithoutEvents(b)
	stopCh := make(chan struct{})
	go sd.Run(stopCh)
	defer close(stopCh)
	// Add just the ServiceEntry with selector. We should see no instances
	createConfigs([]*config.Config{selector, dnsSelector}, store, b)

	// Setup a couple of workload instances for test. These will be selected by the `selector` SE
	fi1 := &model.WorkloadInstance{
		Name:      selector.Name,
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	fi2 := &model.WorkloadInstance{
		Name:      "some-other-name",
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "3.3.3.3",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	fi3 := &model.WorkloadInstance{
		Name:      "another-name",
		Namespace: dnsSelector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "dns-wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(dnsSelector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}
	for i := 0; i < b.N; i++ {
		sd.WorkloadInstanceHandler(fi1, model.EventAdd)
		sd.WorkloadInstanceHandler(fi2, model.EventAdd)
		sd.WorkloadInstanceHandler(fi3, model.EventDelete)

		sd.WorkloadInstanceHandler(fi2, model.EventDelete)
		sd.WorkloadInstanceHandler(fi1, model.EventDelete)
		sd.WorkloadInstanceHandler(fi3, model.EventDelete)
	}
}

func BenchmarkWorkloadEntryHandler(b *testing.B) {
	// Setup a couple workload entries for test. These will be selected by the `selector` SE
	wle := createWorkloadEntry("wl", selector.Name,
		&networking.WorkloadEntry{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: "default",
		})
	wle2 := createWorkloadEntry("wl2", selector.Name,
		&networking.WorkloadEntry{
			Address:        "3.3.3.3",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: "default",
		})
	dnsWle := createWorkloadEntry("dnswl", dnsSelector.Namespace,
		&networking.WorkloadEntry{
			Address:        "4.4.4.4",
			Labels:         map[string]string{"app": "dns-wle"},
			ServiceAccount: "default",
		})

	store, sd := initServiceDiscoveryWithoutEvents(b)
	stopCh := make(chan struct{})
	go sd.Run(stopCh)
	defer close(stopCh)
	// Add just the ServiceEntry with selector. We should see no instances
	createConfigs([]*config.Config{selector}, store, b)

	for i := 0; i < b.N; i++ {
		sd.workloadEntryHandler(config.Config{}, *wle, model.EventAdd)
		sd.workloadEntryHandler(config.Config{}, *dnsWle, model.EventAdd)
		sd.workloadEntryHandler(config.Config{}, *wle2, model.EventAdd)

		sd.workloadEntryHandler(config.Config{}, *wle, model.EventDelete)
		sd.workloadEntryHandler(config.Config{}, *dnsWle, model.EventDelete)
		sd.workloadEntryHandler(config.Config{}, *wle2, model.EventDelete)
	}
}
