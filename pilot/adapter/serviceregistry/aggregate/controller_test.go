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
	"net"
	"testing"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform"
	"istio.io/istio/pilot/test/mock"
)

const (
	platform1 = platform.ServiceRegistry("mockAdapter1")
	platform2 = platform.ServiceRegistry("mockAdapter2")
)

var evVerifier eventVerifier
var mockInstances []*model.ServiceInstance
var helloSvcInst []*model.ServiceInstance
var worldSvcInst []*model.ServiceInstance
var countInstPerSvc int

// MockMeshResourceView specifies a mock MeshResourceView for testing
type MockController struct {
	registry         platform.ServiceRegistry
	serviceDiscovery *mock.ServiceDiscovery
	controllerPath   string
	viewHandler      *model.ControllerViewHandler
}

// eventVerifier checks that the cache invalidator receives notifications only for
// Services and Instances that changed.
type eventVerifier struct {
	controllerMap             map[platform.ServiceRegistry]*MockController
	serviceActualEvictions    model.CacheReferences
	serviceExpectedEvictions  model.CacheReferences
	instanceActualEvictions   model.CacheReferences
	instanceExpectedEvictions model.CacheReferences
}

func (con *MockController) MockReconcileEvent() {
	conServices, err := con.serviceDiscovery.Services()
	if err != nil {
		panic("Unexpected error while mocking reconcile events: " + err.Error())
	}
	controllerView := model.ControllerView{
		Path:             con.controllerPath,
		Services:         conServices,
		ServiceInstances: con.serviceDiscovery.WantHostInstances,
	}
	(*con.viewHandler).Reconcile(&controllerView)
}

func newEventVerifier() *eventVerifier {
	return &eventVerifier{
		controllerMap:             map[platform.ServiceRegistry]*MockController{},
		serviceActualEvictions:    *model.NewCacheReferences(labelServiceName),
		serviceExpectedEvictions:  *model.NewCacheReferences(labelServiceName),
		instanceActualEvictions:   *model.NewCacheReferences(labelInstanceRef),
		instanceExpectedEvictions: *model.NewCacheReferences(labelInstanceRef),
	}
}

func (ev *eventVerifier) EvictCache(cr model.CacheReferences) {
	var actualCr *model.CacheReferences
	switch cr.Kind {
	case labelServiceName:
		actualCr = &ev.serviceActualEvictions
		break
	case labelInstanceRef:
		actualCr = &ev.instanceActualEvictions
		break
	default:
		panic("Unsupported cache type: '" + cr.Kind + "'")
	}
	for key, add := range cr.Keyset {
		if add {
			actualCr.Keyset[key] = true
		}
	}
}

func (ev *eventVerifier) mockSvcEvent(registry platform.ServiceRegistry, s *model.Service, e model.Event) {
	controller, found := ev.controllerMap[registry]
	if found {
		ev.serviceExpectedEvictions.Keyset[s.Hostname] = true
		err := controller.serviceDiscovery.ModifyService(s, e)
		if err != nil {
			panic("Unexpected mock failure: " + err.Error())
		}
		return
	}
	panic("Unexpected mock operation: No controller for registry '" + string(registry) + "' was ever setup.")
}

func (ev *eventVerifier) mockInstEvent(registry platform.ServiceRegistry, i *model.ServiceInstance, e model.Event) {
	controller, found := ev.controllerMap[registry]
	if found {
		ev.instanceExpectedEvictions.Keyset[buildInstanceKey(i)] = true
		err := controller.serviceDiscovery.ModifyServiceInstance(i, e)
		if err != nil {
			panic("Unexpected mock failure: " + err.Error())
		}
		return
	}
	panic("Unexpected mock operation: No controller for registry '" + string(registry) + "' was ever setup.")
}

func (ev *eventVerifier) mockControllerReconcileEvents() {
	for _, con := range ev.controllerMap {
		con.MockReconcileEvent()
	}
}

func (ev *eventVerifier) verifyEvictions(t *testing.T) {
	if !ev.serviceExpectedEvictions.Equals(&ev.serviceActualEvictions) {
		t.Errorf("Keys found for service cache eviction do not match up. Expected '%s'. Actual '%s'",
			ev.serviceExpectedEvictions.String(), ev.serviceActualEvictions.String())
	}
	if !ev.instanceExpectedEvictions.Equals(&ev.instanceActualEvictions) {
		t.Errorf("Keys found for instance cache eviction do not match up. Expected '%s'. Actual '%s'",
			ev.instanceExpectedEvictions.String(), ev.instanceActualEvictions.String())
	}
	ev.serviceActualEvictions = *model.NewCacheReferences(labelServiceName)
	ev.serviceExpectedEvictions = *model.NewCacheReferences(labelServiceName)
	ev.instanceActualEvictions = *model.NewCacheReferences(labelInstanceRef)
	ev.instanceExpectedEvictions = *model.NewCacheReferences(labelInstanceRef)
}

func NewMockController(pl platform.ServiceRegistry) *MockController {
	return &MockController{
		registry:         pl,
		serviceDiscovery: mock.NewDiscovery(map[string]*model.Service{}, 0),
	}
}

func (con *MockController) Handle(p string, h *model.ControllerViewHandler) error {
	con.controllerPath = p
	con.viewHandler = h
	return nil
}

func (con *MockController) Run(<-chan struct{}) {}

func buildMockMeshResourceView() *MeshResourceView {
	controller1 := NewMockController(platform1)
	controller2 := NewMockController(platform2)
	registry1 := Registry{
		Name:       platform1,
		Controller: controller1,
	}
	registry2 := Registry{
		Name:       platform2,
		Controller: controller2,
	}

	evVerifier = *newEventVerifier()
	evVerifier.controllerMap[platform1] = controller1
	evVerifier.controllerMap[platform2] = controller2

	meshView := NewMeshResourceView()
	meshView.AddRegistry(registry1)
	meshView.AddRegistry(registry2)
	meshView.SetCacheEvictionHandler(&evVerifier)
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
	ip := net.ParseIP(i.Endpoint.Address)
	return "[" + i.Service.Hostname + "][" + hex.EncodeToString(ip) + "][" +
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
	t.Run("EmptyMeshNoReconcileEvents", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(), svcs)
		evVerifier.verifyEvictions(t)
	})

	evVerifier.mockControllerReconcileEvents()
	t.Run("EmptyMeshAfterEmptyReconcileEvents", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(), svcs)
		evVerifier.verifyEvictions(t)
	})

	evVerifier.mockSvcEvent(platform1, mock.HelloService, model.EventAdd)
	evVerifier.mockControllerReconcileEvents()
	t.Run("AddHelloServicePlatform1", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService), svcs)
		evVerifier.verifyEvictions(t)
	})

	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventAdd)
	evVerifier.mockControllerReconcileEvents()
	t.Run("AddWorldServicePlatform2", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService), svcs)
		evVerifier.verifyEvictions(t)
	})

	evVerifier.mockSvcEvent(platform1, mock.WorldService, model.EventAdd)
	evVerifier.mockControllerReconcileEvents()
	t.Run("AddWorldServiceAgainButPlatform1", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService, mock.WorldService), svcs)
		evVerifier.verifyEvictions(t)
	})

	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventDelete)
	evVerifier.mockControllerReconcileEvents()
	t.Run("DeleteWorldServicePlatform2", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService), svcs)
		evVerifier.verifyEvictions(t)
	})

	svcToUpdate := mock.MakeService(mock.WorldService.Hostname, mock.WorldService.Address)
	svcToUpdate.LoadBalancingDisabled = true
	evVerifier.mockSvcEvent(platform1, svcToUpdate, model.EventUpdate)
	evVerifier.mockControllerReconcileEvents()
	t.Run("UpdateWorldServicePlatform1", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, svcToUpdate), svcs)
		evVerifier.verifyEvictions(t)
	})

	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventAdd)
	evVerifier.mockControllerReconcileEvents()
	t.Run("AddWorldServiceAgainButToPlatform2", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(mock.HelloService, mock.WorldService, mock.WorldService), svcs)
		evVerifier.verifyEvictions(t)
	})

	evVerifier.mockSvcEvent(platform1, mock.WorldService, model.EventDelete)
	evVerifier.mockSvcEvent(platform2, mock.WorldService, model.EventDelete)
	evVerifier.mockSvcEvent(platform1, mock.HelloService, model.EventDelete)
	evVerifier.mockControllerReconcileEvents()
	t.Run("AfterDeleteAll", func(t *testing.T) {
		svcs, err := meshView.Services()
		if err != nil {
			t.Errorf("Services() encountered unexpected error: %v", err)
			return
		}
		expectServices(t, svcList(), svcs)
		evVerifier.verifyEvictions(t)
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
	evVerifier.mockControllerReconcileEvents()
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
	evVerifier.mockControllerReconcileEvents()
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
	evVerifier.mockControllerReconcileEvents()
	t.Run("AfterDeleteWorldService", func(t *testing.T) {
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
	triggerAddInstanceEvents(t)
	expected := buildMockManagementPorts()
	evVerifier.mockControllerReconcileEvents()
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
		expectedPortMap := map[int]*model.Port{}
		for i, cntExp := 0, len(expected); i < cntExp; i++ {
			port := expected[i]
			expectedPortMap[port.Port] = port
		}
		actualPortMap := map[int]*model.Port{}
		for i, cntExp := 0, len(ports); i < cntExp; i++ {
			port := ports[i]
			actualPortMap[port.Port] = port
		}
		for portNum, expPort := range expectedPortMap {
			actPort, found := actualPortMap[portNum]
			if !found {
				t.Errorf("Expected port '%v' not found.", expPort)
				continue
			}
			if expPort.Name != actPort.Name || expPort.Protocol != actPort.Protocol {
				t.Errorf("Returned management ports result does not match expected one. Expected '%v' Found '%v'", expPort, actPort)
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

func triggerAddInstanceEvents(t *testing.T) {
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
	triggerAddInstanceEvents(t)
	evVerifier.mockControllerReconcileEvents()
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
		evVerifier.verifyEvictions(t)
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
	evVerifier.mockControllerReconcileEvents()
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
		evVerifier.verifyEvictions(t)
	})
	evVerifier.mockInstEvent(platform2, mockInstances[idxToChange], model.EventAdd)
	evVerifier.mockControllerReconcileEvents()
	t.Run("AddOneInstance", func(t *testing.T) {
		t.Run("CheckedAdded", func(t *testing.T) {
			instances, err := meshView.Instances(mock.WorldService.Hostname, []string{}, model.LabelsCollection{})
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
				return
			}
			expectInstances(t, worldSvcInst, instances)
		})
		t.Run("CheckedUntouched", func(t *testing.T) {
			instances, err := meshView.Instances(mock.HelloService.Hostname, []string{}, model.LabelsCollection{})
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.HelloService.Hostname, err)
				return
			}
			expectInstances(t, helloSvcInst, instances)
		})
		evVerifier.verifyEvictions(t)
	})
	instToUpdate := mock.MakeInstance(worldSvcInst[idxToChange].Service, worldSvcInst[idxToChange].Endpoint.ServicePort, 1, "SomeNewAZ")
	evVerifier.mockInstEvent(platform2, instToUpdate, model.EventUpdate)
	evVerifier.mockControllerReconcileEvents()
	t.Run("UpdateOneInstance", func(t *testing.T) {
		t.Run("CheckedUpdated", func(t *testing.T) {
			instances, err := meshView.Instances(mock.WorldService.Hostname, []string{}, model.LabelsCollection{})
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.WorldService.Hostname, err)
				return
			}
			cnt := len(worldSvcInst)
			instToVerify := make([]*model.ServiceInstance, cnt)
			for i := 0; i < cnt; i++ {
				if i != idxToChange {
					instToVerify[i] = worldSvcInst[i]
				} else {
					instToVerify[i] = instToUpdate
				}
			}
			expectInstances(t, instToVerify, instances)
		})
		t.Run("CheckedUntouched", func(t *testing.T) {
			instances, err := meshView.Instances(mock.HelloService.Hostname, []string{}, model.LabelsCollection{})
			if err != nil {
				t.Errorf("Services() encountered unexpected error retrieving instance for host '%s': %v", mock.HelloService.Hostname, err)
				return
			}
			expectInstances(t, helloSvcInst, instances)
		})
		evVerifier.verifyEvictions(t)
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
	triggerAddInstanceEvents(t)
	evVerifier.mockControllerReconcileEvents()
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
	evVerifier.mockControllerReconcileEvents()
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
	evVerifier.mockControllerReconcileEvents()
	t.Run("EmptyView", func(t *testing.T) {
		address := instanceToVerify.Endpoint.Address
		accounts := meshView.GetIstioServiceAccounts(address, []string{})
		if len(accounts) > 0 {
			t.Errorf("Expected 0 instances. Found: '%v'", accounts)
		}
	})
	triggerAddInstanceEvents(t)
	evVerifier.mockControllerReconcileEvents()
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
	evVerifier.mockControllerReconcileEvents()
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
	evVerifier.mockControllerReconcileEvents()
	t.Run("LabelExternalName", func(t *testing.T) {
		svcs := meshView.serviceByLabels(
			resourceLabels{
				resourceLabel{labelServiceName, &extSvc.Hostname},
				resourceLabel{labelServiceExternalName, &extSvc.ExternalName},
			})
		expectServices(t, []*model.Service{extSvc}, svcs)
	})
	evVerifier.mockSvcEvent(platform2, extSvc, model.EventDelete)
	evVerifier.mockControllerReconcileEvents()
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
