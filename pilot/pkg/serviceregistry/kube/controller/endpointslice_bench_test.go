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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

// Compare reconstruction plus aggregation for an 8k-pod workload selected by two
// Services. This intentionally excludes EDS diffing and push scheduling.
func BenchmarkPodEndpointRebuild(b *testing.B) {
	const podCount, sliceSize = 8000, 100
	objects := []runtime.Object{}
	pods := make([]*corev1.Pod, podCount)
	for i := range pods {
		pods[i] = generatePod([]string{fmt.Sprintf("10.0.%d.%d", i/250, i%250+1)}, fmt.Sprintf("pod-%d", i), "ns", "", "", map[string]string{"app": "test"}, nil)
		objects = append(objects, pods[i])
	}
	services := []*corev1.Service{}
	for _, name := range []string{"first", "second"} {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}, Spec: corev1.ServiceSpec{
			ClusterIP: "10.1.0.1", Selector: map[string]string{"app": "test"}, Ports: []corev1.ServicePort{{Name: "http", Port: 8080}},
		}}
		services = append(services, svc)
		objects = append(objects, svc)
		for start := 0; start < podCount; start += sliceSize {
			portName, port := "http", int32(8080)
			slice := &discovery.EndpointSlice{
				ObjectMeta:  metav1.ObjectMeta{Name: fmt.Sprintf("%s-%d", name, start), Namespace: "ns", Labels: map[string]string{discovery.LabelServiceName: name}},
				AddressType: discovery.AddressTypeIPv4, Ports: []discovery.EndpointPort{{Name: &portName, Port: &port}},
			}
			for _, pod := range pods[start : start+sliceSize] {
				slice.Endpoints = append(slice.Endpoints, discovery.Endpoint{Addresses: []string{pod.Status.PodIP}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace}})
			}
			objects = append(objects, slice)
		}
	}
	c, _ := NewFakeControllerWithOptions(b, FakeControllerOptions{Client: kubelib.NewFakeClient(objects...), SkipRun: true})
	assert.NoError(b, c.syncPods())
	converted := []*model.Service{}
	for _, svc := range services {
		assert.NoError(b, c.onServiceEvent(nil, svc, model.EventAdd))
		converted = append(converted, c.GetService(kube.ServiceHostname(svc.Name, svc.Namespace, c.opts.DomainSuffix)))
	}
	for _, svc := range converted {
		if got := len(c.buildEndpointsForService(svc, true)); got != podCount {
			b.Fatalf("expected %d endpoints, got %d", podCount, got)
		}
	}
	for _, full := range []bool{true, false} {
		b.Run(fmt.Sprintf("fullRebuild=%v", full), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, svc := range converted {
					if !full {
						c.endpoints.updateEndpointCacheForPod(svc, pods[0])
					}
					c.buildEndpointsForService(svc, full)
				}
			}
		})
	}
}
