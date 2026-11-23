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

package clienttest

import (
	"net/netip"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
)

const (
	// FakeEndpointSliceManager is the managed-by label value of slices written by RunEndpointSliceController.
	FakeEndpointSliceManager = "fake-endpointslice-controller.istio.io"
	// ManualEndpointSlicesAnnotation opts a Service out of RunEndpointSliceController, for tests that write the
	// Service's EndpointSlices themselves.
	ManualEndpointSlicesAnnotation = "test.istio.io/manual-endpointslices"
)

// RunEndpointSliceController runs a minimal version of the Kubernetes EndpointSlice controller, as fake clients do not
// run controllers. For each Service with a selector, it writes one EndpointSlice per IP family containing all matching,
// non-terminal pods with an IP. Slice size limits, topology hints, and named ports are not supported.
func RunEndpointSliceController(t test.Failer, c kube.Client) {
	stop := test.NewStop(t)
	pods := kclient.New[*corev1.Pod](c)
	services := kclient.New[*corev1.Service](c)
	endpointSlices := kclient.New[*discovery.EndpointSlice](c)
	writer := kclient.NewWriteClient[*discovery.EndpointSlice](c)

	q := controllers.NewQueue("fake endpointslice controller",
		controllers.WithReconciler(func(key types.NamespacedName) error {
			return syncNamespace(key.Namespace, pods, services, endpointSlices, writer)
		}),
		controllers.WithMaxAttempts(5))
	enqueue := controllers.ObjectHandler(func(o controllers.Object) {
		q.Add(types.NamespacedName{Namespace: o.GetNamespace()})
	})
	pods.AddEventHandler(enqueue)
	services.AddEventHandler(enqueue)
	c.RunAndWait(stop)
	go q.Run(stop)
}

func syncNamespace(
	ns string,
	pods kclient.Client[*corev1.Pod],
	services kclient.Client[*corev1.Service],
	endpointSlices kclient.Client[*discovery.EndpointSlice],
	writer kclient.Writer[*discovery.EndpointSlice],
) error {
	want := map[string]*discovery.EndpointSlice{}
	for _, svc := range services.List(ns, klabels.Everything()) {
		if len(svc.Spec.Selector) == 0 || svc.Annotations[ManualEndpointSlicesAnnotation] == "true" {
			continue
		}
		byFamily := map[discovery.AddressType][]discovery.Endpoint{}
		for _, p := range pods.List(ns, klabels.SelectorFromSet(svc.Spec.Selector)) {
			if kube.CheckPodTerminal(p) || p.DeletionTimestamp != nil {
				continue
			}
			for _, ip := range podIPs(p) {
				addr, err := netip.ParseAddr(ip)
				if err != nil {
					continue
				}
				family := discovery.AddressTypeIPv4
				if addr.Is6() {
					family = discovery.AddressTypeIPv6
				}
				ready := podReady(p)
				byFamily[family] = append(byFamily[family], discovery.Endpoint{
					Addresses: []string{ip},
					Conditions: discovery.EndpointConditions{
						Ready:       ptr.Of(ready),
						Serving:     ptr.Of(ready),
						Terminating: ptr.Of(false),
					},
					NodeName: nodeName(p),
					TargetRef: &corev1.ObjectReference{
						Kind:      "Pod",
						Namespace: p.Namespace,
						Name:      p.Name,
						UID:       p.UID,
					},
				})
			}
		}
		ports := make([]discovery.EndpointPort, 0, len(svc.Spec.Ports))
		for _, sp := range svc.Spec.Ports {
			ports = append(ports, discovery.EndpointPort{
				Name:     ptr.Of(sp.Name),
				Protocol: ptr.Of(sp.Protocol),
				Port:     ptr.Of(sp.TargetPort.IntVal),
			})
		}
		for _, family := range []discovery.AddressType{discovery.AddressTypeIPv4, discovery.AddressTypeIPv6} {
			eps := byFamily[family]
			if family == discovery.AddressTypeIPv6 && len(eps) == 0 {
				continue
			}
			name := svc.Name + "-fake-" + string(family)
			want[name] = &discovery.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns,
					Labels: map[string]string{
						discovery.LabelServiceName: svc.Name,
						discovery.LabelManagedBy:   FakeEndpointSliceManager,
					},
				},
				AddressType: family,
				Endpoints:   eps,
				Ports:       ports,
			}
		}
	}
	for _, have := range endpointSlices.List(ns, klabels.SelectorFromSet(map[string]string{discovery.LabelManagedBy: FakeEndpointSliceManager})) {
		w, f := want[have.Name]
		if !f {
			// The informer cache may lag behind our own writes, so the slice may already be gone.
			if err := writer.Delete(have.Name, ns); err != nil && !kerrors.IsNotFound(err) {
				return err
			}
			continue
		}
		if reflect.DeepEqual(have.Endpoints, w.Endpoints) && reflect.DeepEqual(have.Ports, w.Ports) &&
			reflect.DeepEqual(have.Labels, w.Labels) {
			delete(want, have.Name)
		}
	}
	for _, w := range want {
		// On a conflict with a write the cache has not seen yet, return the error so the queue retries.
		if _, err := kclient.CreateOrUpdate[*discovery.EndpointSlice](writer, w); err != nil {
			return err
		}
	}
	return nil
}

func podIPs(p *corev1.Pod) []string {
	if len(p.Status.PodIPs) > 0 {
		res := make([]string, 0, len(p.Status.PodIPs))
		for _, ip := range p.Status.PodIPs {
			res = append(res, ip.IP)
		}
		return res
	}
	if p.Status.PodIP != "" {
		return []string{p.Status.PodIP}
	}
	return nil
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func nodeName(p *corev1.Pod) *string {
	if p.Spec.NodeName == "" {
		return nil
	}
	return ptr.Of(p.Spec.NodeName)
}
