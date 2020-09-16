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

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

func createConfigs(configs []*model.Config, store model.IstioConfigStore, t *testing.T) {
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

func callInstanceHandlers(instances []*model.WorkloadInstance, sd *ServiceEntryStore, ev model.Event, t *testing.T) {
	t.Helper()
	for _, instance := range instances {
		sd.WorkloadInstanceHandler(instance, ev)
	}
}

func deleteConfigs(configs []*model.Config, store model.IstioConfigStore, t *testing.T) {
	t.Helper()
	for _, cfg := range configs {
		err := store.Delete(cfg.GroupVersionKind, cfg.Name, cfg.Namespace)
		if err != nil {
			t.Errorf("error occurred crearting ServiceEntry config: %v", err)
		}
	}
}

type Event struct {
	kind      string
	host      string
	namespace string
	endpoints int
	pushReq   *model.PushRequest
}

type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan Event
}

func (fx *FakeXdsUpdater) EDSUpdate(_, hostname string, namespace string, entry []*model.IstioEndpoint) error {
	fx.Events <- Event{kind: "eds", host: hostname, namespace: namespace, endpoints: len(entry)}
	return nil
}

func (fx *FakeXdsUpdater) ConfigUpdate(req *model.PushRequest) {
	fx.Events <- Event{kind: "xds", pushReq: req}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_, _ string) {
}

func (fx *FakeXdsUpdater) SvcUpdate(_, hostname string, namespace string, _ model.Event) {
	fx.Events <- Event{kind: "svcupdate", host: hostname, namespace: namespace}
}

func waitForEvent(t *testing.T, ch chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event")
		return Event{}
	}
}

type channelTerminal struct {
}

func initServiceDiscovery() (model.IstioConfigStore, *ServiceEntryStore, chan Event, func()) {
	store := memory.Make(collections.Pilot)
	configController := memory.NewController(store)

	stop := make(chan struct{})
	go configController.Run(stop)

	eventch := make(chan Event, 10)

	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}

	istioStore := model.MakeIstioStore(configController)
	serviceController := NewServiceDiscovery(configController, istioStore, xdsUpdater)
	return istioStore, serviceController, eventch, func() {
		stop <- channelTerminal{}
	}
}

func TestServiceDiscoveryServices(t *testing.T) {
	store, sd, _, stopFn := initServiceDiscovery()
	defer stopFn()

	expectedServices := []*model.Service{
		makeService("*.google.com", "httpDNS", constants.UnspecifiedIP, map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
		makeService("tcpstatic.com", "tcpStatic", "172.217.0.1", map[string]int{"tcp-444": 444}, true, model.ClientSideLB),
	}

	createConfigs([]*model.Config{httpDNS, tcpStatic}, store, t)

	services, err := sd.Services()
	if err != nil {
		t.Errorf("Services() encountered unexpected error: %v", err)
	}
	sortServices(services)
	sortServices(expectedServices)
	if err := compare(t, services, expectedServices); err != nil {
		t.Error(err)
	}
}

func TestServiceDiscoveryGetService(t *testing.T) {
	hostname := "*.google.com"
	hostDNE := "does.not.exist.local"

	store, sd, _, stopFn := initServiceDiscovery()
	defer stopFn()

	createConfigs([]*model.Config{httpDNS, tcpStatic}, store, t)

	service, err := sd.GetService(host.Name(hostDNE))
	if err != nil {
		t.Errorf("GetService() encountered unexpected error: %v", err)
	}
	if service != nil {
		t.Errorf("GetService(%q) => should not exist, got %s", hostDNE, service.Hostname)
	}

	service, err = sd.GetService(host.Name(hostname))
	if err != nil {
		t.Errorf("GetService(%q) encountered unexpected error: %v", hostname, err)
	}
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
	httpStaticOverlayUpdated := func() *model.Config {
		c := httpStaticOverlay.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		se.Endpoints = append(se.Endpoints, &networking.WorkloadEntry{
			Address: "6.6.6.6",
			Labels:  map[string]string{"other": "bar"},
		})
		return &c
	}()
	// httpStaticOverlayUpdatedInstance is the same as httpStaticOverlayUpdated but with an extra endpoint added that has the same address
	httpStaticOverlayUpdatedInstance := func() *model.Config {
		c := httpStaticOverlayUpdated.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		se.Endpoints = append(se.Endpoints, &networking.WorkloadEntry{
			Address: "6.6.6.6",
			Labels:  map[string]string{"some-new-label": "bar"},
		})
		return &c
	}()

	// httpStaticOverlayUpdatedNop is the same as httpStaticOverlayUpdated but with a NOP change
	httpStaticOverlayUpdatedNop := func() *model.Config {
		c := httpStaticOverlayUpdated.DeepCopy()
		c.ResourceVersion = "foo"
		return &c
	}()

	// httpStaticOverlayUpdatedNs is the same as httpStaticOverlay but with an extra endpoint and different namespace added to test updates
	httpStaticOverlayUpdatedNs := func() *model.Config {
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
		createConfigs([]*model.Config{httpStatic}, store, t)
		instances := baseInstances
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStatic.Namespace},
			Event{kind: "eds", host: "*.google.com", namespace: httpStatic.Namespace, endpoints: len(instances)},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: httpStatic.Spec.(*networking.ServiceEntry).Hosts[0], Namespace: httpStatic.Namespace}: {}}}})
	})

	t.Run("add entry", func(t *testing.T) {
		// Create another SE for the same host, expect these instances to get added
		createConfigs([]*model.Config{httpStaticOverlay}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "eds", host: "*.google.com", namespace: httpStatic.Namespace, endpoints: len(instances)},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: httpStaticOverlay.Spec.(*networking.ServiceEntry).Hosts[0], Namespace: httpStaticOverlay.Namespace}: {}}}})
	})

	t.Run("add endpoint", func(t *testing.T) {
		// Update the SE for the same host, expect these instances to get added
		createConfigs([]*model.Config{httpStaticOverlayUpdated}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})

		// Make a NOP change, expect that there are no changes
		createConfigs([]*model.Config{httpStaticOverlayUpdatedNop}, store, t)
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedNop, 0, instances)
		// TODO this could trigger no changes
		expectEvents(t, events, Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})
	})

	t.Run("overlapping address", func(t *testing.T) {
		// Add another SE with an additional endpoint with a matching address
		createConfigs([]*model.Config{httpStaticOverlayUpdatedInstance}, store, t)
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
		createConfigs([]*model.Config{httpStaticOverlayUpdated}, store, t)
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
		createConfigs([]*model.Config{httpStaticOverlay}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStaticOverlay, 0, instances)
		expectEvents(t, events,
			Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)})
	})

	t.Run("different namespace", func(t *testing.T) {
		// Update the SE for the same host in a different ns, expect these instances to get added
		createConfigs([]*model.Config{httpStaticOverlayUpdatedNs}, store, t)
		instances := []*model.ServiceInstance{
			makeInstance(httpStaticOverlayUpdatedNs, "5.5.5.5", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlayUpdatedNs, "7.7.7.7", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"namespace": "bar"}, PlainText),
		}
		// This lookup is per-namespace, so we should only see the objects in the same namespace
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedNs, 0, instances)
		// Expect a full push, as the Service has changed
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: "other"},
			Event{kind: "eds", host: "*.google.com", namespace: "other", endpoints: len(instances)},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Hosts[0], Namespace: httpStaticOverlayUpdatedNs.Namespace}: {}}}})
	})

	t.Run("delete entry", func(t *testing.T) {
		// Delete the additional SE, expect it to get removed
		deleteConfigs([]*model.Config{httpStaticOverlayUpdated}, store, t)
		expectServiceInstances(t, sd, httpStatic, 0, baseInstances)
		// Check the other namespace is untouched
		instances := []*model.ServiceInstance{
			makeInstance(httpStaticOverlayUpdatedNs, "5.5.5.5", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlayUpdatedNs, "7.7.7.7", 4567, httpStaticOverlayUpdatedNs.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"namespace": "bar"}, PlainText),
		}
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedNs, 0, instances)
		// svc update is only triggered on deletion. Also expect a full push as the service has changed
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "*.google.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}})

		// Add back the ServiceEntry, expect these instances to get added
		createConfigs([]*model.Config{httpStaticOverlayUpdated}, store, t)
		instances = append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
		// Service change, so we need a full push
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "*.google.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}})
	})

	t.Run("change host", func(t *testing.T) {
		// same as httpStaticOverlayUpdated but with an additional host
		httpStaticHost := func() *model.Config {
			c := httpStaticOverlayUpdated.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Hosts = append(se.Hosts, "other.com")
			return &c
		}()
		createConfigs([]*model.Config{httpStaticHost}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		// This is not applied, just to make makeInstance pick the right service.
		otherHost := func() *model.Config {
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
			Event{kind: "eds", host: "other.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances2)},
			Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "other.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}}) // service added

		// restore this config and remove the added host.
		createConfigs([]*model.Config{httpStaticOverlayUpdated}, store, t)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "other.com", namespace: httpStatic.Namespace},
			Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: len(instances)},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "other.com", Namespace: httpStaticOverlayUpdated.Namespace}: {}}}}) // service deleted
	})

	t.Run("change dns endpoints", func(t *testing.T) {
		// Setup the expected instances for `httpStatic`. This will be added/removed from as we add various configs
		instances1 := []*model.ServiceInstance{
			makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0],
				nil, MTLS),
			makeInstance(tcpDNS, "in.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0],
				nil, MTLS),
		}

		// This is not applied, just to make makeInstance pick the right service.
		tcpDNSUpdated := func() *model.Config {
			c := tcpDNS.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Endpoints = []*networking.WorkloadEntry{
				{
					Address: "lon.google.com",
					Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
				},
			}
			return &c
		}()

		instances2 := []*model.ServiceInstance{
			makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0],
				nil, MTLS),
		}

		createConfigs([]*model.Config{tcpDNS}, store, t)
		expectServiceInstances(t, sd, tcpDNS, 0, instances1)
		// Service change, so we need a full push
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "tcpdns.com", namespace: tcpDNS.Namespace},
			//Event{kind: "eds", host: "tcpdns.com", namespace: tcpDNS.Namespace, endpoints: len(instances1)},
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "tcpdns.com", Namespace: tcpDNS.Namespace}: {}}}}) // service added

		// now update the config
		createConfigs([]*model.Config{tcpDNSUpdated}, store, t)
		expectEvents(t, events,
			Event{kind: "xds", pushReq: &model.PushRequest{ConfigsUpdated: map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "tcpdns.com",
				Namespace: tcpDNSUpdated.Namespace}: {}}}}) // service deleted
		expectServiceInstances(t, sd, tcpDNS, 0, instances2)
	})

	// TODO this is suspicious
	t.Run("change workload selector", func(t *testing.T) {
		// same as selector but with an additional host
		selector1 := func() *model.Config {
			c := httpStaticOverlay.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Hosts = append(se.Hosts, "selector1.com")
			se.Endpoints = nil
			se.WorkloadSelector = &networking.WorkloadSelector{
				Labels: map[string]string{"app": "wle"},
			}
			return &c
		}()
		createConfigs([]*model.Config{selector1}, store, t)
		// Service change, so we need a full push
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "eds", host: "*.google.com", namespace: httpStaticOverlay.Namespace, endpoints: 6},
			Event{kind: "svcupdate", host: "selector1.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "xds"})

		selector1Updated := func() *model.Config {
			c := selector1.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.WorkloadSelector = &networking.WorkloadSelector{
				Labels: map[string]string{"app": "wle1"},
			}
			return &c
		}()
		createConfigs([]*model.Config{selector1Updated}, store, t)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "*.google.com", namespace: httpStaticOverlay.Namespace},
			Event{kind: "svcupdate", host: "selector1.com", namespace: httpStaticOverlay.Namespace}) // service updated
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

	t.Run("service entry", func(t *testing.T) {
		// Add just the ServiceEntry with selector. We should see no instances
		createConfigs([]*model.Config{selector}, store, t)
		instances := []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "selector.com", namespace: selector.Namespace},
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace},
			Event{kind: "xds"})
	})

	t.Run("add workload", func(t *testing.T) {
		// Add a WLE, we expect this to update
		createConfigs([]*model.Config{wle}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0],
				map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1],
				map[string]string{"app": "wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com",
			namespace: selector.Namespace, endpoints: 2})
	})

	t.Run("another workload", func(t *testing.T) {
		// Add a different WLE
		createConfigs([]*model.Config{wle2}, store, t)
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

	t.Run("deletion", func(t *testing.T) {
		// Delete the configs, it should be gone
		deleteConfigs([]*model.Config{wle2}, store, t)
		instances := []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2})

		// Delete the other config
		deleteConfigs([]*model.Config{wle}, store, t)
		instances = []*model.ServiceInstance{}
		expectServiceInstances(t, sd, selector, 0, instances)
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 0})

		// Add the config back
		createConfigs([]*model.Config{wle}, store, t)
		instances = []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
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

	t.Run("service entry", func(t *testing.T) {
		// Add just the ServiceEntry with selector. We should see no instances
		createConfigs([]*model.Config{selector}, store, t)
		instances := []*model.ServiceInstance{}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events,
			Event{kind: "svcupdate", host: "selector.com", namespace: selector.Namespace},
			Event{kind: "eds", host: "selector.com", namespace: selector.Namespace},
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

		// Delete the other instance
		callInstanceHandlers([]*model.WorkloadInstance{fi1}, sd, model.EventDelete, t)
		instances = []*model.ServiceInstance{}
		expectServiceInstances(t, sd, selector, 0, instances)
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 0})

		// Add the instance back
		callInstanceHandlers([]*model.WorkloadInstance{fi1}, sd, model.EventAdd, t)
		instances = []*model.ServiceInstance{
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 444,
				selector.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"app": "wle"}, "default"),
			makeInstanceWithServiceAccount(selector, "2.2.2.2", 445,
				selector.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"app": "wle"}, "default"),
		}
		expectProxyInstances(t, sd, instances, "2.2.2.2")
		expectServiceInstances(t, sd, selector, 0, instances)
		expectEvents(t, events, Event{kind: "eds", host: "selector.com", namespace: selector.Namespace, endpoints: 2})
	})
}

func expectProxyInstances(t *testing.T, sd *ServiceEntryStore, expected []*model.ServiceInstance, ip string) {
	t.Helper()
	// The system is eventually consistent, so add some retries
	retry.UntilSuccessOrFail(t, func() error {
		instances, err := sd.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{ip}})
		if err != nil {
			return fmt.Errorf("getProxyServiceInstances() encountered unexpected error: %v", err)
		}
		sortServiceInstances(instances)
		sortServiceInstances(expected)
		if err := compare(t, instances, expected); err != nil {
			return err
		}
		return nil
	}, retry.Converge(2), retry.Timeout(time.Second*5))
}

func expectEvents(t *testing.T, ch chan Event, events ...Event) {
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
	rec := []Event{}
	for range events {
		rec = append(rec, waitForEvent(t, ch))
	}
	removeList := func(i int) {
		copy(rec[i:], rec[i+1:])  // Shift rec[i+1:] left one index.
		rec[len(rec)-1] = Event{} // Erase last element (write zero value).
		rec = rec[:len(rec)-1]    // Truncate slice.
	}
	for _, event := range events {
		match := -1
		for i, got := range rec {
			if event.pushReq != nil {
				if !cmpPushRequest(event.pushReq, got.pushReq) {
					continue
				}
			}

			event.pushReq, got.pushReq = nil, nil
			if event != got {
				continue
			}
			match = i
		}
		if match != -1 {
			removeList(match)
		} else {
			t.Errorf("didn't find matching event for %+v. Have %+v", event, rec)
		}
	}
	for _, unmatched := range rec {
		t.Errorf("got unexpected event: %+v", unmatched)
	}
	// Drain events
	for {
		select {
		case e := <-ch:
			log.Warnf("ignoring event %+v", e)
			if len(events) == 0 {
				t.Fatalf("got unexpected event %+v", e)
			}
		default:
			return
		}
	}
}

func expectServiceInstances(t *testing.T, sd *ServiceEntryStore, cfg *model.Config, port int, expected ...[]*model.ServiceInstance) {
	t.Helper()
	svcs := convertServices(*cfg)
	if len(svcs) != len(expected) {
		t.Fatalf("got more services than expected: %v vs %v", len(svcs), len(expected))
	}
	// The system is eventually consistent, so add some retries
	retry.UntilSuccessOrFail(t, func() error {
		for i, svc := range svcs {
			instances, err := sd.InstancesByPort(svc, port, nil)
			if err != nil {
				return fmt.Errorf("instancesByPort() encountered unexpected error: %v", err)
			}
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

	createConfigs([]*model.Config{httpStatic, tcpStatic}, store, t)

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

	createConfigs([]*model.Config{httpDNS, tcpStatic}, store, t)

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

	createConfigs([]*model.Config{httpDNS, tcpStatic}, store, t)

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
	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
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
	createConfigs([]*model.Config{httpDNS, tcpStatic}, store, t)
	expectServiceInstances(t, sd, httpDNS, 80, []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, MTLS),
	})
}

// nolint: lll
func TestServicesDiff(t *testing.T) {
	var updatedHTTPDNS = &model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind:  collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
			Name:              "httpDNS",
			Namespace:         "httpDNS",
			CreationTimestamp: GlobalTime,
			Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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
					Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "uk.google.com",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "de.google.com",
					Labels:  map[string]string{"foo": "bar", label.TLSMode: model.IstioMutualTLSModeLabel},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DNS,
		},
	}

	var updatedHTTPDNSPort = func() *model.Config {
		c := updatedHTTPDNS.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		var ports []*networking.Port
		ports = append(ports, se.Ports...)
		ports = append(ports, &networking.Port{Number: 9090, Name: "http-new-port", Protocol: "http"})
		se.Ports = ports
		return &c
	}()

	var updatedEndpoint = func() *model.Config {
		c := updatedHTTPDNS.DeepCopy()
		se := c.Spec.(*networking.ServiceEntry)
		var endpoints []*networking.WorkloadEntry
		endpoints = append(endpoints, se.Endpoints...)
		endpoints = append(endpoints, &networking.WorkloadEntry{
			Address: "in.google.com",
			Labels:  map[string]string{"foo": "bar", label.TLSMode: model.IstioMutualTLSModeLabel},
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
		name string
		a    *model.Config
		b    *model.Config

		added     []host.Name
		deleted   []host.Name
		updated   []host.Name
		unchanged []host.Name
	}{
		{
			name:      "same config",
			a:         updatedHTTPDNS,
			b:         updatedHTTPDNS,
			unchanged: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name: "different config",
			a:    updatedHTTPDNS,
			b: func() *model.Config {
				c := updatedHTTPDNS.DeepCopy()
				c.Name = "httpDNS1"
				return &c
			}(),
			unchanged: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name: "different resolution",
			a:    updatedHTTPDNS,
			b: func() *model.Config {
				c := updatedHTTPDNS.DeepCopy()
				c.Spec.(*networking.ServiceEntry).Resolution = networking.ServiceEntry_NONE
				return &c
			}(),
			updated: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name: "config modified with added/deleted host",
			a:    updatedHTTPDNS,
			b: func() *model.Config {
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
			a:       updatedHTTPDNS,
			b:       updatedHTTPDNSPort,
			updated: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
		{
			name:      "same config with additional endpoint",
			a:         updatedHTTPDNS,
			b:         updatedEndpoint,
			unchanged: stringsToHosts(updatedHTTPDNS.Spec.(*networking.ServiceEntry).Hosts),
		},
	}

	servicesHostnames := func(services []*model.Service) map[host.Name]struct{} {
		ret := make(map[host.Name]struct{})
		for _, svc := range services {
			ret[svc.Hostname] = struct{}{}
		}
		return ret
	}
	hostnamesToMap := func(hostnames []host.Name) map[host.Name]struct{} {
		ret := make(map[host.Name]struct{})
		for _, hostname := range hostnames {
			ret[hostname] = struct{}{}
		}
		return ret
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			as := convertServices(*tt.a)
			bs := convertServices(*tt.b)
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
				if !reflect.DeepEqual(servicesHostnames(item.services), hostnamesToMap(item.hostnames)) {
					t.Errorf("ServicesChanged %d got %v, want %v", i, servicesHostnames(item.services), hostnamesToMap(item.hostnames))
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
					Hostname:   "foo.com",
					Resolution: model.Passthrough,
					Address:    "0.0.0.0",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:   "foo.com",
					Resolution: model.Passthrough,
					Address:    "0.0.0.0",
				},
			},
		},
		{
			name: "no allocation if address exists",
			inServices: []*model.Service{
				{
					Hostname:   "foo.com",
					Resolution: model.ClientSideLB,
					Address:    "1.1.1.1",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:   "foo.com",
					Resolution: model.ClientSideLB,
					Address:    "1.1.1.1",
				},
			},
		},
		{
			name: "no allocation if hostname is wildcard",
			inServices: []*model.Service{
				{
					Hostname:   "*.foo.com",
					Resolution: model.ClientSideLB,
					Address:    "1.1.1.1",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:   "*.foo.com",
					Resolution: model.ClientSideLB,
					Address:    "1.1.1.1",
				},
			},
		},
		{
			name: "allocate IP for clientside lb",
			inServices: []*model.Service{
				{
					Hostname:   "foo.com",
					Resolution: model.ClientSideLB,
					Address:    "0.0.0.0",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:             "foo.com",
					Resolution:           model.ClientSideLB,
					Address:              "0.0.0.0",
					AutoAllocatedAddress: "240.240.0.1",
				},
			},
		},
		{
			name: "allocate IP for dns lb",
			inServices: []*model.Service{
				{
					Hostname:   "foo.com",
					Resolution: model.DNSLB,
					Address:    "0.0.0.0",
				},
			},
			wantServices: []*model.Service{
				{
					Hostname:             "foo.com",
					Resolution:           model.DNSLB,
					Address:              "0.0.0.0",
					AutoAllocatedAddress: "240.240.0.1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoAllocateIPs(tt.inServices); !reflect.DeepEqual(got, tt.wantServices) {
				t.Errorf("autoAllocateIPs() = %v, want %v", got, tt.wantServices)
			}
		})
	}
}

func Test_autoAllocateIP_values(t *testing.T) {
	inServices := make([]*model.Service, 512)
	for i := 0; i < 512; i++ {
		temp := model.Service{
			Hostname:   "foo.com",
			Resolution: model.ClientSideLB,
			Address:    constants.UnspecifiedIP,
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
	if gotServices[len(gotServices)-1].AutoAllocatedAddress != expectedLastIP {
		t.Errorf("expected last IP address to be %s, got %s", expectedLastIP, gotServices[len(gotServices)-1].AutoAllocatedAddress)
	}

	gotIPMap := make(map[string]bool)
	for _, svc := range gotServices {
		if svc.AutoAllocatedAddress == "" || doNotWant[svc.AutoAllocatedAddress] {
			t.Errorf("unexpected value for auto allocated IP address %s", svc.AutoAllocatedAddress)
		}
		if gotIPMap[svc.AutoAllocatedAddress] {
			t.Errorf("multiple allocations of same IP address to different services: %s", svc.AutoAllocatedAddress)
		}
		gotIPMap[svc.AutoAllocatedAddress] = true
	}
}
