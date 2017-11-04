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
	"errors"
	"fmt"
	"testing"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform"
	"istio.io/istio/pilot/test/mock"
)

// MockController specifies a mock Controller for testing
type MockController struct{}

func (c *MockController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	return nil
}

func (c *MockController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	return nil
}

func (c *MockController) Run(<-chan struct{}) {}

var discovery1 *mock.ServiceDiscovery
var discovery2 *mock.ServiceDiscovery

func buildMockController() *Controller {
	discovery1 = mock.NewDiscovery(
		map[string]*model.Service{
			mock.HelloService.Hostname:   mock.HelloService,
			mock.ExtHTTPService.Hostname: mock.ExtHTTPService,
		}, 2)

	discovery2 = mock.NewDiscovery(
		map[string]*model.Service{
			mock.WorldService.Hostname:    mock.WorldService,
			mock.ExtHTTPSService.Hostname: mock.ExtHTTPSService,
		}, 2)

	registry1 := Registry{
		Name:             platform.ServiceRegistry("mockAdapter1"),
		ServiceDiscovery: discovery1,
		ServiceAccounts:  discovery1,
		Controller:       &MockController{},
	}

	registry2 := Registry{
		Name:             platform.ServiceRegistry("mockAdapter2"),
		ServiceDiscovery: discovery2,
		ServiceAccounts:  discovery2,
		Controller:       &MockController{},
	}

	ctls := NewController()
	ctls.AddRegistry(registry1)
	ctls.AddRegistry(registry2)

	return ctls
}

func TestServicesError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.ServicesError = errors.New("mock Services() error")

	// List Services from aggregate controller
	_, err := aggregateCtl.Services()
	if err == nil {
		t.Fatal("Aggregate controller should return error if one discovery client experience error")
	}
}

func TestServices(t *testing.T) {
	aggregateCtl := buildMockController()
	// List Services from aggregate controller
	services, err := aggregateCtl.Services()

	// Set up ground truth hostname values
	serviceMap := map[string]bool{
		mock.HelloService.Hostname:    false,
		mock.ExtHTTPService.Hostname:  false,
		mock.WorldService.Hostname:    false,
		mock.ExtHTTPSService.Hostname: false,
	}

	if err != nil {
		t.Fatalf("Services() encountered unexpected error: %v", err)
	}

	svcCount := 0
	// Compare return value to ground truth
	for _, svc := range services {
		if counted, existed := serviceMap[svc.Hostname]; existed && !counted {
			svcCount++
			serviceMap[svc.Hostname] = true
		}
	}

	if svcCount != len(serviceMap) {
		t.Fatal("Return services does not match ground truth")
	}
}

func TestGetService(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get service from mockAdapter1
	svc, err := aggregateCtl.GetService(mock.HelloService.Hostname)
	if err != nil {
		t.Fatalf("GetService() encountered unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("Fail to get service")
	}
	if svc.Hostname != mock.HelloService.Hostname {
		t.Fatal("Returned service is incorrect")
	}

	// Get service from mockAdapter2
	svc, err = aggregateCtl.GetService(mock.WorldService.Hostname)
	if err != nil {
		t.Fatalf("GetService() encountered unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("Fail to get service")
	}
	if svc.Hostname != mock.WorldService.Hostname {
		t.Fatal("Returned service is incorrect")
	}
}

func TestGetServiceError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.GetServiceError = errors.New("mock GetService() error")

	// Get service from client with error
	svc, err := aggregateCtl.GetService(mock.HelloService.Hostname)
	if err == nil {
		fmt.Println(svc)
		t.Fatal("Aggregate controller should return error if one discovery client experiences " +
			"error and no service is found")
	}
	if svc != nil {
		t.Fatal("GetService() should return nil if no service found")
	}

	// Get service from client without error
	svc, err = aggregateCtl.GetService(mock.WorldService.Hostname)
	if err != nil {
		t.Fatal("Aggregate controller should not return error if service is found")
	}
	if svc == nil {
		t.Fatal("Fail to get service")
	}
	if svc.Hostname != mock.WorldService.Hostname {
		t.Fatal("Returned service is incorrect")
	}
}

func TestHostInstances(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get Instances from mockAdapter1
	instances, err := aggregateCtl.HostInstances(map[string]bool{mock.HelloInstanceV0: true})
	if err != nil {
		t.Fatalf("HostInstances() encountered unexpected error: %v", err)
	}
	if len(instances) != 5 {
		t.Fatalf("Returned HostInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.HelloService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}

	// Get Instances from mockAdapter2
	instances, err = aggregateCtl.HostInstances(map[string]bool{mock.MakeIP(mock.WorldService, 1): true})
	if err != nil {
		t.Fatalf("HostInstances() encountered unexpected error: %v", err)
	}
	if len(instances) != 5 {
		t.Fatalf("Returned HostInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}
}

func TestHostInstancesError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.HostInstancesError = errors.New("mock HostInstances() error")

	// Get Instances from client with error
	instances, err := aggregateCtl.HostInstances(map[string]bool{mock.HelloInstanceV0: true})
	if err == nil {
		t.Fatal("Aggregate controller should return error if one discovery client experiences " +
			"error and no instances are found")
	}
	if len(instances) != 0 {
		t.Fatal("HostInstances() should return no instances is client experiences error")
	}

	// Get Instances from client without error
	instances, err = aggregateCtl.HostInstances(map[string]bool{mock.MakeIP(mock.WorldService, 1): true})
	if err != nil {
		t.Fatal("Aggregate controller should not return error if instances are found")
	}
	if len(instances) != 5 {
		t.Fatalf("Returned HostInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}
}

func TestInstances(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get Instances from mockAdapter1
	instances, err := aggregateCtl.Instances(mock.HelloService.Hostname,
		[]string{mock.PortHTTP.Name},
		model.LabelsCollection{})
	if err != nil {
		t.Fatalf("Instances() encountered unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatal("Returned wrong number of instances from controller")
	}
	for _, instance := range instances {
		if instance.Service.Hostname != mock.HelloService.Hostname {
			t.Fatal("Returned instance's hostname does not match desired value")
		}
		if _, ok := instance.Service.Ports.Get(mock.PortHTTP.Name); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}

	// Get Instances from mockAdapter2
	instances, err = aggregateCtl.Instances(mock.WorldService.Hostname,
		[]string{mock.PortHTTP.Name},
		model.LabelsCollection{})
	if err != nil {
		t.Fatalf("Instances() encountered unexpected error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatal("Returned wrong number of instances from controller")
	}
	for _, instance := range instances {
		if instance.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned instance's hostname does not match desired value")
		}
		if _, ok := instance.Service.Ports.Get(mock.PortHTTP.Name); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}
}

func TestInstancesError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.InstancesError = errors.New("mock Instances() error")

	// Get Instances from client with error
	instances, err := aggregateCtl.Instances(mock.HelloService.Hostname,
		[]string{mock.PortHTTP.Name},
		model.LabelsCollection{})
	if err == nil {
		t.Fatal("Aggregate controller should return error if one discovery client experiences " +
			"error and no instances are found")
	}
	if len(instances) != 0 {
		t.Fatal("Returned wrong number of instances from controller")
	}

	// Get Instances from client without error
	instances, err = aggregateCtl.Instances(mock.WorldService.Hostname,
		[]string{mock.PortHTTP.Name},
		model.LabelsCollection{})
	if err != nil {
		t.Fatalf("Instances() should not return error is instances are found: %v", err)
	}
	if len(instances) != 2 {
		t.Fatal("Returned wrong number of instances from controller")
	}
	for _, instance := range instances {
		if instance.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned instance's hostname does not match desired value")
		}
		if _, ok := instance.Service.Ports.Get(mock.PortHTTP.Name); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}
}

func TestGetIstioServiceAccounts(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get accounts from mockAdapter1
	accounts := aggregateCtl.GetIstioServiceAccounts(mock.HelloService.Hostname, []string{})
	expected := []string{}

	if len(accounts) != len(expected) {
		t.Fatal("Incorrect account result returned")
	}

	for i := 0; i < len(accounts); i++ {
		if accounts[i] != expected[i] {
			t.Fatal("Returned account result does not match expected one")
		}
	}

	// Get accounts from mockAdapter2
	accounts = aggregateCtl.GetIstioServiceAccounts(mock.WorldService.Hostname, []string{})
	expected = []string{
		"spiffe://cluster.local/ns/default/sa/serviceaccount1",
		"spiffe://cluster.local/ns/default/sa/serviceaccount2",
	}

	if len(accounts) != len(expected) {
		t.Fatal("Incorrect account result returned")
	}

	for i := 0; i < len(accounts); i++ {
		if accounts[i] != expected[i] {
			t.Fatal("Returned account result does not match expected one")
		}
	}
}

func TestManagementPorts(t *testing.T) {
	aggregateCtl := buildMockController()
	expected := model.PortList{{
		Name:     "http",
		Port:     3333,
		Protocol: model.ProtocolHTTP,
	}, {
		Name:     "custom",
		Port:     9999,
		Protocol: model.ProtocolTCP,
	}}

	// Get management ports from mockAdapter1
	ports := aggregateCtl.ManagementPorts(mock.HelloInstanceV0)
	if len(ports) != 2 {
		t.Fatal("Returned wrong number of ports from controller")
	}
	for i := 0; i < len(ports); i++ {
		if ports[i].Name != expected[i].Name || ports[i].Port != expected[i].Port ||
			ports[i].Protocol != expected[i].Protocol {
			t.Fatal("Returned management ports result does not match expected one")
		}
	}

	// Get management ports from mockAdapter2
	ports = aggregateCtl.ManagementPorts(mock.MakeIP(mock.WorldService, 0))
	if len(ports) != len(expected) {
		t.Fatal("Returned wrong number of ports from controller")
	}
	for i := 0; i < len(ports); i++ {
		if ports[i].Name != expected[i].Name || ports[i].Port != expected[i].Port ||
			ports[i].Protocol != expected[i].Protocol {
			t.Fatal("Returned management ports result does not match expected one")
		}
	}
}
