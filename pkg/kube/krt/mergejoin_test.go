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
	"context"
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

func TestJoinWithMergeSimpleCollection(t *testing.T) {
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

	allServices := func(cs ...krt.Collection[SimpleService]) krt.Collection[SimpleService] {
		return krt.JoinWithMergeCollection(
			cs,
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
	}
	AllServices := allServices(SimpleServices)
	tt := assert.NewTracker[string](t)
	AllServices.RegisterBatch(BatchedTrackerHandler[SimpleService](tt), true)

	c.RunAndWait(opts.Stop())

	assert.EventuallyEqual(t, func() bool {
		return AllServices.WaitUntilSynced(opts.Stop())
	}, true)

	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo"},
	})

	tt.WaitOrdered("add/namespace/svc")

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

	AllServices = allServices(SimpleServices, SimpleServices2)
	tt = assert.NewTracker[string](t)
	AllServices.RegisterBatch(BatchedTrackerHandler[SimpleService](tt), true)

	assert.EventuallyEqual(t, func() bool {
		return AllServices.WaitUntilSynced(opts.Stop())
	}, true)

	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
	})

	tt.WaitOrdered("add/namespace/svc")
}

func TestJoinWithMergeAndIndexSimpleCollection(t *testing.T) {
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

	allServices := func(cs ...krt.Collection[SimpleService]) krt.Collection[SimpleService] {
		return krt.JoinWithMergeCollection(
			cs,
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
	}
	AllServices := allServices(SimpleServices)
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

	assert.EventuallyEqual(t, func() *NamespaceIPs {
		return IPsForNamespace.GetKey("namespace")
	}, &NamespaceIPs{
		Namespace: "namespace",
		IPs:       []string{"1.2.3.4"},
	})

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

	assert.EventuallyEqual(t, func() bool {
		return AllServices.WaitUntilSynced(opts.Stop()) && IPsForNamespace.WaitUntilSynced(opts.Stop())
	}, true)

	AllServices = allServices(SimpleServices, SimpleServices2)
	ServiceNamespaces = krt.NewNamespaceIndex(AllServices)

	IPsForNamespace = krt.NewCollection(ServiceNamespaces.AsCollection(
		opts.WithName("ServicesByNamespace")...,
	), func(ctx krt.HandlerContext, i krt.IndexObject[string, SimpleService]) *NamespaceIPs {
		ni := &NamespaceIPs{
			Namespace: i.Key,
			IPs:       slices.Sort(slices.Map(i.Objects, func(s SimpleService) string { return s.IP })),
		}
		return ni
	}, opts.WithName("NamespaceIPs")...)
	tt = assert.NewTracker[string](t)
	IPsForNamespace.RegisterBatch(BatchedTrackerHandler[NamespaceIPs](tt), true)

	assert.EventuallyEqual(t, func() *NamespaceIPs {
		return IPsForNamespace.GetKey("namespace")
	}, &NamespaceIPs{
		Namespace: "namespace",
		IPs:       []string{"1.2.3.4", "1.2.3.5"},
	})

	tt.WaitOrdered("add/namespace")

	c.Kube().CoreV1().Services("namespace").Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc3",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "1.2.3.6",
		},
	}, metav1.CreateOptions{})

	assert.EventuallyEqual(t, func() *NamespaceIPs {
		return IPsForNamespace.GetKey("namespace")
	}, &NamespaceIPs{
		Namespace: "namespace",
		IPs:       []string{"1.2.3.4", "1.2.3.5", "1.2.3.6"},
	})

	tt.WaitOrdered("update/namespace")
}
