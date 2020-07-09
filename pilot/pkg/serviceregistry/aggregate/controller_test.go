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

package aggregate

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/mock"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
)

var discovery1 *mock.ServiceDiscovery
var discovery2 *mock.ServiceDiscovery

func buildMockController() *Controller {
	discovery1 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.HelloService.Hostname:   mock.HelloService,
			mock.ExtHTTPService.Hostname: mock.ExtHTTPService,
		}, 2)

	discovery2 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.WorldService.Hostname:    mock.WorldService,
			mock.ExtHTTPSService.Hostname: mock.ExtHTTPSService,
		}, 2)

	registry1 := serviceregistry.Simple{
		ProviderID:       serviceregistry.ProviderID("mockAdapter1"),
		ServiceDiscovery: discovery1,
		Controller:       &mock.Controller{},
	}

	registry2 := serviceregistry.Simple{
		ProviderID:       serviceregistry.ProviderID("mockAdapter2"),
		ServiceDiscovery: discovery2,
		Controller:       &mock.Controller{},
	}

	ctls := NewController()
	ctls.AddRegistry(registry1)
	ctls.AddRegistry(registry2)

	return ctls
}

func buildMockControllerForMultiCluster() *Controller {
	discovery1 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.HelloService.Hostname: mock.MakeService("hello.default.svc.cluster.local", "10.1.1.0"),
		}, 2)

	discovery2 = mock.NewDiscovery(
		map[host.Name]*model.Service{
			mock.HelloService.Hostname: mock.MakeService("hello.default.svc.cluster.local", "10.1.2.0"),
			mock.WorldService.Hostname: mock.WorldService,
		}, 2)

	registry1 := serviceregistry.Simple{
		ProviderID:       serviceregistry.ProviderID("mockAdapter1"),
		ClusterID:        "cluster-1",
		ServiceDiscovery: discovery1,
		Controller:       &mock.Controller{},
	}

	registry2 := serviceregistry.Simple{
		ProviderID:       serviceregistry.ProviderID("mockAdapter2"),
		ClusterID:        "cluster-2",
		ServiceDiscovery: discovery2,
		Controller:       &mock.Controller{},
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
func TestServicesForMultiCluster(t *testing.T) {
	aggregateCtl := buildMockControllerForMultiCluster()
	// List Services from aggregate controller
	services, err := aggregateCtl.Services()
	if err != nil {
		t.Fatalf("Services() encountered unexpected error: %v", err)
	}

	// Set up ground truth hostname values
	serviceMap := map[host.Name]bool{
		mock.HelloService.Hostname: false,
		mock.WorldService.Hostname: false,
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
		t.Fatalf("Service map expected size %d, actual %v", svcCount, serviceMap)
	}

	//Now verify ClusterVIPs for each service
	ClusterVIPs := map[host.Name]map[string]string{
		mock.HelloService.Hostname: {
			"cluster-1": "10.1.1.0",
			"cluster-2": "10.1.2.0",
		},
		mock.WorldService.Hostname: {
			"cluster-2": "10.2.0.0",
		},
	}
	for _, svc := range services {
		if !reflect.DeepEqual(svc.ClusterVIPs, ClusterVIPs[svc.Hostname]) {
			t.Fatalf("Service %s ClusterVIPs actual %v, expected %v", svc.Hostname, svc.ClusterVIPs, ClusterVIPs[svc.Hostname])
		}
	}
	t.Logf("Return service ClusterVIPs match ground truth")
}

func TestServices(t *testing.T) {
	aggregateCtl := buildMockController()
	// List Services from aggregate controller
	services, err := aggregateCtl.Services()

	// Set up ground truth hostname values
	serviceMap := map[host.Name]bool{
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

func TestGetProxyServiceInstances(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get Instances from mockAdapter1
	instances, err := aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.HelloInstanceV0}})
	if err != nil {
		t.Fatalf("GetProxyServiceInstances() encountered unexpected error: %v", err)
	}
	if len(instances) != 6 {
		t.Fatalf("Returned GetProxyServiceInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.HelloService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}

	// Get Instances from mockAdapter2
	instances, err = aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.MakeIP(mock.WorldService, 1)}})
	if err != nil {
		t.Fatalf("GetProxyServiceInstances() encountered unexpected error: %v", err)
	}
	if len(instances) != 6 {
		t.Fatalf("Returned GetProxyServiceInstances' amount %d is not correct", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != mock.WorldService.Hostname {
			t.Fatal("Returned Instance is incorrect")
		}
	}
}

func TestGetProxyWorkloadLabels(t *testing.T) {
	// If no registries return workload labels, we must return nil, rather than an empty list.
	// This ensures callers can distinguish between no labels, and labels not found.
	aggregateCtl := buildMockController()

	instances, err := aggregateCtl.GetProxyWorkloadLabels(&model.Proxy{IPAddresses: []string{mock.HelloInstanceV0}})
	if err != nil {
		t.Fatalf("GetProxyServiceInstances() encountered unexpected error: %v", err)
	}
	if instances != nil {
		t.Fatalf("expected nil workload labels, got: %v", instances)
	}
}

func TestGetProxyServiceInstancesError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.GetProxyServiceInstancesError = errors.New("mock GetProxyServiceInstances() error")

	// Get Instances from client with error
	instances, err := aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.HelloInstanceV0}})
	if err == nil {
		t.Fatal("Aggregate controller should return error if one discovery client experiences " +
			"error and no instances are found")
	}
	if len(instances) != 0 {
		t.Fatal("GetProxyServiceInstances() should return no instances is client experiences error")
	}

	// Get Instances from client without error
	instances, err = aggregateCtl.GetProxyServiceInstances(&model.Proxy{IPAddresses: []string{mock.MakeIP(mock.WorldService, 1)}})
	if err != nil {
		t.Fatal("Aggregate controller should not return error if instances are found")
	}
	if len(instances) != 6 {
		t.Fatalf("Returned GetProxyServiceInstances' amount %d is not correct", len(instances))
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
	instances, err := aggregateCtl.InstancesByPort(mock.HelloService,
		80,
		labels.Collection{})
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
		if _, ok := instance.Service.Ports.Get(mock.PortHTTPName); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}

	// Get Instances from mockAdapter2
	instances, err = aggregateCtl.InstancesByPort(mock.WorldService,
		80,
		labels.Collection{})
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
		if _, ok := instance.Service.Ports.Get(mock.PortHTTPName); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}
}

func TestInstancesError(t *testing.T) {
	aggregateCtl := buildMockController()

	discovery1.InstancesError = errors.New("mock Instances() error")

	// Get Instances from client with error
	instances, err := aggregateCtl.InstancesByPort(mock.HelloService,
		80,
		labels.Collection{})
	if err == nil {
		t.Fatal("Aggregate controller should return error if one discovery client experiences " +
			"error and no instances are found")
	}
	if len(instances) != 0 {
		t.Fatal("Returned wrong number of instances from controller")
	}

	// Get Instances from client without error
	instances, err = aggregateCtl.InstancesByPort(mock.WorldService,
		80,
		labels.Collection{})
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
		if _, ok := instance.Service.Ports.Get(mock.PortHTTPName); !ok {
			t.Fatal("Returned instance does not contain desired port")
		}
	}
}

func TestGetIstioServiceAccounts(t *testing.T) {
	aggregateCtl := buildMockController()

	// Get accounts from mockAdapter1
	accounts := aggregateCtl.GetIstioServiceAccounts(mock.HelloService, []int{})
	expected := make([]string, 0)

	if len(accounts) != len(expected) {
		t.Fatal("Incorrect account result returned")
	}

	for i := 0; i < len(accounts); i++ {
		if accounts[i] != expected[i] {
			t.Fatal("Returned account result does not match expected one")
		}
	}

	// Get accounts from mockAdapter2
	accounts = aggregateCtl.GetIstioServiceAccounts(mock.WorldService, []int{})
	expected = []string{
		"spiffe://cluster.local/ns/default/sa/serviceaccount1",
		"spiffe://cluster.local/ns/default/sa/serviceaccount2",
	}

	if len(accounts) != len(expected) {
		t.Fatal("Incorrect account result returned")
	}

	for i := 0; i < len(accounts); i++ {
		if accounts[i] != expected[i] {
			t.Fatal("Returned account result does not match expected one", accounts[i], expected[i])
		}
	}
}

func TestAddRegistry(t *testing.T) {

	registries := []serviceregistry.Simple{
		{
			ProviderID: "registry1",
			ClusterID:  "cluster1",
		},
		{
			ProviderID: "registry2",
			ClusterID:  "cluster2",
		},
	}
	ctrl := NewController()
	for _, r := range registries {
		ctrl.AddRegistry(r)
	}
	if l := len(ctrl.registries); l != 2 {
		t.Fatalf("Expected length of the registries slice should be 2, got %d", l)
	}
}

func TestDeleteRegistry(t *testing.T) {
	registries := []serviceregistry.Simple{
		{
			ProviderID: "registry1",
			ClusterID:  "cluster1",
		},
		{
			ProviderID: "registry2",
			ClusterID:  "cluster2",
		},
	}
	ctrl := NewController()
	for _, r := range registries {
		ctrl.AddRegistry(r)
	}
	ctrl.DeleteRegistry(registries[0].ClusterID)
	if l := len(ctrl.registries); l != 1 {
		t.Fatalf("Expected length of the registries slice should be 1, got %d", l)
	}
}

func TestGetRegistries(t *testing.T) {
	registries := []serviceregistry.Simple{
		{
			ProviderID: "registry1",
			ClusterID:  "cluster1",
		},
		{
			ProviderID: "registry2",
			ClusterID:  "cluster2",
		},
	}
	ctrl := NewController()
	for _, r := range registries {
		ctrl.AddRegistry(r)
	}
	result := ctrl.GetRegistries()
	if len(ctrl.registries) != len(result) {
		t.Fatal("Length of the original registries slice does not match to returned by GetRegistries.")
	}

	for i := range result {
		if !reflect.DeepEqual(result[i], ctrl.registries[i]) {
			t.Fatal("The original registries slice and resulting slice supposed to be identical.")
		}
	}
}

func TestSkipSearchingRegistryForProxy(t *testing.T) {
	cases := []struct {
		node     string
		registry string
		self     string
		want     bool
	}{
		{"main", "remote", "main", true},
		{"remote", "main", "main", true},
		{"remote", "Kubernetes", "main", true},

		{"main", "Kubernetes", "main", false},
		{"main", "main", "main", false},
		{"remote", "remote", "main", false},
		{"", "main", "main", false},
		{"main", "", "main", false},
		{"main", "Kubernetes", "", false},
		{"", "", "", false},
	}

	for i, c := range cases {
		got := skipSearchingRegistryForProxy(c.node, c.registry, c.self)
		if got != c.want {
			t.Errorf("%s: got %v want %v",
				fmt.Sprintf("[%v] registry=%v node=%v", i, c.registry, c.node),
				got, c.want)
		}
	}
}
