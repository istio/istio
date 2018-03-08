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
	"sync"
	"testing"
	"time"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/config/memory"
)

const (
	resync          = 5 * time.Millisecond
	notifyThreshold = resync * 10
)

// TODO: ensure this test is reliable (no timing issues) on different systems
func TestController(t *testing.T) {
	configDescriptor := model.ConfigDescriptor{
		model.ExternalService,
	}
	store := memory.Make(configDescriptor)
	configController := memory.NewController(store)
	configStore := model.MakeIstioStore(configController)

	countMutex := sync.Mutex{}
	count := 0

	incrementCount := func() {
		countMutex.Lock()
		defer countMutex.Unlock()
		count++
	}
	getCountAndReset := func() int {
		countMutex.Lock()
		defer countMutex.Unlock()
		c := count
		count = 0
		return c
	}

	ctl := NewController(configStore, resync)
	err := ctl.AppendInstanceHandler(func(instance *model.ServiceInstance, event model.Event) { incrementCount() })
	if err != nil {
		t.Errorf("AppendInstanceHandler() => %q", err)
	}

	err = ctl.AppendServiceHandler(func(service *model.Service, event model.Event) { incrementCount() })
	if err != nil {
		t.Errorf("AppendServiceHandler() => %q", err)
	}

	stop := make(chan struct{})
	go ctl.Run(stop)
	defer close(stop)

	time.Sleep(notifyThreshold)
	if c := getCountAndReset(); c != 0 {
		t.Errorf("got %d notifications from controller, want %d", c, 0)
	}

	config := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.ExternalService.Type,
			Name:      "example",
			Namespace: "default",
			Domain:    "cluster.local",
		},
		Spec: &networking.ExternalService{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-number", Protocol: "http"},
				{Number: 8080, Name: "http2-number", Protocol: "http2"},
			},
			Discovery: networking.ExternalService_NONE,
		},
	}

	_, err = store.Create(config)
	if err != nil {
		t.Errorf("error occurred crearting ExternalService config: %v", err)
	}

	time.Sleep(notifyThreshold)
	if c := getCountAndReset(); c != 2 {
		t.Errorf("got %d notifications from controller, want %d", count, 2)
	}
}
