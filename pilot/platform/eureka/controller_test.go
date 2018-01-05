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
	"istio.io/istio/pilot/platform/test"
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

func buildExpectedControllerView(t *testing.T, cl *mockSyncClient) *model.ControllerView {
	modelSvcs := []*model.Service{}
	apps, err := cl.Applications()
	if err != nil {
		t.Fatalf("Unexpected test setup failure: '%s'", err.Error())
	}
	hostSvcMap := convertServices(apps, nil)
	for _, svc := range hostSvcMap {
		modelSvcs = append(modelSvcs, svc)
	}
	modelInsts := []*model.ServiceInstance{}
	for _, instance := range convertServiceInstances(hostSvcMap, apps) {
		modelInsts = append(modelInsts, instance)
	}
	return test.BuildExpectedControllerView(modelSvcs, modelInsts)
}

// The only thing being tested are the public interfaces of the controller
// namely: Handle() and Run(). Everything else is implementation detail.
func TestController(t *testing.T) {
	cl := &mockSyncClient{}
	cl.SetApplications([]*application{
		{
			Name: "APP",
			Instances: []*instance{
				makeInstance("hello.world.local", "10.0.0.1", 8080, -1, nil),
				makeInstance("hello.world.local", "10.0.0.2", 8080, -1, nil),
			},
		},
	})

	mockHandler := test.NewMockControllerViewHandler()
	controller := NewController(cl, *mockHandler.GetTicker())
	mockHandler.AssertControllerOK(t, controller, buildExpectedControllerView(t, cl))
}
