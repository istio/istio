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
	"strconv"
	"testing"

	route "istio.io/api/networking/v1alpha3"
)

// samplePoint stores the index to the rule, index to the subset and the count of endpoints in that subset.
// It's returned by buildTestEndpoints() for data sets > 1000 and provides samples for median and the first 2 standard deviations that can
// be used for performance tests. The provided sample points are stable across multiple test runs to allow for meaningful performance regressions.
type samplePoint struct {
	name      string
	rule      int
	subset    int
	endpoints int
}

// dataSet holds the benchmark profile.
type dataSet struct {
	cntEps           int
	cntSvcs          int
	cntSubsetsPerSvc int
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
