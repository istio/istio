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
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	authz "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	apiv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func TestAmbientIndex_WorkloadEntries(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)
	cfg := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ConfigController:     cfg,
		MeshWatcher:          mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
		ClusterID:            "cluster0",
		WorkloadEntryEnabled: true,
	})
	pc := clienttest.Wrap(t, controller.podsClient)
	sc := clienttest.Wrap(t, controller.services)
	cfg.RegisterEventHandler(gvk.AuthorizationPolicy, controller.AuthorizationPolicyHandler)
	cfg.RegisterEventHandler(gvk.WorkloadEntry, controller.WorkloadEntryHandler)
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
		want := strings.Join(ip, ",")
		fx.MatchOrFail(t, xdsfake.Event{Type: "xds", ID: want})
	}
	deleteWorkloadEntry := func(name string) {
		t.Helper()
		cfg.Delete(gvk.WorkloadEntry, name, "ns1", nil)
	}
	addWorkloadEntries := func(ip string, name, sa string, labels map[string]string) {
		t.Helper()
		wkEntry := generateWorkloadEntry(ip, name, "ns1", sa, labels, nil)

		w := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.WorkloadEntry,
				Name:             wkEntry.GetObjectMeta().GetName(),
				Namespace:        wkEntry.GetObjectMeta().GetNamespace(),
				Labels:           wkEntry.GetObjectMeta().GetLabels(),
			},
			Spec: wkEntry.Spec.DeepCopy(),
		}
		_, err := cfg.Create(w)
		if err != nil && strings.Contains(err.Error(), "item already exists") {
			_, err = cfg.Update(w)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	deletePod := func(name string) {
		t.Helper()
		if err := controller.client.Kube().CoreV1().Pods("ns1").Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	addPods := func(ip string, name, sa string, labels map[string]string, annotations map[string]string) {
		t.Helper()
		pod := generatePod(ip, name, "ns1", sa, "node1", labels, annotations)

		p := pc.Get(name, pod.Namespace)
		if p == nil {
			// Apiserver doesn't allow Create to modify the pod status; in real world its a 2 part process
			pod.Status = corev1.PodStatus{}
			newPod := pc.Create(pod)
			setPodReady(newPod)
			newPod.Status.PodIP = ip
			newPod.Status.PodIPs = []corev1.PodIP{
				{
					IP: ip,
				},
			}
			newPod.Status.Phase = corev1.PodRunning
			pc.UpdateStatus(newPod)
		} else {
			pc.Update(pod)
		}
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

	addWorkloadEntries("127.0.0.1", "name1", "sa1", map[string]string{"app": "a"})
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1")

	addWorkloadEntries("127.0.0.2", "name2", "sa1", map[string]string{"app": "a", "other": "label"})
	addWorkloadEntries("127.0.0.3", "name3", "sa1", map[string]string{"app": "other"})
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	assertWorkloads("/127.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name1")
	assertWorkloads("/127.0.0.2", workloadapi.WorkloadStatus_HEALTHY, "name2")
	assert.Equal(t, controller.ambientIndex.Lookup("/127.0.0.3"), []*model.AddressInfo{{
		Address: &workloadapi.Address{
			Type: &workloadapi.Address_Workload{
				Workload: &workloadapi.Workload{
					Uid:               "cluster0/networking.istio.io/WorkloadEntry/ns1/name3",
					Name:              "name3",
					Namespace:         "ns1",
					Addresses:         [][]byte{parseIP("127.0.0.3")},
					ServiceAccount:    "sa1",
					Node:              "127.0.0.3",
					CanonicalName:     "other",
					CanonicalRevision: "latest",
					WorkloadType:      workloadapi.WorkloadType_POD,
					WorkloadName:      "name3",
				},
			},
		},
	}})
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name2")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name3")

	// Non-existent IP should have no response
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY)
	fx.Clear()

	createService(controller, "svc1", "ns1",
		map[string]string{}, // labels
		map[string]string{}, // annotations
		[]int32{80},
		map[string]string{"app": "a"}, // selector
		t)
	// Service shouldn't change workload list
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	assertWorkloads("/127.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name1")
	// Now we should be able to look up a VIP as well
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2")
	// We should get an event for the two WEs and the selecting service impacted
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1", "cluster0/networking.istio.io/WorkloadEntry/ns1/name2",
		"ns1/svc1.ns1.svc.company.com")

	// Add a new pod to the service, we should see it
	addWorkloadEntries("127.0.0.4", "name4", "sa1", map[string]string{"app": "a"})
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3", "name4")
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name4")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name4")

	// Delete it, should remove from the Service as well
	deleteWorkloadEntry("name4")
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2")
	assertWorkloads("/127.0.0.4", workloadapi.WorkloadStatus_HEALTHY) // Should not be accessible anymore
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name4")

	fx.Clear()
	// Update Service to have a more restrictive label selector
	createService(controller, "svc1", "ns1",
		map[string]string{}, // labels
		map[string]string{}, // annotations
		[]int32{80},
		map[string]string{"app": "a", "other": "label"}, // selector
		t)
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name2")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name2", "ns1/svc1.ns1.svc.company.com")
	// assertEvent("127.0.0.2") TODO: This should be the event, but we are not efficient here.

	// Update an existing WE into the service
	addWorkloadEntries("127.0.0.3", "name3", "sa1", map[string]string{"app": "a", "other": "label"})
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name2", "name3")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name3")

	// And remove it again from the service VIP mapping by changing its label to not match the service svc1.ns1 selector
	addWorkloadEntries("127.0.0.3", "name3", "sa1", map[string]string{"app": "a"})
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name2")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name3")

	// Delete the service entirely
	controller.client.Kube().CoreV1().Services("ns1").Delete(context.Background(), "svc1", metav1.DeleteOptions{})
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY)
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name2", "ns1/svc1.ns1.svc.company.com")
	assert.Equal(t, len(controller.ambientIndex.byService), 0)

	// Add a waypoint proxy pod for namespace
	addPods("127.0.0.200", "waypoint-ns-pod", "namespace-wide",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "namespace-wide",
		}, nil)
	assertAddresses("", "name1", "name2", "name3", "waypoint-ns-pod")
	assertEvent("cluster0//Pod/ns1/waypoint-ns-pod")
	// create the waypoint service
	addService("waypoint-ns", "ns1",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{},
		[]int32{80}, map[string]string{constants.GatewayNameLabel: "namespace-wide"}, "10.0.0.2")
	assertAddresses("", "name1", "name2", "name3", "waypoint-ns", "waypoint-ns-pod")
	// All these workloads updated, so push them
	assertEvent("cluster0//Pod/ns1/waypoint-ns-pod",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name1",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name2",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name3",
		"ns1/waypoint-ns.ns1.svc.company.com",
	)
	// We should now see the waypoint service IP
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.3")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	// Add another one, expect the same result
	addPods("127.0.0.201", "waypoint2-ns-pod", "namespace-wide",
		map[string]string{
			constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel,
			constants.GatewayNameLabel:    "namespace-wide",
		}, nil)
	assertAddresses("", "name1", "name2", "name3", "waypoint-ns", "waypoint-ns-pod", "waypoint2-ns-pod")
	// all these workloads already have a waypoint, only expect the new waypoint pod
	assertEvent("cluster0//Pod/ns1/waypoint2-ns-pod")

	// Waypoints do not have waypoints
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.200")[0].Address.GetWorkload().GetWaypoint(),
		nil)

	createService(controller, "svc1", "ns1",
		map[string]string{}, // labels
		map[string]string{}, // annotations
		[]int32{80},
		map[string]string{"app": "a"}, // selector
		t)
	assertWorkloads("/10.0.0.1", workloadapi.WorkloadStatus_HEALTHY, "name1", "name2", "name3")
	// Send update for the workloads as well...
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1", "cluster0/networking.istio.io/WorkloadEntry/ns1/name2",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name3", "ns1/svc1.ns1.svc.company.com")

	// Delete a waypoint pod
	deletePod("waypoint2-ns-pod")
	assertEvent("cluster0//Pod/ns1/waypoint2-ns-pod") // only expect event on the single waypoint pod

	// Adding a new WorkloadEntry should also see the waypoint
	addWorkloadEntries("127.0.0.6", "name6", "sa1", map[string]string{"app": "a"})
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name6")
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.6")[0].Address.GetWorkload().Waypoint.GetAddress().Address,
		netip.MustParseAddr("10.0.0.2").AsSlice())

	deleteWorkloadEntry("name6")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name6")

	deleteService("waypoint-ns")
	// all affected addresses with the waypoint should be updated
	assertEvent("cluster0//Pod/ns1/waypoint-ns-pod",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name1",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name2",
		"cluster0/networking.istio.io/WorkloadEntry/ns1/name3",
		"ns1/waypoint-ns.ns1.svc.company.com")

	deleteWorkloadEntry("name3")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name3")
	deleteWorkloadEntry("name2")
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name2")

	addPolicy("global", "istio-system", nil)
	addPolicy("namespace", "default", nil)
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.1")[0].GetWorkload().GetAuthorizationPolicies(),
		nil)
	fx.Clear()

	addPolicy("selector", "ns1", map[string]string{"app": "a"})
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1")
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.1")[0].GetWorkload().GetAuthorizationPolicies(),
		[]string{"ns1/selector"})

	// WorkloadEntry not in policy
	addWorkloadEntries("127.0.0.2", "name2", "sa1", map[string]string{"app": "not-a"})
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.2")[0].GetWorkload().GetAuthorizationPolicies(),
		nil)

	// Add it to the policy by updating its selector
	addWorkloadEntries("127.0.0.2", "name2", "sa1", map[string]string{"app": "a"})
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.2")[0].GetWorkload().GetAuthorizationPolicies(),
		[]string{"ns1/selector"})

	addPolicy("global-selector", "istio-system", map[string]string{"app": "a"})
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1", "cluster0/networking.istio.io/WorkloadEntry/ns1/name2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.1")[0].GetWorkload().GetAuthorizationPolicies(),
		[]string{"istio-system/global-selector", "ns1/selector"})

	// Update selector to not select
	addPolicy("global-selector", "istio-system", map[string]string{"app": "not-a"})
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1", "cluster0/networking.istio.io/WorkloadEntry/ns1/name2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.1")[0].GetWorkload().GetAuthorizationPolicies(),
		[]string{"ns1/selector"})

	cfg.Delete(gvk.AuthorizationPolicy, "selector", "ns1", nil)
	assertEvent("cluster0/networking.istio.io/WorkloadEntry/ns1/name1", "cluster0/networking.istio.io/WorkloadEntry/ns1/name2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("/127.0.0.1")[0].GetWorkload().GetAuthorizationPolicies(),
		nil)
}

func generateWorkloadEntry(ip, name, namespace, saName string, labels map[string]string, annotations map[string]string) *apiv1alpha3.WorkloadEntry {
	return &apiv1alpha3.WorkloadEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
		},
		Spec: istiov1alpha3.WorkloadEntry{
			Address:        ip,
			ServiceAccount: saName,
			Labels:         labels,
		},
	}
}
