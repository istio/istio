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
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pilot/pkg/model"
)

func TestPodCache(t *testing.T) {

	testCases := []struct {
		name         string
		pods         []*v1.Pod
		keys         map[string]string
		wantLabels   map[string]model.Labels
		wantNotFound bool
	}{
		{
			name: "Should find all addresses in the map",
			pods: []*v1.Pod{
				generatePod("pod1", "nsA", "", "", map[string]string{"app": "test-app"}, map[string]string{}),
				generatePod("pod2", "nsA", "", "", map[string]string{"app": "prod-app-1"}, map[string]string{}),
				generatePod("pod3", "nsB", "", "", map[string]string{"app": "prod-app-2"}, map[string]string{}),
			},
			keys: map[string]string{
				"128.0.0.1": "nsA/pod1",
				"128.0.0.2": "nsA/pod2",
				"128.0.0.3": "nsB/pod3",
			},
			wantLabels: map[string]model.Labels{
				"128.0.0.1": {"app": "test-app"},
				"128.0.0.2": {"app": "prod-app-1"},
				"128.0.0.3": {"app": "prod-app-2"},
			},
		},
		{
			name:         "Should fail if addr not in keys",
			wantLabels:   map[string]model.Labels{"128.0.0.1": nil},
			wantNotFound: true,
		},
		{
			name:         "Should fail if addr in keys but pod not in cache",
			wantLabels:   map[string]model.Labels{"128.0.0.1": nil},
			keys:         map[string]string{"128.0.0.1": "nsA/pod1"},
			wantNotFound: true,
		},
	}
	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			clientSet := fake.NewSimpleClientset()
			controller := NewController(clientSet, ControllerOptions{
				WatchedNamespace: "default",
				ResyncPeriod:     resync,
				DomainSuffix:     domainSuffix,
			})

			// Populate podCache
			for _, pod := range c.pods {
				if err := controller.pods.informer.GetStore().Add(pod); err != nil {
					t.Errorf("Cannot create %s in namespace %s (error: %v)", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, err)
				}
			}

			// Populate key
			controller.pods.keys = c.keys

			// Verify podCache
			for addr, wantTag := range c.wantLabels {
				tag, found := controller.pods.labelsByIP(addr)
				if !reflect.DeepEqual(wantTag, tag) {
					t.Errorf("Expected %v got %v", wantTag, tag)
				}
				if c.wantNotFound {
					if found {
						t.Error("Expected not found but was found")
					}
				}
			}

		})
	}

}

func TestPodCacheEvents(t *testing.T) {
	handler := &ChainHandler{}
	cache := newPodCache(cacheHandler{handler: handler}, nil)
	if len(handler.funcs) != 1 {
		t.Fatal("failed to register handlers")
	}

	f := handler.funcs[0]

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
