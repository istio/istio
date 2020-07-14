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

package memory_test

import (
	"sync"
	"testing"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestMonitorLifecycle(t *testing.T) {
	// Regression test to ensure no race conditions during monitor shutdown
	store := memory.Make(collections.Mocks)
	m := memory.NewMonitor(store)
	stop := make(chan struct{})
	go m.Run(stop)
	m.ScheduleProcessEvent(memory.ConfigEvent{})
	close(stop)
	m.ScheduleProcessEvent(memory.ConfigEvent{})
}

func TestEventConsistency(t *testing.T) {
	store := memory.Make(collections.Mocks)
	controller := memory.NewController(store)

	testConfig := mock.Make(TestNamespace, 0)
	var testEvent model.Event

	done := make(chan bool)

	lock := sync.Mutex{}

	controller.RegisterEventHandler(collections.Mock.Resource().GroupVersionKind(), func(_, config model.Config, event model.Event) {

		lock.Lock()
		tc := testConfig
		lock.Unlock()

		if event != testEvent {
			t.Errorf("desired %v, but %v", testEvent, event)
		}
		if !mock.Compare(tc, config) {
			t.Errorf("desired %v, but %v", tc, config)
		}
		done <- true
	})

	stop := make(chan struct{})
	go controller.Run(stop)

	// Test Add Event
	testEvent = model.EventAdd
	var rev string
	var err error
	if rev, err = controller.Create(testConfig); err != nil {
		t.Error(err)
		return
	}

	lock.Lock()
	testConfig.ResourceVersion = rev
	lock.Unlock()

	<-done

	// Test Update Event
	testEvent = model.EventUpdate
	if _, err := controller.Update(testConfig); err != nil {
		t.Error(err)
		return
	}
	<-done

	// Test Delete Event
	testEvent = model.EventDelete
	if err := controller.Delete(collections.Mock.Resource().GroupVersionKind(), testConfig.Name, TestNamespace); err != nil {
		t.Error(err)
		return
	}
	<-done
	close(stop)
}
