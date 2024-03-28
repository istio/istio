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
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/meta/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	auth "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientsecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	kubeclient "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	"istio.io/istio/pkg/workloadapi/security"
)

const (
	testNS   = "ns1"
	systemNS = "istio-system"
	testNW   = "testnetwork"
	testC    = "cluster0"
)

func init() {
	features.EnableAmbientWaypoints = true
	features.EnableAmbientControllers = true
}

func TestAmbientIndex_NetworkAndClusterIDs(t *testing.T) {
	cases := []struct {
		name    string
		cluster cluster.ID
		network network.ID
	}{
		{
			name:    "values unset",
			cluster: "",
			network: "",
		},
		{
			name:    "values set",
			cluster: testC,
			network: testNW,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newAmbientTestServer(t, c.cluster, c.network)
			s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
			s.assertEvent(t, s.podXdsName("pod1"))
			s.assertAddresses(t, s.addrXdsName("127.0.0.1"), "pod1")
		})
	}
}

func TestAmbientIndex_WorkloadNotFound(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	// Add a pod.
	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)

	// Lookup a different address and verify nothing is returned.
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"))
}

func TestAmbientIndex_LookupWorkloads(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, "", "pod1")
	s.assertEvent(t, s.podXdsName("pod1"))

	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a", "other": "label"}, nil, true, corev1.PodRunning)
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "other"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, "", "pod1", "pod2", "pod3")
	s.assertAddresses(t, s.addrXdsName("127.0.0.1"), "pod1")
	s.assertAddresses(t, s.addrXdsName("127.0.0.2"), "pod2")
	for _, key := range []string{s.podXdsName("pod3"), s.addrXdsName("127.0.0.3")} {
		assert.Equal(t, s.lookup(key), []model.AddressInfo{
			{
				Address: &workloadapi.Address{
					Type: &workloadapi.Address_Workload{
						Workload: &workloadapi.Workload{
							Name:              "pod3",
							Namespace:         testNS,
							Addresses:         [][]byte{netip.MustParseAddr("127.0.0.3").AsSlice()},
							Network:           testNW,
							ServiceAccount:    "sa1",
							Uid:               s.podXdsName("pod3"),
							Node:              "node1",
							CanonicalName:     "other",
							CanonicalRevision: "latest",
							WorkloadType:      workloadapi.WorkloadType_POD,
							WorkloadName:      "pod3",
							ClusterId:         testC,
							Status:            workloadapi.WorkloadStatus_HEALTHY,
						},
					},
				},
			},
		})
	}
	s.assertEvent(t, s.podXdsName("pod2"))
	s.assertEvent(t, s.podXdsName("pod3"))
}

func TestAmbientIndex_ServiceAttachedWaypoints(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := newAmbientTestServer(t, testC, testNW)

	s.addWaypoint(t, "10.0.0.10", "test-wp", "default", true)

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))

	// Now add a service that will select pods with label "a".
	s.addService(t, "svc1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	s.assertEvent(t, s.podXdsName("pod1"), s.svcXdsName("svc1"))

	s.addService(t, "svc1",
		map[string]string{},
		map[string]string{constants.AmbientUseWaypoint: "test-wp"},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	s.assertEvent(t, s.svcXdsName("svc1"))
	s.assertNoEvent(t)

	// We should now see the waypoint service IP when we look up the annotated svc
	assert.Equal(t,
		s.lookup(s.addrXdsName("10.0.0.1"))[0].Address.GetService().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.10").AsSlice())
}

func TestAmbientIndex_ServiceSelectsCorrectWorkloads(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	// Add 2 pods with the "a" label, and one without.
	// We should get an event for the new Service and the two *Pod* IPs impacted
	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a", "other": "label"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "other"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod3"))
	s.clearEvents()

	// Now add a service that will select pods with label "a".
	s.addService(t, "svc1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"), s.svcXdsName("svc1"))

	// Services should appear with workloads when we get all resources.
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "svc1")

	// Look up the resources by VIP.
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"), "pod1", "pod2", "svc1")

	s.clearEvents()

	// Add a new pod to the service, we should see it
	s.addPods(t, "127.0.0.4", "pod4", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "pod4", "svc1")
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"), "pod1", "pod2", "pod4", "svc1")
	s.assertEvent(t, s.podXdsName("pod4"))
	s.clearEvents()

	// Delete it, should remove from the Service as well
	s.deletePod(t, "pod4")
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "svc1")
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"), "pod1", "pod2", "svc1")
	s.assertAddresses(t, s.addrXdsName("127.0.0.4")) // Should not be accessible anymore
	s.assertAddresses(t, s.podXdsName("pod4"))
	s.assertEvent(t, s.podXdsName("pod4"))
	s.clearEvents()

	// Update Service to have a more restrictive label selector
	s.addService(t, "svc1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a", "other": "label"}, "10.0.0.1")
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "svc1")
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"), "pod2", "svc1")
	s.assertEvent(t, s.podXdsName("pod1"))
	s.clearEvents()

	// Update a pod to add it to the service
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "a", "other": "label"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "svc1")
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"), "pod2", "pod3", "svc1")
	s.assertEvent(t, s.podXdsName("pod3"))
	s.clearEvents()

	// And remove it again
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "svc1")
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"), "pod2", "svc1")
	s.assertEvent(t, s.podXdsName("pod3"))
	s.clearEvents()

	// Delete the service entirely
	s.deleteService(t, "svc1")
	s.assertAddresses(t, "", "pod1", "pod2", "pod3")
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"))
	s.assertEvent(t, s.podXdsName("pod2"), s.svcXdsName("svc1"))
}

func TestAmbientIndex_WaypointConfiguredOnlyWhenReady(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	s.addPods(t,
		"127.0.0.1",
		"pod1",
		"sa1",
		map[string]string{"app": "a"},
		map[string]string{constants.AmbientUseWaypoint: "waypoint-sa1"},
		true,
		corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t,
		"127.0.0.2",
		"pod2",
		"sa2",
		map[string]string{"app": "b"},
		map[string]string{constants.AmbientUseWaypoint: "waypoint-sa2"},
		true,
		corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))

	s.addWaypoint(t, "10.0.0.1", "waypoint-sa1", "sa1", false)
	s.addWaypoint(t, "10.0.0.2", "waypoint-sa2", "sa2", true)
	s.assertEvent(t, s.podXdsName("pod2"))

	// make waypoint-sa1 ready
	s.addWaypoint(t, "10.0.0.1", "waypoint-sa1", "sa1", true)
	// if waypoint-sa1 was configured when not ready "pod2" assertions should skip the "pod1" xds event and this should fail
	s.assertEvent(t, s.podXdsName("pod1"))
}

func TestAmbientIndex_WaypointAddressAddedToWorkloads(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	s.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
			Annotations: map[string]string{
				constants.AmbientUseWaypoint: "waypoint-ns",
			},
		},
	})

	// Add pods for app "a".
	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a", "other": "label"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod3"))
	// Add pods for app "b".
	s.addPods(t,
		"127.0.0.4",
		"pod4",
		"sa2",
		map[string]string{"app": "b"},
		map[string]string{constants.AmbientUseWaypoint: "waypoint-sa2"},
		true,
		corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod4"))

	s.addWaypoint(t, "10.0.0.2", "waypoint-ns", "", true)
	// All these workloads updated, so push them
	s.assertEvent(t, s.podXdsName("pod1"),
		s.podXdsName("pod2"),
		s.podXdsName("pod3"),
	)

	// Add a waypoint proxy pod for namespace
	s.addPods(t, "127.0.0.200", "waypoint-ns-pod", "namespace-wide",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "namespace-wide",
		}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("waypoint-ns-pod"))
	// create the waypoint service
	s.addService(t, "waypoint-ns",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{},
		[]int32{80}, map[string]string{constants.GatewayNameLabel: "namespace-wide"}, "10.0.0.2")
	s.assertEvent(t,
		s.podXdsName("waypoint-ns-pod"),
		s.svcXdsName("waypoint-ns"),
	)
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "pod4", "waypoint-ns", "waypoint-ns-pod")

	s.addWaypoint(t, "10.0.0.3", "waypoint-sa2", "sa2", true)
	s.assertEvent(t, s.podXdsName("pod4"))
	// Add a waypoint proxy pod for sa2
	s.addPods(t, "127.0.0.250", "waypoint-sa2-pod", "service-account",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "service-account",
		}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("waypoint-sa2-pod"))
	// create the waypoint service
	s.addService(t, "waypoint-sa2",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{},
		[]int32{80}, map[string]string{constants.GatewayNameLabel: "service-account"}, "10.0.0.3")
	s.assertEvent(t,
		s.podXdsName("waypoint-sa2-pod"),
		s.svcXdsName("waypoint-sa2"),
	)
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "pod4", "waypoint-ns", "waypoint-ns-pod", "waypoint-sa2-pod", "waypoint-sa2")

	// We should now see the waypoint service IP
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.3"))[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.4"))[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.3").AsSlice())

	// Lookup for service VIP should return Workload and Service AddressInfo objects
	assert.Equal(t,
		len(s.lookup(s.addrXdsName("10.0.0.2"))),
		2)
	for _, k := range s.lookup(s.addrXdsName("10.0.0.2")) {
		switch k.Type.(type) {
		case *workloadapi.Address_Workload:
			assert.Equal(t, k.Address.GetWorkload().Name, "waypoint-ns-pod")
			assert.Equal(t, k.Address.GetWorkload().Waypoint, nil)
		case *workloadapi.Address_Service:
			assert.Equal(t, k.Address.GetService().Name, "waypoint-ns")
		}
	}

	// Lookup for service via namespace/hostname returns Service and Workload AddressInfo
	assert.Equal(t,
		len(s.lookup(s.svcXdsName("waypoint-ns"))), 2)
	for _, k := range s.lookup(s.svcXdsName("waypoint-ns")) {
		switch k.Type.(type) {
		case *workloadapi.Address_Workload:
			assert.Equal(t, k.Address.GetWorkload().Name, "waypoint-ns-pod")
			assert.Equal(t, k.Address.GetWorkload().Waypoint, nil)
		case *workloadapi.Address_Service:
			assert.Equal(t, k.Address.GetService().Hostname, s.hostnameForService("waypoint-ns"))
		}
	}

	// Add another waypoint pod, expect no updates for other pods since waypoint address refers to service VIP
	s.addPods(t, "127.0.0.201", "waypoint2-ns-pod", "namespace-wide",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "namespace-wide",
		}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("waypoint2-ns-pod"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.3"))[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())
	// Waypoints do not have waypoints
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.200"))[0].Address.GetWorkload().Waypoint,
		nil)

	// make sure looking up the waypoint for a wl by network and address functions correctly
	assert.Equal(t, len(s.Waypoint(testNW, "127.0.0.1")), 1)
	for _, k := range s.Waypoint(testNW, "127.0.0.1") {
		assert.Equal(t, k.AsSlice(), netip.MustParseAddr("10.0.0.2").AsSlice())
	}

	s.addService(t, "svc1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"), "pod1", "pod2", "pod3", "svc1")
	// Send update for the workloads as well...
	s.assertEvent(t, s.podXdsName("pod1"),
		s.podXdsName("pod2"),
		s.podXdsName("pod3"),
		s.svcXdsName("svc1"),
	)
	// Make sure Service sees waypoints as well
	assert.Equal(t,
		s.lookup(s.addrXdsName("10.0.0.1"))[1].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	// Delete a waypoint
	s.deletePod(t, "waypoint2-ns-pod")
	s.assertEvent(t, s.podXdsName("waypoint2-ns-pod"))

	// Workload should not be updated since service has not changed
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.3"))[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	// As should workload via Service
	assert.Equal(t,
		s.lookup(s.addrXdsName("10.0.0.1"))[1].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	s.addPods(t, "127.0.0.201", "waypoint2-sa", "waypoint-sa",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{constants.WaypointServiceAccount: "sa2"}, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("waypoint2-sa"))
	// Unrelated SA should not change anything
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.3"))[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	// Adding a new pod should also see the waypoint
	s.addPods(t, "127.0.0.6", "pod6", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod6"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.6"))[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	s.deletePod(t, "pod6")
	s.assertEvent(t, s.podXdsName("pod6"))

	s.deletePod(t, "pod3")
	s.assertEvent(t, s.podXdsName("pod3"))
	s.deletePod(t, "pod2")
	s.assertEvent(t, s.podXdsName("pod2"))

	s.deleteWaypoint(t, "waypoint-ns")
	s.assertEvent(t, s.podXdsName("pod1"))
	s.deleteService(t, "waypoint-ns")
	s.assertEvent(t,
		s.podXdsName("waypoint-ns-pod"),
		s.svcXdsName("waypoint-ns"))

	s.deleteWaypoint(t, "waypoint-sa2")
	s.assertEvent(t, s.podXdsName("pod4"))
	s.deleteService(t, "waypoint-sa2")
	s.assertEvent(t,
		s.podXdsName("waypoint-sa2-pod"),
		s.svcXdsName("waypoint-sa2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("10.0.0.1"))[1].Address.GetWorkload().Waypoint,
		nil)
}

// TODO(nmittler): Consider splitting this into multiple, smaller tests.
func TestAmbientIndex_Policy(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t, "127.0.0.200", "waypoint-ns-pod", "namespace-wide",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "namespace-wide",
		}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("waypoint-ns-pod"))
	s.addPods(t, "127.0.0.201", "waypoint2-sa", "waypoint-sa",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{constants.WaypointServiceAccount: "sa2"}, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("waypoint2-sa"))
	s.addWaypoint(t, "10.0.0.2", "waypoint-ns", "", true)
	s.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
			Annotations: map[string]string{
				constants.AmbientUseWaypoint: "waypoint-ns",
			},
		},
	})
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addService(t, "waypoint-ns",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{},
		[]int32{80}, map[string]string{constants.GatewayNameLabel: "namespace-wide"}, "10.0.0.2")
	s.assertUnorderedEvent(t, s.podXdsName("waypoint-ns-pod"), s.svcXdsName("waypoint-ns"))
	s.clearEvents()
	selectorPolicyName := "selector"

	// Test that PeerAuthentications are added to the ambient index
	s.addPolicy(t, "global", systemNS, nil, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
		}
	})
	s.clearEvents()

	s.addPolicy(t, "namespace", testNS, nil, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	// Should add the static policy to all pods in the ns1 namespace since the effective mode is STRICT
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})
	s.clearEvents()

	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	// Expect no event since the effective policy doesn't change
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})

	// Change the workload policy to be permissive
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
		}
	})
	s.assertEvent(t, s.podXdsName("pod1")) // Static policy should be removed since it isn't STRICT
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	// Add a port-level STRICT exception to the workload policy
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
		}
		pol.Spec.PortLevelMtls = map[uint32]*auth.PeerAuthentication_MutualTLS{
			9090: {
				Mode: auth.PeerAuthentication_MutualTLS_STRICT,
			},
		}
	})
	s.assertEvent(t, s.podXdsName("pod1")) // Selector policy should be added back since there is now a STRICT exception
	time.Sleep(time.Second)
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      selectorPolicyName,
			Namespace: "ns1",
		}))})

	// Pod not in selector policy, but namespace policy should take effect (hence static policy)
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "not-a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.2"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})

	// Add it to the policy by updating its selector
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.2"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      selectorPolicyName,
			Namespace: "ns1",
		}))})

	// Add global selector policy; nothing should happen since PeerAuthentication doesn't support global mesh wide selectors
	s.addPolicy(t, "global-selector", systemNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      selectorPolicyName,
			Namespace: "ns1",
		}))})

	// Delete global selector policy
	s.pa.Delete("global-selector", systemNS)

	// Update workload policy to be PERMISSIVE
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
		}
		pol.Spec.PortLevelMtls = map[uint32]*auth.PeerAuthentication_MutualTLS{
			9090: {
				Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
			},
		}
	})
	// There should be an event since effective policy moves to PERMISSIVE
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	// Change namespace policy to be PERMISSIVE
	s.addPolicy(t, "namespace", testNS, nil, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
		}
	})

	// All pods have an event (since we're only testing one namespace) but still no policies attached
	s.assertEvent(t, s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	// Change workload policy to be STRICT and remove port-level overrides
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
		pol.Spec.PortLevelMtls = nil
	})

	// Selected pods receive an event
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)}) // Effective mode is STRICT so set policy

	// Add a permissive port-level override
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
		pol.Spec.PortLevelMtls = map[uint32]*auth.PeerAuthentication_MutualTLS{
			9090: {
				Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
			},
		}
	})
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2")) // Matching pods receive an event
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      selectorPolicyName,
			Namespace: "ns1",
		}))})

	// Set workload policy to be UNSET with a STRICT port-level override
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = nil // equivalent to UNSET
		pol.Spec.PortLevelMtls = map[uint32]*auth.PeerAuthentication_MutualTLS{
			9090: {
				Mode: auth.PeerAuthentication_MutualTLS_STRICT,
			},
		}
	})
	// The policy should still be added since the effective policy is PERMISSIVE
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      selectorPolicyName,
			Namespace: "ns1",
		}))})

	// Change namespace policy back to STRICT
	s.addPolicy(t, "namespace", testNS, nil, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	// All pods have an event (since we're only testing one namespace)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"), s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)}) // Effective mode is STRICT so set static policy

	// Set workload policy to be UNSET with a PERMISSIVE port-level override
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = nil // equivalent to UNSET
		pol.Spec.PortLevelMtls = map[uint32]*auth.PeerAuthentication_MutualTLS{
			9090: {
				Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
			},
		}
	})
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2")) // Matching pods receive an event
	// The policy should still be added since the effective policy is STRICT
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName), fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      selectorPolicyName,
			Namespace: "ns1",
		}))})

	// Clear PeerAuthentication from workload
	s.pa.Delete("selector", testNS)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"))
	// Effective policy is still STRICT so the static policy should still be set
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})

	// Now remove the namespace and global policies along with the pods
	s.pa.Delete("namespace", testNS)
	s.pa.Delete("global", systemNS)
	s.deletePod(t, "pod2")
	s.assertEvent(t, s.podXdsName("pod2"), s.podXdsName("pod1"))
	s.clearEvents()

	// Test AuthorizationPolicies
	s.addPolicy(t, "global", systemNS, nil, gvk.AuthorizationPolicy, nil)
	s.addPolicy(t, "namespace", testNS, nil, gvk.AuthorizationPolicy, nil)
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	// Pod not in policy
	s.addPods(t, "127.0.0.2", "pod3", "sa1", map[string]string{"app": "not-a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod3"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.2"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	// Add it to the policy by updating its selector
	s.addPods(t, "127.0.0.2", "pod3", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod3"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.2"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	s.addPolicy(t, "global-selector", systemNS, map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3"))

	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"istio-system/global-selector", "ns1/selector"})

	// Update selector to not select
	s.addPolicy(t, "global-selector", systemNS, map[string]string{"app": "not-a"}, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3"))

	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	// Add STRICT global PeerAuthentication
	s.addPolicy(t, "strict", systemNS, nil, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	// Every workload should receive an event
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3"), s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
	// Static STRICT policy should be sent
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector", fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})

	// Now add a STRICT workload PeerAuthentication
	s.addPolicy(t, "selector-strict", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	// Effective policy is still STRICT so only static policy should be referenced
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector", fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})

	// Change the workload policy to PERMISSIVE
	s.addPolicy(t, "selector-strict", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
		}
	})
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3")) // Matching workloads should receive an event
	// Static STRICT policy should disappear
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	// Change the workload policy to DISABLE
	s.addPolicy(t, "selector-strict", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_DISABLE,
		}
	})

	// No event because there's effectively no change

	// Static STRICT policy should disappear
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	// Now make the workload policy STRICT but have a PERMISSIVE port-level override
	s.addPolicy(t, "selector-strict", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
		pol.Spec.PortLevelMtls = map[uint32]*auth.PeerAuthentication_MutualTLS{
			9090: {
				Mode: auth.PeerAuthentication_MutualTLS_PERMISSIVE,
			},
		}
	})
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3")) // Matching workloads should receive an event
	// Workload policy should be added since there's a port level exclusion
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector", fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      "selector-strict",
			Namespace: "ns1",
		}))})

	// Now add a rule allowing a specific source principal to the workload AuthorizationPolicy
	s.addPolicy(t, selectorPolicyName, testNS, map[string]string{"app": "a"}, gvk.AuthorizationPolicy, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.AuthorizationPolicy)
		pol.Spec.Rules = []*auth.Rule{
			{
				From: []*auth.Rule_From{{Source: &auth.Source{Principals: []string{"cluster.local/ns/ns1/sa/sa1"}}}},
			},
		}
	})
	// No event since workload policy should still be there (both workloads' policy references remain the same).
	// Since PeerAuthentications are translated into DENY policies we can safely apply them
	// alongside ALLOW authorization policies
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector", fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      "selector-strict",
			Namespace: "ns1",
		}))})

	s.authz.Delete("selector", testNS)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%s", model.GetAmbientPolicyConfigName(model.ConfigKey{
			Kind:      kind.PeerAuthentication,
			Name:      "selector-strict",
			Namespace: "ns1",
		}))})

	// Delete selector policy
	s.pa.Delete("selector-strict", testNS)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3")) // Matching workloads should receive an event
	// Static STRICT policy should now be sent because of the global policy
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})

	// Delete global policy
	s.pa.Delete("strict", systemNS)
	// Every workload should receive an event
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod3"), s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
	// Now no policies are in effect
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	s.clearEvents()
	s.addPolicy(t, "gateway-targeted", testNS, nil, gvk.AuthorizationPolicy, func(o controllers.Object) {
		p := o.(*clientsecurityv1beta1.AuthorizationPolicy)
		p.Spec.TargetRef = &v1beta1.PolicyTargetReference{
			Group: gvk.KubernetesGateway.Group,
			Kind:  gvk.KubernetesGateway.Kind,
			Name:  "dummy-waypoint",
		}
	})
	// there should be no event for creation of a gateway-targeted policy because we should not configure WDS with a policy
	// when expressed user intent is specifically to have that policy enforced by a gateway
	s.assertNoEvent(t)
}

func TestPodLifecycleWorkloadGates(t *testing.T) {
	s := newAmbientTestServer(t, "", "")

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, "//Pod/ns1/pod1")
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1")

	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a", "other": "label"}, nil, false, corev1.PodRunning)
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "other"}, nil, false, corev1.PodPending)
	s.addPods(t, "", "pod4", "sa1", map[string]string{"app": "another"}, nil, false, corev1.PodPending)
	s.assertEvent(t, "//Pod/ns1/pod2")
	// Still healthy
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1")
	// Unhealthy
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_UNHEALTHY, "pod2", "pod3")
	// pod3 is pending but have be assigned IP
	// pod4 is pending and not have IP
}

func TestAddressInformation(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	// Add 2 pods with the "a" label, and one without.
	// We should get an event for the new Service and the two *Pod* IPs impacted
	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a", "other": "label"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "other"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod3"))
	s.clearEvents()

	// Now add a service that will select pods with label "a".
	s.addService(t, "svc1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"), s.svcXdsName("svc1"))

	addrs, _ := s.AddressInformation(sets.New[string](s.svcXdsName("svc1"), s.podXdsName("pod2")))
	got := sets.New[string]()
	for _, addr := range addrs {
		if got.Contains(addr.ResourceName()) {
			t.Fatalf("got duplicate address %v", addr.ResourceName())
		}
		got.Insert(addr.ResourceName())
	}
}

func TestRBACConvert(t *testing.T) {
	files := file.ReadDirOrFail(t, "testdata")
	if len(files) == 0 {
		// Just in case
		t.Fatal("expected test cases")
	}
	for _, f := range files {
		name := filepath.Base(f)
		if !strings.Contains(name, "-in.yaml") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			pol, _, err := crd.ParseInputs(file.AsStringOrFail(t, f))
			assert.NoError(t, err)
			var o *security.Authorization
			switch pol[0].GroupVersionKind {
			case gvk.AuthorizationPolicy:
				o = convertAuthorizationPolicy(systemNS, &clientsecurityv1beta1.AuthorizationPolicy{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:      pol[0].Name,
						Namespace: pol[0].Namespace,
					},
					Spec: *((pol[0].Spec).(*auth.AuthorizationPolicy)), //nolint: govet
				})
			case gvk.PeerAuthentication:
				o = convertPeerAuthentication(systemNS, &clientsecurityv1beta1.PeerAuthentication{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:      pol[0].Name,
						Namespace: pol[0].Namespace,
					},
					Spec: *((pol[0].Spec).(*auth.PeerAuthentication)), //nolint: govet
				})
			default:
				t.Fatalf("unknown kind %v", pol[0].GroupVersionKind)
			}
			msg := ""
			if o != nil {
				msg, err = protomarshal.ToYAML(o)
				assert.NoError(t, err)
			}
			golden := filepath.Join("testdata", strings.ReplaceAll(name, "-in", ""))
			util.CompareContent(t, []byte(msg), golden)
		})
	}
}

func TestEmptyVIPsExcluded(t *testing.T) {
	testSVC := corev1.Service{
		Spec: corev1.ServiceSpec{
			ClusterIP: "",
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP: "",
					},
				},
			},
		},
	}
	vips := getVIPs(&testSVC)
	assert.Equal(t, 0, len(vips), "optional IP fields should be ignored if empty")
}

// assertWaypointAddressForPod takes a pod name for key and the expected waypoint IP Address
// if the IP is empty we assume you're asserting that the pod's waypoint address is nil
// will assert that the GW address for the pod's waypoint is the expected address
// nolint: unparam
func (s *ambientTestServer) assertWaypointAddressForPod(t *testing.T, key, expectedIP string) {
	t.Helper()
	var expectedAddress *workloadapi.GatewayAddress
	if expectedIP != "" { // "" is assumed to mean a nil address
		expectedAddress = &workloadapi.GatewayAddress{
			Destination: &workloadapi.GatewayAddress_Address{
				Address: &workloadapi.NetworkAddress{
					Address: netip.MustParseAddr(expectedIP).AsSlice(),
				},
			},
			HboneMtlsPort: 15008,
		}
	}
	workloads := s.lookup(s.podXdsName(key))
	if len(workloads) < 1 {
		t.Log("no workloads provided, assertion must fail")
		t.Fail()
	}
	for _, workload := range workloads {
		assert.Equal(t, expectedAddress.String(), workload.GetWorkload().GetWaypoint().String())
	}
}

func TestUpdateWaypointForWorkload(t *testing.T) {
	s := newAmbientTestServer(t, "", "")

	// add our waypoints but they won't be used until annotations are added
	// add a new waypoint
	s.addWaypoint(t, "10.0.0.2", "waypoint-sa1", "sa1", true)
	// Add a namespace waypoint to the pod
	s.addWaypoint(t, "10.0.0.1", "waypoint-ns", "", true)

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, "", "pod1")
	s.assertEvent(t, s.podXdsName("pod1"))
	// assert that no waypoint is being used
	s.assertWaypointAddressForPod(t, "pod1", "")

	// let use a waypoint by namespace annotation
	s.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
			Annotations: map[string]string{
				constants.AmbientUseWaypoint: "waypoint-ns",
			},
		},
	})
	s.assertEvent(t, s.podXdsName("pod1"))
	s.assertWaypointAddressForPod(t, "pod1", "10.0.0.1")

	// annotate pod1 to use a different waypoint than the namespace specifies
	s.annotatePod(t, "pod1", testNS, map[string]string{constants.AmbientUseWaypoint: "waypoint-sa1"})
	s.assertEvent(t, s.podXdsName("pod1"))
	// assert that we're using the correct waypoint for pod1
	s.assertWaypointAddressForPod(t, "pod1", "10.0.0.2")

	// remove the use-waypoint annotation from pod1
	s.annotatePod(t, "pod1", testNS, map[string]string{})
	s.assertEvent(t, s.podXdsName("pod1"))
	// assert that pod1 is using the waypoint specified on the namespace
	s.assertWaypointAddressForPod(t, "pod1", "10.0.0.1")

	// unannotate the namespace too
	s.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testNS,
			Annotations: map[string]string{},
		},
	})
	s.assertEvent(t, s.podXdsName("pod1"))
	// assert that we're once again using no waypoint
	s.assertWaypointAddressForPod(t, "pod1", "")

	// annotate pod2 to use a waypoint
	s.annotatePod(t, "pod1", testNS, map[string]string{constants.AmbientUseWaypoint: "waypoint-sa1"})
	s.assertEvent(t, s.podXdsName("pod1"))
	// assert that the correct waypoint was configured
	s.assertWaypointAddressForPod(t, "pod1", "10.0.0.2")

	// add a namespace annotation to use the namespace-scope waypoint
	s.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
			Annotations: map[string]string{
				constants.AmbientUseWaypoint: "waypoint-ns",
			},
		},
	})
	// pod2 should not experience any xds event
	s.assertNoEvent(t)
	// assert that pod2 is still using the waypoint specified in it's annotation
	s.assertWaypointAddressForPod(t, "pod1", "10.0.0.2")

	// assert local waypoint opt-out works as expected
	s.annotatePod(t, "pod1", testNS, map[string]string{constants.AmbientUseWaypoint: "#none"})
	s.assertEvent(t, s.podXdsName("pod1"))
	// assert that we're using no waypoint
	s.assertWaypointAddressForPod(t, "pod1", "")
	// check that the other opt out also works
	s.annotatePod(t, "pod1", testNS, map[string]string{constants.AmbientUseWaypoint: "~"})
	s.assertNoEvent(t)
	s.assertWaypointAddressForPod(t, "pod1", "")
}

func TestWorkloadsForWaypoint(t *testing.T) {
	s := newAmbientTestServer(t, "", testNW)

	assertWaypoint := func(t *testing.T, waypointNetwork string, waypointAddress string, expected ...string) {
		t.Helper()
		wl := sets.New(slices.Map(s.WorkloadsForWaypoint(model.WaypointKey{
			Network:   waypointNetwork,
			Addresses: []string{waypointAddress},
		}), func(e model.WorkloadInfo) string {
			return e.ResourceName()
		})...)
		assert.Equal(t, wl, sets.New(expected...))
	}
	// Add a namespace waypoint to the pod
	s.addWaypoint(t, "10.0.0.1", "waypoint-ns", "", true)
	s.addWaypoint(t, "10.0.0.2", "waypoint-sa1", "sa1", true)

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t, "127.0.0.2", "pod2", "sa2", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))

	s.ns.Update(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
			Annotations: map[string]string{
				constants.AmbientUseWaypoint: "waypoint-ns",
			},
		},
	})

	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"))
	assertWaypoint(t, testNW, "10.0.0.1", s.podXdsName("pod1"), s.podXdsName("pod2"))
	// TODO: should this be returned? Or should it be filtered because such a waypoint does not exist

	// Add a service account waypoint to the pod
	s.annotatePod(t, "pod1", testNS, map[string]string{constants.AmbientUseWaypoint: "waypoint-sa1"})
	s.assertEvent(t, s.podXdsName("pod1"))

	assertWaypoint(t, testNW, "10.0.0.2", s.podXdsName("pod1"))
	assertWaypoint(t, testNW, "10.0.0.1", s.podXdsName("pod2"))

	// Revert back
	s.annotatePod(t, "pod1", testNS, map[string]string{})
	s.assertEvent(t, s.podXdsName("pod1"))

	assertWaypoint(t, testNW, "10.0.0.1", s.podXdsName("pod1"), s.podXdsName("pod2"))
}

func TestWorkloadsForWaypointOrder(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := newAmbientTestServer(t, "", testNW)

	assertOrderedWaypoint := func(t *testing.T, network, address string, expected ...string) {
		t.Helper()
		wls := s.WorkloadsForWaypoint(model.WaypointKey{
			Network:   network,
			Addresses: []string{address},
		})
		wl := make([]string, len(wls))
		for i, e := range wls {
			wl[i] = e.ResourceName()
		}
		assert.Equal(t, wl, expected)
	}
	s.addWaypoint(t, "10.0.0.1", "waypoint", "", true)

	// expected order is pod3, pod1, pod2, which is the order of creation
	s.addPods(t,
		"127.0.0.3",
		"pod3",
		"sa3",
		map[string]string{"app": "a"},
		map[string]string{constants.AmbientUseWaypoint: "waypoint"},
		true,
		corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod3"))
	s.addPods(t,
		"127.0.0.1",
		"pod1",
		"sa1",
		map[string]string{"app": "a"},
		map[string]string{constants.AmbientUseWaypoint: "waypoint"},
		true,
		corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPods(t,
		"127.0.0.2",
		"pod2",
		"sa2",
		map[string]string{"app": "a"},
		map[string]string{constants.AmbientUseWaypoint: "waypoint"},
		true,
		corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	assertOrderedWaypoint(t, testNW, "10.0.0.1",
		s.podXdsName("pod3"), s.podXdsName("pod1"), s.podXdsName("pod2"))
}

// This is a regression test for a case where policies added after pods were not applied when
// querying by service
func TestPolicyAfterPod(t *testing.T) {
	s := newAmbientTestServer(t, testC, testNW)

	s.addService(t, "svc1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	s.assertEvent(t, s.svcXdsName("svc1"))
	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod1"))
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"))
	assert.Equal(t, s.lookup(s.svcXdsName("svc1"))[1].GetWorkload().GetAuthorizationPolicies(), []string{"ns1/selector"})
}

type ambientTestServer struct {
	*index
	clusterID cluster.ID
	network   network.ID
	fx        *xdsfake.Updater
	pc        clienttest.TestClient[*corev1.Pod]
	sc        clienttest.TestWriter[*corev1.Service]
	ns        clienttest.TestWriter[*corev1.Namespace]
	grc       clienttest.TestWriter[*k8sbeta.Gateway]
	se        clienttest.TestWriter[*apiv1alpha3.ServiceEntry]
	we        clienttest.TestWriter[*apiv1alpha3.WorkloadEntry]
	pa        clienttest.TestWriter[*clientsecurityv1beta1.PeerAuthentication]
	authz     clienttest.TestWriter[*clientsecurityv1beta1.AuthorizationPolicy]
	t         *testing.T
}

func newAmbientTestServer(t *testing.T, clusterID cluster.ID, networkID network.ID) *ambientTestServer {
	up := xdsfake.NewFakeXDS()
	up.SplitEvents = true
	cl := kubeclient.NewFakeClient()
	for _, crd := range []schema.GroupVersionResource{
		gvr.AuthorizationPolicy,
		gvr.PeerAuthentication,
		gvr.KubernetesGateway,
		gvr.WorkloadEntry,
		gvr.ServiceEntry,
	} {
		clienttest.MakeCRD(t, cl, crd)
	}
	idx := New(Options{
		Client:          cl,
		SystemNamespace: systemNS,
		DomainSuffix:    "company.com",
		ClusterID:       clusterID,
		XDSUpdater:      up,
		LookupNetwork: func(endpointIP string, labels labels.Instance) network.ID {
			return networkID
		},
	})
	cl.RunAndWait(test.NewStop(t))

	t.Cleanup(func() {
		if t.Failed() {
			idx := idx.(*index)
			krt.Dump(idx.authorizationPolicies)
			krt.Dump(idx.workloads.Collection)
			krt.Dump(idx.services.Collection)
			krt.Dump(idx.waypoints.Collection)
		}
	})
	a := &ambientTestServer{
		t:         t,
		clusterID: clusterID,
		network:   networkID,
		index:     idx.(*index),
		fx:        up,
		pc:        clienttest.NewDirectClient[*corev1.Pod, corev1.Pod, *corev1.PodList](t, cl),
		sc:        clienttest.NewWriter[*corev1.Service](t, cl),
		ns:        clienttest.NewWriter[*corev1.Namespace](t, cl),
		grc:       clienttest.NewWriter[*k8sbeta.Gateway](t, cl),
		se:        clienttest.NewWriter[*apiv1alpha3.ServiceEntry](t, cl),
		we:        clienttest.NewWriter[*apiv1alpha3.WorkloadEntry](t, cl),
		pa:        clienttest.NewWriter[*clientsecurityv1beta1.PeerAuthentication](t, cl),
		authz:     clienttest.NewWriter[*clientsecurityv1beta1.AuthorizationPolicy](t, cl),
	}

	// ns is more important now that we want to be able to annotate ns for svc, wl waypoint selection
	// always create the testNS enabled for ambient
	a.ns.Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testNS,
			Labels: map[string]string{"istio.io/dataplane-mode": "ambient"},
		},
	})

	return a
}

func (s *ambientTestServer) addWaypoint(t *testing.T, ip, name, sa string, ready bool) {
	t.Helper()

	fromSame := k8sv1.NamespacesFromSame
	gatewaySpec := k8sbeta.GatewaySpec{
		GatewayClassName: constants.WaypointGatewayClassName,
		Listeners: []k8sbeta.Listener{
			{
				Name:     "mesh",
				Port:     15008,
				Protocol: "HBONE",
				AllowedRoutes: &k8sbeta.AllowedRoutes{
					Namespaces: &k8sbeta.RouteNamespaces{
						From: &fromSame,
					},
				},
			},
		},
	}

	gateway := k8sbeta.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.KubernetesGateway.Kind,
			APIVersion: gvk.KubernetesGateway.GroupVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS,
		},
		Spec:   gatewaySpec,
		Status: k8sbeta.GatewayStatus{},
	}
	if sa != "" {
		annotations := make(map[string]string, 1)
		annotations[constants.WaypointServiceAccount] = sa
		gateway.Annotations = annotations
	}
	if ready {
		addrType := k8sbeta.IPAddressType
		gateway.Status = k8sbeta.GatewayStatus{
			// addresses:
			// - type: IPAddress
			//   value: 10.96.59.188
			Addresses: []k8sv1.GatewayStatusAddress{
				{
					Type:  &addrType,
					Value: ip,
				},
			},
		}
	}
	s.grc.CreateOrUpdate(&gateway)
}

func (s *ambientTestServer) deleteWaypoint(t *testing.T, name string) {
	t.Helper()
	s.grc.Delete(name, testNS)
}

func (s *ambientTestServer) addPods(t *testing.T, ip string, name, sa string, labels map[string]string,
	annotations map[string]string, markReady bool, phase corev1.PodPhase,
) {
	t.Helper()
	pod := generatePod(ip, name, testNS, sa, "node1", labels, annotations)

	p := s.pc.Get(name, pod.Namespace)
	if p == nil {
		// Apiserver doesn't allow Create to modify the pod status; in real world it's a 2 part process
		pod.Status = corev1.PodStatus{}
		newPod := s.pc.Create(pod)
		if markReady {
			setPodReady(newPod)
		}
		newPod.Status.PodIP = ip
		newPod.Status.Phase = phase
		newPod.Status.PodIPs = []corev1.PodIP{
			{
				IP: ip,
			},
		}
		s.pc.UpdateStatus(newPod)
	} else {
		s.pc.Update(pod)
	}
}

// just overwrites the annotations
// nolint: unparam
func (s *ambientTestServer) annotatePod(t *testing.T, name, ns string, annotations map[string]string) {
	t.Helper()

	p := s.pc.Get(name, ns)
	if p == nil {
		return
	}
	p.ObjectMeta.Annotations = annotations
	s.pc.Update(p)
}

func (s *ambientTestServer) addWorkloadEntries(t *testing.T, ip string, name, sa string, labels map[string]string) {
	t.Helper()
	s.we.CreateOrUpdate(generateWorkloadEntry(ip, name, "ns1", sa, labels, nil))
}

func generateWorkloadEntry(ip, name, namespace, saName string, labels map[string]string, annotations map[string]string) *apiv1alpha3.WorkloadEntry {
	return &apiv1alpha3.WorkloadEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
		},
		Spec: v1alpha3.WorkloadEntry{
			Address:        ip,
			ServiceAccount: saName,
			Labels:         labels,
		},
	}
}

func (s *ambientTestServer) deleteWorkloadEntry(t *testing.T, name string) {
	t.Helper()
	s.we.Delete(name, "ns1")
}

func (s *ambientTestServer) addServiceEntry(t *testing.T,
	hostStr string,
	addresses []string,
	name,
	ns string,
	labels map[string]string,
	epAddresses []string,
) {
	t.Helper()

	se := &apiv1alpha3.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec:   *generateServiceEntry(hostStr, addresses, labels, epAddresses),
		Status: v1alpha1.IstioStatus{},
	}
	s.se.CreateOrUpdate(se)
}

func generateServiceEntry(host string, addresses []string, labels map[string]string, epAddresses []string) *v1alpha3.ServiceEntry {
	var endpoints []*v1alpha3.WorkloadEntry
	var workloadSelector *v1alpha3.WorkloadSelector

	if epAddresses == nil {
		workloadSelector = &v1alpha3.WorkloadSelector{
			Labels: labels,
		}
	} else {
		endpoints = []*v1alpha3.WorkloadEntry{}
		for _, addr := range epAddresses {
			endpoints = append(endpoints, &v1alpha3.WorkloadEntry{
				Address: addr,
				Labels:  labels,
				Ports: map[string]uint32{
					"http": 8081, // we will override the SE http port
				},
			})
		}
	}

	return &v1alpha3.ServiceEntry{
		Hosts:     []string{host},
		Addresses: addresses,
		Ports: []*v1alpha3.ServicePort{
			{
				Name:       "http",
				Number:     80,
				TargetPort: 8080,
			},
		},
		WorkloadSelector: workloadSelector,
		Endpoints:        endpoints,
	}
}

func (s *ambientTestServer) deleteServiceEntry(t *testing.T, name, ns string) {
	t.Helper()
	s.se.Delete(name, ns)
}

func (s *ambientTestServer) assertAddresses(t *testing.T, lookup string, names ...string) {
	t.Helper()
	want := sets.New(names...)
	assert.EventuallyEqual(t, func() sets.String {
		addresses := s.lookup(lookup)
		have := sets.New[string]()
		for _, address := range addresses {
			switch addr := address.Address.Type.(type) {
			case *workloadapi.Address_Workload:
				have.Insert(addr.Workload.Name)
			case *workloadapi.Address_Service:
				have.Insert(addr.Service.Name)
			}
		}
		return have
	}, want, retry.Timeout(time.Second*3))
}

func (s *ambientTestServer) assertWorkloads(t *testing.T, lookup string, state workloadapi.WorkloadStatus, names ...string) {
	t.Helper()
	want := sets.New(names...)
	assert.EventuallyEqual(t, func() sets.String {
		workloads := s.lookup(lookup)
		have := sets.New[string]()
		for _, wl := range workloads {
			switch addr := wl.Address.Type.(type) {
			case *workloadapi.Address_Workload:
				if addr.Workload.Status == state {
					have.Insert(addr.Workload.Name)
				}
			}
		}
		return have
	}, want, retry.Timeout(time.Second*3))
}

// Make sure there are no two workloads in the index with similar UIDs
func (s *ambientTestServer) assertUniqueWorkloads(t *testing.T) {
	t.Helper()
	uids := sets.New[string]()
	workloads := s.lookup("")
	for _, wl := range workloads {
		if wl.GetWorkload() != nil && uids.InsertContains(wl.GetWorkload().GetUid()) {
			t.Fatal("Index has workloads with the same UID")
		}
	}
}

func (s *ambientTestServer) deletePolicy(name, ns string, kind config.GroupVersionKind,
) {
	switch kind {
	case gvk.AuthorizationPolicy:
		s.authz.Delete(name, ns)
	case gvk.PeerAuthentication:
		s.pa.Delete(name, ns)
	}
}

func (s *ambientTestServer) addPolicy(t *testing.T, name, ns string, selector map[string]string,
	kind config.GroupVersionKind, modify func(controllers.Object),
) {
	t.Helper()
	var sel *v1beta1.WorkloadSelector
	if selector != nil {
		sel = &v1beta1.WorkloadSelector{
			MatchLabels: selector,
		}
	}
	switch kind {
	case gvk.AuthorizationPolicy:
		pol := &clientsecurityv1beta1.AuthorizationPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: auth.AuthorizationPolicy{
				Selector: sel,
			},
		}
		if modify != nil {
			modify(pol)
		}
		s.authz.CreateOrUpdate(pol)
	case gvk.PeerAuthentication:
		pol := &clientsecurityv1beta1.PeerAuthentication{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: auth.PeerAuthentication{
				Selector: sel,
			},
		}
		if modify != nil {
			modify(pol)
		}
		s.pa.CreateOrUpdate(pol)
	}
}

func (s *ambientTestServer) deletePod(t *testing.T, name string) {
	t.Helper()
	s.pc.Delete(name, testNS)
}

func (s *ambientTestServer) assertEvent(t *testing.T, ip ...string) {
	t.Helper()
	s.assertUnorderedEvent(t, ip...)
}

func (s *ambientTestServer) assertUnorderedEvent(t *testing.T, ip ...string) {
	t.Helper()
	ev := []xdsfake.Event{}
	for _, i := range ip {
		ev = append(ev, xdsfake.Event{Type: "xds", ID: i})
	}
	s.fx.MatchOrFail(t, ev...)
}

func (s *ambientTestServer) assertNoEvent(t *testing.T) {
	t.Helper()
	s.fx.AssertEmpty(t, time.Millisecond*10)
}

func (s *ambientTestServer) deleteService(t *testing.T, name string) {
	t.Helper()
	s.sc.Delete(name, testNS)
}

func (s *ambientTestServer) addService(t *testing.T, name string, labels, annotations map[string]string,
	ports []int32, selector map[string]string, ip string,
) {
	t.Helper()
	service := generateService(name, testNS, labels, annotations, ports, selector, ip)
	s.sc.CreateOrUpdate(service)
}

func (s *ambientTestServer) lookup(key string) []model.AddressInfo {
	if key == "" {
		return s.All()
	}
	return s.Lookup(key)
}

func (s *ambientTestServer) clearEvents() {
	s.fx.Clear()
}

// Returns the XDS resource name for the given pod.
func (s *ambientTestServer) podXdsName(name string) string {
	return fmt.Sprintf("%s//Pod/%s/%s",
		s.clusterID, testNS, name)
}

// Returns the XDS resource name for the given address.
func (s *ambientTestServer) addrXdsName(addr string) string {
	return string(s.network) + "/" + addr
}

// Returns the XDS resource name for the given service.
func (s *ambientTestServer) svcXdsName(serviceName string) string {
	return fmt.Sprintf("%s/%s", testNS, s.hostnameForService(serviceName))
}

// Returns the hostname for the given service.
func (s *ambientTestServer) hostnameForService(serviceName string) string {
	return fmt.Sprintf("%s.%s.svc.company.com", serviceName, testNS)
}

// Returns the XDS resource name for the given WorkloadEntry.
func (s *ambientTestServer) wleXdsName(wleName string) string {
	return fmt.Sprintf("%s/networking.istio.io/WorkloadEntry/%s/%s",
		s.clusterID, testNS, wleName)
}

// Returns the XDS resource name for the given ServiceEntry IP address.
func (s *ambientTestServer) seIPXdsName(name string, ip string) string {
	return fmt.Sprintf("%s/networking.istio.io/ServiceEntry/%s/%s/%s",
		s.clusterID, testNS, name, ip)
}

func generatePod(ip, name, namespace, saName, node string, labels map[string]string, annotations map[string]string) *corev1.Pod {
	automount := false
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:           saName,
			NodeName:                     node,
			AutomountServiceAccountToken: &automount,
			// Validation requires this
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "ununtu",
				},
			},
		},
		// The cache controller uses this as key, required by our impl.
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
			PodIP:  ip,
			HostIP: ip,
			PodIPs: []corev1.PodIP{
				{
					IP: ip,
				},
			},
			Phase: corev1.PodRunning,
		},
	}
}

func setPodReady(pod *corev1.Pod) {
	pod.Status.Conditions = []corev1.PodCondition{
		{
			Type:               corev1.PodReady,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
}

func generateService(name, namespace string, labels, annotations map[string]string,
	ports []int32, selector map[string]string, ip string,
) *corev1.Service {
	svcPorts := make([]corev1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: ip,
			Ports:     svcPorts,
			Selector:  selector,
			Type:      corev1.ServiceTypeClusterIP,
		},
	}
}
