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

package memory

import (
	"sync"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestMonitorLifecycle(t *testing.T) {
	// Regression test to ensure no race conditions during monitor shutdown
	for i := 0; i < 100; i++ {
		store := Make(collections.Mocks)
		m := NewMonitor(store)

		stop := make(chan struct{})
		go func() {
			m.Run(stop)
		}()
		go func() {
			for i := 0; i < 100; i++ {
				m.ScheduleProcessEvent(ConfigEvent{
					config: config.Config{
						Meta: config.Meta{
							GroupVersionKind: collections.Mock.Resource().GroupVersionKind(),
						},
					},
				})
			}
		}()
		go close(stop)
		m.WaitCompleted()
	}
}

func TestMonitorGracefulExit(t *testing.T) {
	// Regression test to ensure no race conditions during monitor shutdown
	store := Make(collections.Mocks)
	m := NewMonitor(store)

	var number int
	m.AppendEventHandler(collections.Mock.Resource().GroupVersionKind(), func(c config.Config, c2 config.Config, event model.Event) {
		number++
		time.Sleep(time.Millisecond * 10)
	})

	stop := make(chan struct{})
	go func() {
		m.Run(stop)
	}()
	eventsNumber := 100
	for i := 0; i < eventsNumber; i++ {
		m.ScheduleProcessEvent(ConfigEvent{
			config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: collections.Mock.Resource().GroupVersionKind(),
				},
			},
		})
	}
	close(stop)
	m.WaitCompleted()
	if number != eventsNumber {
		t.Fatalf("should process the handler %d times, get got %d", eventsNumber, number)
	}
}

func TestEventConsistency(t *testing.T) {
	store := Make(collections.Mocks)
	controller := NewController(store)

	testConfig := mock.Make(TestNamespace, 0)
	var testEvent model.Event

	done := make(chan bool)

	lock := sync.Mutex{}

	controller.RegisterEventHandler(collections.Mock.Resource().GroupVersionKind(), func(_, config config.Config, event model.Event) {
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
	if err := controller.Delete(collections.Mock.Resource().GroupVersionKind(), testConfig.Name, TestNamespace, nil); err != nil {
		t.Error(err)
		return
	}
	<-done
	close(stop)
}
