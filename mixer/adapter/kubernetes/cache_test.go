// Copyright 2017 Istio Authors.
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

package kubernetes

import (
	"sync"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func TestEventType_String(t *testing.T) {
	for e := addition; e <= deletion; e++ {
		if s := e.String(); s == "Unknown" {
			t.Errorf("String(%d) => Unknown; want known value", e)
		}
	}

	unknown := eventType(4)
	if unknown.String() != "Unknown" {
		t.Errorf("String(%d) => %s, want 'Unknown'", unknown, unknown)
	}
}

func newfakeInformer() *fakeInformer {
	f := &fakeInformer{}
	f.cond = sync.NewCond(f)
	return f
}

type fakeInformer struct {
	sync.Mutex
	cache.SharedInformer

	store     cache.Store
	synced    bool
	runCalled bool
	cond      *sync.Cond
}

func (f *fakeInformer) HasSynced() bool {
	f.Lock()
	defer f.Unlock()
	return f.synced
}

func (f *fakeInformer) SetSynced(synced bool) {
	f.Lock()
	defer f.Unlock()
	f.synced = synced
}

func (f *fakeInformer) GetStore() cache.Store { return f.store }

func (f *fakeInformer) Run(<-chan struct{}) {
	f.Lock()
	defer f.Unlock()
	f.cond.Broadcast()
	f.runCalled = true
}

// timeout is to ensure that racetest does not fail before giving it some time.
// ?   	istio.io/istio/mixer/adapter/ipListChecker/config	[no test files]
// --- FAIL: TestClusterInfoCache_RunLoggerStopInSelect (0.00s)
func (f *fakeInformer) RunCalled(timeout time.Duration) bool {
	f.Lock()
	defer f.Unlock()
	if !f.runCalled {
		timer := time.NewTimer(timeout)
		go func() {
			<-timer.C
			// ensure there is some timeout
			f.cond.Broadcast()
		}()
		f.cond.Wait()
		timer.Stop()
	}
	return f.runCalled
}

type fakeStore struct {
	cache.Store

	pods     map[string]*v1.Pod
	services map[string]*v1.Service
}

func (f fakeStore) GetByKey(key string) (interface{}, bool, error) {
	p, found := f.pods[key]
	if found {
		return p, true, nil
	}
	s, found := f.services[key]
	if found {
		return s, true, nil
	}
	return nil, false, nil
}

func TestClusterInfoCache_GetPod(t *testing.T) {
	workingStore := fakeStore{
		pods: map[string]*v1.Pod{
			"default/test": {},
		},
	}
	tests := []struct {
		name  string
		key   string
		store cache.Store
		want  bool
	}{
		{"found", "default/test", workingStore, true},
		{"not found", "custom/missing", workingStore, false},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			informer := &fakeInformer{store: v.store}

			c := &controllerImpl{
				pods:          informer,
				env:           test.NewEnv(t),
				ipPodMapMutex: &sync.RWMutex{},
			}

			_, got := c.GetPod(v.key)
			if got != v.want {
				t.Errorf("GetPod() => (_, %t), wanted (_, %t)", got, v.want)
			}
		})
	}
}

func TestClusterInfoCache_UpdateIPPodMap(t *testing.T) {
	workingStore := fakeStore{
		pods: map[string]*v1.Pod{
			"default/test": {},
		},
	}

	foundPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Status:     v1.PodStatus{PodIP: "10.1.10.1"},
	}

	missingPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "missing", Namespace: "default"},
		Status:     v1.PodStatus{PodIP: "10.1.10.2"},
	}

	informer := &fakeInformer{store: workingStore}

	c := &controllerImpl{
		pods:          informer,
		env:           test.NewEnv(t),
		ipPodMapMutex: &sync.RWMutex{},
		ipPodMap:      make(map[string]string),
	}

	tests := []struct {
		name   string
		newPod interface{}
		getKey string
		want   bool
	}{
		{"found", foundPod, foundPod.Status.PodIP, true},
		{"not found", missingPod, missingPod.Status.PodIP, false},
		{"nil", nil, "10.1.10.3", false},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			c.updateIPPodMap(v.newPod)
			_, got := c.GetPod(v.getKey)
			if got != v.want {
				t.Errorf("GetPod(%s) => (_, %t), wanted (_, %t)", v.getKey, got, v.want)
			}
		})
	}
}

func TestClusterInfoCache_DeleteFromIPPodMap(t *testing.T) {
	workingStore := fakeStore{
		pods: map[string]*v1.Pod{
			"default/test": {},
		},
	}

	toDelete := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Status:     v1.PodStatus{PodIP: "10.1.10.1"},
	}

	informer := &fakeInformer{store: workingStore}

	c := &controllerImpl{
		pods:          informer,
		env:           test.NewEnv(t),
		ipPodMapMutex: &sync.RWMutex{},
		ipPodMap:      map[string]string{toDelete.Status.PodIP: key(toDelete.Namespace, toDelete.Name)},
	}

	tests := []struct {
		name   string
		pod    interface{}
		getKey string
		want   bool
	}{
		{"deleted", toDelete, toDelete.Status.PodIP, false},
		{"nil", nil, toDelete.Status.PodIP, false},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			c.deleteFromIPPodMap(v.pod)
			_, got := c.GetPod(v.getKey)
			if got != v.want {
				t.Errorf("GetPod(%s) => (_, %t), wanted (_, %t)", v.getKey, got, v.want)
			}
		})
	}
}

func TestClusterInfoCache(t *testing.T) {

	tests := []struct {
		name string
		do   func(impl *controllerImpl, fakeInformer *fakeInformer)
	}{
		{"RunLoggerStopInSelect", nil},
		{"Run", func(c *controllerImpl, informer *fakeInformer) {
			informer.SetSynced(true)
			c.mutationsChan <- resourceMutation{kind: addition, obj: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns"},
			}}
			c.mutationsChan <- resourceMutation{kind: update, obj: v1.Pod{}}
		}},
	}
	timeout := 5 * time.Second
	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			informer := newfakeInformer()
			c := &controllerImpl{
				env:           test.NewEnv(t),
				pods:          informer,
				mutationsChan: make(chan resourceMutation),
			}

			stop := make(chan struct{})
			go c.Run(stop)

			c.mutationsChan <- resourceMutation{kind: deletion, obj: v1.Pod{}}

			if v.do != nil {
				v.do(c, informer)
			}
			close(stop)

			if !informer.RunCalled(timeout) {
				t.Errorf("Expected cache to have been started in %v.", timeout)
			}
		})
	}
}
