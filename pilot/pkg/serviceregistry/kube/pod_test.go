// Copyright 2017 Istio Authors
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

package kube

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"
)

// Prepare k8s. This can be used in multiple tests, to
// avoid duplicating creation, which can be tricky. It can be used with the fake or
// standalone apiserver.
func initTestEnv(ki kubernetes.Interface, c *Controller, fx *FakeXdsUpdater) {
	cleanup(ki, fx)
	for _, n := range []string{"nsa", "nsb"} {
		_, _ = ki.CoreV1().Namespaces().Create(&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: n,
				Labels: map[string]string{
					"istio-injection": "enabled",
				},
			},
		})

		// K8S 1.10 also checks if service account exists
		_, _ = ki.CoreV1().ServiceAccounts(n).Create(&v1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
				Annotations: map[string]string{
					"kubernetes.io/enforce-mountable-secrets": "false",
				},
			},
			Secrets: []v1.ObjectReference{
				v1.ObjectReference{
					Name: "default-token-2",
					UID:  "1",
				},
			},
		})

		_, _ = ki.CoreV1().Secrets(n).Create(&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-token-2",
				Annotations: map[string]string{
					"kubernetes.io/service-account.name": "default",
					"kubernetes.io/service-account.uid":  "1",
				},
			},
			Type: v1.SecretTypeServiceAccountToken,
			Data: map[string][]byte{
				"token": []byte("1"),
			},
		})
	}
	fx.Clear()
}

func cleanup(ki kubernetes.Interface, fx *FakeXdsUpdater) {
	for _, n := range []string{"nsa", "nsb"} {
		n := n
		pods, err := ki.CoreV1().Pods(n).List(metav1.ListOptions{})
		if err == nil {
			// Make sure the pods don't exist
			for _, pod := range pods.Items {
				err := ki.CoreV1().Pods(pod.Namespace).Delete(pod.Name, nil)
				if err == nil {
					// pod existed before, wait for event
					_ = fx.Wait("workload")
				}
			}
		}
	}
}

func TestPodCache(t *testing.T) {
	t.Run("localApiserver", func(t *testing.T) {
		c, fx := newLocalController(t)
		defer c.Stop()
		defer cleanup(c.client, fx)
		testPodCache(t, c, fx)
	})
	t.Run("fakeApiserver", func(t *testing.T) {
		t.Parallel()
		c, fx := newFakeController(t)
		defer c.Stop()
		testPodCache(t, c, fx)
	})
}

func testPodCache(t *testing.T, c *Controller, fx *FakeXdsUpdater) {
	initTestEnv(c.client, c, fx)

	// Namespace must be lowercase (nsA doesn't work)
	pods := []*v1.Pod{
		generatePod("128.0.0.1", "cpod1", "nsa", "", "", map[string]string{"app": "test-app"}, map[string]string{}),
		generatePod("128.0.0.2", "cpod2", "nsa", "", "", map[string]string{"app": "prod-app-1"}, map[string]string{}),
		generatePod("128.0.0.3", "cpod3", "nsb", "", "", map[string]string{"app": "prod-app-2"}, map[string]string{}),
	}

	for _, pod := range pods {
		pod := pod
		addPods(t, c, pod)
		// Wait for the workload event

		ev := fx.Wait("workload")
		if ev == nil {
			t.Error("No event ", pod.Name)
			continue
		}
		if ev.ID != pod.Status.PodIP {
			t.Error("Workload event expected ", pod.Status.PodIP, "got", ev.ID, ev.Type)
			continue
		}
	}

	// Verify podCache
	wantLabels := map[string]model.Labels{
		"128.0.0.1": {"app": "test-app"},
		"128.0.0.2": {"app": "prod-app-1"},
		"128.0.0.3": {"app": "prod-app-2"},
	}
	for addr, wantTag := range wantLabels {
		tag, found := c.pods.labelsByIP(addr)
		if !found {
			t.Error("Not found ", addr)
			continue
		}
		if !reflect.DeepEqual(wantTag, tag) {
			t.Errorf("Expected %v got %v", wantTag, tag)
		}
	}

	// Former 'wantNotFound' test. A pod not in the cache results in found = false
	_, found := c.pods.labelsByIP("128.0.0.4")
	if found {
		t.Error("Expected not found but was found")
	}
}

// Checks that events from the watcher create the proper internal structures
func TestPodCacheEvents(t *testing.T) {
	t.Parallel()
	handler := &ChainHandler{}
	c, _ := newFakeController(t)
	cache := newPodCache(cacheHandler{handler: handler}, c)

	f := cache.event

	ns := "default"
	ip := "172.0.3.35"
	pod1 := metav1.ObjectMeta{Name: "pod1", Namespace: ns}
	if err := f(&v1.Pod{ObjectMeta: pod1}, model.EventAdd); err != nil {
		t.Error(err)
	}
	if err := f(&v1.Pod{ObjectMeta: pod1, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodPending}}, model.EventUpdate); err != nil {
		t.Error(err)
	}

	if pod, exists := cache.getPodKey(ip); !exists || pod != "default/pod1" {
		t.Errorf("getPodKey => got %s, pod1 not found or incorrect", pod)
	}

	pod2 := metav1.ObjectMeta{Name: "pod2", Namespace: ns}
	if err := f(&v1.Pod{ObjectMeta: pod1, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodFailed}}, model.EventUpdate); err != nil {
		t.Error(err)
	}
	if err := f(&v1.Pod{ObjectMeta: pod2, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodRunning}}, model.EventAdd); err != nil {
		t.Error(err)
	}

	if pod, exists := cache.getPodKey(ip); !exists || pod != "default/pod2" {
		t.Errorf("getPodKey => got %s, pod2 not found or incorrect", pod)
	}

	if err := f(&v1.Pod{ObjectMeta: pod1, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodFailed}}, model.EventDelete); err != nil {
		t.Error(err)
	}

	if pod, exists := cache.getPodKey(ip); !exists || pod != "default/pod2" {
		t.Errorf("getPodKey => got %s, pod2 not found or incorrect", pod)
	}

	if err := f(&v1.Pod{ObjectMeta: pod2, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodFailed}}, model.EventDelete); err != nil {
		t.Error(err)
	}

	if pod, exists := cache.getPodKey(ip); exists {
		t.Errorf("getPodKey => got %s, want none", pod)
	}
}
