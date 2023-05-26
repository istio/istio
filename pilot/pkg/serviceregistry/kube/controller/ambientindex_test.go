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
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	istio "istio.io/api/networking/v1alpha3"
	authz "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func TestAmbientIndex(t *testing.T) {
	log.SetOutputLevel(istiolog.DebugLevel)
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	cfg := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ConfigController: cfg,
		MeshWatcher:      mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
		ClusterID:        "cluster0",
	})
	controller.network = "testnetwork"
	pc := clienttest.Wrap(t, controller.podsClient)
	sc := clienttest.Wrap(t, controller.services)
	gc := clienttest.NewWriter[*gateway.Gateway](t, controller.client)
	cfg.RegisterEventHandler(gvk.AuthorizationPolicy, controller.AuthorizationPolicyHandler)
	go cfg.Run(test.NewStop(t))
	addPolicy := func(name, ns string, selector map[string]string) {
		t.Helper()
		var sel *v1beta1.WorkloadSelector
		if selector != nil {
			sel = &v1beta1.WorkloadSelector{
				MatchLabels: selector,
			}
		}
		p := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.AuthorizationPolicy,
				Name:             name,
				Namespace:        ns,
			},
			Spec: &authz.AuthorizationPolicy{
				Selector: sel,
			},
		}
		_, err := cfg.Create(p)
		if err != nil && strings.Contains(err.Error(), "item already exists") {
			_, err = cfg.Update(p)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	assertAddresses := func(lookup string, names ...string) {
		t.Helper()
		want := sets.New(names...)
		assert.EventuallyEqual(t, func() sets.String {
			var addresses []*model.AddressInfo
			if lookup == "" {
				addresses = controller.ambientIndex.All()
			} else {
				addresses = controller.ambientIndex.Lookup(lookup)
			}
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
	assertEvent := func(ip ...string) {
		t.Helper()
		want := []xdsfake.Event{}
		for _, i := range ip {
			want = append(want, xdsfake.Event{Type: "xds", ID: i})
		}
		fx.MatchOrFail(t, want...)
	}
	deletePod := func(name string) {
		t.Helper()
		pc.Delete(name, "ns1")
	}
	deleteService := func(name string) {
		t.Helper()
		sc.Delete(name, "ns1")
	}
	addService := func(name, namespace string, labels, annotations map[string]string,
		ports []int32, selector map[string]string, ip string,
	) {
		t.Helper()
		service := generateService(name, namespace, labels, annotations, ports, selector, ip)
		sc.CreateOrUpdate(service)
	}
	addWorkloadEntry := func(ip string, name, sa string, labels map[string]string, annotations map[string]string) {
		t.Helper()
		_, err := cfg.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.WorkloadEntry,
				Name:             name,
				Namespace:        "ns1",
				Annotations:      annotations,
			},
			Spec: &istio.WorkloadEntry{
				Address:        ip,
				Labels:         labels,
				ServiceAccount: sa,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	addPods := func(ip string, name, sa string, labels map[string]string, annotations map[string]string) {
		t.Helper()
		pod := generatePod(ip, name, "ns1", sa, "node1", labels, annotations)
		status := *pod.Status.DeepCopy()

		p := pc.Get(name, pod.Namespace)
		if p == nil {
			// Apiserver doesn't allow Create to modify the pod status; in real world its a 2 part process
			pod.Status = corev1.PodStatus{}
			newPod := pc.Create(pod)
			newPod.Status = status
			pc.UpdateStatus(newPod)
		} else {
			pc.Update(pod)
		}
	}
	setEndpoints := func(name, ns string, podIPs ...string) {
		t.Helper()
		createEndpoints(t, controller, name, ns, []string{"tcp-port"}, podIPs, nil, nil)
	}
	createWaypoint := func(name string, podIP, serviceIP string) {
		t.Helper()
		// create pod
		addPods(podIP, name+"-pod", "namespace-wide",
			map[string]string{
				constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
				constants.GatewayNameLabel:    name,
			}, nil)
		// create the waypoint service
		addService(name, "ns1",
			map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
			map[string]string{},
			[]int32{15008}, map[string]string{constants.GatewayNameLabel: name}, serviceIP)
		setEndpoints(name, "ns1", podIP)
		gtw := &gateway.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "ns1",
			},
			Spec: gateway.GatewaySpec{
				GatewayClassName: constants.WaypointGatewayClassName,
			},
			Status: gateway.GatewayStatus{
				Addresses: []gateway.GatewayAddress{{
					Type:  ptr.Of(gateway.HostnameAddressType),
					Value: name + "ns1.svc.cluster.local",
				}},
			},
		}
		gc.CreateOrUpdate(gtw)
		gc.UpdateStatus(gtw)
	}
	addPods("127.0.0.1", "name1", "sa1", map[string]string{"app": "a"}, nil)
	assertAddresses("", "name1")
	assertEvent("testnetwork/127.0.0.1")

	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "a", "other": "label"}, nil)
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "other"}, nil)
	assertAddresses("", "name1", "name2", "name3")
	assertAddresses("testnetwork/127.0.0.1", "name1")
	assertAddresses("testnetwork/127.0.0.2", "name2")
	assert.Equal(t, controller.ambientIndex.Lookup("testnetwork/127.0.0.3"), []*model.AddressInfo{
		{
			Address: &workloadapi.Address{
				Type: &workloadapi.Address_Workload{
					Workload: &workloadapi.Workload{
						Name:              "name3",
						Namespace:         "ns1",
						Address:           netip.MustParseAddr("127.0.0.3").AsSlice(),
						Network:           "testnetwork",
						ServiceAccount:    "sa1",
						Node:              "node1",
						CanonicalName:     "other",
						CanonicalRevision: "latest",
						WorkloadType:      workloadapi.WorkloadType_POD,
						WorkloadName:      "name3",
						ClusterId:         "cluster0",
						Status:            workloadapi.WorkloadStatus_HEALTHY,
					},
				},
			},
		},
	})
	assertEvent("testnetwork/127.0.0.2", "testnetwork/127.0.0.3")

	// Non-existent IP should have no response
	assertAddresses("testnetwork/10.0.0.1")
	fx.Clear()

	addWorkloadEntry("127.0.0.9", "we1", "sa1", map[string]string{"app": "a"}, nil)
	assertAddresses("", "name1", "name2", "name3", "we1")
	assertAddresses("testnetwork/127.0.0.9", "we1")

	addService("svc1", "ns1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	// Service shouldn't change workload list
	assertAddresses("", "name1", "name2", "name3", "we1")
	assertAddresses("testnetwork/127.0.0.1", "name1")
	// Endpoints not set yet, so this is missing pods
	assertAddresses("testnetwork/10.0.0.1", "we1", "svc1")
	setEndpoints("svc1", "ns1", "127.0.0.1", "127.0.0.2")
	// Now we should be able to look up a VIP as well
	assertAddresses("testnetwork/10.0.0.1", "name1", "name2", "we1", "svc1")
	// We should get an event for the new Service, two *Pod* IPs, and WE impacted
	assertEvent("ns1/svc1.ns1.svc.cluster.local", "testnetwork/127.0.0.1", "testnetwork/127.0.0.2", "testnetwork/127.0.0.9")

	// Add a new pod to the service, we should see it
	addPods("127.0.0.4", "name4", "sa1", map[string]string{"app": "a"}, nil)
	setEndpoints("svc1", "ns1", "127.0.0.1", "127.0.0.2", "127.0.0.4")
	assertAddresses("", "name1", "name2", "name3", "name4", "we1", "svc1")
	assertAddresses("testnetwork/10.0.0.1", "name1", "name2", "name4", "we1", "svc1")
	assertEvent("testnetwork/127.0.0.4")

	// Delete it, should remove from the Service as well
	deletePod("name4")
	setEndpoints("svc1", "ns1", "127.0.0.1", "127.0.0.2")
	assertAddresses("", "name1", "name2", "name3", "svc1", "we1")
	assertAddresses("testnetwork/10.0.0.1", "name1", "name2", "svc1", "we1")
	assertAddresses("testnetwork/127.0.0.4") // Should not be accessible anymore
	assertEvent("testnetwork/127.0.0.4")

	fx.Clear()
	// Update Service to have a more restrictive label selector
	addService("svc1", "ns1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a", "other": "label"}, "10.0.0.1")
	setEndpoints("svc1", "ns1", "127.0.0.2")
	assertAddresses("", "name1", "name2", "name3", "svc1", "we1")
	assertAddresses("testnetwork/10.0.0.1", "name2", "svc1")
	// Need to update the *old* workloads only
	assertEvent("testnetwork/127.0.0.1", "testnetwork/127.0.0.9")

	// Update an existing pod into the service
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "a", "other": "label"}, nil)
	setEndpoints("svc1", "ns1", "127.0.0.2", "127.0.0.3")
	assertAddresses("", "name1", "name2", "name3", "we1", "svc1")
	assertAddresses("testnetwork/10.0.0.1", "name2", "name3", "svc1")
	assertEvent("testnetwork/127.0.0.3")

	// And remove it again
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "a"}, nil)
	setEndpoints("svc1", "ns1", "127.0.0.2")
	assertAddresses("", "name1", "name2", "name3", "svc1", "we1")
	assertAddresses("testnetwork/10.0.0.1", "name2", "svc1")
	assertEvent("testnetwork/127.0.0.3")

	// Delete the service entirely
	deleteService("svc1")
	assertAddresses("", "name1", "name2", "name3", "we1")
	assertAddresses("testnetwork/10.0.0.1")
	assertEvent("ns1/svc1.ns1.svc.cluster.local", "testnetwork/127.0.0.2")
	assert.Equal(t, controller.ambientIndex.serviceVipIndex.KeyCount(), 0)

	// Add a waypoint proxy pod for namespace
	createWaypoint("waypoint-ns", "127.0.0.200", "10.0.0.2")
	assertAddresses("", "name1", "name2", "name3", "waypoint-ns", "waypoint-ns-pod", "we1")
	// All these workloads updated, so push them
	assertEvent("ns1/waypoint-ns.ns1.svc.cluster.local",
		"testnetwork/127.0.0.1",
		"testnetwork/127.0.0.2",
		"testnetwork/127.0.0.200",
		"testnetwork/127.0.0.3",
		"testnetwork/127.0.0.9")
	// We should now see the waypoint service IP
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.3")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.9")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	// Lookup for service IP should return Workload and Service AddressInfo objects
	assert.Equal(t,
		len(controller.ambientIndex.Lookup("testnetwork/10.0.0.2")),
		2)
	for _, k := range controller.ambientIndex.Lookup("testnetwork/10.0.0.2") {
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
		len(controller.ambientIndex.Lookup("ns1/waypoint-ns.ns1.svc.cluster.local")), 2)
	for _, k := range controller.ambientIndex.Lookup("ns1/waypoint-ns.ns1.svc.cluster.local") {
		switch k.Type.(type) {
		case *workloadapi.Address_Workload:
			assert.Equal(t, k.Address.GetWorkload().Name, "waypoint-ns-pod")
			assert.Equal(t, k.Address.GetWorkload().Waypoint, nil)
		case *workloadapi.Address_Service:
			assert.Equal(t, k.Address.GetService().Hostname, "waypoint-ns.ns1.svc.cluster.local")
		}
	}

	// Add another waypoint pod, expect no updates for other pods since waypoint address refers to service IP
	addPods("127.0.0.201", "waypoint2-ns-pod", "namespace-wide",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "namespace-wide",
		}, nil)
	assertEvent("testnetwork/127.0.0.201")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.3")[0].Address.GetWorkload().Waypoint.GetAddress().Address, netip.MustParseAddr("10.0.0.2").AsSlice())
	// Waypoints do not have waypoints
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.200")[0].Address.GetWorkload().Waypoint,
		nil)
	assert.Equal(t, len(controller.Waypoint(model.WaypointScope{Namespace: "ns1", ServiceAccount: "namespace-wide"})), 1)
	for _, k := range controller.Waypoint(model.WaypointScope{Namespace: "ns1", ServiceAccount: "namespace-wide"}) {
		assert.Equal(t, k.AsSlice(), netip.MustParseAddr("10.0.0.2").AsSlice())
	}
	addService("svc1", "ns1",
		map[string]string{},
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, "10.0.0.1")
	setEndpoints("svc1", "ns1", "127.0.0.1", "127.0.0.2", "127.0.0.3")
	assertAddresses("testnetwork/10.0.0.1", "name1", "name2", "name3", "svc1", "we1")
	// Send update for the workloads as well...
	assertEvent("ns1/svc1.ns1.svc.cluster.local", "testnetwork/10.0.0.1", "testnetwork/127.0.0.1", "testnetwork/127.0.0.2", "testnetwork/127.0.0.3", "127.0.0.9")
	// Make sure Service sees waypoints as well
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/10.0.0.1")[0].Address.GetWorkload().Waypoint.GetAddress().Address, netip.MustParseAddr("10.0.0.2").AsSlice())

	// Delete a waypoint
	deletePod("waypoint2-ns-pod")
	assertEvent("testnetwork/127.0.0.201")
	// Workload should not be updated since service has not changed
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.3")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())
	// As should workload via Service
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/10.0.0.1")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	addPods("127.0.0.201", "waypoint2-sa", "waypoint-sa",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{constants.WaypointServiceAccount: "sa2"})
	assertEvent("testnetwork/127.0.0.201")
	// Unrelated SA should not change anything
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.3")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	// Adding a new pod should also see the waypoint
	addPods("127.0.0.6", "name6", "sa1", map[string]string{"app": "a"}, nil)
	assertEvent("testnetwork/127.0.0.6")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.6")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	deletePod("name6")
	assertEvent("testnetwork/127.0.0.6")

	deletePod("name3")
	assertEvent("testnetwork/127.0.0.3")
	deletePod("name2")
	assertEvent("testnetwork/127.0.0.2")

	deleteService("waypoint-ns")
	assertEvent("ns1/waypoint-ns.ns1.svc.cluster.local", "testnetwork/10.0.0.2", "testnetwork/127.0.0.1", "testnetwork/127.0.0.200")

	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/10.0.0.1")[0].Address.GetWorkload().Waypoint,
		nil)

	addPolicy("global", "istio-system", nil)
	addPolicy("namespace", "default", nil)
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.1")[0].Address.GetWorkload().AuthorizationPolicies,
		nil)
	fx.Clear()

	addPolicy("selector", "ns1", map[string]string{"app": "a"})
	assertEvent("testnetwork/127.0.0.1")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.1")[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	// Pod not in policy
	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "not-a"}, nil)
	assertEvent("testnetwork/127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.2")[0].Address.GetWorkload().AuthorizationPolicies,
		nil)

	// Add it to the policy by updating its selector
	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "a"}, nil)
	assertEvent("testnetwork/127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.2")[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	addPolicy("global-selector", "istio-system", map[string]string{"app": "a"})
	assertEvent("testnetwork/127.0.0.1", "testnetwork/127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.1")[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"istio-system/global-selector", "ns1/selector"})

	// Update selector to not select
	addPolicy("global-selector", "istio-system", map[string]string{"app": "not-a"})
	assertEvent("testnetwork/127.0.0.1", "testnetwork/127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.1")[0].Address.GetWorkload().AuthorizationPolicies,
		[]string{"ns1/selector"})

	cfg.Delete(gvk.AuthorizationPolicy, "selector", "ns1", nil)
	assertEvent("testnetwork/127.0.0.1", "testnetwork/127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("testnetwork/127.0.0.1")[0].Address.GetWorkload().AuthorizationPolicies,
		nil)
}

func TestPodLifecycleWorkloadGates(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	cfg := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ConfigController: cfg,
		MeshWatcher:      mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
	})
	pc := clienttest.Wrap(t, controller.podsClient)
	cfg.RegisterEventHandler(gvk.AuthorizationPolicy, controller.AuthorizationPolicyHandler)
	go cfg.Run(test.NewStop(t))
	assertWorkloads := func(lookup string, state workloadapi.WorkloadStatus, names ...string) {
		t.Helper()
		want := sets.New(names...)
		assert.EventuallyEqual(t, func() sets.String {
			var workloads []*model.AddressInfo
			if lookup == "" {
				workloads = controller.ambientIndex.All()
			} else {
				workloads = controller.ambientIndex.Lookup(lookup)
			}
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
	assertEvent := func(ip ...string) {
		t.Helper()
		want := strings.Join(ip, ",")
		fx.MatchOrFail(t, xdsfake.Event{Type: "xds", ID: want})
	}
	addPods := func(ip string, name, sa string, labels map[string]string, markReady bool, phase corev1.PodPhase) {
		t.Helper()
		pod := generatePod(ip, name, "ns1", sa, "node1", labels, nil)

		p := pc.Get(name, pod.Namespace)
		if p == nil {
			// Apiserver doesn't allow Create to modify the pod status; in real world its a 2 part process
			pod.Status = corev1.PodStatus{}
			newPod := pc.Create(pod)
			if markReady {
				setPodReady(newPod)
			}
			newPod.Status.PodIPs = []corev1.PodIP{{IP: ip}}
			newPod.Status.Phase = phase
			pc.UpdateStatus(newPod)
		} else {
			pc.Update(pod)
		}
	}

	addPods("127.0.0.1", "name1", "sa1", map[string]string{"app": "a"}, true, corev1.PodRunning)
	assertEvent("/127.0.0.1")
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1")

	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "a", "other": "label"}, false, corev1.PodRunning)
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "other"}, false, corev1.PodPending)
	assertEvent("/127.0.0.2")
	// Still healthy
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1")
	// Unhealthy
	assertWorkloads("", workloadapi.WorkloadStatus_UNHEALTHY, "name2")
	// name3 isn't running at all
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
			o := convertAuthorizationPolicy("istio-system", pol[0])
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
