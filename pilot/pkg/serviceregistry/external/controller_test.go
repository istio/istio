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
<<<<<<< HEAD
	"sync"
=======
	"fmt"
	"sync/atomic"
>>>>>>> f4a7f15ad7... Fix test by making it non racy (#14789)
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

const (
	notifyThreshold = 5 * time.Millisecond
)

// TODO: ensure this test is reliable (no timing issues) on different systems
func TestController(t *testing.T) {
	configDescriptor := model.ConfigDescriptor{
		model.ServiceEntry,
	}
	store := memory.Make(configDescriptor)
	configController := memory.NewController(store)

	count := int64(0)

<<<<<<< HEAD
	ctl := NewServiceDiscovery(configController, model.MakeIstioStore(configController))
	err := ctl.AppendInstanceHandler(func(instance *model.ServiceInstance, event model.Event) { incrementCount() })
=======
	ctl := external.NewServiceDiscovery(configController, model.MakeIstioStore(configController))
	err := ctl.AppendInstanceHandler(func(instance *model.ServiceInstance, event model.Event) { atomic.AddInt64(&count, 1) })
>>>>>>> f4a7f15ad7... Fix test by making it non racy (#14789)
	if err != nil {
		t.Errorf("AppendInstanceHandler() => %q", err)
	}

	err = ctl.AppendServiceHandler(func(service *model.Service, event model.Event) { atomic.AddInt64(&count, 1) })
	if err != nil {
		t.Errorf("AppendServiceHandler() => %q", err)
	}

	stop := make(chan struct{})
	go configController.Run(stop)
	defer close(stop)

	time.Sleep(notifyThreshold)
	if c := getCountAndReset(); c != 0 {
		t.Errorf("got %d notifications from controller, want %d", c, 0)
	}
<<<<<<< HEAD
=======
	expectedCount := int64(7) // 1 service + 6 instances
>>>>>>> f4a7f15ad7... Fix test by making it non racy (#14789)

	_, err = configController.Create(*httpStatic)
	if err != nil {
		t.Errorf("error occurred crearting ServiceEntry config: %v", err)
	}

<<<<<<< HEAD
	time.Sleep(notifyThreshold)
	if c := getCountAndReset(); c != 7 {
		t.Errorf("got %d notifications from controller, want %d", c, 7)
=======
	if err := retry.UntilSuccess(func() error {
		if gotcount := atomic.AddInt64(&count, 0); gotcount != expectedCount {
			return fmt.Errorf("got %d notifications from controller, want %d", gotcount, expectedCount)
		}
		return nil
	}, retry.Delay(50*time.Millisecond), retry.Timeout(5*time.Second)); err != nil {
		t.Fatal(err)
>>>>>>> f4a7f15ad7... Fix test by making it non racy (#14789)
	}
}
