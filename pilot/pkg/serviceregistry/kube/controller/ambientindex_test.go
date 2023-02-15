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
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	authz "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
)

func TestAmbientIndex(t *testing.T) {
	cfg := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI))
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ConfigController: cfg,
		MeshWatcher:      mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
	})
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
	assertWorkloads := func(lookup string, names ...string) {
		t.Helper()
		want := sets.New(names...)
		assert.EventuallyEqual(t, func() sets.String {
			var workloads []*model.WorkloadInfo
			if lookup == "" {
				workloads = controller.ambientIndex.All()
			} else {
				workloads = controller.ambientIndex.Lookup(lookup)
			}
			have := sets.New[string]()
			for _, wl := range workloads {
				have.Insert(wl.Name)
			}
			return have
		}, want, retry.Timeout(time.Second*3))
	}
	assertEvent := func(ip ...string) {
		t.Helper()
		want := strings.Join(ip, ",")
		attempts := 0
		for attempts < 10 {
			attempts++
			ev := fx.WaitOrFail(t, "xds")
			if ev.ID != want {
				t.Logf("skip event %v, wanted %v", ev.ID, want)
			} else {
				return
			}
		}
		t.Fatalf("didn't find event for %v", ip)
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

		p, _ := controller.client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), name, metav1.GetOptions{})
		if p == nil {
			// Apiserver doesn't allow Create to modify the pod status; in real world its a 2 part process
			pod.Status = corev1.PodStatus{}
			newPod, err := controller.client.Kube().CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Cannot create %s: %v", pod.ObjectMeta.Name, err)
			}
			setPodReady(newPod)
			newPod.Status.PodIP = ip
			newPod.Status.Phase = corev1.PodRunning
			if _, err := controller.client.Kube().CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), newPod, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("Cannot update status %s: %v", pod.ObjectMeta.Name, err)
			}
		} else {
			_, err := controller.client.Kube().CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("Cannot update %s: %v", pod.ObjectMeta.Name, err)
			}
		}
	}
	addPods("127.0.0.1", "name1", "sa1", map[string]string{"app": "a"}, nil)
	assertWorkloads("", "name1")
	assertEvent("127.0.0.1")

	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "a", "other": "label"}, nil)
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "other"}, nil)
	assertWorkloads("", "name1", "name2", "name3")
	assertWorkloads("127.0.0.1", "name1")
	assertWorkloads("127.0.0.2", "name2")
	assert.Equal(t, controller.ambientIndex.Lookup("127.0.0.3"), []*model.WorkloadInfo{{
		Workload: &workloadapi.Workload{
			Name:              "name3",
			Namespace:         "ns1",
			Address:           netip.MustParseAddr("127.0.0.3").AsSlice(),
			ServiceAccount:    "sa1",
			Node:              "node1",
			CanonicalName:     "other",
			CanonicalRevision: "latest",
			WorkloadType:      workloadapi.WorkloadType_POD,
			WorkloadName:      "name3",
		},
	}})
	assertEvent("127.0.0.2")
	assertEvent("127.0.0.3")

	// Non-existent IP should have no response
	assertWorkloads("10.0.0.1")
	fx.Clear()

	createService(controller, "svc1", "ns1",
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, t)
	// Service shouldn't change workload list
	assertWorkloads("", "name1", "name2", "name3")
	assertWorkloads("127.0.0.1", "name1")
	// Now we should be able to look up a VIP as well
	assertWorkloads("10.0.0.1", "name1", "name2")
	// We should get an event for the two *Pod* IPs impacted
	assertEvent("127.0.0.1", "127.0.0.2")

	// Add a new pod to the service, we should see it
	addPods("127.0.0.4", "name4", "sa1", map[string]string{"app": "a"}, nil)
	assertWorkloads("", "name1", "name2", "name3", "name4")
	assertWorkloads("10.0.0.1", "name1", "name2", "name4")
	assertEvent("127.0.0.4")

	// Delete it, should remove from the Service as well
	deletePod("name4")
	assertWorkloads("", "name1", "name2", "name3")
	assertWorkloads("10.0.0.1", "name1", "name2")
	assertWorkloads("127.0.0.4") // Should not be accessible anymore
	assertEvent("127.0.0.4")

	fx.Clear()
	// Update Service to have a more restrictive label selector
	createService(controller, "svc1", "ns1",
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a", "other": "label"}, t)
	assertWorkloads("", "name1", "name2", "name3")
	assertWorkloads("10.0.0.1", "name2")
	// Need to update the *old* workload only
	assertEvent("127.0.0.1", "127.0.0.2")
	// assertEvent("127.0.0.2") TODO: This should be the event, but we are not efficient here.

	// Update an existing pod into the service
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "a", "other": "label"}, nil)
	assertWorkloads("", "name1", "name2", "name3")
	assertWorkloads("10.0.0.1", "name2", "name3")
	assertEvent("127.0.0.3")

	// And remove it again
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "a"}, nil)
	assertWorkloads("", "name1", "name2", "name3")
	assertWorkloads("10.0.0.1", "name2")
	assertEvent("127.0.0.3")

	// Delete the service entirely
	controller.client.Kube().CoreV1().Services("ns1").Delete(context.Background(), "svc1", metav1.DeleteOptions{})
	assertWorkloads("", "name1", "name2", "name3")
	assertWorkloads("10.0.0.1")
	assertEvent("127.0.0.2")
	assert.Equal(t, len(controller.ambientIndex.byService), 0)

	// Add a waypoint proxy for namespace
	addPods("127.0.0.200", "waypoint-ns", "namespace-wide", map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel}, nil)
	assertWorkloads("", "name1", "name2", "name3", "waypoint-ns")
	// All these workloads updated, so push them
	assertEvent("127.0.0.1", "127.0.0.2", "127.0.0.200", "127.0.0.3")
	// We should now see the waypoint IP
	assert.Equal(t, controller.ambientIndex.Lookup("127.0.0.3")[0].WaypointAddresses, [][]byte{netip.MustParseAddr("127.0.0.200").AsSlice()})

	// Add another one, expect the same result
	addPods("127.0.0.201", "waypoint2-ns", "namespace-wide", map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel}, nil)
	assertEvent("127.0.0.1", "127.0.0.2", "127.0.0.201", "127.0.0.3")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.3")[0].WaypointAddresses,
		[][]byte{netip.MustParseAddr("127.0.0.200").AsSlice(), netip.MustParseAddr("127.0.0.201").AsSlice()})
	// Waypoints do not have waypoints
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.200")[0].WaypointAddresses,
		[][]byte{})

	createService(controller, "svc1", "ns1",
		map[string]string{},
		[]int32{80}, map[string]string{"app": "a"}, t)
	assertWorkloads("10.0.0.1", "name1", "name2", "name3")
	// Send update for the workloads as well...
	assertEvent("127.0.0.1", "127.0.0.2", "127.0.0.3")
	// Make sure Service sees waypoints as well
	assert.Equal(t,
		controller.ambientIndex.Lookup("10.0.0.1")[0].WaypointAddresses,
		[][]byte{netip.MustParseAddr("127.0.0.200").AsSlice(), netip.MustParseAddr("127.0.0.201").AsSlice()})

	// Delete a waypoint
	deletePod("waypoint2-ns")
	assertEvent("127.0.0.1", "127.0.0.2", "127.0.0.201", "127.0.0.3")
	// Workload should be updated
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.3")[0].WaypointAddresses,
		[][]byte{netip.MustParseAddr("127.0.0.200").AsSlice()})
	// As should workload via Service
	assert.Equal(t,
		controller.ambientIndex.Lookup("10.0.0.1")[0].WaypointAddresses,
		[][]byte{netip.MustParseAddr("127.0.0.200").AsSlice()})

	addPods("127.0.0.201", "waypoint2-sa", "waypoint-sa",
		map[string]string{constants.ManagedGatewayLabel: constants.ManagedGatewayMeshControllerLabel},
		map[string]string{constants.WaypointServiceAccount: "sa2"})
	assertEvent("127.0.0.201")
	// Unrelated SA should not change anything
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.3")[0].WaypointAddresses,
		[][]byte{netip.MustParseAddr("127.0.0.200").AsSlice()})

	// Adding a new pod should also see the waypoint
	addPods("127.0.0.6", "name6", "sa1", map[string]string{"app": "a"}, nil)
	assertEvent("127.0.0.6")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.6")[0].WaypointAddresses,
		[][]byte{netip.MustParseAddr("127.0.0.200").AsSlice()})

	deletePod("name6")
	assertEvent("127.0.0.6")

	deletePod("name3")
	assertEvent("127.0.0.3")
	deletePod("name2")
	assertEvent("127.0.0.2")

	addPolicy("global", "istio-system", nil)
	addPolicy("namespace", "default", nil)
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.1")[0].AuthorizationPolicies,
		nil)
	fx.Clear()

	addPolicy("selector", "ns1", map[string]string{"app": "a"})
	assertEvent("127.0.0.1")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.1")[0].AuthorizationPolicies,
		[]string{"ns1/selector"})

	// Pod not in policy
	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "not-a"}, nil)
	assertEvent("127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.2")[0].AuthorizationPolicies,
		nil)

	// Add it to the policy by updating its selector
	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "a"}, nil)
	assertEvent("127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.2")[0].AuthorizationPolicies,
		[]string{"ns1/selector"})

	addPolicy("global-selector", "istio-system", map[string]string{"app": "a"})
	assertEvent("127.0.0.1", "127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.1")[0].AuthorizationPolicies,
		[]string{"istio-system/global-selector", "ns1/selector"})

	// Update selector to not select
	addPolicy("global-selector", "istio-system", map[string]string{"app": "not-a"})
	assertEvent("127.0.0.1", "127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.1")[0].AuthorizationPolicies,
		[]string{"ns1/selector"})

	cfg.Delete(gvk.AuthorizationPolicy, "selector", "ns1", nil)
	assertEvent("127.0.0.1", "127.0.0.2")
	assert.Equal(t,
		controller.ambientIndex.Lookup("127.0.0.1")[0].AuthorizationPolicies,
		nil)
}

func TestPodLifecycleWorkloadGates(t *testing.T) {
	cfg := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI))
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ConfigController: cfg,
		MeshWatcher:      mesh.NewFixedWatcher(&meshconfig.MeshConfig{RootNamespace: "istio-system"}),
	})
	cfg.RegisterEventHandler(gvk.AuthorizationPolicy, controller.AuthorizationPolicyHandler)
	go cfg.Run(test.NewStop(t))
	assertWorkloads := func(lookup string, state workloadapi.WorkloadStatus, names ...string) {
		t.Helper()
		want := sets.New(names...)
		assert.EventuallyEqual(t, func() sets.String {
			var workloads []*model.WorkloadInfo
			if lookup == "" {
				workloads = controller.ambientIndex.All()
			} else {
				workloads = controller.ambientIndex.Lookup(lookup)
			}
			have := sets.New[string]()
			for _, wl := range workloads {
				if wl.Status == state {
					have.Insert(wl.Name)
				}
			}
			return have
		}, want, retry.Timeout(time.Second*3))
	}
	assertEvent := func(ip ...string) {
		t.Helper()
		want := strings.Join(ip, ",")
		attempts := 0
		for attempts < 10 {
			attempts++
			ev := fx.WaitOrFail(t, "xds")
			if ev.ID != want {
				t.Logf("skip event %v, wanted %v", ev.ID, want)
			} else {
				return
			}
		}
		t.Fatalf("didn't find event for %v", ip)
	}
	addPods := func(ip string, name, sa string, labels map[string]string, markReady bool, phase corev1.PodPhase) {
		t.Helper()
		pod := generatePod(ip, name, "ns1", sa, "node1", labels, nil)

		p, _ := controller.client.Kube().CoreV1().Pods(pod.Namespace).Get(context.Background(), name, metav1.GetOptions{})
		if p == nil {
			// Apiserver doesn't allow Create to modify the pod status; in real world its a 2 part process
			pod.Status = corev1.PodStatus{}
			newPod, err := controller.client.Kube().CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Cannot create %s: %v", pod.ObjectMeta.Name, err)
			}
			if markReady {
				setPodReady(newPod)
			}
			newPod.Status.PodIP = ip
			newPod.Status.Phase = phase
			if _, err := controller.client.Kube().CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), newPod, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("Cannot update status %s: %v", pod.ObjectMeta.Name, err)
			}
		} else {
			_, err := controller.client.Kube().CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("Cannot update %s: %v", pod.ObjectMeta.Name, err)
			}
		}
	}

	addPods("127.0.0.1", "name1", "sa1", map[string]string{"app": "a"}, true, corev1.PodRunning)
	assertEvent("127.0.0.1")
	assertWorkloads("", workloadapi.WorkloadStatus_HEALTHY, "name1")

	addPods("127.0.0.2", "name2", "sa1", map[string]string{"app": "a", "other": "label"}, false, corev1.PodRunning)
	addPods("127.0.0.3", "name3", "sa1", map[string]string{"app": "other"}, false, corev1.PodPending)
	assertEvent("127.0.0.2")
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
