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

package controller

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/meta/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	auth "istio.io/api/security/v1beta1"
	authz "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientsecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/cv2"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/network"
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

func TestAmbientIndex_NetworkAndClusterIDs(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)

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
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := newAmbientTestServer(t, testC, testNW)

	// Add a pod.
	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)

	// Lookup a different address and verify nothing is returned.
	s.assertAddresses(t, s.addrXdsName("10.0.0.1"))
}

func TestAmbientIndex_LookupWorkloads(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
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

func TestAmbientIndex_ServiceSelectsCorrectWorkloads(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
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
	s.assertEvent(t, s.podXdsName("pod1"), s.svcXdsName("svc1"))
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

func TestAmbientIndex_WaypointAddressAddedToWorkloads(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := newAmbientTestServer(t, testC, testNW)

	// Add pods for app "a".
	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a", "other": "label"}, nil, true, corev1.PodRunning)
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.clearEvents()

	// Add a waypoint proxy pod for namespace
	s.addPods(t, "127.0.0.200", "waypoint-ns-pod", "namespace-wide",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "namespace-wide",
		}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "waypoint-ns-pod")
	s.assertEvent(t, s.podXdsName("waypoint-ns-pod"))

	// create the waypoint service
	s.addService(t, "waypoint-ns",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{},
		[]int32{80}, map[string]string{constants.GatewayNameLabel: "namespace-wide"}, "10.0.0.2")
	s.assertAddresses(t, "", "pod1", "pod2", "pod3", "waypoint-ns", "waypoint-ns-pod")
	// All these workloads updated, so push them
	s.assertEvent(t, s.podXdsName("pod1"),
		s.podXdsName("pod2"),
		s.podXdsName("pod3"),
		s.podXdsName("waypoint-ns-pod"),
		s.svcXdsName("waypoint-ns"),
	)
	// We should now see the waypoint service IP
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.3"))[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

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
		s.lookup(s.addrXdsName("127.0.0.3"))[0].Address.GetWorkload().Waypoint.GetAddress().Address, netip.MustParseAddr("10.0.0.2").AsSlice())
	// Waypoints do not have waypoints
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.200"))[0].Address.GetWorkload().Waypoint,
		nil)
	assert.Equal(t, len(s.controller.Waypoint(model.WaypointScope{Namespace: testNS, ServiceAccount: "namespace-wide"})), 1)
	for _, k := range s.controller.Waypoint(model.WaypointScope{Namespace: testNS, ServiceAccount: "namespace-wide"}) {
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
		s.lookup(s.addrXdsName("10.0.0.1"))[1].Address.GetWorkload().Waypoint.GetAddress().Address, netip.MustParseAddr("10.0.0.2").AsSlice())

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

	s.deleteService(t, "waypoint-ns")
	s.assertEvent(t, s.podXdsName("pod1"),
		s.podXdsName("waypoint-ns-pod"),
		s.svcXdsName("waypoint-ns"),
	)
	assert.Equal(t,
		s.lookup(s.addrXdsName("10.0.0.1"))[1].Address.GetWorkload().Waypoint,
		nil)
}

// TODO(nmittler): Consider splitting this into multiple, smaller tests.
func TestAmbientIndex_Policy(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := newAmbientTestServer(t, testC, testNW)
	t.Cleanup(func() {
		log.Errorf("howardjohn: dumping")
		cv2.Dump(s.controller.ambientIndex.workloads.Collection)
	})

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
	s.addService(t, "waypoint-ns",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{},
		[]int32{80}, map[string]string{constants.GatewayNameLabel: "namespace-wide"}, "10.0.0.2")
	s.assertUnorderedEvent(t, s.podXdsName("pod1"), s.podXdsName("waypoint-ns-pod"), s.svcXdsName("waypoint-ns"))
	s.clearEvents()

	istiolog.FindScope("cv2").SetOutputLevel(istiolog.DebugLevel)
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

	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
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
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
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
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
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
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%sselector", convertedPeerAuthenticationPrefix)})

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
		[]string{fmt.Sprintf("ns1/%sselector", convertedPeerAuthenticationPrefix)})

	// Add global selector policy; nothing should happen since PeerAuthentication doesn't support global mesh wide selectors
	s.addPolicy(t, "global-selector", systemNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%sselector", convertedPeerAuthenticationPrefix)})

	// Delete global selector policy
	s.pa.Delete("global-selector", systemNS)

	// Update workload policy to be PERMISSIVE
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
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
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
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
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
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
		[]string{fmt.Sprintf("ns1/%sselector", convertedPeerAuthenticationPrefix)})

	// Set workload policy to be UNSET with a STRICT port-level override
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = nil // equivalent to UNSET
		pol.Spec.PortLevelMtls = map[uint32]*auth.PeerAuthentication_MutualTLS{
			9090: {
				Mode: auth.PeerAuthentication_MutualTLS_STRICT,
			},
		}
	})
	t.Log("before///")
	// The policy should still be added since the effective policy is PERMISSIVE
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%sselector", convertedPeerAuthenticationPrefix)})

	// Change namespace policy back to STRICT
	s.addPolicy(t, "namespace", testNS, nil, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	t.Log("after///")
	// All pods have an event (since we're only testing one namespace)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"), s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)}) // Effective mode is STRICT so set static policy

	// Set workload policy to be UNSET with a PERMISSIVE port-level override
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.PeerAuthentication, func(c controllers.Object) {
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
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName), fmt.Sprintf("ns1/%sselector", convertedPeerAuthenticationPrefix)})

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
	s.assertEvent(t, s.podXdsName("pod2"))
	s.clearEvents()

	// Test AuthorizationPolicies
	s.addPolicy(t, "global", systemNS, nil, gvk.AuthorizationPolicy, nil)
	s.addPolicy(t, "namespace", testNS, nil, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	// Pod not in policy
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "not-a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.2"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	// Add it to the policy by updating its selector
	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, s.podXdsName("pod2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.2"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	s.addPolicy(t, "global-selector", systemNS, map[string]string{"app": "a"}, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"))

	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"istio-system/global-selector", "ns1/selector"})

	// Update selector to not select
	s.addPolicy(t, "global-selector", systemNS, map[string]string{"app": "not-a"}, gvk.AuthorizationPolicy, nil)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"))

	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	time.Sleep(time.Second)
	t.Log("CLEAR")

	// Add STRICT global PeerAuthentication
	s.addPolicy(t, "strict", systemNS, nil, gvk.PeerAuthentication, func(c controllers.Object) {
		pol := c.(*clientsecurityv1beta1.PeerAuthentication)
		pol.Spec.Mtls = &auth.PeerAuthentication_MutualTLS{
			Mode: auth.PeerAuthentication_MutualTLS_STRICT,
		}
	})
	// Every workload should receive an event
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"), s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
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
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2")) // Matching workloads should receive an event
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
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2")) // Matching workloads should receive an event
	// Workload policy should be added since there's a port level exclusion
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector", fmt.Sprintf("ns1/%sselector-strict", convertedPeerAuthenticationPrefix)})

	// Now add a rule allowing a specific source principal to the workload AuthorizationPolicy
	s.addPolicy(t, "selector", testNS, map[string]string{"app": "a"}, gvk.AuthorizationPolicy, func(c controllers.Object) {
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
		[]string{"ns1/selector", fmt.Sprintf("ns1/%sselector-strict", convertedPeerAuthenticationPrefix)})

	s.authz.Delete("selector", testNS)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"))
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("ns1/%sselector-strict", convertedPeerAuthenticationPrefix)})

	// Delete selector policy
	s.pa.Delete("selector-strict", testNS)
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2")) // Matching workloads should receive an event
	// Static STRICT policy should now be sent because of the global policy
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{fmt.Sprintf("istio-system/%s", staticStrictPolicyName)})

	// Delete global policy
	s.pa.Delete("strict", systemNS)
	// Every workload should receive an event
	s.assertEvent(t, s.podXdsName("pod1"), s.podXdsName("pod2"), s.podXdsName("waypoint-ns-pod"), s.podXdsName("waypoint2-sa"))
	// Now no policies are in effect
	assert.Equal(t,
		s.lookup(s.addrXdsName("127.0.0.1"))[0].Address.GetWorkload().AuthorizationPolicies,
		nil)
}

func TestPodLifecycleWorkloadGates(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	s := newAmbientTestServer(t, "", "")

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertEvent(t, "//Pod/ns1/pod1")
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1")

	s.addPods(t, "127.0.0.2", "pod2", "sa1", map[string]string{"app": "a", "other": "label"}, nil, false, corev1.PodRunning)
	s.addPods(t, "127.0.0.3", "pod3", "sa1", map[string]string{"app": "other"}, nil, false, corev1.PodPending)
	s.assertEvent(t, "//Pod/ns1/pod2")
	// Still healthy
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_HEALTHY, "pod1")
	// Unhealthy
	s.assertWorkloads(t, "", workloadapi.WorkloadStatus_UNHEALTHY, "pod2")
	// pod3 isn't running at all
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
					Spec: *((pol[0].Spec).(*authz.AuthorizationPolicy)),
				})
			case gvk.PeerAuthentication:
				o = convertPeerAuthentication(systemNS, &clientsecurityv1beta1.PeerAuthentication{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:      pol[0].Name,
						Namespace: pol[0].Namespace,
					},
					Spec: *((pol[0].Spec).(*authz.PeerAuthentication)),
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

type ambientTestServer struct {
	cfg        *memory.Controller
	controller *FakeController
	fx         *xdsfake.Updater
	pc         clienttest.TestClient[*corev1.Pod]
	sc         clienttest.TestClient[*corev1.Service]
	se         clienttest.TestWriter[*apiv1alpha3.ServiceEntry]
	we         clienttest.TestWriter[*apiv1alpha3.WorkloadEntry]
	pa         clienttest.TestWriter[*clientsecurityv1beta1.PeerAuthentication]
	authz      clienttest.TestWriter[*clientsecurityv1beta1.AuthorizationPolicy]
}

func newAmbientTestServer(t *testing.T, clusterID cluster.ID, networkID network.ID) *ambientTestServer {
	cfg := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))
	up := xdsfake.NewFakeXDS()
	up.SplitEvents = true
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ConfigController: cfg,
		XDSUpdater:       up,
		MeshWatcher:      mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: systemNS}),
		ClusterID:        clusterID,
	})
	controller.network = networkID
	pc := clienttest.Wrap(t, controller.podsClient)
	sc := clienttest.Wrap(t, controller.services)

	go cfg.Run(test.NewStop(t))

	return &ambientTestServer{
		cfg:        cfg,
		controller: controller,
		fx:         fx,
		pc:         pc,
		sc:         sc,
		se:         clienttest.NewWriter[*apiv1alpha3.ServiceEntry](t, controller.client),
		we:         clienttest.NewWriter[*apiv1alpha3.WorkloadEntry](t, controller.client),
		pa:         clienttest.NewWriter[*clientsecurityv1beta1.PeerAuthentication](t, controller.client),
		authz:      clienttest.NewWriter[*clientsecurityv1beta1.AuthorizationPolicy](t, controller.client),
	}
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

func (s *ambientTestServer) addWorkloadEntries(t *testing.T, ip string, name, sa string, labels map[string]string) {
	t.Helper()

	s.controller.namespaces.Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ns1", Labels: map[string]string{"istio.io/dataplane-mode": "ambient"}},
	})

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

func (s *ambientTestServer) addServiceEntry(t *testing.T, hostStr string, addresses []string, name, ns string, labels map[string]string) {
	t.Helper()

	s.controller.namespaces.Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"istio.io/dataplane-mode": "ambient"}},
	})

	se := &apiv1alpha3.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec:   *generateServiceEntry(hostStr, addresses, labels),
		Status: v1alpha1.IstioStatus{},
	}
	s.se.CreateOrUpdate(se)
}

func generateServiceEntry(host string, addresses []string, labels map[string]string) *v1alpha3.ServiceEntry {
	var endpoints []*v1alpha3.WorkloadEntry
	var workloadSelector *v1alpha3.WorkloadSelector

	if len(labels) > 0 {
		workloadSelector = &v1alpha3.WorkloadSelector{
			Labels: labels,
		}
	} else {
		endpoints = []*v1alpha3.WorkloadEntry{
			{
				Address: "127.0.0.1",
				Ports: map[string]uint32{
					"http": 8081, // we will override the SE http port
				},
			},
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
		clienttest.NewWriter[*clientsecurityv1beta1.AuthorizationPolicy](t, s.controller.client).CreateOrUpdate(pol)
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
		clienttest.NewWriter[*clientsecurityv1beta1.PeerAuthentication](t, s.controller.client).CreateOrUpdate(pol)
	}
}

func (s *ambientTestServer) deletePod(t *testing.T, name string) {
	t.Helper()
	s.pc.Delete(name, testNS)
}

func (s *ambientTestServer) assertEvent(t *testing.T, ip ...string) {
	t.Helper()
	s.assertUnorderedEvent(t, ip...)
	// want := strings.Join(ip, ",")
	// s.fx.MatchOrFail(t, xdsfake.Event{Type: "xds", ID: want})
}

func (s *ambientTestServer) assertUnorderedEvent(t *testing.T, ip ...string) {
	t.Helper()
	ev := []xdsfake.Event{}
	for _, i := range ip {
		ev = append(ev, xdsfake.Event{Type: "xds", ID: i})
	}
	s.fx.MatchOrFail(t, ev...)
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
		return s.controller.ambientIndex.All()
	}
	return s.controller.ambientIndex.Lookup(key)
}

func (s *ambientTestServer) clearEvents() {
	s.fx.Clear()
}

// Returns the XDS resource name for the given pod.
func (s *ambientTestServer) podXdsName(name string) string {
	return fmt.Sprintf("%s//Pod/%s/%s",
		s.controller.clusterID, testNS, name)
}

// Returns the XDS resource name for the given address.
func (s *ambientTestServer) addrXdsName(addr string) string {
	return string(s.controller.network) + "/" + addr
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
		s.controller.clusterID, testNS, wleName)
}

// Returns the XDS resource name for the given ServiceEntry IP address.
func (s *ambientTestServer) seIPXdsName(name string, ip string) string {
	return fmt.Sprintf("%s/networking.istio.io/ServiceEntry/%s/%s/%s",
		s.controller.clusterID, testNS, name, ip)
}
