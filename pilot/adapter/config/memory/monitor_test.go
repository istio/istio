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

package memory_test

import (
	"testing"

	"istio.io/istio/pilot/adapter/config/memory"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/test/mock"
)

func TestEventConsistency(t *testing.T) {
	store := memory.Make(mock.Types)
	controller := memory.NewController(store)

	testConfig := mock.Make(TestNamespace, 0)
	var testEvent model.Event

	done := make(chan bool)

	controller.RegisterEventHandler(model.MockConfig.Type, func(config model.Config, event model.Event) {
		if event != testEvent {
			t.Errorf("desired %v, but %v", testEvent, event)
		}
		if !mock.Compare(testConfig, config) {
			t.Errorf("desired %v, but %v", testConfig, config)
		}
		done <- true
	})

	stop := make(chan struct{})
	go controller.Run(stop)

	// Test Add Event
	testEvent = model.EventAdd
	if rev, err := controller.Create(testConfig); err != nil {
		t.Error(err)
		return
	} else {
		testConfig.ResourceVersion = rev
	}
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
	if err := controller.Delete(model.MockConfig.Type, testConfig.Name, TestNamespace); err != nil {
		t.Error(err)
		return
	}
	<-done
	close(stop)
}
