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

package v2

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	testService1 = "test-service-1"
	testService2 = "test-service-2"
)

// Enums used to tweak attributes while building an endpoint for testing.
// Enums can be combined to change multiple attributes at once.
type attrToChange int

const (
	_                         = iota
	noAttrChange attrToChange = 1 << iota
	diffName
	diffService
	diffNamespace
	diffDomains
	diffAddr
	diffPort
	diffProtocol
	diffLabels
	diffUser
	diffServiceOrder
	diffDomainOrder
)

var (
	allAttrToChange = []attrToChange{
		noAttrChange,
		diffName,
		diffService,
		diffNamespace,
		diffDomains,
		diffAddr,
		diffPort,
		diffProtocol,
		diffLabels,
		diffUser,
		diffServiceOrder,
		diffDomainOrder,
	}

	// Test domain sets to associate with a service.
	testDomainSets = [][]string{
		{"default.domain-1.com", "my-namespace.my-own-domain-1.com", "my-other-ns.domain-1.com"},
		{"domain-2.com", "my-own-domain-2.com"},
		{"domain-3.com", "my-own-domain-3.com", "my-other-domain-3.com"},
	}

	// Applies only to services using testDomainSets[0]
	testNamespaces = []string{"default", "my-namespace", "my-other-ns"}

	testPorts = []uint32{80, 443, 8080}

	testProtocols = []string{"http", "https", "grpc", "redis", "mongo"}
)

func TestMesh_NewMesh(t *testing.T) {
	tm := NewMesh()
	if tm == nil {
		t.Error("expecting valid Mesh, found nil")
	}
}

func TestMesh_Reconcile(t *testing.T) {
	// Build Test data
	countEps, countSvcs, countSubsetsPerSvc := 32, 2, 2
	_, _, expectedEps, _ := buildTestEndpoints(t, countEps, countSvcs, countSubsetsPerSvc)
	subsetsToVerify := []string{
		"test-service-1.default.domain-1.com",
		"test-service-2.domain-2.com",
	}

	tm := NewMesh()
	err := tm.Reconcile(expectedEps)
	if err != nil {
		t.Errorf("unable to reconcile(): %v", err)
		return
	}
	t.Run("EndpointsAdded", func(t *testing.T) {
		actual := tm.SubsetEndpoints(subsetsToVerify)
		assertEqualEndpointLists(t, expectedEps, actual)
	})
	err = tm.Reconcile(expectedEps[:countEps/2])
	if err != nil {
		t.Errorf("unable to reconcile(): %v", err)
		return
	}
	t.Run("EndpointsDeleted", func(t *testing.T) {
		actual := tm.SubsetEndpoints(subsetsToVerify)
		assertEqualEndpointLists(t, expectedEps[:countEps/2], actual)
		actual = tm.SubsetEndpoints(subsetsToVerify[1:])
		assertEqualEndpointLists(t, []*Endpoint{}, actual)
	})
	expectedDomains := expectedEps[2].getMultiValuedAttrs(DestinationDomain.AttrName())
	expectedDomains = expectedDomains[1:]
	expectedEps[2].setMultiValuedAttrs(DestinationDomain.AttrName(), expectedDomains)
	t.Run("EndpointsUpdated", func(t *testing.T) {
		err := tm.Reconcile(expectedEps[0 : countEps/2])
		if err != nil {
			t.Errorf("unable to reconcile(): %v", err)
			return
		}
		actual := tm.SubsetEndpoints(subsetsToVerify[:1])
		assertEqualEndpointLists(t, expectedEps[0:countEps/2], actual)
		for _, ep := range actual {
			if ep.getSingleValuedAttrs()[DestinationUID.AttrName()] == expectedEps[2].getSingleValuedAttrs()[DestinationUID.AttrName()] {
				actualDomains := ep.getMultiValuedAttrs(DestinationDomain.AttrName())
				sort.Strings(expectedDomains)
				sort.Strings(actualDomains)
				if !reflect.DeepEqual(expectedDomains, actualDomains) {
					t.Errorf("expected domains %q does not match actual %q", expectedDomains, actualDomains)
				}
			}
		}
	})
	t.Run("NoChangeForIdenticalUpdates", func(t *testing.T) {
		_, _, epsFirstUpdate, _ := buildTestEndpoints(t, countEps, countSvcs, countSubsetsPerSvc)
		err := tm.Reconcile(epsFirstUpdate)
		if err != nil {
			t.Errorf("unable to reconcile(): %v", err)
			return
		}
		prevEps := tm.SubsetEndpoints(subsetsToVerify)
		expectedEps := make(map[string]*Endpoint, len(prevEps))
		for _, ep := range tm.allEndpoints {
			expectedEps[ep.getSingleValuedAttrs()[DestinationUID.AttrName()]] = ep
		}
		_, _, epsIdenticalUpdate, _ := buildTestEndpoints(t, countEps, countSvcs, countSubsetsPerSvc)
		err = tm.Reconcile(epsIdenticalUpdate)
		if err != nil {
			t.Errorf("unable to reconcile(): %v", err)
			return
		}
		actualEps := tm.SubsetEndpoints(subsetsToVerify)
		if len(expectedEps) != len(actualEps) {
			t.Error("mismatched counts for identical datasets for Reconcile()")
		}
		// Using pointer equality to detect unintended updates.
		for _, ep := range actualEps {
			epUID := ep.getSingleValuedAttrs()[DestinationUID.AttrName()]
			expectedEp := expectedEps[epUID]
			if ep != expectedEp {
				t.Error("unexpected underlying updates of identical datasets for Reconcile()")
			}
			delete(expectedEps, epUID)
		}
		if len(expectedEps) > 0 {
			t.Errorf("expected %v, found none", expectedEps)
		}
	})
}

func TestMesh_SubsetEndpoints(t *testing.T) {
	tm := NewMesh()
	expectedEps := []*Endpoint{}
	t.Run("EmptyMesh", func(t *testing.T) {
		actual := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
		assertEqualEndpointLists(t, expectedEps, actual)
	})

	countEps, countSvcs, countSubsetsPerSvc := 32, 2, 2
	rules, subsets, expectedEps, _ := buildTestEndpoints(t, countEps, countSvcs, countSubsetsPerSvc)
	subsetsToVerify := []string{}
	for i := 0; i < countSvcs; i++ {
		servicePrefix := "test-service-" + strconv.Itoa(i+1)
		for _, domain := range testDomainSets[i] {
			subsetsToVerify = append(subsetsToVerify, servicePrefix+"."+domain)
		}
	}
	err := tm.Reconcile(expectedEps)
	if err != nil {
		t.Fatalf("test setup precondition failued: %v", err)
	}
	t.Run("EndpointsAdded", func(t *testing.T) {
		t.Run("SingleSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
			assertEqualEndpointLists(t, expectedEps[0:countEps/countSvcs], actualEps)
		})
		t.Run("AllSubsets", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints(subsetsToVerify)
			assertEqualEndpointLists(t, expectedEps, actualEps)
		})
		t.Run("NonExistentSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-non-existent-service.default.domain-1.com"})
			assertEqualEndpointLists(t, []*Endpoint{}, actualEps)
		})
	})

	t.Run("SubsetsAdded", func(t *testing.T) {
		err := tm.UpdateRules([]RuleChange{{
			Rule: rules[0],
			Type: ConfigAdd,
		}})
		if err != nil {
			t.Fatalf("test setup precondition failued: %v", err)
		}
		t.Run("LabeledSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{
				"test-service-1.default.domain-1.com|" + subsets[0].Name})
			assertEqualEndpointLists(t, expectedEps[0:countEps/(countSvcs*countSubsetsPerSvc)], actualEps)
		})
		t.Run("SingleSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
			assertEqualEndpointLists(t, expectedEps[0:countEps/countSvcs], actualEps)
		})
		t.Run("AllSubsets", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints(subsetsToVerify)
			assertEqualEndpointLists(t, expectedEps, actualEps)
		})
		t.Run("NonExistentSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-non-existent-service.default.domain-1.com"})
			assertEqualEndpointLists(t, []*Endpoint{}, actualEps)
		})
	})

	t.Run("SubsetsDeleted", func(t *testing.T) {
		err := tm.UpdateRules([]RuleChange{{
			Rule: rules[0],
			Type: ConfigDelete,
		}})
		if err != nil {
			t.Fatalf("test setup precondition failued: %v", err)
		}
		t.Run("LabeledSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{
				"test-service-1.default.domain-1.com|" + subsets[0].Name})
			assertEqualEndpointLists(t, []*Endpoint{}, actualEps)
		})
		t.Run("SingleSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
			assertEqualEndpointLists(t, expectedEps[0:countEps/countSvcs], actualEps)
		})
		t.Run("AllSubsets", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints(subsetsToVerify)
			assertEqualEndpointLists(t, expectedEps, actualEps)
		})
		t.Run("NonExistentSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-non-existent-service.default.domain-1.com"})
			assertEqualEndpointLists(t, []*Endpoint{}, actualEps)
		})
	})
}

func TestMesh_SubsetNames(t *testing.T) {
	tm := NewMesh()
	expectedEps := []*Endpoint{}
	t.Run("EmptyMesh", func(t *testing.T) {
		actual := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
		assertEqualEndpointLists(t, expectedEps, actual)
	})

	countEps, countSvcs, countSubsetsPerSvc := 32, 2, 2
	rules, _, expectedEps, _ := buildTestEndpoints(t, countEps, countSvcs, countSubsetsPerSvc)
	subsetsToVerify := []string{}
	for i := 0; i < countSvcs; i++ {
		servicePrefix := "test-service-" + strconv.Itoa(i+1)
		for _, domain := range testDomainSets[i] {
			subsetsToVerify = append(subsetsToVerify, servicePrefix+"."+domain)
		}
	}
	err := tm.Reconcile(expectedEps)
	if err != nil {
		t.Fatalf("test setup precondition failued: %v", err)
	}
	t.Run("EndpointsAdded", func(t *testing.T) {
		t.Run("ServiceNameSubsets", func(t *testing.T) {
			t.Parallel()
			actualSubsets := tm.SubsetNames()
			assertEqualsSubsetNames(t, subsetsToVerify, actualSubsets)
		})
	})

	err = tm.Reconcile(expectedEps[countEps/2:])
	if err != nil {
		t.Fatalf("test setup precondition failed: %v", err)
	}
	t.Run("EndpointsDeleted", func(t *testing.T) {
		t.Run("ServiceNameSubsets", func(t *testing.T) {
			t.Parallel()
			actualSubsets := tm.SubsetNames()
			assertEqualsSubsetNames(t, subsetsToVerify[3:], actualSubsets)
		})
	})

	ruleSubsets := make([]string, 0, countSvcs*countSubsetsPerSvc)
	for _, rule := range rules {
		for _, subset := range rule.Subsets {
			ruleSubsets = append(ruleSubsets, rule.Name+RuleSubsetSeparator+subset.Name)
		}
	}
	ruleChanges := []RuleChange{{
		Rule: rules[0],
		Type: ConfigAdd,
	}, {
		Rule: rules[1],
		Type: ConfigAdd,
	}}
	err = tm.Reconcile(expectedEps)
	if err != nil {
		t.Fatalf("test setup precondition failed: %v", err)
	}
	err = tm.UpdateRules(ruleChanges)
	if err != nil {
		t.Fatalf("test setup precondition failed: %v", err)
	}
	t.Run("SubsetsAdded", func(t *testing.T) {
		actualSubsets := tm.SubsetNames()
		assertEqualsSubsetNames(t, append(subsetsToVerify, ruleSubsets...), actualSubsets)
	})

	t.Run("SubsetsDeleted", func(t *testing.T) {
		err := tm.UpdateRules([]RuleChange{{
			Rule: rules[0],
			Type: ConfigDelete,
		}})
		if err != nil {
			t.Fatalf("test setup precondition failed: %v", err)
		}
		actualSubsets := tm.SubsetNames()
		assertEqualsSubsetNames(t, append(subsetsToVerify, ruleSubsets[countSubsetsPerSvc:]...), actualSubsets)
	})
}

// TestMeshEndpointDeepEquals ensures that reflect.DeepEquals() works correctly for Endpoints created via NewEndpoint().
// DeepEquals() is essential for Mesh.Reconcile() to detect updates to existing Endpoints in Mesh.
func TestEndpoint_DeepEquals(t *testing.T) {
	// epEqualsTestCase encapsulates test data for subtests involving reflect.DeepEquals() for Endpoint.
	type epEqualsTestCase struct {
		// Name to use for the sub test
		tcName string
		// The set of attributes to change.
		attrToChange attrToChange
		// Expected result from reflect.DeepEquals()
		expectation bool
	}

	// Build parameterized test cases for subtests.
	testCases := make([]epEqualsTestCase, len(allAttrToChange))
	// Test cases involving a single attribute change.
	// Example, only the label for DestinationService differs between the two Endpoints.
	for idx, attrToChange := range allAttrToChange {
		testCases[idx] = epEqualsTestCase{
			attrToChange.String(),
			attrToChange,
			// Only the following values of attrToChange should return true for DeepEquals().
			attrToChange == noAttrChange || attrToChange == diffDomainOrder || attrToChange == diffServiceOrder,
		}
	}
	// Test cases involving multiple attribute changes.
	otherTestCases := []epEqualsTestCase{{
		"MultipleAttributes",
		diffNamespace | diffDomains,
		false,
	}}
	testCases = append(testCases, otherTestCases...)

	for _, tc := range testCases {
		t.Run(tc.tcName, func(t *testing.T) {
			t.Parallel()
			firstEp, _ := buildEndpoint(t, noAttrChange, true)
			secondEp, _ := buildEndpoint(t, tc.attrToChange, true)
			actual := reflect.DeepEqual(*firstEp, *secondEp)
			if tc.expectation != actual {
				t.Errorf("Expecting %v, found %v.\nFirst: [%v]\nSecond [%v]",
					tc.expectation, actual, *firstEp, *secondEp)
			}
		})
	}
}

// assertEqualsSubsetNames asserts that the actual and expected subset names
// match up. The arrays do not need to be in order.
func assertEqualsSubsetNames(t *testing.T, expected, actual []string) {
	expectedSubsets := make(map[string]bool, len(expected))
	for _, subset := range expected {
		expectedSubsets[subset] = true
	}
	actualSubsets := make(map[string]bool, len(actual))
	for _, subset := range actual {
		actualSubsets[subset] = true
	}
	for subset := range expectedSubsets {
		_, found := actualSubsets[subset]
		if !found {
			t.Errorf("expecting subset '%s', none found", subset)
		}
		delete(actualSubsets, subset)
	}
	for subset := range actualSubsets {
		t.Errorf("unexpected subset '%s' found", subset)
	}
}

// assertEqualEndpoints is a utility function used for tests that need to assert
// that the expected and actual service endpoint match, ignoring order of endpoints
// in either array.
func assertEqualEndpointLists(t *testing.T, expected, actual []*Endpoint) {
	expectedSet := map[string]*Endpoint{}
	for _, ep := range expected {
		uid, found := ep.getSingleValuedAttrs()[DestinationUID.AttrName()]
		if !found {
			t.Fatalf("expected ep found with no UID is an indication of bad test data: '%v'", ep)
		}
		expectedSet[uid] = ep
	}
	actualSet := map[string]*Endpoint{}
	for _, ep := range actual {
		uid, found := ep.getSingleValuedAttrs()[DestinationUID.AttrName()]
		if !found {
			t.Errorf("actual ep found with no UID '%s'", epDebugInfo(ep))
			continue
		}
		actualSet[uid] = ep
	}
	for uid, expectedEp := range expectedSet {
		actualEp, found := actualSet[uid]
		if !found {
			t.Errorf("expecting endpoint\nShortForm: %s\nLongForm  : %s\nfound none", epDebugInfo(expectedEp), *expectedEp)
			continue
		}
		assertEqualEndpoints(t, expectedEp, actualEp)
		delete(actualSet, uid)
	}
	for _, ep := range actualSet {
		t.Errorf("unexpected endpoint found: %s", epDebugInfo(ep))
	}
	if len(expected) != len(actual) {
		t.Errorf("expected endpoint count: %d do not tally with actual count: %d", len(expected), len(actual))
	}
}

// epDebugInfo prints out a limited set of endpoint attributes for the supplied endpoint.
func epDebugInfo(ep *Endpoint) string {
	var epDebugInfo string
	svcName, found := ep.getSingleValuedAttrs()[DestinationName.AttrName()]
	if found {
		epDebugInfo = svcName
	}
	if ep.Endpoint.GetAddress() != nil && ep.Endpoint.GetAddress().GetSocketAddress() != nil {
		addr := ep.Endpoint.GetAddress().GetSocketAddress()
		if epDebugInfo != "" {
			epDebugInfo += "|"
		}
		epDebugInfo += addr.Address
		if epDebugInfo != "" {
			epDebugInfo += "|"
		}
		if addr.GetPortValue() != 0 {
			epDebugInfo += strconv.Itoa((int)(addr.GetPortValue()))
		}
	}
	return epDebugInfo
}

// assertEqualEndpoint is a utility function used for tests that need to assert
// that the expected and actual service endpoints match.
func assertEqualEndpoints(t *testing.T, expected, actual *Endpoint) {
	if !reflect.DeepEqual(*expected, *actual) {
		t.Errorf("Expected endpoint: %v, Actual %v", expected, actual)
	}
}

// String outputs a string representation of attr.
func (attr attrToChange) String() string {
	switch attr {
	case noAttrChange:
		return "NoAttrChange"
	case diffName:
		return "DiffName"
	case diffService:
		return "DiffService"
	case diffNamespace:
		return "DiffNamespace"
	case diffDomains:
		return "DiffDomains"
	case diffAddr:
		return "DiffAddr"
	case diffPort:
		return "DiffPort"
	case diffProtocol:
		return "DiffProtocol"
	case diffLabels:
		return "DiffLabels"
	case diffUser:
		return "DiffUser"
	case diffServiceOrder:
		return "DiffServiceOrder"
	case diffDomainOrder:
		return "DiffDomainOrder"

	}
	return (string)(attr)
}

// buildEndpoint builds a single endpoint. The endpoint's values are always the same, except for
// the specified attribute if delta is not noAttrChange. If assertError is set to true, this
// method fails the test if an error is encountered for NewEndpoint(). Otherwise the error
// is returned.
func buildEndpoint(t testing.TB, delta attrToChange, assertError bool) (*Endpoint, error) {
	// Setup defaults
	namespace := "default"
	service := testService1
	if delta&diffService == diffService {
		service = testService2
	}
	var uidPart, addr string
	if service == testService1 {
		uidPart = "0A0A"
		addr = "10.4.1.1"
	} else {
		uidPart = "B0B0"
		addr = "10.4.4.1"
	}
	var port uint32 = 80
	aliases := []string{
		service + ".domain1.com",
		service + ".otherdomain.com",
		service + ".domain2.com",
		"other-host.domain1.com",
	}
	protocol := "http"
	labels := map[string]string{
		"app":        service + "-app",
		"ver":        "1.0",
		"experiment": "1A20",
	}
	user := "some-user" + service
	// Perform required deltas
	if delta&diffAddr == diffAddr {
		uidPart += "11"
		addr += "0"
	}
	if delta&diffNamespace == diffNamespace {
		namespace = "my-namespace"
	}
	if delta&diffPort == diffPort {
		uidPart += "22"
		port += 8000
	}
	if delta&diffProtocol == diffProtocol {
		protocol = "grpc"
	}
	if delta&diffDomains == diffDomains {
		aliases = append(aliases, service+".a-very-different-domain.com")
	}
	if delta&diffLabels == diffLabels {
		labels["experiment"] = "8H22"
	}
	if delta&diffUser == diffUser {
		user = user + "-other"
	}

	fqService := service + ".somedomain.com"
	epLabels := []EndpointLabel{}
	for n, v := range labels {
		epLabels = append(epLabels, EndpointLabel{Name: n, Value: v})
	}
	svcName := service
	if delta&diffName == diffName {
		svcName += "-svc"
	}

	epLabels = append(epLabels, EndpointLabel{Name: DestinationName.AttrName(), Value: svcName})
	epLabels = append(epLabels, EndpointLabel{Name: DestinationService.AttrName(), Value: fqService})

	aliasesToUse := make([]string, len(aliases))
	copy(aliasesToUse, aliases)
	if delta&diffServiceOrder == diffServiceOrder {
		sort.Sort(sort.Reverse(sort.StringSlice(aliasesToUse)))
	}
	for _, alias := range aliases {
		epLabels = append(epLabels, EndpointLabel{Name: DestinationService.AttrName(), Value: alias})
	}

	aliasesForDomains := make([]string, len(aliases))
	copy(aliasesForDomains, aliases)
	if delta&diffDomainOrder == diffDomainOrder {
		sort.Sort(sort.Reverse(sort.StringSlice(aliasesForDomains)))
	}
	for _, alias := range aliasesForDomains {
		subDomains := strings.Split(alias, ".")
		if len(subDomains) < 3 {
			t.Fatalf("bad test data: unable to parse domain from alias '%s'", alias)
		}
		aliasDomain := strings.Join(subDomains[1:], ".")
		epLabels = append(epLabels, EndpointLabel{Name: DestinationDomain.AttrName(), Value: aliasDomain})
	}

	epLabels = append(epLabels, EndpointLabel{Name: DestinationProtocol.AttrName(), Value: protocol})
	epLabels = append(epLabels, EndpointLabel{Name: DestinationUser.AttrName(), Value: user})
	epLabels = append(epLabels, EndpointLabel{
		Name: DestinationUID.AttrName(), Value: "some-controller://" + service + "-app" + uidPart + "." + namespace})

	ep, err := NewEndpoint(addr, port, SocketProtocolTCP, epLabels)
	if err != nil && assertError {
		t.Fatalf("bad test data: %s", err.Error())
	}
	return ep, err
}
