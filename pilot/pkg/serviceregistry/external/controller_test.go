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

package external_test

import (
	"testing"
	"time"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

var (
	serviceEntrySchemas = collection.SchemasFor(collections.IstioNetworkingV1Alpha3Serviceentries)
	serviceEntryKind    = collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind()
)

type Event struct {
	kind      string
	push      *model.PushRequest
	host      string
	namespace string
	eps       []*model.IstioEndpoint
}

type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan Event
}

func (fx *FakeXdsUpdater) EDSUpdate(_, hostname string, namespace string, entry []*model.IstioEndpoint) error {
	if len(entry) > 0 {
		fx.Events <- Event{kind: "eds", host: hostname, namespace: namespace, eps: entry}
	}
	return nil
}

func (fx *FakeXdsUpdater) ConfigUpdate(push *model.PushRequest) {
	fx.Events <- Event{kind: "xds", push: push}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_, _ string) {
}

func (fx *FakeXdsUpdater) SvcUpdate(_, hostname string, namespace string, _ model.Event) {
	fx.Events <- Event{kind: "svcupdate", host: hostname, namespace: namespace}
}

func TestController(t *testing.T) {
	store := memory.Make(serviceEntrySchemas)
	configController := memory.NewController(store)

	eventch := make(chan Event)
	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}

	external.NewServiceDiscovery(configController, model.MakeIstioStore(configController), xdsUpdater)

	stop := make(chan struct{})
	go configController.Run(stop)
	defer close(stop)

	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              serviceEntryKind.Kind,
			Version:           serviceEntryKind.Version,
			Group:             serviceEntryKind.Group,
			Name:              "fake",
			Namespace:         "fake-ns",
			CreationTimestamp: time.Now(),
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
				{Number: 8080, Name: "http-alt-port", Protocol: "http"},
			},
			Endpoints: []*networking.ServiceEntry_Endpoint{
				{
					Address: "2.2.2.2",
					Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
				},
				{
					Address: "3.3.3.3",
					Ports:   map[string]uint32{"http-port": 1080},
				},
				{
					Address: "4.4.4.4",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{"foo": "bar"},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_STATIC,
		},
	}

	_, err := configController.Create(cfg)

	if err != nil {
		t.Fatalf("Error in creating service entry %v", err)
	}

	handler := waitForEvent(t, eventch)
	if handler.kind != "xds" && !handler.push.Full {
		t.Fatalf("Expected full push config update to be called, but got %v", handler)
	}
}

// Validate that Service Entry changes trigger appropriate handlers.
func TestServiceEntryChanges(t *testing.T) {
	store := memory.Make(serviceEntrySchemas)
	configController := memory.NewController(store)

	eventch := make(chan Event)

	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}

	external.NewServiceDiscovery(configController, model.MakeIstioStore(configController), xdsUpdater)

	stop := make(chan struct{})
	go configController.Run(stop)
	defer close(stop)

	ct := time.Now()

	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              serviceEntryKind.Kind,
			Version:           serviceEntryKind.Version,
			Group:             serviceEntryKind.Group,
			Name:              "fake",
			Namespace:         "fake-ns",
			CreationTimestamp: ct,
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
				{Number: 8080, Name: "http-alt-port", Protocol: "http"},
			},
			Endpoints: []*networking.ServiceEntry_Endpoint{
				{
					Address: "2.2.2.2",
					Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
				},
				{
					Address: "3.3.3.3",
					Ports:   map[string]uint32{"http-port": 1080},
				},
				{
					Address: "4.4.4.4",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{"foo": "bar"},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_STATIC,
		},
	}

	revision, err := configController.Create(cfg)

	if err != nil {
		t.Fatalf("Error in creating service entry %v", err)
	}

	handler := waitForEvent(t, eventch)
	if handler.kind != "xds" && !handler.push.Full {
		t.Fatalf("Expected config update to be called, but got %v", handler)
	}

	// Update service entry with Host changes
	updatecfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              serviceEntryKind.Kind,
			Version:           serviceEntryKind.Version,
			Group:             serviceEntryKind.Group,
			Name:              "fake",
			Namespace:         "fake-ns",
			ResourceVersion:   revision,
			CreationTimestamp: ct,
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.google.com", "test.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
				{Number: 8080, Name: "http-alt-port", Protocol: "http"},
			},
			Endpoints: []*networking.ServiceEntry_Endpoint{
				{
					Address: "2.2.2.2",
					Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
				},
				{
					Address: "3.3.3.3",
					Ports:   map[string]uint32{"http-port": 1080},
				},
				{
					Address: "4.4.4.4",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{"foo": "bar"},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_STATIC,
		},
	}

	revision, err = configController.Update(updatecfg)

	if err != nil {
		t.Fatalf("Error in creating service entry %v", err)
	}

	handler = waitForEvent(t, eventch)
	if handler.kind != "xds" && !handler.push.Full {
		t.Fatalf("Expected config update to be called, but got %v", handler)
	}

	// Update Service Entry with Endpoint changes
	updatecfg = model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              serviceEntryKind.Kind,
			Version:           serviceEntryKind.Version,
			Group:             serviceEntryKind.Group,
			Name:              "fake",
			Namespace:         "fake-ns",
			ResourceVersion:   revision,
			CreationTimestamp: ct,
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.google.com", "test.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
				{Number: 8080, Name: "http-alt-port", Protocol: "http"},
			},
			Endpoints: []*networking.ServiceEntry_Endpoint{
				{
					Address: "2.2.2.2",
					Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
				},
				{
					Address: "3.3.3.3",
					Ports:   map[string]uint32{"http-port": 1080},
				},
				{
					Address: "5.5.5.5",
					Ports:   map[string]uint32{"http-port": 1080},
				},
				{
					Address: "6.6.6.6",
					Ports:   map[string]uint32{"http-port": 1080},
				},
				{
					Address: "4.4.4.4",
					Ports:   map[string]uint32{"http-port": 1080},
					Labels:  map[string]string{"foo": "bar"},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_STATIC,
		},
	}

	_, err = configController.Update(updatecfg)

	if err != nil {
		t.Fatalf("Error in creating service entry %v", err)
	}

	// Here we expect only eds updates to be called twice, once for each service.
	handler = waitForEvent(t, eventch)
	if handler.kind != "eds" && handler.host != "*.google.com" && handler.namespace != "fake-ns" {
		t.Fatalf("Expected eds update to be called for %s, but got %v", "*.google.com", handler)
	}

	handler = waitForEvent(t, eventch)
	if handler.kind != "eds" && handler.host != "test.com" && handler.namespace != "fake-ns" {
		t.Fatalf("Expected eds update to be called for %s, but got %v", "test.com", handler)
	}
}

func TestServiceEntryDelete(t *testing.T) {
	store := memory.Make(serviceEntrySchemas)
	configController := memory.NewController(store)

	eventch := make(chan Event)

	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}

	external.NewServiceDiscovery(configController, model.MakeIstioStore(configController), xdsUpdater)

	stop := make(chan struct{})
	go configController.Run(stop)
	defer close(stop)

	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      serviceEntryKind.Kind,
			Version:   serviceEntryKind.Version,
			Group:     serviceEntryKind.Group,
			Name:      "httpbin-egress",
			Namespace: "test-ns",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"httpbin.default.svc.cluster.local"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
			},
			Endpoints: []*networking.ServiceEntry_Endpoint{
				{
					Address: "10.31.241.103",
					Ports:   map[string]uint32{"http-port": 80},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_STATIC,
		},
	}

	_, err := configController.Create(cfg)

	if err != nil {
		t.Fatalf("Error in creating service entry %v", err)
	}

	handler := waitForEvent(t, eventch)
	if handler.kind != "xds" {
		t.Fatalf("Expected config update to be called, but got %v", handler)
	}

	// delete service entry.
	err = configController.Delete(serviceEntryKind, "httpbin-egress", "test-ns")

	if err != nil {
		t.Fatalf("Error in deleting service entry %v", err)
	}

	// Validate that it triggers SvcUpdate event then followed by full push.
	handler = waitForEvent(t, eventch)
	if handler.kind != "svcupdate" && handler.host != "httpbin.default.svc.cluster.local" && handler.namespace == "test-ns" {
		t.Fatalf("Expected svc update to be called, but got %v", handler)
	}

	handler = waitForEvent(t, eventch)
	if handler.kind != "xds" {
		t.Fatalf("Expected config update to be called, but got %v", handler)
	}
}

func waitForEvent(t *testing.T, ch chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for event")
		return Event{}
	}
}
