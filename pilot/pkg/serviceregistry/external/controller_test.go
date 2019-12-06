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
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
)

type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan string
}

func (fx *FakeXdsUpdater) EDSUpdate(shard, hostname string, namespace string, entry []*model.IstioEndpoint) error {
	if len(entry) > 0 {
		fx.Events <- "eds"
	}
	return nil
}

func (fx *FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {
	fx.Events <- "xds"
}

func (fx *FakeXdsUpdater) ProxyUpdate(clusterID, ip string) {
}

func (fx *FakeXdsUpdater) SvcUpdate(shard, hostname string, namespace string, event model.Event) {
}
func TestController(t *testing.T) {
	configDescriptor := schema.Set{
		schemas.ServiceEntry,
	}
	store := memory.Make(configDescriptor)
	configController := memory.NewController(store)

	eventch := make(chan string)
	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}

	external.NewServiceDiscovery(configController, model.MakeIstioStore(configController), xdsUpdater)

	stop := make(chan struct{})
	go configController.Run(stop)
	defer close(stop)

	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schemas.ServiceEntry.Type,
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

	handler := <-eventch
	if handler != "xds" {
		t.Fatalf("Expected config update to be called, but got %s", handler)
	}
}

// Validate that Service Entry changes trigger appropriate handlers.
func TestServiceEntryChanges(t *testing.T) {
	configDescriptor := schema.Set{
		schemas.ServiceEntry,
	}
	store := memory.Make(configDescriptor)
	configController := memory.NewController(store)

	eventch := make(chan string)

	xdsUpdater := &FakeXdsUpdater{
		Events: eventch,
	}

	external.NewServiceDiscovery(configController, model.MakeIstioStore(configController), xdsUpdater)

	stop := make(chan struct{})
	go configController.Run(stop)
	defer close(stop)

	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      schemas.ServiceEntry.Type,
			Name:      "fake",
			Namespace: "fake-ns",
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

	handler := <-eventch
	if handler != "xds" {
		t.Fatalf("Expected config update to be called, but got %s", handler)
	}

	// Update service entry with Host changes
	updatecfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            schemas.ServiceEntry.Type,
			Name:            "fake",
			Namespace:       "fake-ns",
			ResourceVersion: revision,
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

	handler = <-eventch
	if handler != "xds" {
		t.Fatalf("Expected config update to be called, but got %s", handler)
	}

	// Update Service Entry with Endpoint changes
	updatecfg = model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            schemas.ServiceEntry.Type,
			Name:            "fake",
			Namespace:       "fake-ns",
			ResourceVersion: revision,
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

	// Here we expect only eds update to be called.
	handler = <-eventch
	if handler != "eds" {
		t.Fatalf("Expected eds update to be called, but got %s", handler)
	}
}
