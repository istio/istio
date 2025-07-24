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
	"testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestNestedJoin2WithMergeSimpleCollection(t *testing.T) {
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
	MultiServices := krt.NewStaticCollection(
		nil,
		[]krt.Collection[SimpleService]{SimpleServices},
		opts.WithName("MultiServices")...,
	)

	AllServices := krt.NestedJoinWithMergeCollection(
		MultiServices,
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
	// These are added very close to one another, so
	// from the perspective of the collection, there's just one add event
	MultiServices.UpdateObject(SimpleServices)
	MultiServices.UpdateObject(SimpleServices2)
	tt.WaitOrdered("add/namespace/svc")
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
	})
}

func TestNestedJoin2WithMergeAndIndexSimpleCollection(t *testing.T) {
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
	MultiServices := krt.NewStaticCollection(
		nil,
		[]krt.Collection[SimpleService]{SimpleServices},
		opts.WithName("MultiServices")...,
	)

	AllServices := krt.NestedJoinWithMergeCollection(
		MultiServices,
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
		log.Infof("Creating NamespaceIPs for %s: %v", i.Key, ni.IPs)
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
