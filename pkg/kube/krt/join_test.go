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

package krt_test

import (
	"testing"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	istio "istio.io/api/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestJoinCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := krt.NewStatic[Named](nil, true)
	c2 := krt.NewStatic[Named](nil, true)
	c3 := krt.NewStatic[Named](nil, true)
	j := krt.JoinCollection(
		[]krt.Collection[Named]{c1.AsCollection(), c2.AsCollection(), c3.AsCollection()},
		opts.WithName("Join")...,
	)
	last := atomic.NewString("")
	j.Register(func(o krt.Event[Named]) {
		last.Store(o.Latest().ResourceName())
	})
	assert.EventuallyEqual(t, last.Load, "")
	c1.Set(&Named{"c1", "a"})
	assert.EventuallyEqual(t, last.Load, "c1/a")

	c2.Set(&Named{"c2", "a"})
	assert.EventuallyEqual(t, last.Load, "c2/a")

	c3.Set(&Named{"c3", "a"})
	assert.EventuallyEqual(t, last.Load, "c3/a")

	c1.Set(&Named{"c1", "b"})
	assert.EventuallyEqual(t, last.Load, "c1/b")
	// ordered by c1, c2, c3
	sortf := func(a Named) string {
		return a.ResourceName()
	}
	assert.Equal(
		t,
		slices.SortBy(j.List(), sortf),
		slices.SortBy([]Named{
			{"c1", "b"},
			{"c2", "a"},
			{"c3", "a"},
		}, sortf),
	)
}

func TestCollectionJoin(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	services := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	serviceEntries := krt.NewInformer[*istioclient.ServiceEntry](c, opts.WithName("ServiceEntrys")...)
	c.RunAndWait(stop)
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))
	sec := clienttest.Wrap(t, kclient.New[*istioclient.ServiceEntry](c))
	SimplePods := SimplePodCollection(pods, opts)
	ExtraSimplePods := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name-static"},
		Labeled: Labeled{map[string]string{"app": "foo"}},
		IP:      "9.9.9.9",
	}, true)
	SimpleServices := SimpleServiceCollection(services, opts)
	SimpleServiceEntries := SimpleServiceCollectionFromEntries(serviceEntries, opts)
	AllServices := krt.JoinCollection(
		[]krt.Collection[SimpleService]{SimpleServices, SimpleServiceEntries},
		opts.WithName("AllServices")...,
	)
	AllPods := krt.JoinCollection(
		[]krt.Collection[SimplePod]{SimplePods, ExtraSimplePods.AsCollection()},
		opts.WithName("AllPods")...,
	)
	SimpleEndpoints := SimpleEndpointsCollection(AllPods, AllServices, opts)

	fetch := func() []SimpleEndpoint {
		return slices.SortBy(SimpleEndpoints.List(), func(s SimpleEndpoint) string { return s.ResourceName() })
	}

	assert.Equal(t, fetch(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
	}
	pc.Create(pod)
	assert.Equal(t, fetch(), nil)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
	}
	sc.Create(svc)

	pod.Status = corev1.PodStatus{PodIP: "1.2.3.4"}
	pc.UpdateStatus(pod)

	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"},
		{"name-static", svc.Name, pod.Namespace, "9.9.9.9"},
	})

	ExtraSimplePods.Set(nil)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"},
	})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.5"}})

	pc.Delete(pod.Name, pod.Namespace)
	assert.EventuallyEqual(t, fetch, nil)

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "2.3.4.5"},
	}
	pc.CreateOrUpdateStatus(pod)
	pc.CreateOrUpdateStatus(pod2)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, svc.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, svc.Name, pod2.Namespace, pod2.Status.PodIP},
	})

	se := &istioclient.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-entry",
			Namespace: "namespace",
		},
		Spec: istio.ServiceEntry{WorkloadSelector: &istio.WorkloadSelector{Labels: map[string]string{"app": "foo"}}},
	}
	sec.Create(se)
	assert.EventuallyEqual(t, fetch, []SimpleEndpoint{
		{pod.Name, se.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, se.Name, pod2.Namespace, pod2.Status.PodIP},
		{pod.Name, svc.Name, pod.Namespace, pod.Status.PodIP},
		{pod2.Name, svc.Name, pod2.Namespace, pod2.Status.PodIP},
	})
}

func TestCollectionJoinSync(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	})
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)
	ExtraSimplePods := krt.NewStatic(&SimplePod{
		Named:   Named{"namespace", "name-static"},
		Labeled: Labeled{map[string]string{"app": "foo"}},
		IP:      "9.9.9.9",
	}, true)
	AllPods := krt.JoinCollection(
		[]krt.Collection[SimplePod]{SimplePods, ExtraSimplePods.AsCollection()},
		opts.WithName("AllPods")...,
	)
	assert.Equal(t, AllPods.WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper
	assert.Equal(t, fetcherSorted(AllPods)(), []SimplePod{
		{Named{"namespace", "name"}, NewLabeled(map[string]string{"app": "foo"}), "1.2.3.4"},
		{Named{"namespace", "name-static"}, NewLabeled(map[string]string{"app": "foo"}), "9.9.9.9"},
	})
}

func TestJoinWithMergeCollection(t *testing.T) {
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

	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"version": "v1"},
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
	c.RunAndWait(opts.Stop())

	c2 := kube.NewFakeClient(svc2)
	services2 := krt.NewInformer[*corev1.Service](c2, opts.WithName("Services")...)
	SimpleServices2 := krt.NewCollection(services2, func(ctx krt.HandlerContext, o *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    Named{o.Namespace, o.Name},
			Selector: o.Spec.Selector,
		}
	}, opts.WithName("SimpleServices2")...)
	c2.RunAndWait(opts.Stop())

	tt := assert.NewTracker[string](t)

	AllServices := krt.JoinWithMergeCollection(
		[]krt.Collection[SimpleService]{SimpleServices, SimpleServices2},
		func(ts []SimpleService) *SimpleService {
			if len(ts) == 0 {
				return nil
			}

			simpleService := ts[0]

			for i, t := range ts {
				if i == 0 {
					continue
				}
				// Existing labels take precedence
				newSelector := maps.MergeCopy(t.Selector, simpleService.Selector)
				simpleService.Selector = newSelector
			}

			return &simpleService
		},
		opts.With(
			krt.WithName("AllServices"),
		)...,
	)
	AllServices.Register(TrackerHandler[SimpleService](tt))
	assert.EventuallyEqual(t, func() bool {
		return AllServices.WaitUntilSynced(opts.Stop())
	}, true)

	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v1"},
	})

	// We should see one add event and then an update event since one collection's add
	// will be handled first (the add) and then the other one will be handled (the update)
	tt.WaitOrdered("add/namespace/svc", "update/namespace/svc")

	// Update the service selector
	svc2.Spec.Selector = map[string]string{"version": "v2"}
	_, err := c2.Kube().CoreV1().Services(svc2.Namespace).Update(t.Context(), svc2, metav1.UpdateOptions{})
	assert.NoError(t, err)
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo", "version": "v2"},
	})
	tt.WaitOrdered("update/namespace/svc")

	// Delete the second service
	err = c2.Kube().CoreV1().Services(svc2.Namespace).Delete(t.Context(), svc2.Name, metav1.DeleteOptions{})
	assert.NoError(t, err)
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo"},
	})
	// Removal looks like update with a merge
	tt.WaitOrdered("update/namespace/svc")

	// Delete the last service
	err = c.Kube().CoreV1().Services(svc.Namespace).Delete(t.Context(), svc.Name, metav1.DeleteOptions{})
	assert.NoError(t, err)
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	},
		nil,
	)
	tt.WaitOrdered("delete/namespace/svc")

	// Add the service back to confirm that we get a new add event
	_, err = c.Kube().CoreV1().Services(svc.Namespace).Create(t.Context(), svc, metav1.CreateOptions{})
	assert.NoError(t, err)
	assert.EventuallyEqual(t, func() *SimpleService {
		return AllServices.GetKey("namespace/svc")
	}, &SimpleService{
		Named:    Named{"namespace", "svc"},
		Selector: map[string]string{"app": "foo"},
	})

	// Should be a fresh add
	tt.WaitOrdered("add/namespace/svc")
}
