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

package kubernetesenv

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func TestClusterInfoCache_Pod(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test",
			},
			Status: v1.PodStatus{PodIP: "10.1.10.1"},
		},
	)
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"found", "default/test", true},
		{"not found", "custom/missing", false},
		{"by IP", "10.1.10.1", true},
		{"by wrong IP", "10.1.10.2", false},
	}

	for _, v := range tests {
		t.Run(v.name, func(tt *testing.T) {
			stopCh := make(chan struct{})
			c := newCacheController(clientset, 0, test.NewEnv(t), stopCh)
			defer close(stopCh)
			go c.Run(stopCh)
			if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
				tt.Fatal("Failed to sync")
			}

			_, got := c.Pod(v.key)
			if got != v.want {
				tt.Errorf("GetPod() => (_, %t), wanted (_, %t)", got, v.want)
			}
		})
	}
}

func TestClusterInfoCache_MultiplePods(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         "foo",
				Name:              "bar",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
			Status: v1.PodStatus{PodIP: "10.1.10.3", Phase: v1.PodSucceeded},
		},
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         "baz",
				Name:              "quux",
				CreationTimestamp: metav1.Now(),
			},
			Status: v1.PodStatus{PodIP: "10.1.10.3", Phase: v1.PodRunning},
		},
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         "alpha",
				Name:              "beta",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
			},
			Status: v1.PodStatus{PodIP: "10.1.10.3", Phase: v1.PodSucceeded},
		},
	)

	stopCh := make(chan struct{})
	c := newCacheController(clientset, 0, test.NewEnv(t), stopCh)
	defer close(stopCh)
	go c.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
		t.Fatal("Failed to sync")
	}

	pod, found := c.Pod("10.1.10.3")
	if !found {
		t.Errorf("Expected the pod to be found")
	}
	if pod.Namespace != "baz" && pod.Name != "quux" {
		t.Errorf("Expected pod baz/quux to be returned, found %s/%s", pod.Namespace, pod.Name)
	}
}

func TestClusterInfoCache_Workload_ReplicationController(t *testing.T) {
	controller := true
	clientset := fake.NewSimpleClientset(
		&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-pod",
				OwnerReferences: []metav1.OwnerReference{{
					Controller: &controller,
					Kind:       "ReplicationController",
					Name:       "test-rc",
				}},
			},
			Status: v1.PodStatus{PodIP: "10.1.10.1"},
		},
		&v1.ReplicationController{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-rc",
				OwnerReferences: []metav1.OwnerReference{{
					Controller: &controller,
					Kind:       "DeploymentConfig",
					Name:       "test-dc",
				}},
			},
		},
	)

	tests := []struct {
		name     string
		pod      string
		workload string
	}{
		{"Workload from ReplicationController", "default/test-pod", "test-dc"},
	}

	for _, v := range tests {
		t.Run(v.name, func(tt *testing.T) {
			stopCh := make(chan struct{})
			c := newCacheController(clientset, 0, test.NewEnv(t), stopCh)
			defer close(stopCh)
			go c.Run(stopCh)
			if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
				tt.Fatal("Failed to sync")
			}
			pod, _ := c.Pod(v.pod)
			workload := c.Workload(pod)
			if workload.name != v.workload {
				tt.Errorf("GetWorkload() => (_, %s), wanted (_, %s)", workload.name, v.workload)
			}
		})
	}
}
