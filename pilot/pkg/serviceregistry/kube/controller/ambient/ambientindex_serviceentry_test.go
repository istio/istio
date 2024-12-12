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

package ambient

import (
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestAmbientIndexDuplicates(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)
	s.addWorkloadEntries(t, "140.140.0.10", "name0", "sa1", map[string]string{"app": "a"})
	s.addPods(t, "140.140.0.10", "pod0", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.addWorkloadEntries(t, "140.140.0.10", "name1", "sa1", map[string]string{"app": "a"})
	s.addPods(t, "140.140.0.10", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.wleXdsName("name0"), s.wleXdsName("name1"), s.podXdsName("pod0"), s.podXdsName("pod1"))
	s.assertAddresses(t, "", "pod0")
}

func TestAmbientIndex_ServiceEntry(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	// test code path where service entry creates a workload entry via `ServiceEntry.endpoints`
	// and the inlined WE has a port override
	s.addServiceEntry(t, "se.istio.io", []string{"240.240.23.45"}, "name1", testNS, nil, []string{"127.0.0.1"})
	s.assertEvent(t, s.seIPXdsName("name1", "127.0.0.1"), "ns1/se.istio.io")
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1")
	assert.Equal(t, s.lookup(s.addrXdsName("127.0.0.1")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.seIPXdsName("name1", "127.0.0.1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("127.0.0.1")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "name1",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services: map[string]*workloadapi.PortList{
						"ns1/se.istio.io": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8081, // port is overridden by inlined WE port
								},
							},
						},
					},
					ClusterId: testC,
				},
			},
		},
	}})

	s.deleteServiceEntry(t, "name1", testNS)
	s.assertEvent(t, s.seIPXdsName("name1", "127.0.0.1"), "ns1/se.istio.io")
	assert.Equal(t, s.lookup(s.addrXdsName("127.0.0.1")), nil)
	s.clearEvents()

	// workload entry that has an address of future pod will be dropped from result once pod is added
	s.addWorkloadEntries(t, "140.140.0.10", "name0", "sa1", map[string]string{"app": "a"})
	s.assertEvent(t, s.wleXdsName("name0"))
	// workload entry is included in the result until pod1 with the same address below is added
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name0")
	// lookup by address should return the workload entry's address info
	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.wleXdsName("name0"),
					Name:              "name0",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name0",
					ClusterId:         testC,
				},
			},
		},
	}})

	// test code path where service entry selects workloads via `ServiceEntry.workloadSelector`
	s.addPods(t, "140.140.0.10", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))

	// lookup by address should return the pod's address info (ignore the workload entry with similar address)
	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod1"),
					Name:              "pod1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Network:           testNW,
					ClusterId:         testC,
					Node:              "node1",
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
				},
			},
		},
	}})

	s.addPods(t, "140.140.0.11", "pod2", "sa1", map[string]string{"app": "other"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2")
	s.addWorkloadEntries(t, "240.240.34.56", "name1", "sa1", map[string]string{"app": "a"})
	s.assertEvent(t, s.wleXdsName("name1"))
	s.addWorkloadEntries(t, "240.240.34.57", "name2", "sa1", map[string]string{"app": "other"})
	s.assertEvent(t, s.wleXdsName("name2"))
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1", "name2")

	s.addWorkloadEntries(t, "140.140.0.11", "name3", "sa1", map[string]string{"app": "other"})
	s.assertEvent(t, s.wleXdsName("name3"))
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1", "name2")

	// a service entry should not be able to select across namespaces
	s.addServiceEntry(t, "mismatched.istio.io", []string{"240.240.23.45"}, "name1", "mismatched-ns", map[string]string{"app": "a"}, nil)
	s.assertEvent(t, "mismatched-ns/mismatched.istio.io")
	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod1"),
					Name:              "pod1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					Services:          nil, // should not be selected by the mismatched service entry
					ClusterId:         testC,
				},
			},
		},
	}})
	assert.Equal(t, s.lookup(s.addrXdsName("240.240.34.56")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.wleXdsName("name1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services:          nil, // should not be selected by the mismatched service entry
					ClusterId:         testC,
				},
			},
		},
	}})

	s.addServiceEntry(t, "se.istio.io", []string{"240.240.23.45"}, "name1", testNS, map[string]string{"app": "a"}, nil)
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1", "name2")
	// we should see an update for the workloads selected by the service entry
	// do not expect event for pod2 since it is not selected by the service entry
	s.assertEvent(t, s.podXdsName("pod1"), s.wleXdsName("name0"), s.wleXdsName("name1"), "ns1/se.istio.io")

	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod1"),
					Name:              "pod1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					Services: map[string]*workloadapi.PortList{
						"ns1/se.istio.io": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8080,
								},
							},
						},
					},
					ClusterId: testC,
				},
			},
		},
	}})

	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.11")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod2"),
					Name:              "pod2",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.11")},
					Node:              "node1",
					Network:           testNW,
					ClusterId:         testC,
					CanonicalName:     "other",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod2",
					Services:          nil, // labels don't match workloadSelector, this should be nil
				},
			},
		},
	}})

	assert.Equal(t, s.lookup(s.addrXdsName("240.240.34.56")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.wleXdsName("name1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services: map[string]*workloadapi.PortList{
						"ns1/se.istio.io": {
							Ports: []*workloadapi.Port{
								{
									ServicePort: 80,
									TargetPort:  8080,
								},
							},
						},
					},
					ClusterId: testC,
				},
			},
		},
	}})

	s.deleteServiceEntry(t, "name1", testNS)
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1", "pod2", "name1", "name2")
	s.assertUniqueWorkloads(t)
	// we should see an update for the workloads selected by the service entry
	s.assertEvent(t, s.podXdsName("pod1"), s.wleXdsName("name0"), s.wleXdsName("name1"), "ns1/se.istio.io")
	assert.Equal(t, s.lookup(s.addrXdsName("140.140.0.10")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.podXdsName("pod1"),
					Name:              "pod1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("140.140.0.10")},
					Node:              "node1",
					Network:           testNW,
					ClusterId:         testC,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "pod1",
					Services:          nil, // vips for pod1 should be gone now
				},
			},
		},
	}})

	assert.Equal(t, s.lookup(s.addrXdsName("240.240.34.56")), []model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               s.wleXdsName("name1"),
					Name:              "name1",
					Namespace:         testNS,
					Addresses:         [][]byte{parseIP("240.240.34.56")},
					Node:              "",
					Network:           testNW,
					CanonicalName:     "a",
					CanonicalRevision: "latest",
					ServiceAccount:    "sa1",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name1",
					Services:          nil, // vips for workload entry 1 should be gone now
					ClusterId:         testC,
				},
			},
		},
	}})
}

func TestAmbientIndex_ServiceEntry_DisableK8SServiceSelectWorkloadEntries(t *testing.T) {
	s := newAmbientTestServerWithFlags(t, testC, testNW, FeatureFlags{
		DefaultAllowFromWaypoint:              features.DefaultAllowFromWaypoint,
		EnableK8SServiceSelectWorkloadEntries: false,
	})

	s.addPods(t, "140.140.0.10", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t, "140.140.0.11", "pod2", "sa1", map[string]string{"app": "other"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	s.addWorkloadEntries(t, "240.240.34.56", "name1", "sa1", map[string]string{"app": "a"})
	s.assertEvent(t, s.wleXdsName("name1"))
	s.addWorkloadEntries(t, "240.240.34.57", "name2", "sa1", map[string]string{"app": "other"})
	s.assertEvent(t, s.wleXdsName("name2"))
	s.addServiceEntry(t, "se.istio.io", []string{"240.240.23.45"}, "name1", testNS, map[string]string{"app": "a"}, nil)
	s.assertEvent(t, s.podXdsName("pod1"), s.wleXdsName("name1"), "ns1/se.istio.io")
	s.clearEvents()

	// Setting the PILOT_ENABLE_K8S_SELECT_WORKLOAD_ENTRIES to false shouldn't affect the workloads selected by the service
	// entry
	s.assertWorkloads(t, s.addrXdsName("240.240.23.45"), workloadapi.WorkloadStatus_HEALTHY, "pod1", "name1")
}

func parseIP(ip string) []byte {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	return addr.AsSlice()
}
