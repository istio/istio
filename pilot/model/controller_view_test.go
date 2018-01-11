// Copyright 2018 Istio Authors
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

package model

import (
	"fmt"
	"reflect"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

const (
	// Fake service accounts
	serviceAccount1 = "serviceAccount1"
	serviceAccount2 = "serviceAccount2"
	serviceAccount3 = "serviceAccount3"
)

var (
	// Test services and instances needed by multiple tests
	helloService    *Service
	worldService    *Service
	externalService *Service
	testInstances   []*ServiceInstance

	// Test cases for tetsting ControllerView.Instances()
	testCasesInstances []testCaseInstances

	// Test cases for testing  ControllerView.HostInstances()
	testCasesHostInstances []testCaseHostInstances

	// Test cases for testing  ControllerView.GetIstioServiceAccounts()
	testCasesGetIstioServiceAccounts []testCaseGetIstioServiceAccounts
)

// Parameterized tests for TestInstances
type testCaseInstances struct {
	// Test case name
	Name string
	// Hostname method parm
	Hostname string
	// Ports method parm
	Ports []string
	// Labels used for fetching expected results
	MatchingLabels Labels
	// This along with matching labels is passed as method parm
	// labelsCollection
	ExtraLables LabelsCollection
	// Count of expected results mostly as a guard for
	// the test function that computes expectations from this
	// test case
	ExpectedCount int
}

type testCaseHostInstances struct {
	// Test case name
	Name string
	// Addrs method parm
	Addrs []string
	// Count of expected results mostly as a guard for
	// the test function that computes expectations from this
	// test case
	ExpectedCount int
}

type testCaseGetIstioServiceAccounts struct {
	// Test case name
	Name string
	// Hostname method parm
	Hostname string
	// Ports method parm
	Ports []string
	// Count of expected results mostly as a guard for
	// the test function that computes expectations from this
	// test case
	ExpectedCount int
}

func TestServices(t *testing.T) {
	testView := NewControllerView()
	expectedServices := []*Service{}
	t.Run("EmptyMeshNotReconciled", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
	testView.Reconcile(expectedServices, []*ServiceInstance{})
	t.Run("EmptyMeshAfterEmptyReconcile", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
	expectedServices = []*Service{helloService}
	testView.Reconcile(expectedServices, []*ServiceInstance{})
	t.Run("AddHelloService", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
	expectedServices = []*Service{helloService, worldService, externalService}
	testView.Reconcile(expectedServices, []*ServiceInstance{})
	t.Run("AddTwoMoreServices", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
	expectedServices = []*Service{helloService, externalService}
	testView.Reconcile(expectedServices, []*ServiceInstance{})
	t.Run("DeleteWorldService", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
	svcToUpdate := buildHelloService()
	svcToUpdate.LoadBalancingDisabled = true
	expectedServices = []*Service{svcToUpdate, externalService}
	testView.Reconcile(expectedServices, []*ServiceInstance{})
	t.Run("UpdateHelloService", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
	expectedServices = []*Service{helloService, worldService, externalService}
	testView.Reconcile(expectedServices, []*ServiceInstance{})
	t.Run("AddWorldServiceBack", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
	expectedServices = []*Service{}
	testView.Reconcile(expectedServices, []*ServiceInstance{})
	t.Run("AfterDeleteAll", func(t *testing.T) {
		svcs, err := testView.Services()
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertServicesEquals(t, expectedServices, svcs)
	})
}

func TestGetService(t *testing.T) {
	testView := NewControllerView()
	t.Run("EmptyView", func(t *testing.T) {
		svc, err := testView.GetService(helloService.Hostname)
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("expected nil but found: '%v'", svc)
		}
	})
	svcsToReconcile := []*Service{helloService, worldService, externalService}
	testView.Reconcile(svcsToReconcile, []*ServiceInstance{})
	t.Run("AfterAddingMultiple", func(t *testing.T) {
		svc, err := testView.GetService(worldService.Hostname)
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		if svc == nil {
			t.Errorf("Expected '%v'. Found: none", svc)
		}
		assertEquals(t, worldService, svc)
	})
	t.Run("NonExistent", func(t *testing.T) {
		svc, err := testView.GetService("HelloX")
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("expected nil but found: '%v'", svc)
		}
	})
	svcsToReconcile = []*Service{worldService, externalService}
	testView.Reconcile(svcsToReconcile, []*ServiceInstance{})
	t.Run("HelloAfterDeleteHello", func(t *testing.T) {
		svc, err := testView.GetService(helloService.Hostname)
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("expected nil but found: '%v'", svc)
		}
	})
	t.Run("WorldAfterDeleteHello", func(t *testing.T) {
		svc, err := testView.GetService(worldService.Hostname)
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		if svc == nil {
			t.Errorf("Expected '%v'. Found: none", svc)
		}
		assertEquals(t, worldService, svc)
	})
	testView.Reconcile([]*Service{}, []*ServiceInstance{})
	t.Run("WorldAfterDeleteAll", func(t *testing.T) {
		svc, err := testView.GetService(worldService.Hostname)
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("expected nil but found: '%v'", svc)
		}
	})
	t.Run("ExternalAfterDeleteAll", func(t *testing.T) {
		svc, err := testView.GetService(externalService.Hostname)
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		if svc != nil {
			t.Errorf("expected nil but found: '%v'", svc)
		}
	})
}

func TestInstances(t *testing.T) {
	testView := NewControllerView()
	expectedInstances := []*ServiceInstance{}
	t.Run("EmptyView", func(t *testing.T) {
		instances, err := testView.Instances(helloService.Hostname, []string{"http"}, LabelsCollection{})
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertInstancesEquals(t, expectedInstances, instances)
	})
	t.Run("AfterReconcile", func(t *testing.T) {
		testView.Reconcile([]*Service{helloService, worldService, externalService}, testInstances)
		for _, testCase := range testCasesInstances {
			t.Run(testCase.Name, func(t *testing.T) {
				countTotalLabels := len(testCase.ExtraLables)
				matchingLabelsExist := len(testCase.MatchingLabels) > 0
				if matchingLabelsExist {
					countTotalLabels++
				}
				labelsCollection := make(LabelsCollection, countTotalLabels)
				if countTotalLabels > 0 {
					copy(labelsCollection, testCase.ExtraLables)
					if matchingLabelsExist {
						labelsCollection[countTotalLabels-1] = testCase.MatchingLabels
					}
				}
				instances, err := testView.Instances(testCase.Hostname, testCase.Ports, labelsCollection)
				if err != nil {
					t.Errorf("encountered unexpected error: %v", err)
					return
				}
				expectedInstances =
					getExpectedInstances(testCase.Hostname, testCase.Ports, testCase.MatchingLabels)
				if len(expectedInstances) != testCase.ExpectedCount {
					t.Errorf("bad test case data, test case expected count '%d' "+
						"mismatched with computed expected result count '%d'", testCase.ExpectedCount,
						len(expectedInstances))
				}
				assertInstancesEquals(t, expectedInstances, instances)
			})
		}
	})
	t.Run("AfterDelete", func(t *testing.T) {
		expectedInstances = []*ServiceInstance{}
		testView.Reconcile([]*Service{helloService, worldService, externalService}, expectedInstances)
		instances, err := testView.Instances(helloService.Hostname, []string{"http"}, LabelsCollection{})
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertInstancesEquals(t, expectedInstances, instances)
	})
}

func TestHostInstances(t *testing.T) {
	testView := NewControllerView()
	expectedInstances := []*ServiceInstance{}
	t.Run("EmptyView", func(t *testing.T) {
		instances, err := testView.HostInstances(map[string]bool{"10.4.1.1": true})
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertInstancesEquals(t, expectedInstances, instances)
	})
	t.Run("AfterReconcile", func(t *testing.T) {
		testView.Reconcile([]*Service{helloService, worldService, externalService}, testInstances)
		for _, testCase := range testCasesHostInstances {
			t.Run(testCase.Name, func(t *testing.T) {
				addrs := make(map[string]bool, len(testCase.Addrs))
				for _, addr := range testCase.Addrs {
					addrs[addr] = true
				}
				instances, err := testView.HostInstances(addrs)
				if err != nil {
					t.Errorf("encountered unexpected error: %v", err)
					return
				}
				expectedInstances = getExpectedHostInstances(testCase.Addrs)
				if len(expectedInstances) != testCase.ExpectedCount {
					t.Errorf("bad test case data, test case expecte dcount '%d' "+
						"mismatched with computed expected result count '%d'", testCase.ExpectedCount,
						len(expectedInstances))
				}
				assertInstancesEquals(t, expectedInstances, instances)
			})
		}
	})
	t.Run("AfterDelete", func(t *testing.T) {
		expectedInstances = []*ServiceInstance{}
		testView.Reconcile([]*Service{helloService, worldService, externalService}, expectedInstances)
		instances, err := testView.HostInstances(map[string]bool{"10.4.1.2": true})
		if err != nil {
			t.Errorf("encountered unexpected error: %v", err)
			return
		}
		assertInstancesEquals(t, expectedInstances, instances)
	})
}

func TestGetIstioServiceAccounts(t *testing.T) {
	testView := NewControllerView()
	expectedAccts := []string{}
	t.Run("EmptyView", func(t *testing.T) {
		actualAccts := testView.GetIstioServiceAccounts(helloService.Hostname, []string{"http"})
		assertAccountsEquals(t, expectedAccts, actualAccts)
	})
	t.Run("AfterReconcile", func(t *testing.T) {
		testView.Reconcile([]*Service{helloService, worldService, externalService}, testInstances)
		for _, testCase := range testCasesGetIstioServiceAccounts {
			t.Run(testCase.Name, func(t *testing.T) {
				actualAccts := testView.GetIstioServiceAccounts(testCase.Hostname, testCase.Ports)
				expectedAccts = getExpectedAccounts(testCase.Hostname, testCase.Ports)
				if len(expectedAccts) != testCase.ExpectedCount {
					t.Errorf("bad test case data, test case expecte dcount '%d' "+
						"mismatched with computed expected result count '%d'", testCase.ExpectedCount,
						len(expectedAccts))
				}
				assertAccountsEquals(t, expectedAccts, actualAccts)
			})
		}
	})
	t.Run("AfterDelete", func(t *testing.T) {
		expectedAccts = []string{}
		testView.Reconcile([]*Service{helloService, worldService, externalService}, []*ServiceInstance{})
		actualAccts := testView.GetIstioServiceAccounts(helloService.Hostname, []string{"http"})
		assertAccountsEquals(t, expectedAccts, actualAccts)
	})
}

// assertServicesEquals is a utility function used for tests that need to assert
// that the expected and actual services match, ignoring order
func assertServicesEquals(t *testing.T, expected, actual []*Service) {
	expectedSet := map[string]*Service{}
	for _, svc := range expected {
		expectedSet[svc.Hostname] = svc
	}
	actualSet := map[string]*Service{}
	for _, svc := range actual {
		actualSet[svc.Hostname] = svc
	}
	hasErrors := false
	for svcName, expectedSvc := range expectedSet {
		actualSvc, found := actualSet[svcName]
		if !found {
			hasErrors = true
			t.Errorf("expecting service %v, found none", *expectedSvc)
			continue
		}
		hasErrors = !assertEquals(t, expectedSvc, actualSvc)
		if hasErrors {
			continue
		}
		delete(actualSet, svcName)
	}
	if len(actualSet) > 0 {
		for _, svc := range actualSet {
			hasErrors = true
			t.Errorf("Unexpected service found: %v", *svc)
		}
	}
	if hasErrors {
		t.Errorf("mismatched expectations, expected services: %s, actual %s", svcListDebugInfo(expected), svcListDebugInfo(actual))
	}
}

// assertEquals is a utility function used for tests that need to assert
// the expected and actual services match
func assertEquals(t *testing.T, expected, actual interface{}) bool {
	check := reflect.DeepEqual(expected, actual)
	if !check {
		t.Errorf("non matching services expected service %v, actual service %v", expected, actual)
	}
	return check
}

// svcListDebugInfo outputs the list of services for debugging test failures
func svcListDebugInfo(svcs []*Service) string {
	out := "["
	for idx, svc := range svcs {
		if idx > 1 {
			out = out + ", "
		}
		out = out + fmt.Sprintf("%v", *svc)
	}
	out = out + "]"
	return out
}

// assertInstancesEquals is a utility function used for tests that need to assert
// that the expected and actual service instances match, ignoring order
func assertInstancesEquals(t *testing.T, expected, actual []*ServiceInstance) {
	expectedSet := map[string]*ServiceInstance{}
	for _, inst := range expected {
		instKey := inst.Service.Hostname + "|" + inst.Endpoint.Address + "|" +
			fmt.Sprintf("%d", inst.Endpoint.Port)
		expectedSet[instKey] = inst
	}
	actualSet := map[string]*ServiceInstance{}
	for _, inst := range actual {
		instKey := inst.Service.Hostname + "|" + inst.Endpoint.Address + "|" +
			fmt.Sprintf("%d", inst.Endpoint.Port)
		actualSet[instKey] = inst
	}
	hasErrors := false
	for instKey, expectedInst := range expectedSet {
		actualInst, found := actualSet[instKey]
		if !found {
			hasErrors = true
			t.Errorf("expecting service %v, found none", *expectedInst)
			continue
		}
		hasErrors = !assertEquals(t, expectedInst, actualInst)
		if hasErrors {
			continue
		}
		delete(actualSet, instKey)
	}
	if len(actualSet) > 0 {
		for _, inst := range actualSet {
			hasErrors = true
			t.Errorf("Unexpected service found: %v", *inst)
		}
	}
	if hasErrors {
		t.Errorf("mismatched expectations, expected service instances: %s, actual %s", instListDebugInfo(expected), instListDebugInfo(actual))
	}
}

// assertInstancesEquals is a utility function used for tests that need to assert
// that the expected and actual service accounts match, ignoring order
func assertAccountsEquals(t *testing.T, expected, actual []string) {
	expectedSet := map[string]bool{}
	for _, acct := range expected {
		expectedSet[acct] = true
	}
	actualSet := map[string]bool{}
	for _, acct := range actual {
		actualSet[acct] = true
	}
	hasErrors := false
	for acct := range expectedSet {
		_, found := actualSet[acct]
		if !found {
			hasErrors = true
			t.Errorf("expecting account %s, found none", acct)
			continue
		}
		delete(actualSet, acct)
	}
	if len(actualSet) > 0 {
		for acct := range actualSet {
			hasErrors = true
			t.Errorf("Unexpected accounts found: %s", acct)
		}
	}
	if hasErrors {
		t.Errorf("mismatched expectations, expected accounts: %v, actual %v", expected, actual)
	}
}

// instListDebugInfo outputs the list of service instances for debugging test failures
func instListDebugInfo(instances []*ServiceInstance) string {
	out := "["
	for idx, inst := range instances {
		if idx > 1 {
			out = out + ", "
		}
		out = out + fmt.Sprintf("%v", *inst)
	}
	out = out + "]"
	return out
}

// getExpectedInstances manually computes the desired list of Instances that match the
// the combination of hostname, one or more ports and all the supplied labels
func getExpectedInstances(hostname string, ports []string, labels Labels) []*ServiceInstance {
	out := []*ServiceInstance{}
	for _, inst := range testInstances {
		if inst.Service.Hostname != hostname {
			continue
		}
		portFound := false
		for _, namedPort := range ports {
			if inst.Endpoint.ServicePort.Name == namedPort {
				portFound = true
				break
			}
		}
		if !portFound {
			continue
		}
		if !labels.SubsetOf(inst.Labels) {
			continue
		}
		out = append(out, inst)
	}
	return out
}

// getExpectedHostInstances manually computes the desired list of Instances that
// match one or more addresses
func getExpectedHostInstances(addrs []string) []*ServiceInstance {
	out := []*ServiceInstance{}
	if len(addrs) == 0 {
		return out
	}
	for _, inst := range testInstances {
		for _, addr := range addrs {
			if inst.Endpoint.Address != addr {
				continue
			}
			out = append(out, inst)
			break
		}
	}
	return out
}

// getExpectedInstances manually computes the desired list of Instances that match the
// the combination of hostname, one or more ports
func getExpectedAccounts(hostname string, ports []string) []string {
	accts := map[string]bool{}
	for _, inst := range testInstances {
		if inst.Service.Hostname != hostname {
			continue
		}
		portFound := false
		for _, namedPort := range ports {
			if inst.Endpoint.ServicePort.Name == namedPort {
				portFound = true
				break
			}
		}
		if !portFound {
			continue
		}
		accts[inst.ServiceAccount] = true
		for _, acct := range inst.Service.ServiceAccounts {
			accts[acct] = true
		}
	}
	out := []string{}
	for acct := range accts {
		out = append(out, acct)
	}
	return out
}

// Initialize vars used by multiple tests
func init() {
	helloService = buildHelloService()
	worldService = buildWorldService()
	externalService = buildExternalService()
	addToTestInstances(helloService)
	addToTestInstances(worldService)

	testCasesInstances = []testCaseInstances{{
		Name:     "EmptyPortList",
		Hostname: helloService.Hostname,
	}, {
		Name:          "HelloHttp",
		Hostname:      helloService.Hostname,
		Ports:         []string{"http"},
		ExpectedCount: 1,
	}, {
		Name:          "HelloHttpBothPorts",
		Hostname:      helloService.Hostname,
		Ports:         []string{"http", "https"},
		ExpectedCount: 2,
	}, {
		Name:     "WorldWithLabels",
		Hostname: worldService.Hostname,
		Ports:    []string{"http"},
		MatchingLabels: Labels{
			"version": "v0",
			"app":     worldService.Hostname + "-app",
		},
		ExpectedCount: 1,
	}, {
		Name:     "WorldWithNonExistentLabels",
		Hostname: worldService.Hostname,
		Ports:    []string{"http"},
		MatchingLabels: Labels{
			"version": "non-existent",
			"app":     worldService.Hostname + "-app",
		},
	}, {
		Name:     "WorldWithOverlappingLabelSets",
		Hostname: worldService.Hostname,
		Ports:    []string{"http"},
		MatchingLabels: Labels{
			"version": "v0",
			"app":     worldService.Hostname + "-app",
		},
		ExtraLables: LabelsCollection{
			Labels{
				"version": "v0",
				"app":     worldService.Hostname + "-app",
			}},
		ExpectedCount: 1,
	}, {
		Name:     "WorldWithDisjointLabelSets",
		Hostname: worldService.Hostname,
		Ports:    []string{"http"},
		MatchingLabels: Labels{
			"app":    worldService.Hostname + "-app",
			"minver": "50",
		},
		ExtraLables: LabelsCollection{
			Labels{
				"app":     worldService.Hostname + "-app",
				"version": "v0",
			}},
		ExpectedCount: 1,
	},
	}

	testCasesHostInstances = []testCaseHostInstances{{
		Name: "EmptySet",
	}, {
		Name:  "NonExistent",
		Addrs: []string{"10.8.4.1"},
	}, {
		Name:          "Single",
		Addrs:         []string{"10.4.1.2"},
		ExpectedCount: 1,
	}, {
		Name:          "Multiple",
		Addrs:         []string{"10.4.1.2", "10.4.1.3"},
		ExpectedCount: 2,
	},
	}

	testCasesGetIstioServiceAccounts = []testCaseGetIstioServiceAccounts{{
		Name:     "EmptyPortList",
		Hostname: helloService.Hostname,
	}, {
		Name:          "HelloHttp",
		Hostname:      helloService.Hostname,
		Ports:         []string{"http"},
		ExpectedCount: 2,
	}, {
		Name:          "HelloHttpBothPorts",
		Hostname:      helloService.Hostname,
		Ports:         []string{"http", "https"},
		ExpectedCount: 2,
	}}
}

// Example of load-balanced service
func buildHelloService() *Service {
	return &Service{
		Hostname:        "Hello",
		Address:         "10.0.0.2",
		Ports:           PortList{buildHTTPPort(), buildHTTPSPort()},
		ServiceAccounts: []string{serviceAccount1, serviceAccount2},
	}
}

// Example of non-load-balanced service
func buildWorldService() *Service {
	return &Service{
		Hostname:              "World",
		Ports:                 PortList{buildHTTPPort(), buildHTTPSPort()},
		ServiceAccounts:       []string{serviceAccount2},
		LoadBalancingDisabled: true,
	}
}

// Example of external service
func buildExternalService() *Service {
	return &Service{
		Hostname:              "SomeExternal",
		Address:               "",
		ExternalName:          "some-external-name.some-domain.com",
		Ports:                 PortList{buildHTTPPort(), buildHTTPSPort()},
		ServiceAccounts:       []string{serviceAccount3},
		LoadBalancingDisabled: true,
	}
}

// Build fake instances given the service
func addToTestInstances(s *Service) {
	instToAppend := make([]*ServiceInstance, len(s.Ports))
	lastOctet := len(testInstances) + 1
	serviceAcct := ""
	if len(s.ServiceAccounts) > 1 {
		serviceAcct = s.ServiceAccounts[1]
	}
	for idx, port := range s.Ports {
		version := fmt.Sprintf("v%d", (idx % 2))
		minVersion := fmt.Sprintf("%d", (idx%2)+50)
		instToAppend[idx] = &ServiceInstance{
			Endpoint: NetworkEndpoint{
				Address:     fmt.Sprintf("10.4.1.%d", (lastOctet + idx)),
				Port:        port.Port + 8000,
				ServicePort: port,
			},
			Service: s,
			Labels: map[string]string{
				"version": version,
				"minver":  minVersion,
				"app":     s.Hostname + "-app",
			},
			AvailabilityZone: "some-availability-zone",
			ServiceAccount:   serviceAcct,
		}
	}
	testInstances = append(testInstances, instToAppend...)
}

func buildHTTPPort() *Port {
	return &Port{
		Name:                 "http",
		Port:                 80,
		Protocol:             ProtocolHTTP,
		AuthenticationPolicy: meshconfig.AuthenticationPolicy_NONE,
	}
}

func buildHTTPSPort() *Port {
	return &Port{
		Name:                 "https",
		Port:                 443,
		Protocol:             ProtocolHTTPS,
		AuthenticationPolicy: meshconfig.AuthenticationPolicy_NONE,
	}
}
