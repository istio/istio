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
	"fmt"
	"net"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
)

type Workload struct {
	krt.Named
	ServiceNames []string
	IP           string
}

// GetLabelSelector defaults to using Reflection which is slow. Provide a specialized implementation that does it more efficiently.
type ServiceWrapper struct{ *v1.Service }

func (s ServiceWrapper) GetLabelSelector() map[string]string {
	return s.Spec.Selector
}

var _ krt.LabelSelectorer = ServiceWrapper{}

func NewModern(c kube.Client, events chan string, _ <-chan struct{}) {
	Pods := krt.NewInformer[*v1.Pod](c)
	Services := krt.NewInformer[*v1.Service](c, krt.WithObjectAugmentation(func(o any) any {
		return ServiceWrapper{o.(*v1.Service)}
	}))
	Workloads := krt.NewCollection(Pods, func(ctx krt.HandlerContext, p *v1.Pod) *Workload {
		if p.Status.PodIP == "" {
			return nil
		}
		services := krt.Fetch(ctx, Services, krt.FilterNamespace(p.Namespace), krt.FilterSelectsNonEmpty(p.GetLabels()))
		return &Workload{
			Named:        krt.NewNamed(p),
			IP:           p.Status.PodIP,
			ServiceNames: slices.Map(services, func(e *v1.Service) string { return e.Name }),
		}
	})
	Workloads.Register(func(e krt.Event[Workload]) {
		events <- fmt.Sprintf(e.Latest().Name, e.Event)
	})
}

type legacy struct {
	pods      kclient.Client[*v1.Pod]
	services  kclient.Client[*v1.Service]
	queue     controllers.Queue
	workloads map[types.NamespacedName]*Workload
	handler   func(event krt.Event[Workload])
}

func getPodServices(allServices []*v1.Service, pod *v1.Pod) []*v1.Service {
	var services []*v1.Service
	for _, service := range allServices {
		if labels.Instance(service.Spec.Selector).Match(pod.Labels) {
			services = append(services, service)
		}
	}

	return services
}

func (l *legacy) Reconcile(key types.NamespacedName) error {
	pod := l.pods.Get(key.Name, key.Namespace)
	if pod == nil || pod.Status.PodIP == "" {
		old := l.workloads[key]
		if old != nil {
			ev := krt.Event[Workload]{
				Old:   old,
				Event: controllers.EventDelete,
			}
			l.handler(ev)
			delete(l.workloads, key)
		}

		return nil
	}
	allServices := l.services.List(pod.Namespace, klabels.Everything())
	services := getPodServices(allServices, pod)
	wl := &Workload{
		Named:        krt.NewNamed(pod),
		IP:           pod.Status.PodIP,
		ServiceNames: slices.Map(services, func(e *v1.Service) string { return e.Name }),
	}
	old := l.workloads[key]
	if reflect.DeepEqual(old, wl) {
		// No changes, NOP
		return nil
	}
	// Changed. Update and call handlers
	l.workloads[key] = wl
	if old == nil {
		l.handler(krt.Event[Workload]{
			New:   wl,
			Event: controllers.EventAdd,
		})
	} else {
		l.handler(krt.Event[Workload]{
			Old:   old,
			New:   wl,
			Event: controllers.EventUpdate,
		})
	}
	return nil
}

func NewLegacy(cl kube.Client, events chan string, stop <-chan struct{}) {
	c := &legacy{
		workloads: map[types.NamespacedName]*Workload{},
	}
	c.pods = kclient.New[*v1.Pod](cl)
	c.services = kclient.New[*v1.Service](cl)
	c.queue = controllers.NewQueue("pods", controllers.WithReconciler(c.Reconcile))
	c.pods.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))
	c.services.AddEventHandler(controllers.FromEventHandler(func(e controllers.Event) {
		o := e.Latest()
		for _, pod := range c.pods.List(o.GetNamespace(), klabels.SelectorFromValidatedSet(o.(*v1.Service).Spec.Selector)) {
			c.queue.AddObject(pod)
		}
	}))
	c.handler = func(e krt.Event[Workload]) {
		events <- fmt.Sprintf(e.Latest().Name, e.Event)
	}
	go c.queue.Run(stop)
}

var nextIP = net.ParseIP("10.0.0.10")

func GetIP() string {
	i := nextIP.To4()
	ret := i.String()
	v := uint(i[0])<<24 + uint(i[1])<<16 + uint(i[2])<<8 + uint(i[3])
	v++
	v3 := byte(v & 0xFF)
	v2 := byte((v >> 8) & 0xFF)
	v1 := byte((v >> 16) & 0xFF)
	v0 := byte((v >> 24) & 0xFF)
	nextIP = net.IPv4(v0, v1, v2, v3)
	return ret
}

func drainN(c chan string, n int) {
	for n > 0 {
		n--
		<-c
	}
}

func BenchmarkControllers(b *testing.B) {
	watch.DefaultChanSize = 100_000
	initialPods := []*v1.Pod{}
	for i := 0; i < 1000; i++ {
		initialPods = append(initialPods, &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: fmt.Sprintf("ns-%d", i%2),
				Labels: map[string]string{
					"app": fmt.Sprintf("app-%d", i%25),
				},
			},
			Spec: v1.PodSpec{
				ServiceAccountName: "fake-sa",
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
				PodIP: GetIP(),
			},
		})
	}
	initialServices := []*v1.Service{}
	for i := 0; i < 50; i++ {
		initialServices = append(initialServices, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: fmt.Sprintf("ns-%d", i%2),
			},
			Spec: v1.ServiceSpec{
				Selector: map[string]string{
					"app": fmt.Sprintf("app-%d", i%25),
				},
			},
		})
	}
	benchmark := func(b *testing.B, fn func(client kube.Client, events chan string, stop <-chan struct{})) {
		c := kube.NewFakeClient()
		events := make(chan string, 1000)
		stop := test.NewStop(b)
		fn(c, events, stop)
		pods := clienttest.NewWriter[*v1.Pod](b, c)
		services := clienttest.NewWriter[*v1.Service](b, c)
		for _, p := range initialPods {
			pods.Create(p)
		}
		for _, p := range initialServices {
			services.Create(p)
		}
		b.ResetTimer()
		c.RunAndWait(test.NewStop(b))
		drainN(events, 1000)
		for n := 0; n < b.N; n++ {
			for i := 0; i < 1000; i++ {
				pods.Update(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("pod-%d", i),
						Namespace: fmt.Sprintf("ns-%d", i%2),
						Labels: map[string]string{
							"app": fmt.Sprintf("app-%d", i%25),
						},
					},
					Spec: v1.PodSpec{
						ServiceAccountName: "fake-sa",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
						PodIP: GetIP(),
					},
				})
			}
			drainN(events, 1000)
		}
	}
	b.Run("krt", func(b *testing.B) {
		benchmark(b, NewModern)
	})
	b.Run("legacy", func(b *testing.B) {
		benchmark(b, NewLegacy)
	})
}
