// Copyright 2017 Istio Authors
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

package external

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/util/retry"
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

func deleteConfigs(configs []*model.Config, store model.IstioConfigStore, t *testing.T) {
	t.Helper()
	for _, cfg := range configs {
		err := store.Delete(cfg.GroupVersionKind(), cfg.Name, cfg.Namespace)
		if err != nil {
			t.Errorf("error occurred crearting ServiceEntry config: %v", err)
		}
	}
}

type channelTerminal struct {
}

// FakeXdsUpdater is used to test the event handlers.
type FakeXdsUpdater struct {
}

func (fx *FakeXdsUpdater) EDSUpdate(_, _, _ string, _ []*model.IstioEndpoint) error {
	return nil
}

func (fx *FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {
}

func (fx *FakeXdsUpdater) ProxyUpdate(_, _ string) {
}

func (fx *FakeXdsUpdater) SvcUpdate(_, _ string, _ string, _ model.Event) {
}

func initServiceDiscovery() (model.IstioConfigStore, *ServiceEntryStore, func()) {
	store := memory.Make(collections.Pilot)
	configController := memory.NewController(store)

	stop := make(chan struct{})
	go configController.Run(stop)

	istioStore := model.MakeIstioStore(configController)
	xdsUpdater := &FakeXdsUpdater{}
	serviceController := NewServiceDiscovery(configController, istioStore, xdsUpdater)
	return istioStore, serviceController, func() {
		stop <- channelTerminal{}
	}
}

func TestServiceDiscoveryServices(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
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

	store, sd, stopFn := initServiceDiscovery()
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
		t.Errorf("GetService(%q) => should exist", hostname)
	}
	if service.Hostname != host.Name(hostname) {
		t.Errorf("GetService(%q) => %q, want %q", hostname, service.Hostname, hostname)
	}
}

// TestServiceDiscoveryServiceUpdate test various add/update/delete events for ServiceEntry
// nolint: lll
func TestServiceDiscoveryServiceUpdate(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
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
	})

	t.Run("add entry", func(t *testing.T) {
		// Create another SE for the same host, expect these instances to get added
		createConfigs([]*model.Config{httpStaticOverlay}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)
	})

	t.Run("add endpoint", func(t *testing.T) {
		// Update the SE for the same host, expect these instances to get added
		createConfigs([]*model.Config{httpStaticOverlayUpdated}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)

		// Make a NOP change, expect that there are no changes
		createConfigs([]*model.Config{httpStaticOverlayUpdatedNop}, store, t)
		expectServiceInstances(t, sd, httpStaticOverlayUpdatedNop, 0, instances)
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
	})

	t.Run("update removes endpoint", func(t *testing.T) {
		// Update the SE for the same host to remove the endpoint
		createConfigs([]*model.Config{httpStaticOverlay}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStaticOverlay, 0, instances)
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
	})
	// TODO add hostname updates

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
	})

	t.Run("change host", func(t *testing.T) {
		// Update the SE for the same host, expect these instances to get added
		createConfigs([]*model.Config{httpStaticOverlayUpdated}, store, t)
		instances := append(baseInstances,
			makeInstance(httpStaticOverlay, "5.5.5.5", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"overlay": "bar"}, PlainText),
			makeInstance(httpStaticOverlay, "6.6.6.6", 4567, httpStaticOverlay.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"other": "bar"}, PlainText))
		expectServiceInstances(t, sd, httpStatic, 0, instances)

		// same as httpStaticOverlayUpdated but with an additional host
		httpStaticHost := func() *model.Config {
			c := httpStaticOverlayUpdated.DeepCopy()
			se := c.Spec.(*networking.ServiceEntry)
			se.Hosts = append(se.Hosts, "other.com")
			return &c
		}()
		createConfigs([]*model.Config{httpStaticHost}, store, t)
		instances = append(baseInstances,
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
	}, retry.Converge(2), retry.Timeout(time.Second*5))
}

func TestServiceDiscoveryGetProxyServiceInstances(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
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
	store, sd, stopFn := initServiceDiscovery()
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
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	createConfigs([]*model.Config{httpDNS, tcpStatic}, store, t)

	expectServiceInstances(t, sd, httpDNS, 80, []*model.ServiceInstance{
		makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
		makeInstance(httpDNS, "de.google.com", 80, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, MTLS),
	})
}

func TestNonServiceConfig(t *testing.T) {
	store, sd, stopFn := initServiceDiscovery()
	defer stopFn()

	// Create a non-service configuration element. This should not affect the service registry at all.
	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Kind(),
			Group:             collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Group(),
			Version:           collections.IstioNetworkingV1Alpha3Destinationrules.Resource().Version(),
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

func TestServicesChanged(t *testing.T) {

	var updatedHTTPDNS = &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Kind(),
			Name:              "httpDNS",
			Namespace:         "httpDNS",
			CreationTimestamp: GlobalTime,
			Labels:            map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
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
					Labels:  map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "uk.google.com",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "de.google.com",
					Labels:  map[string]string{"foo": "bar", model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DNS,
		},
	}

	var updatedHTTPDNSPort = &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Kind(),
			Name:              "httpDNS",
			Namespace:         "httpDNS",
			CreationTimestamp: GlobalTime,
			Labels:            map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.google.com", "*.mail.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
				{Number: 8080, Name: "http-alt-port", Protocol: "http"},
				{Number: 9090, Name: "http-new-port", Protocol: "http"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{
					Address: "us.google.com",
					Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
					Labels:  map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "uk.google.com",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "de.google.com",
					Labels:  map[string]string{"foo": "bar", model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DNS,
		},
	}

	var updatedEndpoint = &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Kind(),
			Name:              "httpDNS",
			Namespace:         "httpDNS",
			CreationTimestamp: GlobalTime,
			Labels:            map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
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
					Labels:  map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "uk.google.com",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "de.google.com",
					Labels:  map[string]string{"foo": "bar", model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
				{
					Address: "in.google.com",
					Labels:  map[string]string{"foo": "bar", model.TLSModeLabelName: model.IstioMutualTLSModeLabel},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DNS,
		},
	}
	cases := []struct {
		name string
		a    *model.Config
		b    *model.Config
		want bool
	}{
		{
			"same config",
			httpDNS,
			httpDNS,
			false,
		},
		{
			"different config",
			httpDNS,
			httpNoneInternal,
			true,
		},
		{
			"different resolution",
			tcpDNS,
			tcpStatic,
			true,
		},
		{
			"config modified with additional host",
			httpDNS,
			updatedHTTPDNS,
			true,
		},
		{
			"config modified with additional port",
			updatedHTTPDNS,
			updatedHTTPDNSPort,
			true,
		},
		{
			"same config with additional endpoint",
			updatedHTTPDNS,
			updatedEndpoint,
			false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			as := convertServices(*tt.a)
			bs := convertServices(*tt.b)
			got := servicesChanged(as, bs)
			if got != tt.want {
				t.Errorf("ServicesChanged got %v, want %v", got, tt.want)
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
