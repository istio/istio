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

package eureka

import (
	"sync"
	"testing"
	"time"

	"istio.io/istio/pilot/model"
)

const (
	resync          = 5 * time.Millisecond
	notifyThreshold = resync * 10
)

type mockSyncClient struct {
	mutex sync.Mutex
	apps  []*application
}

func (m *mockSyncClient) Applications() ([]*application, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	apps := make([]*application, len(m.apps))
	copy(apps, m.apps)
	return apps, nil
}

func (m *mockSyncClient) SetApplications(apps []*application) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.apps = apps
}

var _ Client = (*mockSyncClient)(nil)

// TODO: ensure this test is reliable (no timing issues) on different systems
func TestController(t *testing.T) {
	cl := &mockSyncClient{}

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

	ctl := NewController(cl, resync)
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

	cl.SetApplications([]*application{
		{
			Name: "APP",
			Instances: []*instance{
				makeInstance("hello.world.local", "10.0.0.1", 8080, -1, nil),
				makeInstance("hello.world.local", "10.0.0.2", 8080, -1, nil),
			},
		},
	})
	time.Sleep(notifyThreshold)
	if c := getCountAndReset(); c != 2 {
		t.Errorf("got %d notifications from controller, want %d", count, 2)
	}
}
