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
	"context"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/labels"
)

// Prepare k8s. This can be used in multiple tests, to
// avoid duplicating creation, which can be tricky. It can be used with the fake or
// standalone apiserver.
func initTestEnv(t *testing.T, ki kubernetes.Interface, fx *FakeXdsUpdater) {
	cleanup(ki)
	for _, n := range []string{"nsa", "nsb"} {
		_, err := ki.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: n,
				Labels: map[string]string{
					"istio-injection": "enabled",
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed creating test namespace: %v", err)
		}

		// K8S 1.10 also checks if service account exists
		_, err = ki.CoreV1().ServiceAccounts(n).Create(context.TODO(), &v1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
				Annotations: map[string]string{
					"kubernetes.io/enforce-mountable-secrets": "false",
				},
			},
			Secrets: []v1.ObjectReference{
				{
					Name: "default-token-2",
					UID:  "1",
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed creating test service account: %v", err)
		}

		_, err = ki.CoreV1().Secrets(n).Create(context.TODO(), &v1.Secret{
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
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed creating test secret: %v", err)
		}
	}
	fx.Clear()
}

func cleanup(ki kubernetes.Interface) {
	for _, n := range []string{"nsa", "nsb"} {
		n := n
		pods, err := ki.CoreV1().Pods(n).List(context.TODO(), metav1.ListOptions{})
		if err == nil {
			// Make sure the pods don't exist
			for _, pod := range pods.Items {
				_ = ki.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			}
		}
	}
}

func TestPodCache(t *testing.T) {
	t.Run("fakeApiserver", func(t *testing.T) {
		t.Parallel()
		testPodCache(t)
	})
}

// Regression test for https://github.com/istio/istio/issues/20676
func TestIPReuse(t *testing.T) {
	c, fx := NewFakeControllerWithOptions(FakeControllerOptions{Mode: EndpointsOnly})
	defer c.Stop()
	initTestEnv(t, c.client, fx)

	createPod(t, c, "128.0.0.1", "pod")
	if p, f := c.pods.getPodKey("128.0.0.1"); !f || p != "ns/pod" {
		t.Fatalf("unexpected pod: %v", p)
	}

	// Change the pod IP. This can happen if the pod moves to another node, for example.
	createPod(t, c, "128.0.0.2", "pod")
	if p, f := c.pods.getPodKey("128.0.0.2"); !f || p != "ns/pod" {
		t.Fatalf("unexpected pod: %v", p)
	}
	if p, f := c.pods.getPodKey("128.0.0.1"); f {
		t.Fatalf("expected no pod, got pod: %v", p)
	}

	// A new pod is created with the old IP. We should get new-pod, not pod
	createPod(t, c, "128.0.0.1", "new-pod")
	if p, f := c.pods.getPodKey("128.0.0.1"); !f || p != "ns/new-pod" {
		t.Fatalf("unexpected pod: %v", p)
	}

	// A new pod is created with the same IP. In theory this should never happen, but maybe we miss an update somehow.
	createPod(t, c, "128.0.0.1", "another-pod")
	if p, f := c.pods.getPodKey("128.0.0.1"); !f || p != "ns/another-pod" {
		t.Fatalf("unexpected pod: %v", p)
	}

	err := c.client.CoreV1().Pods("ns").Delete(context.TODO(), "another-pod", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Cannot delete pod: %v", err)
	}
	if err := wait.Poll(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		if _, ok := c.pods.getPodKey("128.0.0.1"); ok {
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
}

func createPod(t *testing.T, c *FakeController, ip, name string) {
	addPods(t, c, generatePod(ip, name, "ns", "1", "", map[string]string{}, map[string]string{}))
	if err := waitForPod(c, ip); err != nil {
		t.Fatal(err)
	}
}

func waitForPod(c *FakeController, ip string) error {
	return wait.Poll(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		c.pods.RLock()
		defer c.pods.RUnlock()
		if _, ok := c.pods.podsByIP[ip]; ok {
			return true, nil
		}
		return false, nil
	})
}

func testPodCache(t *testing.T) {
	c, fx := NewFakeControllerWithOptions(FakeControllerOptions{
		Mode:              EndpointsOnly,
		WatchedNamespaces: "nsa,nsb",
	})
	defer c.Stop()

	initTestEnv(t, c.client, fx)

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
		_ = waitForPod(c, pod.Status.PodIP)
	}

	// Verify podCache
	wantLabels := map[string]labels.Instance{
		"128.0.0.1": {"app": "test-app"},
		"128.0.0.2": {"app": "prod-app-1"},
		"128.0.0.3": {"app": "prod-app-2"},
	}
	for addr, wantTag := range wantLabels {
		pod := c.pods.getPodByIP(addr)
		if pod == nil {
			t.Error("Not found ", addr)
			continue
		}
		if !reflect.DeepEqual(wantTag, labels.Instance(pod.Labels)) {
			t.Errorf("Expected %v got %v", wantTag, labels.Instance(pod.Labels))
		}
	}

	// This pod exists, but should not be in the cache because it is in a
	// namespace not watched by the controller.
	pod := c.pods.getPodByIP("128.0.0.4")
	if pod != nil {
		t.Error("Expected not found but was found")
	}

	// This pod should not be in the cache because it never existed.
	pod = c.pods.getPodByIP("128.0.0.128")
	if pod != nil {
		t.Error("Expected not found but was found")
	}
}

// Checks that events from the watcher create the proper internal structures
func TestPodCacheEvents(t *testing.T) {
	t.Parallel()
	c, fx := NewFakeControllerWithOptions(FakeControllerOptions{Mode: EndpointsOnly})
	defer c.Stop()

	ns := "default"
	podCache := c.pods

	f := podCache.onEvent

	ip := "172.0.3.35"
	pod1 := metav1.ObjectMeta{Name: "pod1", Namespace: ns}
	if err := f(&v1.Pod{ObjectMeta: pod1}, model.EventAdd); err != nil {
		t.Error(err)
	}

	// The first time pod occur
	fx.Wait("xds")

	if err := f(&v1.Pod{ObjectMeta: pod1, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodPending}}, model.EventUpdate); err != nil {
		t.Error(err)
	}

	if pod, exists := podCache.getPodKey(ip); !exists || pod != "default/pod1" {
		t.Errorf("getPodKey => got %s, pod1 not found or incorrect", pod)
	}

	pod2 := metav1.ObjectMeta{Name: "pod2", Namespace: ns}
	if err := f(
		&v1.Pod{ObjectMeta: pod1, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodFailed}}, model.EventUpdate); err != nil {
		t.Error(err)
	}
	if err := f(&v1.Pod{ObjectMeta: pod2, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodRunning}}, model.EventAdd); err != nil {
		t.Error(err)
	}

	if pod, exists := podCache.getPodKey(ip); !exists || pod != "default/pod2" {
		t.Errorf("getPodKey => got %s, pod2 not found or incorrect", pod)
	}

	if err := f(&v1.Pod{ObjectMeta: pod1, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodFailed}}, model.EventDelete); err != nil {
		t.Error(err)
	}

	if pod, exists := podCache.getPodKey(ip); !exists || pod != "default/pod2" {
		t.Errorf("getPodKey => got %s, pod2 not found or incorrect", pod)
	}

	if err := f(&v1.Pod{ObjectMeta: pod2, Status: v1.PodStatus{PodIP: ip, Phase: v1.PodFailed}}, model.EventDelete); err != nil {
		t.Error(err)
	}

	if pod, exists := podCache.getPodKey(ip); exists {
		t.Errorf("getPodKey => got %s, want none", pod)
	}
}
