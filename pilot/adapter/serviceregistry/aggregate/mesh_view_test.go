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

package aggregate

import (
	"encoding/hex"
	"fmt"
	"testing"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform"
	"istio.io/istio/pilot/test/mock"
)

var platform1 platform.ServiceRegistry
var platform2 platform.ServiceRegistry
var discovery1 *mock.ServiceDiscovery
var discovery2 *mock.ServiceDiscovery
var evVerifier eventVerifier
var mockInstances []*model.ServiceInstance
var helloSvcInst []*model.ServiceInstance
var worldSvcInst []*model.ServiceInstance
var countInstPerSvc int

// MockMeshResourceView specifies a mock MeshResourceView for testing
type MockController struct {
	registry platform.ServiceRegistry
}

type serviceEvent struct {
	*model.Service
	model.Event
}

type instanceEvent struct {
	*model.ServiceInstance
	model.Event
}

type eventVerifier struct {
	mockSvcHandlerMap map[platform.ServiceRegistry]func(*model.Service, model.Event)
	svcTracked        []serviceEvent
	svcToVerify       []serviceEvent

	mockInstHandlerMap map[platform.ServiceRegistry]func(*model.ServiceInstance, model.Event)
	instTracked        []instanceEvent
	instToVerify       []instanceEvent
}

func newEventVerifier() *eventVerifier {
	out := eventVerifier{
		mockSvcHandlerMap:  map[platform.ServiceRegistry]func(*model.Service, model.Event){},
		svcTracked:         []serviceEvent{},
		svcToVerify:        []serviceEvent{},
		mockInstHandlerMap: map[platform.ServiceRegistry]func(*model.ServiceInstance, model.Event){},
		instTracked:        []instanceEvent{},
		instToVerify:       []instanceEvent{},
	}
	return &out
}

func (ev *eventVerifier) trackService(s *model.Service, e model.Event) {
	ev.svcTracked = append(ev.svcTracked, serviceEvent{s, e})
}

func (ev *eventVerifier) trackInstance(i *model.ServiceInstance, e model.Event) {
	ev.instTracked = append(ev.instTracked, instanceEvent{i, e})
}

func (ev *eventVerifier) mockSvcEvent(registry platform.ServiceRegistry, s *model.Service, e model.Event) {
	ev.svcToVerify = append(ev.svcToVerify, serviceEvent{s, e})
	handler, found := ev.mockSvcHandlerMap[registry]
	if found {
		handler(s, e)
	}
}

func (ev *eventVerifier) mockInstEvent(registry platform.ServiceRegistry, i *model.ServiceInstance, e model.Event) {
	ev.instToVerify = append(ev.instToVerify, instanceEvent{i, e})
	handler, found := ev.mockInstHandlerMap[registry]
	if found {
		handler(i, e)
	}
}

func (ev *eventVerifier) verifyServiceEvents(t *testing.T) {
	expCount := len(ev.svcToVerify)
	actCount := len(ev.svcTracked)
	expIdx := 0
	for ; expIdx < expCount && expIdx < actCount; expIdx++ {
		expEvent := ev.svcToVerify[expIdx]
		actEvent := ev.svcTracked[expIdx]
		if expEvent.Service.Hostname != actEvent.Service.Hostname || expEvent.Event != actEvent.Event {
			t.Errorf("Unexpected out-of-sequence service event: expected event '%v', actual event '%v'", expEvent, actEvent)
		}
	}
	for ; expIdx < expCount; expIdx++ {
		expEvent := ev.svcToVerify[expIdx]
		t.Errorf("Expected service event: '%v', none actually tracked", expEvent)
	}
	for ; expIdx < actCount; expIdx++ {
		actEvent := ev.svcTracked[expIdx]
		t.Errorf("Unexpected extra service event: '%v'", actEvent)
	}
	ev.svcToVerify = []serviceEvent{}
	ev.svcTracked = []serviceEvent{}
}

func (ev *eventVerifier) verifyInstanceEvents(t *testing.T) {
	expCount := len(ev.instToVerify)
	actCount := len(ev.instTracked)
	expIdx := 0
	for ; expIdx < expCount && expIdx < actCount; expIdx++ {
		expEvent := ev.instToVerify[expIdx]
		actEvent := ev.instTracked[expIdx]
		if expEvent.ServiceInstance.Service.Hostname != actEvent.ServiceInstance.Service.Hostname ||
			expEvent.ServiceInstance.Endpoint.Address != actEvent.ServiceInstance.Endpoint.Address ||
			expEvent.ServiceInstance.Endpoint.Port != actEvent.ServiceInstance.Endpoint.Port ||
			expEvent.Event != actEvent.Event {
			t.Errorf("Unexpected out-of-sequence instance event: expected event '%v', actual event '%v'", expEvent, actEvent)
		}
	}
	for ; expIdx < expCount; expIdx++ {
		expEvent := ev.svcToVerify[expIdx]
		t.Errorf("Expected instance event: '%v', none actually tracked", expEvent)
	}
	for ; expIdx < actCount; expIdx++ {
		actEvent := ev.svcTracked[expIdx]
		t.Errorf("Unexpected extra instance event: '%v'", actEvent)
	}
	ev.instToVerify = []instanceEvent{}
	ev.instTracked = []instanceEvent{}
}

func (v *MockController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	evVerifier.mockSvcHandlerMap[v.registry] = f
	return nil
}

func (v *MockController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	evVerifier.mockInstHandlerMap[v.registry] = f
	return nil
}

func (v *MockController) Run(<-chan struct{}) {}

func buildMockMeshResourceView() *MeshResourceView {
	evVerifier = *newEventVerifier()
	discovery1 = mock.NewDiscovery(map[string]*model.Service{}, 0)
	discovery2 = mock.NewDiscovery(map[string]*model.Service{}, 0)
	platform1 = platform.ServiceRegistry("mockAdapter1")
	platform2 = platform.ServiceRegistry("mockAdapter2")

	registry1 := Registry{
		Name:       platform1,
		Controller: &MockController{platform1},
	}

	registry2 := Registry{
		Name:       platform2,
		Controller: &MockController{platform2},
	}

	meshView := NewMeshResourceView()
	meshView.AddRegistry(registry1)
	meshView.AddRegistry(registry2)
	meshView.AppendServiceHandler(evVerifier.trackService)
	meshView.AppendInstanceHandler(evVerifier.trackInstance)
	return meshView
}

func expectServices(t *testing.T, expected, actual []*model.Service) {
	expectedSet := map[string]int{}
	for _, svc := range expected {
		currentCount := expectedSet[svc.Hostname]
		expectedSet[svc.Hostname] = currentCount + 1
	}
	actualSet := map[string]int{}
	for _, svc := range actual {
		currentCount := actualSet[svc.Hostname]
		actualSet[svc.Hostname] = currentCount + 1
	}
	for svcName, countExpected := range expectedSet {
		countActual := actualSet[svcName]
		if countExpected != countActual {
			t.Errorf("Incorrect service count in mesh view. Expected '%d', Actual '%d', Service Name '%s'", countExpected, countActual, svcName)
		}
		delete(actualSet, svcName)
	}
	if len(actualSet) > 0 {
		for svcName, countActual := range actualSet {
			t.Errorf("Unexpected service in mesh view. Expected '0', Actual '%d', Service Name '%s'", countActual, svcName)
		}
	}
}

func buildInstanceKey(i *model.ServiceInstance) string {
	return "[" + i.Service.Hostname + "][" + i.Endpoint.Address + "][" +
		hex.EncodeToString([]byte{byte((i.Endpoint.Port >> 8) & 0xFF), byte(i.Endpoint.Port & 0xFF)}) + "]"
}

func expectInstances(t *testing.T, expected, actual []*model.ServiceInstance) {
	expectedSet := map[string]bool{}
	for _, inst := range expected {
		expectedSet[buildInstanceKey(inst)] = true
	}
	actualSet := map[string]bool{}
	for _, inst := range actual {
		actualSet[buildInstanceKey(inst)] = true
	}
	for expectedKey := range expectedSet {
		_, found := actualSet[expectedKey]
		if !found {
			t.Errorf("Expected instance '%s'. Found none", expectedKey)
		}
		delete(actualSet, expectedKey)
	}
	if len(actualSet) > 0 {
		for actualKey := range actualSet {
			t.Errorf("Unexpected instance in mesh view: '%s'", actualKey)
		}
	}
}

func svcList(s ...*model.Service) []*model.Service {
	return s
}

func instList(s ...*model.ServiceInstance) []*model.ServiceInstance {
	return s
}

func TestServices(t *testing.T) {
	meshView := buildMockMeshResourceView()
	t.Run("EmptyMesh", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(), svcs)
		evVerifier.verifyServiceEvents(t)
	})

	evVerifier.mockSvcEvent(platform1, mock.HelloService, model.EventAdd)
	t.Run("AddHelloServicePlatform1", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService), svcs)
		evVerifier.verifyServiceEvents(t)
	})

	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventAdd)
	t.Run("AddWorldServicePlatform2", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService), svcs)
		evVerifier.verifyServiceEvents(t)
	})

	evVerifier.mockSvcEvent(platform1, mock.WorldService, model.EventAdd)
	t.Run("AddWorldServiceAgainButPlatform1", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService, mock.WorldService), svcs)
		evVerifier.verifyServiceEvents(t)
	})

	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventDelete)
	t.Run("DeleteWorldServicePlatform2", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService), svcs)
		evVerifier.verifyServiceEvents(t)
	})

	evVerifier.mockSvcEvent(platform1, mock.WorldService, model.EventUpdate)
	t.Run("UpdateWorldServicePlatform1", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService), svcs)
		evVerifier.verifyServiceEvents(t)
	})

	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventUpdate)
	t.Run("UpsertWorldServicePlatform2", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService, mock.WorldService), svcs)
		evVerifier.verifyServiceEvents(t)
	})

	evVerifier.mockSvcEvent(platform1, mock.WorldService, model.EventDelete)
	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventDelete)
	evVerifier.mockSvcEvent(platform1, mock.HelloService, model.EventDelete)
	t.Run("AfterDeleteAll", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(), svcs)
		evVerifier.verifyServiceEvents(t)
	})
}

func TestGetService(t *testing.T) {
	meshView := buildMockMeshResourceView()
	t.Run("EmptyView", func(t *testing.T) {
		svc, err := meshView.GetService(mock.HelloService.Hostname)
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("Expected nil. Found: '%v'", svc)
		}
	})
	evVerifier.mockSvcEvent(platform1, mock.HelloService, model.EventAdd)
	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventAdd)
	t.Run("AfterAddHelloAndWorld", func(t *testing.T) {
		expectedList := svcList(mock.HelloService, mock.WorldService)
		for _, expectedSvc := range expectedList {
			svc, err := meshView.GetService(expectedSvc.Hostname)
			if err != nil {
				t.Errorf("Services() encountered unexpected error: %v", err)
				return
			}
			if svc == nil {
				t.Errorf("Expected '%v'. Found none", expectedSvc)
			}
		}
	})
	t.Run("NonExistent", func(t *testing.T) {
		svc, err := meshView.GetService("some-non-existent")
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("Expected nil. Found: '%v'", svc)
		}
	})
	evVerifier.mockSvcEvent(platform1, mock.HelloService, model.EventDelete)
	t.Run("AfterDeleteHelloService", func(t *testing.T) {
		svc, err := meshView.GetService(mock.HelloService.Hostname)
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("Expected nil. Found: '%v'", svc)
		}
	})
	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventDelete)
	t.Run("AfterDelete", func(t *testing.T) {
		expectedNilList := svcList(mock.HelloService, mock.WorldService)
		for _, expectedSvc := range expectedNilList {
			svc, err := meshView.GetService(expectedSvc.Hostname)
			if err != nil {
				t.Errorf("Services() encountered unexpected error: %v", err)
				return
			}
			if svc != nil {
				t.Errorf("Expected nil. Found: '%v'", svc)
			}
		}
	})
}

func TestManagementPorts(t *testing.T) {
	meshView := buildMockMeshResourceView()
	mockInstances = buildMockInstances()
	triggerAddInstanceEvents()
	expected := buildMockManagementPorts()
	t.Run("ExpectNone", func(t *testing.T) {
		ports := meshView.ManagementPorts(mock.MakeIP(mock.HelloService, 1))
		if len(ports) != 0 {
			t.Errorf("Returned wrong number of ports from MeshView. Expected '0' Found '%d'", len(ports))
		}
	})
	t.Run("ExpectTwo", func(t *testing.T) {
		ports := meshView.ManagementPorts(mock.MakeIP(mock.WorldService, 1))
		if len(ports) != len(expected) {
			t.Errorf("Returned wrong number of ports from MeshView. Expected '%d' Found '%d'", len(expected), len(ports))
		}
		for i := 0; i < len(ports); i++ {
			if ports[i].Name != expected[i].Name || ports[i].Port != expected[i].Port ||
				ports[i].Protocol != expected[i].Protocol {
				t.Errorf("Returned management ports result does not match expected one. Expected '%v' Found '%v'", expected[i], ports[i])
			}
		}
	})
}

func buildMockManagementPorts() model.PortList {
	return model.PortList{{
		Name:     "http",
		Port:     3333,
		Protocol: model.ProtocolHTTP,
	}, {
		Name:     "custom",
		Port:     9999,
		Protocol: model.ProtocolTCP,
	}}
}

func buildMockInstancesFromService(svc *model.Service) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, len(svc.Ports))
	for pi, port := range svc.Ports {
		inst := mock.MakeInstance(svc, port, 1, "")
		if inst.Service.Hostname != mock.HelloService.Hostname {
			inst.ManagementPorts = buildMockManagementPorts()
		}
		out[pi] = inst
	}
	return out
}

func buildMockInstances() []*model.ServiceInstance {
	helloSvcInst = buildMockInstancesFromService(mock.HelloService)
	worldSvcInst = buildMockInstancesFromService(mock.WorldService)
	return append(helloSvcInst, worldSvcInst...)
}

func triggerAddInstanceEvents() {
	for _, inst := range mockInstances {
		var registry platform.ServiceRegistry
		if inst.Service.Hostname == mock.HelloService.Hostname {
			registry = platform1
		} else {
			registry = platform2
		}
		evVerifier.mockInstEvent(registry, inst, model.EventAdd)
	}
}

func buildLabelCollection(inst []*model.ServiceInstance) model.LabelsCollection {
	out := model.LabelsCollection{}
	for _, i := range inst {
		out = append(out,
			model.Labels{
				labelInstanceIP:        i.Endpoint.Address,
				labelInstanceNamedPort: i.Endpoint.ServicePort.Name,
			})
	}
	return out
}

func TestInstances(t *testing.T) {
	mockInstances = buildMockInstances()
	meshView := buildMockMeshResourceView()
	t.Run("EmptyView", func(t *testing.T) {
		instances, err := meshView.Instances(mock.HelloService.Hostname, []string{}, model.LabelsCollection{})
		if err != nil {
			t.Errorf("Instances() encountered unexpected error: %v", err)
			return
		}
		if len(instances) > 0 {
			t.Errorf("Expected 0 instances. Found: '%v'", instances)
		}
	})
	triggerAddInstanceEvents()
	t.Run("AddInstancesBothPlatforms", func(t *testing.T) {
		instances, err := meshView.Instances(mock.WorldService.Hostname, []string{}, model.LabelsCollection{})
		if err != nil {
			t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
			return
		}
		expectInstances(t, worldSvcInst, instances)
		instances, err = meshView.Instances(mock.HelloService.Hostname, []string{}, model.LabelsCollection{})
		if err != nil {
			t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.HelloService.Hostname, err)
			return
		}
		expectInstances(t, helloSvcInst, instances)
		evVerifier.verifyInstanceEvents(t)
	})
	t.Run("WithLabelSet", func(t *testing.T) {
		t.Run("OneSet", func(t *testing.T) {
			instances, err := meshView.Instances(mock.WorldService.Hostname, []string{fmt.Sprintf("%d", worldSvcInst[0].Endpoint.Port)},
				buildLabelCollection([]*model.ServiceInstance{worldSvcInst[0]}))
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
				return
			}
			expectInstances(t, []*model.ServiceInstance{worldSvcInst[0]}, instances)
		})
		t.Run("MultipleFirst", func(t *testing.T) {
			instances, err := meshView.Instances(mock.WorldService.Hostname, []string{},
				buildLabelCollection([]*model.ServiceInstance{worldSvcInst[0], worldSvcInst[2]}))
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
				return
			}
			if len(instances) == 0 {
				t.Errorf("Expecting at least one of '%v' or '%v'. found none", *worldSvcInst[0], *worldSvcInst[2])
				return
			}
			var instanceToVerify *model.ServiceInstance
			if instances[0].Endpoint.Port == worldSvcInst[0].Endpoint.Port {
				instanceToVerify = worldSvcInst[0]
			} else {
				instanceToVerify = worldSvcInst[2]
			}
			expectInstances(t, []*model.ServiceInstance{instanceToVerify}, instances)
		})
	})
	idxToChange := 2
	evVerifier.mockInstEvent(platform2, worldSvcInst[idxToChange], model.EventDelete)
	t.Run("RemovedOneInstance", func(t *testing.T) {
		t.Run("CheckRemoved", func(t *testing.T) {
			instances, err := meshView.Instances(mock.WorldService.Hostname, []string{}, model.LabelsCollection{})
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
				return
			}
			expectedInstances := worldSvcInst[0:idxToChange]
			expectedInstances = append(expectedInstances, worldSvcInst[idxToChange+1:]...)
			expectInstances(t, expectedInstances, instances)
		})
		t.Run("CheckUntouched", func(t *testing.T) {
			instances, err := meshView.Instances(mock.HelloService.Hostname, []string{}, model.LabelsCollection{})
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.HelloService.Hostname, err)
				return
			}
			expectInstances(t, helloSvcInst, instances)
		})
		evVerifier.verifyInstanceEvents(t)
	})
	evVerifier.mockInstEvent(platform2, mockInstances[idxToChange], model.EventUpdate)
	t.Run("UpsertOneInstance", func(t *testing.T) {
		instances, err := meshView.Instances(mock.WorldService.Hostname, []string{}, model.LabelsCollection{})
		if err != nil {
			t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
			return
		}
		expectInstances(t, worldSvcInst, instances)
		instances, err = meshView.Instances(mock.HelloService.Hostname, []string{}, model.LabelsCollection{})
		if err != nil {
			t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.HelloService.Hostname, err)
			return
		}
		expectInstances(t, helloSvcInst, instances)
		evVerifier.verifyInstanceEvents(t)
	})
	evVerifier.mockInstEvent(platform2, worldSvcInst[idxToChange], model.EventAdd)
	t.Run("MultipleAddsShouldBeUpserts", func(t *testing.T) {
		instances, err := meshView.Instances(mock.WorldService.Hostname, []string{}, model.LabelsCollection{})
		if err != nil {
			t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
			return
		}
		expectInstances(t, worldSvcInst, instances)
		instances, err = meshView.Instances(mock.HelloService.Hostname, []string{}, model.LabelsCollection{})
		if err != nil {
			t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.HelloService.Hostname, err)
			return
		}
		expectInstances(t, helloSvcInst, instances)
		evVerifier.verifyInstanceEvents(t)
	})
}

func TestHostInstances(t *testing.T) {
	mockInstances = buildMockInstances()
	meshView := buildMockMeshResourceView()
	instanceToVerify := worldSvcInst[3]
	t.Run("EmptyView", func(t *testing.T) {
		addresses := map[string]bool{instanceToVerify.Endpoint.Address: true}
		instances, err := meshView.HostInstances(addresses)
		if err != nil {
			t.Errorf("Instances() encountered unexpected error: %v", err)
			return
		}
		if len(instances) > 0 {
			t.Errorf("Expected 0 instances. Found: '%v'", instances)
		}
	})
	triggerAddInstanceEvents()
	t.Run("AfterAdd", func(t *testing.T) {
		addresses := map[string]bool{instanceToVerify.Endpoint.Address: true}
		instances, err := meshView.HostInstances(addresses)
		if err != nil {
			t.Errorf("Instances() encountered unexpected error: %v", err)
			return
		}
		expectInstances(t, worldSvcInst, instances)
	})
	evVerifier.mockInstEvent(platform2, instanceToVerify, model.EventDelete)
	t.Run("AfterDeleteOne", func(t *testing.T) {
		addresses := map[string]bool{instanceToVerify.Endpoint.Address: true}
		instances, err := meshView.HostInstances(addresses)
		if err != nil {
			t.Errorf("Instances() encountered unexpected error: %v", err)
			return
		}
		expectedInstances := []*model.ServiceInstance{}
		for _, inst := range worldSvcInst {
			if inst.Endpoint.Address == instanceToVerify.Endpoint.Address &&
				inst.Endpoint.Port == instanceToVerify.Endpoint.Port {
				continue
			}
			expectedInstances = append(expectedInstances, inst)
		}
		expectInstances(t, expectedInstances, instances)
	})
}

func buildMockAccounts(inst *model.ServiceInstance) map[string]bool {
	svcAcct1, svcAcct2, instAcct := "service-account-1", "service-account-2", "inst-account"
	expectedAccounts := map[string]bool{
		svcAcct1: true,
		svcAcct2: true,
		instAcct: true}
	inst.Service.ServiceAccounts = []string{svcAcct1, svcAcct2}
	inst.ServiceAccount = instAcct
	return expectedAccounts
}

func TestGetIstioServiceAccounts(t *testing.T) {
	mockInstances = buildMockInstances()
	meshView := buildMockMeshResourceView()
	instanceToVerify := worldSvcInst[3]
	evVerifier.mockSvcEvent(platform1, instanceToVerify.Service, model.EventAdd)
	t.Run("EmptyView", func(t *testing.T) {
		address := instanceToVerify.Endpoint.Address
		accounts := meshView.GetIstioServiceAccounts(address, []string{})
		if len(accounts) > 0 {
			t.Errorf("Expected 0 instances. Found: '%v'", accounts)
		}
	})
	triggerAddInstanceEvents()
	t.Run("AfterAddEmptyAddresses", func(t *testing.T) {
		address := instanceToVerify.Endpoint.Address
		accounts := meshView.GetIstioServiceAccounts(address, []string{})
		if len(accounts) > 0 {
			t.Errorf("Expected 0 instances. Found: '%v'", accounts)
		}
	})
	expectedAccts := buildMockAccounts(instanceToVerify)
	t.Run("AfterAddWithAddresses", func(t *testing.T) {
		hostName := instanceToVerify.Service.Hostname
		accounts := meshView.GetIstioServiceAccounts(hostName, []string{})
		if len(accounts) == 0 {
			t.Errorf("Expected service accounts '%v'. Found nothing.", expectedAccts)
			return
		}
		for _, account := range accounts {
			if _, found := expectedAccts[account]; !found {
				t.Errorf("Unexpected account found: '%s'.", account)
			}
		}
		if len(accounts) != len(expectedAccts) {
			t.Errorf("Partial list of accounts found. Expected: '%v', Actual '%v'.", expectedAccts, accounts)
		}
	})
}

func TestServiceByLabels(t *testing.T) {
	meshView := buildMockMeshResourceView()
	evVerifier.mockSvcEvent(platform1, mock.HelloService, model.EventAdd)
	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventAdd)
	t.Run("NonExistentLabel", func(t *testing.T) {
		svcs := meshView.serviceByLabels(
			resourceLabels{
				resourceLabel{labelServiceName, &mock.HelloService.Hostname},
				resourceLabel{"some-non-existent-label", &mock.HelloService.Hostname},
			})
		if len(svcs) != 0 {
			t.Errorf("Expected no services. Found: '%v'", svcs)
		}
	})
	t.Run("LabelVIP", func(t *testing.T) {
		hexAddress := getIPHex(mock.HelloService.Address)
		svcs := meshView.serviceByLabels(
			resourceLabels{
				resourceLabel{labelServiceName, &mock.HelloService.Hostname},
				resourceLabel{labelServiceVIP, &hexAddress},
			})
		expectServices(t, []*model.Service{mock.HelloService}, svcs)
	})
	extSvc := mock.MakeService("hello.default.svc.cluster.local", "10.1.0.0")
	extSvc.ExternalName = "some-external-service.somedomain.com"
	evVerifier.mockSvcEvent(platform2, extSvc, model.EventAdd)
	t.Run("LabelExternalName", func(t *testing.T) {
		svcs := meshView.serviceByLabels(
			resourceLabels{
				resourceLabel{labelServiceName, &extSvc.Hostname},
				resourceLabel{labelServiceExternalName, &extSvc.ExternalName},
			})
		expectServices(t, []*model.Service{extSvc}, svcs)
	})
	evVerifier.mockSvcEvent(platform2, extSvc, model.EventDelete)
	t.Run("LabelExternalNameAfterDelete", func(t *testing.T) {
		svcs := meshView.serviceByLabels(
			resourceLabels{
				resourceLabel{labelServiceName, &extSvc.Hostname},
				resourceLabel{labelServiceExternalName, &extSvc.ExternalName},
			})
		expectServices(t, []*model.Service{}, svcs)
	})
}

func TestRun(t *testing.T) {
	meshView := buildMockMeshResourceView()
	stop := make(chan struct{})
	go meshView.Run(stop)
	var done struct{}
	stop <- done
}
