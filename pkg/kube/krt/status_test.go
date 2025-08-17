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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

type SimpleServiceStatus struct {
	ValidName      bool
	TotalEndpoints int
}

func TestCollectionStatus(t *testing.T) {
	stop := test.NewStop(t)
	opts := testOptions(t)
	c := kube.NewFakeClient()
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	services := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	c.RunAndWait(stop)
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))
	SimplePods := SimplePodCollection(pods, opts)
	Status, SimpleEndpoints := krt.NewStatusManyCollection[*corev1.Service, SimpleServiceStatus, SimpleEndpoint](services,
		func(ctx krt.HandlerContext, svc *corev1.Service) (*SimpleServiceStatus, []SimpleEndpoint) {
			pods := krt.Fetch(ctx, SimplePods, krt.FilterLabel(svc.Spec.Selector))
			status := &SimpleServiceStatus{
				ValidName:      len(svc.Name) == 5, // arbitrary rule for tests
				TotalEndpoints: len(pods),
			}
			for _, pod := range pods {
				if pod.Labels["delete-status"] == "true" {
					// arbitrary rule for tests
					status = nil
				}
			}
			return status, slices.Map(pods, func(pod SimplePod) SimpleEndpoint {
				return SimpleEndpoint{
					Pod:       pod.Name,
					Service:   svc.Name,
					Namespace: svc.Namespace,
					IP:        pod.IP,
				}
			})
		}, opts.WithName("SimpleEndpoints")...)

	tt := assert.NewTracker[string](t)
	Status.Register(TrackerHandler[krt.ObjectWithStatus[*corev1.Service, SimpleServiceStatus]](tt))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
	}
	pc.Create(pod)
	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), nil)
	tt.Empty()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
	}
	sc.Create(svc)
	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), nil)
	tt.WaitOrdered("add/namespace/svc")
	assert.Equal(t, Status.GetKey("namespace/svc").Status, SimpleServiceStatus{TotalEndpoints: 0})

	pod.Status = corev1.PodStatus{PodIP: "1.2.3.4"}
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"}})
	tt.WaitOrdered("update/namespace/svc")
	assert.Equal(t, Status.GetKey("namespace/svc").Status, SimpleServiceStatus{TotalEndpoints: 1})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.5"}})
	// There should NOT be an event caused by the change, as there is no change to report.
	tt.Empty()
	assert.Equal(t, Status.GetKey("namespace/svc").Status, SimpleServiceStatus{TotalEndpoints: 1})

	// Remove status entirely
	pod.Labels["delete-status"] = "true"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.5"}})
	// There should NOT be an event caused by the change, as there is no change to report.
	tt.WaitOrdered("delete/namespace/svc")
	assert.Equal(t, Status.GetKey("namespace/svc"), nil)

	// add status back
	pod.Labels["delete-status"] = "false"
	pc.UpdateStatus(pod)
	tt.WaitOrdered("add/namespace/svc")
	assert.Equal(t, Status.GetKey("namespace/svc").Status, SimpleServiceStatus{TotalEndpoints: 1})

	pc.Delete(pod.Name, pod.Namespace)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), nil)

	tt.WaitOrdered("update/namespace/svc")
	assert.Equal(t, Status.GetKey("namespace/svc").Status, SimpleServiceStatus{TotalEndpoints: 0})

	sc.Delete(svc.Name, svc.Namespace)
	tt.WaitOrdered("delete/namespace/svc")
	assert.Equal(t, Status.GetKey("namespace/svc"), nil)
}
