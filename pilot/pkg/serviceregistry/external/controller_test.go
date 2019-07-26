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
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/test/util/retry"
)

func TestController(t *testing.T) {
	configDescriptor := model.ConfigDescriptor{
		model.ServiceEntry,
	}
	store := memory.Make(configDescriptor)
	configController := memory.NewController(store)

	count := int64(0)

	ctl := external.NewServiceDiscovery(configController, model.MakeIstioStore(configController))
	err := ctl.AppendInstanceHandler(func(instance *model.ServiceInstance, event model.Event) { atomic.AddInt64(&count, 1) })
	if err != nil {
		t.Fatalf("AppendInstanceHandler() => %q", err)
	}

	err = ctl.AppendServiceHandler(func(service *model.Service, event model.Event) { atomic.AddInt64(&count, 1) })
	if err != nil {
		t.Fatalf("AppendServiceHandler() => %q", err)
	}

	stop := make(chan struct{})
	go configController.Run(stop)
	defer close(stop)

	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              model.ServiceEntry.Type,
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
	expectedCount := int64(7) // 1 service + 6 instances

	_, err = configController.Create(cfg)
	if err != nil {
		t.Fatalf("error occurred crearting ServiceEntry config: %v", err)
	}

	if err := retry.UntilSuccess(func() error {
		if gotcount := atomic.AddInt64(&count, 0); gotcount != expectedCount {
			return fmt.Errorf("got %d notifications from controller, want %d", gotcount, expectedCount)
		}
		return nil
	}, retry.Delay(50*time.Millisecond), retry.Timeout(5*time.Second)); err != nil {
		t.Fatal(err)
	}
}
