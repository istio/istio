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
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	route "istio.io/api/routing/v1alpha2"
)

const (
	testService1 = "test-service-1"
	testService2 = "test-service-2"
	testLabelApp = "app"
	testLabelVer = "version"
	testLabelEnv = "environment"
	testLabelExp = "experiment"
	testLabelShd = "shard"
	testSubset1  = "test-subset-1"
	testSubset2  = "test-subset-2"
)

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
	allTestLabels = []string{
		testLabelApp,
		testLabelVer,
		testLabelEnv,
		testLabelExp,
		testLabelShd,
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

// Enums used to tweak attributes while
// building an endpoint for testing
type attrToChange int

type samplePoint struct {
	name      string
	rule      int
	subset    int
	endpoints int
}

type dataSet struct {
	cntEps           int
	cntSvcs          int
	cntSubsetsPerSvc int
}

func TestMeshNewMesh(t *testing.T) {
	tm := NewMesh()
	if tm == nil {
		t.Error("expecting valid Mesh, found nil")
	}
}

func TestMeshXDS(t *testing.T) {
	tm := NewMesh()
	expectedEps := []*Endpoint{}
	t.Run("EmptyMesh", func(t *testing.T) {
		actual := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
		assertEqualEndpointLists(t, expectedEps, actual)
	})
	countEps, countSvcs, countSubsetsPerSvc := 32, 2, 2
	rules, subsets, expectedEps, _ := buildTestEndpoints(t, countEps, countSvcs, countSubsetsPerSvc)
	expectedSubsets := []string{}
	for i := 0; i < countSvcs; i++ {
		servicePrefix := "test-service-" + strconv.Itoa(i+1)
		for _, domain := range testDomainSets[i] {
			expectedSubsets = append(expectedSubsets, servicePrefix+"."+domain)
		}
	}
	err := tm.Reconcile(expectedEps)
	if err != nil {
		t.Errorf("unable to reconcile(): %v", err)
		return
	}
	t.Run("AfterReconcile", func(t *testing.T) {
		if err != nil {
			t.Error(err)
			return
		}
		t.Run("SubsetNames", func(t *testing.T) {
			t.Parallel()
			actualSubsets := tm.SubsetNames()
			assertEqualsSubsetNames(t, expectedSubsets, actualSubsets)
		})
		t.Run("SubsetEndpoints", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints(expectedSubsets)
			assertEqualEndpointLists(t, expectedEps, actualEps)
		})
		t.Run("FetchServiceNoLabels", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
			assertEqualEndpointLists(t, expectedEps[0:countEps/countSvcs], actualEps)
		})
		t.Run("FetchNonExistentService", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-non-existent-service.default.domain-1.com"})
			assertEqualEndpointLists(t, []*Endpoint{}, actualEps)
		})
	})
	t.Run("AfterUpdateRules", func(t *testing.T) {
		err := tm.UpdateRules([]RuleChange{{
			Rule: rules[0],
			Type: ConfigAdd,
		}})
		if err != nil {
			t.Error(err)
			return
		}
		t.Run("LabeledSubset", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{
				"test-service-1.default.domain-1.com|" + subsets[0].Name})
			assertEqualEndpointLists(t, expectedEps[0:countEps/(countSvcs*countSubsetsPerSvc)], actualEps)
		})
		t.Run("SubsetNames", func(t *testing.T) {
			t.Parallel()
			for i := 0; i < countSubsetsPerSvc; i++ {
				for _, subset := range rules[0].Subsets {
					expectedSubsets = append(expectedSubsets, rules[0].Name+"|"+subset.Name)
				}
			}
			actualSubsets := tm.SubsetNames()
			assertEqualsSubsetNames(t, expectedSubsets, actualSubsets)
		})
		t.Run("SubsetEndpoints", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints(expectedSubsets)
			assertEqualEndpointLists(t, expectedEps, actualEps)
		})
		t.Run("FetchServiceNoLabels", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-service-1.default.domain-1.com"})
			assertEqualEndpointLists(t, expectedEps[0:countEps/countSvcs], actualEps)
		})
		t.Run("FetchNonExistentService", func(t *testing.T) {
			t.Parallel()
			actualEps := tm.SubsetEndpoints([]string{"test-non-existent-service.default.domain-1.com"})
			assertEqualEndpointLists(t, []*Endpoint{}, actualEps)
		})
	})
	// Uncomment for debugging!
	//	t.Logf(
	//		"tm.reverseAttrMap:\n%v\n\ntm.reverseEpSubsets:\n%v\n\ntm.subsetEndpoints:\n%v\n\ntm.subsetDefinitions:\n%v\n\ntm.allEndpoints:\n%v\n\n",
	//		tm.reverseAttrMap, tm.reverseEpSubsets, tm.subsetEndpoints, tm.subsetDefinitions, tm.allEndpoints)
}

func BenchmarkMeshXds(b *testing.B) {
	b.Run("SubsetEndpoints", func(b *testing.B) {
		dataSets := []dataSet{{
			cntEps:           20000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 3,
		}, {
			cntEps:           50000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 3,
		}, {
			cntEps:           100000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 4,
		}, {
			cntEps:           1000000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 10,
		}}
		var testRules []*route.DestinationRule
		var testSubsets []*route.Subset
		var testEps []*Endpoint
		var samplePoints []samplePoint
		var tm *Mesh
		for _, ds := range dataSets {
			testRules, testSubsets, testEps, samplePoints =
				buildTestEndpoints(b, ds.cntEps, ds.cntSvcs, ds.cntSubsetsPerSvc)
			tm = NewMesh()
			err := tm.Reconcile(testEps)
			if err != nil {
				b.Error(err)
			}
			cntRules := len(testRules)
			ruleChanges := make([]RuleChange, cntRules)
			for ridx := 0; ridx < ds.cntSvcs; ridx++ {
				ruleChanges[ridx] = RuleChange{Rule: testRules[ridx], Type: ConfigUpdate}
			}
			err = tm.UpdateRules(ruleChanges)
			if err != nil {
				b.Error(err)
			}
			b.ResetTimer()
			for _, samplePoint := range samplePoints {
				b.Run(fmt.Sprintf("EP_%d__%s__Matched_%d",
					ds.cntEps, samplePoint.name, samplePoint.endpoints), func(b *testing.B) {
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						subset := testSubsets[samplePoint.subset]
						subsetName := testRules[samplePoint.rule].Name + "|" + subset.Name
						actualEps := tm.SubsetEndpoints([]string{subsetName})
						if len(actualEps) != samplePoint.endpoints {
							b.Errorf("actual endpoint count '%d' does not match expected '%d'",
								len(actualEps), samplePoint.endpoints)
						}
					}
				})
			}
		}
	})
}

func BenchmarkMeshUpdates(b *testing.B) {
	b.Run("Reconcile", func(b *testing.B) {
		cntEps, cntSvcs, cntSubsetsPerSvc := 50000, 1000, 2
		tm := NewMesh()
		_, _, testEps, _ := buildTestEndpoints(b, cntEps, cntSvcs, cntSubsetsPerSvc)
		benchmarks := []int{1000, 5000, 10000, 25000, cntEps}
		b.ResetTimer()
		for _, bm := range benchmarks {
			b.Run(fmt.Sprintf("EP_%d", bm), func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					err := tm.Reconcile(testEps[0:bm])
					if err != nil {
						b.Error(err)
					}
				}
			})
		}
	})
	b.Run("UpdateRules", func(b *testing.B) {
		dataSets := []dataSet{{
			cntEps:           20000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 3,
		}, {
			cntEps:           50000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 3,
		}, {
			cntEps:           100000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 4,
		}, {
			cntEps:           1000000,
			cntSvcs:          1000,
			cntSubsetsPerSvc: 10,
		}}
		var testRules []*route.DestinationRule
		var testEps []*Endpoint
		var samplePoints []samplePoint
		var tm *Mesh

		for _, ds := range dataSets {
			testRules, _, testEps, samplePoints =
				buildTestEndpoints(b, ds.cntEps, ds.cntSvcs, ds.cntSubsetsPerSvc)
			tm = NewMesh()
			err := tm.Reconcile(testEps)
			if err != nil {
				b.Error(err)
			}
			b.ResetTimer()
			for _, samplePoint := range samplePoints {
				b.Run(fmt.Sprintf("EP_%d__%s__Matched_%d",
					ds.cntEps, samplePoint.name, samplePoint.endpoints), func(b *testing.B) {
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						err = tm.UpdateRules(
							[]RuleChange{{Rule: testRules[samplePoint.rule], Type: ConfigUpdate}})
						if err != nil {
							b.Error(err)
						}
					}
				})
			}
		}
	})
}

func TestMeshEndpointDeepEquals(t *testing.T) {
	type epEqualsTestCase struct {
		tcName       string // name of the test case
		attrToChange attrToChange
		expectation  bool
	}
	testCases := make([]epEqualsTestCase, len(allAttrToChange))
	// Single attribute change test cases
	for idx, attrToChange := range allAttrToChange {
		testCases[idx] = epEqualsTestCase{
			attrToChange.String(),
			attrToChange,
			attrToChange == noAttrChange || attrToChange == diffDomainOrder || attrToChange == diffServiceOrder,
		}
	}
	// Other types of attribute changes
	otherTestCases := []epEqualsTestCase{{
		"MultipleAttributes",
		diffNamespace | diffDomains,
		false,
	}}
	testCases = append(testCases, otherTestCases...)
	for _, tc := range testCases {
		t.Run(tc.tcName, func(t *testing.T) {
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

// epListDebugInfo prints out a limited set of endpoint attributes in the supplied endpoint list.
func epListDebugInfo(epList []*Endpoint) string {
	epListDebugInfo := "["
	for _, ep := range epList {
		if ep == nil {
			continue
		}
		if len(epListDebugInfo) > 1 {
			epListDebugInfo += ", "
		}
		epListDebugInfo += epDebugInfo(ep)
	}
	epListDebugInfo += "]"
	return epListDebugInfo
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

// String outputs the type of attribute change that should be made on the Endpoint.
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

// buildTestEndpoints build a list or test destination rules, the subset definitions, the list of endpoints and a few key sample points of rules and
// subsets. The list is predictable and can be used for guaging regressions on performance tests. The inputs are the count of desired endpoints,
// the count of service involved and the count of subsets that need to be created for each service.
func buildTestEndpoints(t testing.TB, cntEps, cntSvcs, cntSubsetsPerSvc int) ([]*route.DestinationRule, []*route.Subset, []*Endpoint, []samplePoint) {
	type labelSpec struct {
		labelName   string
		countValues int
		currValue   int
	}
	maxLblValues := []int{2, 3, 5, 7, 11, 13, 17, 19, 23, 29}
	currLblValues := make([]labelSpec, len(maxLblValues))
	for idx := range currLblValues {
		labelInfo := &currLblValues[idx]
		labelInfo.labelName = "label-" + strconv.Itoa(idx+1)
		labelInfo.countValues = maxLblValues[idx]
	}

	ttlSubsets := cntSvcs * cntSubsetsPerSvc
	outRules := make([]*route.DestinationRule, cntSvcs)
	outSubsets := make([]*route.Subset, ttlSubsets)
	outEndpoints := make([]*Endpoint, cntEps)
	outSamplePoints := []samplePoint{{
		name: "Median",
	}, {
		name: "1\u03c3",
	}, {
		name: "2\u03c3",
	}, {
		name: "3\u03c3", // This is ignored for now, because sum of long tailed can skew results
	},
	}

	// Assuming fixed endpoints per subsets (only for endpoint counts < 1000)
	epsForSS := cntEps / ttlSubsets
	// For large values of endpoint counts assume normal distribution
	sig := -3.0
	sigIncr := 6.0 / (float64)(ttlSubsets)
	prevCdf := 0.0

	epIdx := 0
	ssIdx := 0
	sampleIdx := 3 // start with the furthest
	for svcIdx := 0; svcIdx < cntSvcs; svcIdx++ {
		serviceName := "test-service-" + strconv.Itoa(svcIdx+1)
		domIdx := svcIdx % len(testDomainSets)
		svcDomains := testDomainSets[domIdx]
		rule := &route.DestinationRule{
			Name:    serviceName + "." + svcDomains[0],
			Subsets: make([]*route.Subset, cntSubsetsPerSvc),
		}
		outRules[svcIdx] = rule
		var namespace string
		countLblNS := 0
		if domIdx == 0 {
			namespace = testNamespaces[svcIdx%len(testNamespaces)]
			countLblNS = 1
		}
		var firstOctet int
		switch {
		case svcIdx%2 == 0:
			firstOctet = 10
		default:
			firstOctet = 72
		}
		for svcSSIdx := 0; svcSSIdx < cntSubsetsPerSvc; svcSSIdx, ssIdx = svcSSIdx+1, ssIdx+1 {
			ssIdx := (svcSSIdx * cntSubsetsPerSvc) + svcSSIdx
			subset := &route.Subset{
				Name:   "subset-" + strconv.Itoa(svcSSIdx),
				Labels: make(map[string]string, len(currLblValues)),
			}
			rule.Subsets[svcSSIdx] = subset
			outSubsets[ssIdx] = subset
			// UID + Protocol + Name + user + possibly Namespace + FQDNs + domains + labels
			epLabels := make([]EndpointLabel, 4+countLblNS+(len(svcDomains)*2)+len(currLblValues))
			lblIdx := 2 // UID, Protocol are endpoint specific
			epLabels[lblIdx] = EndpointLabel{DestinationName.AttrName(), serviceName}
			lblIdx++
			epLabels[lblIdx] = EndpointLabel{DestinationUser.AttrName(),
				serviceName + "-user-" + strconv.Itoa(svcIdx+1)}
			lblIdx++
			if countLblNS != 0 {
				epLabels[lblIdx] = EndpointLabel{DestinationNamespace.AttrName(), namespace}
				lblIdx++
			}
			for _, domain := range svcDomains {
				epLabels[lblIdx] =
					EndpointLabel{DestinationService.AttrName(), serviceName + "." + domain}
				epLabels[lblIdx+1] = EndpointLabel{DestinationDomain.AttrName(), domain}
				lblIdx += 2
			}
			// Fix the labels for this subset
			for liIdx := range currLblValues {
				labelInfo := &currLblValues[liIdx]
				labelName := labelInfo.labelName
				labelInfo.currValue++
				labelValue := labelName + "-" + strconv.Itoa(labelInfo.currValue)
				subset.Labels[labelName] = labelValue
				epLabels[lblIdx] = EndpointLabel{labelName, labelValue}
				lblIdx++
			}
			// Override endpoints per subset for large values of cntEps
			if cntEps >= 1000 {
				sig += sigIncr
				currCdf := (1 + math.Erf(sig/math.Sqrt2)) / 2.0
				epsForSS = (int)(math.Ceil((currCdf - prevCdf) * (float64)(cntEps)))
				if sig > -(float64)(sampleIdx) {
					if sampleIdx >= 0 {
						samplePoint := &outSamplePoints[sampleIdx]
						samplePoint.rule = svcIdx
						samplePoint.subset = ssIdx
						samplePoint.endpoints = epsForSS
						// Uncomment for debugging
						// t.Logf("name: %s, sampleIdx: %d, currentSig: %f, eps %d, currCdf %f prevCdf %f", samplePoint.name, sampleIdx, sig, epsForSS, currCdf, prevCdf)
						sampleIdx--
					}
				}
				prevCdf = currCdf
			}
			for epSSIdx := 0; epIdx < cntEps && epSSIdx < epsForSS; epIdx, epSSIdx = epIdx+1, epSSIdx+1 {
				// Build address of the form 10|72.1.1.1 through 10|72.254.254.254
				addr := strconv.Itoa(firstOctet) + "." +
					strconv.Itoa(((epIdx%16387064)/64516)+1) + "." +
					strconv.Itoa(((epIdx%64516)/254)+1) + "." +
					strconv.Itoa((epIdx%254)+1)
				port := testPorts[epIdx%len(testPorts)]
				// Add the endpoint specific labels: UID + Protocol
				epLabels[0] = EndpointLabel{DestinationUID.AttrName(),
					"ep-uid-" + strconv.Itoa(epIdx)}
				epLabels[1] = EndpointLabel{DestinationProtocol.AttrName(),
					testProtocols[epIdx%len(testProtocols)]}
				// Uncomment for debugging test data build logic
				// t.Logf("Lbsl: %v\n", epLabels)
				ep, err := NewEndpoint(addr, port, SocketProtocolTCP, epLabels)
				if err != nil {
					t.Fatalf("bad test data: %s", err.Error())
				}
				outEndpoints[epIdx] = ep
			}
		}
	}
	// Ignore last sample point, cause sum of long tail can skew result sets
	return outRules, outSubsets, outEndpoints, outSamplePoints[0:3]
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
