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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestAmbientIndex_WorkloadEntries(t *testing.T) {
	cases := []struct {
		name         string
		multicluster bool
	}{
		{
			name: "base",
		},
		{
			name:         "multicluster (enabled but unused)",
			multicluster: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.multicluster {
				test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			}
			s := newAmbientTestServer(t, testC, testNW, "")

			s.addWorkloadEntries(t, "127.0.0.1", "name1", "sa1", map[string]string{"app": "a"})
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1")
			s.assertEvent(t, s.wleXdsName("name1"))

			s.addWorkloadEntries(t, "127.0.0.2", "name2", "sa2", map[string]string{"app": "a", "other": "label"})
			s.addWorkloadEntries(t, "127.0.0.3", "name3", "sa3", map[string]string{"app": "other"})
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			s.assertWorkloads(t, s.addrXdsName("127.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name1")
			s.assertWorkloads(t, s.addrXdsName("127.0.0.2"), workloadapi.WorkloadStatus_HEALTHY, "name2")
			assert.Equal(t, s.lookup(s.addrXdsName("127.0.0.3")), []model.AddressInfo{{
				Address: &workloadapi.Address{
					Type: &workloadapi.Address_Workload{
						Workload: &workloadapi.Workload{
							Uid:               s.wleXdsName("name3"),
							Name:              "name3",
							Namespace:         testNS,
							Network:           testNW,
							Addresses:         [][]byte{parseIP("127.0.0.3")},
							ServiceAccount:    "sa3",
							Node:              "",
							CanonicalName:     "other",
							CanonicalRevision: "latest",
							WorkloadType:      workloadapi.WorkloadType_POD,
							WorkloadName:      "name3",
							ClusterId:         testC,
						},
					},
				},
			}})
			s.assertEvent(t, s.wleXdsName("name2"))
			s.assertEvent(t, s.wleXdsName("name3"))

			// Non-existent IP should have no response
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY)
			s.clearEvents()

			s.addService(t, "svc1", map[string]string{}, // labels
				map[string]string{}, // annotations
				[]int32{80},
				map[string]string{"app": "a"}, // selector
				"10.0.0.1",
			)
			// Service shouldn't change workload list
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			s.assertWorkloads(t, s.addrXdsName("127.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name1")
			// Now we should be able to look up a VIP as well
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name1", "name2")
			// We should get an event for the two WEs and the selecting service impacted
			s.assertEvent(t, s.wleXdsName("name1"), s.wleXdsName("name2"), s.svcXdsName("svc1"))

			// Add a new pod to the service, we should see it
			s.addWorkloadEntries(t, "127.0.0.4", "name4", "sa4", map[string]string{"app": "a"})
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3", "name4")
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name4")
			s.assertEvent(t, s.wleXdsName("name4"))

			// Delete it, should remove from the Service as well
			s.deleteWorkloadEntry(t, "name4")
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name1", "name2")
			s.assertWorkloads(t, s.addrXdsName("127.0.0.4"), workloadapi.WorkloadStatus_HEALTHY) // Should not be accessible anymore
			s.assertEvent(t, s.wleXdsName("name4"))

			s.clearEvents()
			// Update Service to have a more restrictive label selector
			s.addService(t, "svc1", map[string]string{}, // labels
				map[string]string{}, // annotations
				[]int32{80},
				map[string]string{"app": "a", "other": "label"}, // selector
				"10.0.0.1",
			)
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name2")
			s.assertEvent(t, s.wleXdsName("name1"))

			// Update an existing WE into the service
			s.addWorkloadEntries(t, "127.0.0.3", "name3", "sa3", map[string]string{"app": "a", "other": "label"})
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name2", "name3")
			s.assertEvent(t, s.wleXdsName("name3"))

			// And remove it again from the service VIP mapping by changing its label to not match the service svc1.ns1 selector
			s.addWorkloadEntries(t, "127.0.0.3", "name3", "sa3", map[string]string{"app": "a"})
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name2")
			s.assertEvent(t, s.wleXdsName("name3"))

			// Delete the service entirely
			s.sc.Delete("svc1", "ns1")
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY)
			s.assertEvent(t, s.wleXdsName("name2"), s.svcXdsName("svc1"))

			// Add a waypoint proxy pod for namespace
			s.addPods(t, "127.0.0.200", "waypoint-ns-pod", "namespace-wide",
				map[string]string{
					label.GatewayManaged.Name:                    constants.ManagedGatewayMeshControllerLabel,
					label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint-ns",
				}, nil, true, corev1.PodRunning)
			s.assertAddresses(t, "", "name1", "name2", "name3", "waypoint-ns-pod")
			s.assertEvent(t, s.podXdsName("waypoint-ns-pod"))
			s.addWaypoint(t, "10.0.0.2", "waypoint-ns", constants.AllTraffic, true)
			s.ns.CreateOrUpdate(&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNS,
					Labels: map[string]string{
						label.IoIstioUseWaypoint.Name: "waypoint-ns",
					},
				},
			})
			// All these workloads updated, so push them
			s.assertEvent(t,
				s.wleXdsName("name1"),
				s.wleXdsName("name2"),
				s.wleXdsName("name3"),
			)
			// create the waypoint service
			s.addService(t, "waypoint-ns",
				map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayMeshControllerLabel}, // labels
				map[string]string{}, // annotations
				[]int32{80},
				map[string]string{label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint-ns"}, // selector
				"10.0.0.2",
			)
			s.assertEvent(t, s.podXdsName("waypoint-ns-pod"),
				s.svcXdsName("waypoint-ns"),
			)
			s.assertAddresses(t, "", "name1", "name2", "name3", "waypoint-ns", "waypoint-ns-pod")
			// We should now see the waypoint service IP
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.3"))[0].Address.GetWorkload().Waypoint.GetHostname().GetHostname(),
				"waypoint-ns.ns1.svc.company.com")

			// Add another one, expect the same result
			s.addPods(t, "127.0.0.201", "waypoint2-ns-pod", "namespace-wide",
				map[string]string{
					label.GatewayManaged.Name:                    constants.ManagedGatewayMeshControllerLabel,
					label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint-ns",
				}, nil, true, corev1.PodRunning)
			s.assertAddresses(t, "", "name1", "name2", "name3", "waypoint-ns", "waypoint-ns-pod", "waypoint2-ns-pod")
			// all these workloads already have a waypoint, only expect the new waypoint pod
			s.assertEvent(t, s.podXdsName("waypoint2-ns-pod"))

			// Waypoints do not have waypoints
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.200"))[0].Address.GetWorkload().GetWaypoint(),
				nil)

			s.addService(t, "svc1",
				map[string]string{}, // labels
				map[string]string{}, // annotations
				[]int32{80},
				map[string]string{"app": "a"}, // selector
				"10.0.0.1",
			)
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
			// Send update for the workloads as well...
			s.assertEvent(t, s.wleXdsName("name1"),
				s.wleXdsName("name2"),
				s.wleXdsName("name3"),
				s.svcXdsName("svc1"))

			// Delete a waypoint pod
			s.deletePod(t, "waypoint2-ns-pod")
			s.assertEvent(t, s.podXdsName("waypoint2-ns-pod")) // only expect event on the single waypoint pod

			// Adding a new WorkloadEntry should also see the waypoint
			s.addWorkloadEntries(t, "127.0.0.6", "name6", "sa6", map[string]string{"app": "a"})
			s.assertEvent(t, s.wleXdsName("name6"))
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.6"))[0].Address.GetWorkload().Waypoint.GetHostname().GetHostname(),
				"waypoint-ns.ns1.svc.company.com")

			s.deleteWorkloadEntry(t, "name6")
			s.assertEvent(t, s.wleXdsName("name6"))

			s.deleteWaypoint(t, "waypoint-ns")
			s.ns.Update(&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNS,
					Labels: map[string]string{
						label.IoIstioUseWaypoint.Name: "none",
					},
				},
			})
			// all affected addresses with the waypoint should be updated
			s.assertEvent(t,
				s.svcXdsName("svc1"),
				s.wleXdsName("name1"),
				s.wleXdsName("name2"),
				s.wleXdsName("name3"))

			s.deleteService(t, "waypoint-ns")
			s.assertEvent(t, s.podXdsName("waypoint-ns-pod"),
				s.svcXdsName("waypoint-ns"))

			s.deleteWorkloadEntry(t, "name3")
			s.assertEvent(t, s.wleXdsName("name3"))
			s.deleteWorkloadEntry(t, "name2")
			s.assertEvent(t, s.wleXdsName("name2"))

			s.addPolicy(t, "global", "istio-system", nil, gvk.AuthorizationPolicy, nil)
			s.addPolicy(t, "namespace", "default", nil, gvk.AuthorizationPolicy, nil)
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.1"))[0].GetWorkload().GetAuthorizationPolicies(),
				nil)
			s.clearEvents()

			s.addPolicy(t, "selector", "ns1", map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
			s.assertEvent(t, s.wleXdsName("name1"))
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.1"))[0].GetWorkload().GetAuthorizationPolicies(),
				[]string{"ns1/selector"})

			// WorkloadEntry not in policy
			s.addWorkloadEntries(t, "127.0.0.2", "name2", "sa2", map[string]string{"app": "not-a"})
			s.assertEvent(t, s.wleXdsName("name2"))
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.2"))[0].GetWorkload().GetAuthorizationPolicies(),
				nil)

			// Add it to the policy by updating its selector
			s.addWorkloadEntries(t, "127.0.0.2", "name2", "sa2", map[string]string{"app": "a"})
			s.assertEvent(t, s.wleXdsName("name2"))
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.2"))[0].GetWorkload().GetAuthorizationPolicies(),
				[]string{"ns1/selector"})

			s.addPolicy(t, "global-selector", "istio-system", map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
			s.assertEvent(t, s.wleXdsName("name1"), s.wleXdsName("name2"))
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.1"))[0].GetWorkload().GetAuthorizationPolicies(),
				[]string{"istio-system/global-selector", "ns1/selector"})

			// Update selector to not select
			s.addPolicy(t, "global-selector", "istio-system", map[string]string{"app": "not-a"}, gvk.AuthorizationPolicy, nil)
			s.assertEvent(t, s.wleXdsName("name1"), s.wleXdsName("name2"))
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.1"))[0].GetWorkload().GetAuthorizationPolicies(),
				[]string{"ns1/selector"})

			s.authz.Delete("selector", "ns1")
			s.assertEvent(t, s.wleXdsName("name1"), s.wleXdsName("name2"))
			assert.Equal(t,
				s.lookup(s.addrXdsName("127.0.0.1"))[0].GetWorkload().GetAuthorizationPolicies(),
				nil)
		})
	}
}

func TestAmbientIndex_EmptyAddrWorkloadEntries(t *testing.T) {
	cases := []struct {
		name         string
		multicluster bool
	}{
		{
			name: "base",
		},
		{
			name:         "multicluster (enabled but unused)",
			multicluster: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.multicluster {
				test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			}
			s := newAmbientTestServer(t, testC, testNW, "")
			s.addWorkloadEntries(t, "", "emptyaddr1", "sa1", map[string]string{"app": "a"})
			s.assertEvent(t, s.wleXdsName("emptyaddr1"))
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "emptyaddr1")

			s.addWorkloadEntries(t, "", "emptyaddr2", "sa1", map[string]string{"app": "a"})
			s.assertEvent(t, s.wleXdsName("emptyaddr2"))
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "emptyaddr1", "emptyaddr2")

			// ensure we stored and can fetch both; neither was blown away
			assert.Equal(t,
				s.lookup(s.wleXdsName("emptyaddr1"))[0].GetWorkload().GetName(),
				"emptyaddr1") // can lookup this workload by name
			assert.Equal(t,
				s.lookup(s.wleXdsName("emptyaddr2"))[0].GetWorkload().GetName(),
				"emptyaddr2") // can lookup this workload by name

			assert.Equal(t,
				len(s.lookup(s.addrXdsName(""))),
				0) // cannot lookup these workloads by address
		})
	}
}

func TestAmbientIndex_UpdateExistingWorkloadEntry(t *testing.T) {
	cases := []struct {
		name         string
		multicluster bool
	}{
		{
			name: "base",
		},
		{
			name:         "multicluster (enabled but unused)",
			multicluster: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.multicluster {
				test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			}
			s := newAmbientTestServer(t, testC, testNW, "")
			s.addWorkloadEntries(t, "", "emptyaddr1", "sa1", map[string]string{"app": "a"})
			s.assertEvent(t, s.wleXdsName("emptyaddr1"))
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "emptyaddr1")

			// update service account for existing WE and expect a new xds event
			s.addWorkloadEntries(t, "", "emptyaddr1", "sa2", map[string]string{"app": "a"})
			s.assertEvent(t, s.wleXdsName("emptyaddr1"))
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "emptyaddr1")
		})
	}
}

func TestAmbientIndex_InlinedWorkloadEntries(t *testing.T) {
	cases := []struct {
		name         string
		multicluster bool
	}{
		{
			name: "base",
		},
		{
			name:         "multicluster (enabled but unused)",
			multicluster: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.multicluster {
				test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			}
			s := newAmbientTestServer(t, testC, testNW, "")

			s.addServiceEntry(t, "se.istio.io", []string{"240.240.23.45"}, "se1", testNS, map[string]string{"app": "a"}, []string{"127.0.0.1", "127.0.0.2"})
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "se1")
			s.assertEvent(t, s.seIPXdsName("se1", "127.0.0.1"), s.seIPXdsName("se1", "127.0.0.2"), "ns1/se.istio.io")

			s.addPolicy(t, "selector", "ns1", map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
			s.assertEvent(t, s.seIPXdsName("se1", "127.0.0.1"), s.seIPXdsName("se1", "127.0.0.2"))
			assert.Equal(t,
				s.lookup(s.seIPXdsName("se1", "127.0.0.1"))[0].GetWorkload().GetAuthorizationPolicies(),
				[]string{"ns1/selector"})
			assert.Equal(t,
				s.lookup(s.seIPXdsName("se1", "127.0.0.2"))[0].GetWorkload().GetAuthorizationPolicies(),
				[]string{"ns1/selector"})

			s.deletePolicy("selector", "ns1", gvk.AuthorizationPolicy)
			s.assertEvent(t, s.seIPXdsName("se1", "127.0.0.1"), s.seIPXdsName("se1", "127.0.0.2"))
			assert.Equal(t,
				s.lookup(s.seIPXdsName("se1", "127.0.0.1"))[0].GetWorkload().GetAuthorizationPolicies(),
				nil)
			assert.Equal(t,
				s.lookup(s.seIPXdsName("se1", "127.0.0.2"))[0].GetWorkload().GetAuthorizationPolicies(),
				nil)

			s.deleteServiceEntry(t, "se1", testNS)
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY) // Asserting no WE residual
		})
	}
}

func TestAmbientIndex_WorkloadEntries_DisableK8SServiceSelectWorkloadEntries(t *testing.T) {
	cases := []struct {
		name         string
		multicluster bool
	}{
		{
			name: "base",
		},
		{
			name:         "multicluster (enabled but unused)",
			multicluster: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.multicluster {
				test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			}
			s := newAmbientTestServerWithFlags(t, testC, testNW, FeatureFlags{
				DefaultAllowFromWaypoint:              features.DefaultAllowFromWaypoint,
				EnableK8SServiceSelectWorkloadEntries: false,
			}, "")

			s.addWorkloadEntries(t, "127.0.0.1", "name1", "sa1", map[string]string{"app": "a"})
			s.assertEvent(t, s.wleXdsName("name1"))
			s.addWorkloadEntries(t, "127.0.0.2", "name2", "sa2", map[string]string{"app": "a", "other": "label"})
			s.assertEvent(t, s.wleXdsName("name2"))
			s.addWorkloadEntries(t, "127.0.0.3", "name3", "sa3", map[string]string{"app": "other"})
			s.assertEvent(t, s.wleXdsName("name3"))
			s.addPods(t, "127.0.0.201", "pod1", "pod1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
			s.assertEvent(t, s.podXdsName("pod1"))

			s.addService(t, "svc1", map[string]string{}, // labels
				map[string]string{}, // annotations
				[]int32{80},
				map[string]string{"app": "a"}, // selector
				"10.0.0.1",
			)
			s.assertEvent(t, s.podXdsName("pod1"), "ns1/svc1.ns1.svc.company.com")

			s.clearEvents()
			s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3", "pod1")

			// Setting the PILOT_ENABLE_K8S_SELECT_WORKLOAD_ENTRIES to false shouldn't include workload entries when
			// looking up by the k8s service address
			s.assertWorkloads(t, s.addrXdsName("10.0.0.1"), workloadapi.WorkloadStatus_HEALTHY, "pod1")
		})
	}
}
