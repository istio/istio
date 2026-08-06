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

package krt

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

// TestCollectionChangingFilterKeyDependency is a regression test for a memory leak where changing
// the FilterKey used in a Fetch (e.g., relabeling a pod's waypoint) left stale reverse-index
// entries in dependencyState.indexedDependencies that were never cleaned up.
func TestCollectionChangingFilterKeyDependency(t *testing.T) {
	stop := test.NewStop(t)
	opts := NewOptionsBuilder(stop, "test", GlobalDebugHandler)
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	kcc := kclient.New[*corev1.ConfigMap](c)
	pc := clienttest.Wrap(t, kpc)
	cc := clienttest.Wrap(t, kcc)
	pods := WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	configMaps := WrapClient[*corev1.ConfigMap](kcc, opts.WithName("ConfigMaps")...)
	c.RunAndWait(stop)

	type WaypointBinding struct {
		Named
		WaypointData string
	}

	// Derived collection: each pod fetches a ConfigMap by key derived from its "use-waypoint" label.
	Bindings := NewCollection(pods, func(ctx HandlerContext, p *corev1.Pod) *WaypointBinding {
		wpName := p.Labels["use-waypoint"]
		if wpName == "" {
			return nil
		}
		key := p.Namespace + "/" + wpName
		cms := Fetch(ctx, configMaps, FilterKey(key))
		data := ""
		if len(cms) == 1 {
			data = cms[0].Data["value"]
		}
		return &WaypointBinding{
			Named:        NewNamed(p),
			WaypointData: data,
		}
	}, opts.WithName("WaypointBindings")...)

	tt := assert.NewTracker[string](t)
	Bindings.Register(func(o Event[WaypointBinding]) {
		tt.Record(o.Event.String() + "/" + GetKey(o.Latest()))
	})
	Bindings.WaitUntilSynced(stop)

	// Create two "waypoints" (ConfigMaps)
	cc.Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "waypoint-old", Namespace: "ns"},
		Data:       map[string]string{"value": "old"},
	})
	cc.Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "waypoint-new", Namespace: "ns"},
		Data:       map[string]string{"value": "new"},
	})

	// Create a pod pointing to waypoint-old
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod",
			Namespace: "ns",
			Labels:    map[string]string{"use-waypoint": "waypoint-old"},
		},
		Status: corev1.PodStatus{PodIP: "10.0.0.1"},
	}
	pc.CreateOrUpdateStatus(pod)
	tt.WaitOrdered("add/ns/my-pod")

	// Relabel pod to point to waypoint-new
	pod.Labels["use-waypoint"] = "waypoint-new"
	pc.CreateOrUpdate(pod)
	tt.WaitOrdered("update/ns/my-pod")

	// Access the internal manyCollection to inspect indexedDependencies directly.
	mc := Bindings.(*manyCollection[*corev1.Pod, WaypointBinding])

	// Get the UID of the ConfigMaps collection (the secondary dependency).
	configMapsUID := configMaps.(internalCollection[*corev1.ConfigMap]).uid()

	// After relabeling, the reverse-index must only contain the new key, not the old one.
	mc.mu.RLock()
	podKey := Key[*corev1.Pod]("ns/my-pod")

	oldIdxKey := indexedDependency{id: configMapsUID, key: "ns/waypoint-old", typ: getKeyType}
	newIdxKey := indexedDependency{id: configMapsUID, key: "ns/waypoint-new", typ: getKeyType}

	hasOld := mc.dependencyState.indexedDependencies[oldIdxKey].Contains(podKey)
	hasNew := mc.dependencyState.indexedDependencies[newIdxKey].Contains(podKey)
	mc.mu.RUnlock()

	if hasOld {
		t.Fatalf("stale reverse-index entry for 'ns/waypoint-old' still present after relabeling pod to waypoint-new")
	}
	if !hasNew {
		t.Fatalf("expected reverse-index entry for 'ns/waypoint-new' after relabeling, but not found")
	}

	// Also verify after pod deletion nothing remains
	pc.Delete("my-pod", "ns")
	tt.WaitOrdered("delete/ns/my-pod")
	assert.EventuallyEqual(t, Bindings.List, nil)

	mc.mu.RLock()
	hasNewAfterDelete := mc.dependencyState.indexedDependencies[newIdxKey].Contains(podKey)
	mc.mu.RUnlock()

	if hasNewAfterDelete {
		t.Fatalf("reverse-index entry for 'ns/waypoint-new' still present after pod deletion")
	}
}
