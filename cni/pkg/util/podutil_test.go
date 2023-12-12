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

package util

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/util/assert"
)

func TestGetPodIPIfPodIPPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: "derp",
		},
		Status: corev1.PodStatus{
			PodIP: "11.1.1.12",
		},
	}

	podIPs := GetPodIPsIfPresent(pod)
	assert.Equal(t, len(podIPs), 1)
}

func TestGetPodIPsIfPodIPPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: "derp",
		},
		Status: corev1.PodStatus{
			PodIP:  "2.2.2.2",
			PodIPs: []corev1.PodIP{{IP: "2.2.2.2"}, {IP: "3.3.3.3"}},
		},
	}

	podIPs := GetPodIPsIfPresent(pod)
	assert.Equal(t, len(podIPs), 2)
}

func TestGetPodIPsIfNoPodIPPresent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			NodeName: "derp",
		},
		Status: corev1.PodStatus{},
	}

	podIPs := GetPodIPsIfPresent(pod)
	assert.Equal(t, len(podIPs), 0)
}
