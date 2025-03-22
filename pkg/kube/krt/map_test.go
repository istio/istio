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
	"istio.io/istio/pkg/test/util/assert"
)

func TestMapCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := kube.NewFakeClient()
	kpc1 := kclient.New[*corev1.Pod](c1)
	pc1 := clienttest.Wrap(t, kpc1)
	pods := krt.WrapClient[*corev1.Pod](kpc1, opts.WithName("Pods1")...)
	c1.RunAndWait(opts.Stop())
	SimplePods := krt.MapCollection(pods, func(p *corev1.Pod) SimplePod {
		return SimplePod{
			Named: Named{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
			Labeled: Labeled{
				Labels: p.Labels,
			},
			IP: p.Status.PodIP,
		}
	}, opts.WithName("SimplePods")...)
	tt := assert.NewTracker[string](t)
	SimplePods.Register(TrackerHandler[SimplePod](tt))
	// Add a pod and make sure we get the event for it in event handlers from the mapped collection
	pc1.Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "ns1",
			Labels:    map[string]string{"a": "b"},
		},
		Status: corev1.PodStatus{
			PodIP: "1.2.3.4",
		},
	})
	tt.WaitOrdered("add/ns1/pod1")
}
