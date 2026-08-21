// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package krt_test

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNestedJoinWithMergeSimpleCollection(t *testing.T) {
	opts := testOptions(t)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "foo"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
			ClusterIP: "1.2.3.4",
		},
	}

	c := kube.NewFakeClient(svc)
	services1 := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	SimpleServices := krt.NewCollection(services1, func(ctx krt.HandlerContext, o *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    Named{o.Namespace, o.Name},
			Selector: o.Spec.Selector,
		}
	}, opts.WithName("SimpleServices")...)
	MultiServices := krt.NewMutableCollection(
		nil,
		[]krt.Collection[SimpleService]{SimpleServices},
		opts.WithName("MultiServices")...,
	)

	AllServices := krt.NestedJoinWithMergeCollection(
		MultiServices.AsCollection(),
		func(ts []SimpleService) *SimpleService {
			if len(ts) == 0 {
				return nil
			}

			simpleService := SimpleService{
				Named:    ts[0].Named,
				Selector: maps.Clone(ts[0].Selector),
			}

			for i, t := range ts {
				if i == 0 {
					continue
				}
				// SimpleService values always take precedence
				newSelector := maps.MergeCopy(t.Selector, simpleService.Selector)
				simpleService.Selector = newSelector
			}

			// For the purposes of this test, the "app" label should always
			// be set to "foo" if it exists
			if _, ok := simpleService.Selector["app"]; ok {
				simpleService.Selector["app"] = "foo"
			}

			return &simpleService
		},
		opts.With(
			krt.WithName("AllServices"),
		)...,
	)
	tt := assert.NewTracker[string](t)
	AllServices.RegisterBatch(BatchedTrackerHandler[SimpleService](tt), true)

	c.RunAndWait(opts.Stop())
	tt.WaitOrdered("add/namespace/svc")

	assert.EventuallyEqual(t, func() bool {
		return AllServices.WaitUntilSynced(opts.Stop())
	}, true)

	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "bar", "version": "v1"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
			ClusterIP: "1.2.3.4",
		},
	}

	c2 := kube.NewFakeClient(svc2)
	services2 := krt.NewInformer[*corev1.Service](c2, opts.WithName("Services")...)
	SimpleServices2 := krt.NewCollection(services2, func(ctx krt.HandlerContext, o *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    Named{o.Namespace, o.Name},
			Selector: o.Spec.Selector,
		}
	}, opts.WithName("SimpleServices2")...)
	c2.RunAndWait(opts.Stop())

	MultiServices.UpdateObject(SimpleServices2)

	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
	})

	// Have to wait a bit for the events to propagate due to client syncing
	// But what we want is the original add and then an update because the
	// merged value changed
	tt.WaitOrdered("update/namespace/svc")

	// Now delete one of the collections
	MultiServices.DeleteObject(krt.GetKey(SimpleServices2))
	// This should be another update event, not a delete event
	tt.WaitOrdered("update/namespace/svc")
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	},
		&SimpleService{
			Named:    Named{"namespace", "svc"},
			Selector: map[string]string{"app": "foo"},
		},
	)

	// Now delete the other collection; this should be a delete event
	MultiServices.DeleteObject(krt.GetKey(SimpleServices))
	tt.WaitOrdered("delete/namespace/svc")
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, nil)

	// Now add the two collections back
	MultiServices.UpdateObject(SimpleServices)
	tt.WaitOrdered("add/namespace/svc")
	MultiServices.UpdateObject(SimpleServices2)
	tt.WaitOrdered("update/namespace/svc")
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
	})
}

// testCluster stands in for the per-cluster state in multicluster: the name is stable for the
// lifetime of the cluster, while the generation changes every time its credentials are rotated and
// its collections are rebuilt on a new client.
type testCluster struct {
	name string
	gen  int
	// service is the single service the cluster holds, and version the value of its "app" selector.
	service string
	version string
}

func (c testCluster) ResourceName() string {
	return c.name
}

// TestNestedJoinWithMergeCollectionSwap checks how the collections held by a container collection are
// keyed: a collection with a new name is added alongside the existing ones, while one replacing another
// under the same name is an update of a single entry rather than a delete plus an add. The items of the
// collections involved are never deleted along the way. Replacing a collection under the same name is
// what a credential rotation looks like: the same cluster, but new informers and new collections built
// on top of them.
func TestNestedJoinWithMergeCollectionSwap(t *testing.T) {
	opts := testOptions(t)

	clusters := krt.NewMutableCollection(nil, []testCluster{{name: "c0", gen: 1, service: "a", version: "v1"}},
		opts.WithName("Clusters")...)
	perCluster := krt.NewCollection(clusters.AsCollection(), func(ctx krt.HandlerContext, c testCluster) *krt.Collection[SimpleService] {
		// The collection's name depends only on the cluster, not on its generation, so a rebuilt
		// collection replaces the previous one under the same key. This mirrors how the per-cluster
		// informers are named in multicluster.
		col := krt.NewStaticCollection(nil, []SimpleService{{
			Named:    Named{"namespace", c.service},
			Selector: map[string]string{"app": c.version},
		}}, opts.With(krt.WithName(fmt.Sprintf("Services[%s]", c.name)))...)
		return &col
	}, opts.WithName("PerClusterServices")...)
	// Every service here is held by a single cluster, so there is nothing to actually merge.
	merged := krt.NestedJoinWithMergeCollection(perCluster, func(ts []SimpleService) *SimpleService {
		if len(ts) == 0 {
			return nil
		}
		return &ts[0]
	}, opts.With(krt.WithName("MergedServices"))...)

	// containerEvents tracks the collections held by perCluster, mergedEvents the merged items.
	containerEvents, mergedEvents := assert.NewTracker[string](t), assert.NewTracker[string](t)
	perCluster.RegisterBatch(BatchedTrackerHandler[krt.Collection[SimpleService]](containerEvents), true)
	merged.RegisterBatch(BatchedTrackerHandler[SimpleService](mergedEvents), true)
	assert.EventuallyEqual(t, func() bool {
		return merged.WaitUntilSynced(opts.Stop())
	}, true)

	collectionNames := func() []string {
		return slices.Sort(slices.Map(perCluster.List(), krt.GetKey[krt.Collection[SimpleService]]))
	}
	version := func(service string) string {
		if svc := merged.GetKey("namespace/" + service); svc != nil {
			return svc.Selector["app"]
		}
		return ""
	}

	containerEvents.WaitOrdered("add/Services[c0]")
	mergedEvents.WaitOrdered("add/namespace/a")

	// A new name is a new entry: the collection we already had is kept, and only the new item is
	// reported.
	clusters.UpdateObject(testCluster{name: "c1", gen: 1, service: "b", version: "v1"})
	containerEvents.WaitOrdered("add/Services[c1]")
	mergedEvents.WaitOrdered("add/namespace/b")
	assert.EventuallyEqual(t, collectionNames, []string{"Services[c0]", "Services[c1]"})

	// Rebuild c0's collection with new contents. The container holds one entry per cluster before and
	// after, and reports the swap as an update of that entry; the item is updated to the value held by
	// the new collection, never deleted and re-added.
	clusters.UpdateObject(testCluster{name: "c0", gen: 2, service: "a", version: "v2"})
	containerEvents.WaitOrdered("update/Services[c0]")
	mergedEvents.WaitOrdered("update/namespace/a")
	assert.EventuallyEqual(t, collectionNames, []string{"Services[c0]", "Services[c1]"})
	assert.Equal(t, version("a"), "v2")
	assert.Equal(t, version("b"), "v1")

	// A rotation that leaves the cluster's contents unchanged, which is the common case, must not report
	// anything at all. "No event" cannot be asserted by looking at an empty tracker right after the
	// swap, so we follow it with a rotation that does change the contents: any spurious event from the
	// first swap would show up before that update.
	clusters.UpdateObject(testCluster{name: "c0", gen: 3, service: "a", version: "v2"})
	containerEvents.WaitOrdered("update/Services[c0]")

	clusters.UpdateObject(testCluster{name: "c0", gen: 4, service: "a", version: "v3"})
	containerEvents.WaitOrdered("update/Services[c0]")
	mergedEvents.WaitOrdered("update/namespace/a")
	assert.Equal(t, version("a"), "v3")

	// Removing the cluster is still a delete, both of its collection and of the items only it held.
	clusters.DeleteObject(krt.GetKey(testCluster{name: "c0"}))
	containerEvents.WaitOrdered("delete/Services[c0]")
	mergedEvents.WaitOrdered("delete/namespace/a")
	assert.EventuallyEqual(t, collectionNames, []string{"Services[c1]"})
}

func TestNestedJoinWithMergeAndIndexSimpleCollection(t *testing.T) {
	opts := testOptions(t)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "foo"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
			ClusterIP: "1.2.3.4",
		},
	}

	c := kube.NewFakeClient(svc)
	services1 := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	SimpleServices := krt.NewCollection(services1, func(ctx krt.HandlerContext, o *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    Named{o.Namespace, o.Name},
			Selector: o.Spec.Selector,
			IP:       o.Spec.ClusterIP,
		}
	}, opts.WithName("SimpleServices")...)
	MultiServices := krt.NewMutableCollection(
		nil,
		[]krt.Collection[SimpleService]{SimpleServices},
		opts.WithName("MultiServices")...,
	)

	AllServices := krt.NestedJoinWithMergeCollection(
		MultiServices.AsCollection(),
		func(ts []SimpleService) *SimpleService {
			if len(ts) == 0 {
				return nil
			}

			simpleService := SimpleService{
				Named:    ts[0].Named,
				Selector: maps.Clone(ts[0].Selector),
				IP:       ts[0].IP,
			}

			for i, t := range ts {
				if i == 0 {
					continue
				}
				// SimpleService values always take precedence
				newSelector := maps.MergeCopy(t.Selector, simpleService.Selector)
				simpleService.Selector = newSelector
			}

			// For the purposes of this test, the "app" label should always
			// be set to "foo" if it exists
			if _, ok := simpleService.Selector["app"]; ok {
				simpleService.Selector["app"] = "foo"
			}

			return &simpleService
		},
		opts.With(
			krt.WithName("AllServices"),
		)...,
	)

	// Now create an index of namespaces on this merged collection
	ServiceNamespaces := krt.NewNamespaceIndex(AllServices)

	IPsForNamespace := krt.NewCollection(ServiceNamespaces.AsCollection(
		opts.WithName("ServicesByNamespace")...,
	), func(ctx krt.HandlerContext, i krt.IndexObject[string, SimpleService]) *NamespaceIPs {
		ni := &NamespaceIPs{
			Namespace: i.Key,
			IPs:       slices.Sort(slices.Map(i.Objects, func(s SimpleService) string { return s.IP })),
		}
		return ni
	}, opts.WithName("NamespaceIPs")...)
	tt := assert.NewTracker[string](t)
	IPsForNamespace.RegisterBatch(BatchedTrackerHandler[NamespaceIPs](tt), true)

	c.RunAndWait(opts.Stop())

	assert.EventuallyEqual(t, func() bool {
		return AllServices.WaitUntilSynced(opts.Stop()) && IPsForNamespace.WaitUntilSynced(opts.Stop())
	}, true)
	tt.WaitOrdered("add/namespace")

	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc2",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "foo", "version": "v1"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
			ClusterIP: "1.2.3.5",
		},
	}

	svcDup := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "bar", "version": "v1"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
			ClusterIP: "1.2.3.4", // Duplicate IP to test merging of labels
		},
	}

	c2 := kube.NewFakeClient(svc2, svcDup)
	services2 := krt.NewInformer[*corev1.Service](c2, opts.WithName("Services")...)
	SimpleServices2 := krt.NewCollection(services2, func(ctx krt.HandlerContext, o *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    Named{o.Namespace, o.Name},
			Selector: o.Spec.Selector,
			IP:       o.Spec.ClusterIP,
		}
	}, opts.WithName("SimpleServices2")...)
	c2.RunAndWait(opts.Stop())

	MultiServices.UpdateObject(SimpleServices2)

	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
		IP:       "1.2.3.4",
	})

	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc2")
	}, &SimpleService{
		Named:    Named{"namespace", "svc2"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
		IP:       "1.2.3.5",
	})

	tt.WaitOrdered("update/namespace")
}
