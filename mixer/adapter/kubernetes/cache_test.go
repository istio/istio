// Copyright 2017 Google Inc.
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
	"errors"
	"sync"
	"testing"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/mixer/pkg/adapter/test"
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

type fakeInformer struct {
	sync.Mutex
	cache.SharedInformer

	store     cache.Store
	synced    bool
	runCalled bool
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
	f.runCalled = true
}

func (f *fakeInformer) RunCalled() bool {
	f.Lock()
	defer f.Unlock()
	return f.runCalled
}

type fakeStore struct {
	cache.Store

	pods      map[string]*v1.Pod
	returnErr bool
}

func (f fakeStore) GetByKey(key string) (interface{}, bool, error) {
	if f.returnErr {
		return nil, false, errors.New("get by key error")
	}
	p, found := f.pods[key]
	if !found {
		return nil, false, nil
	}
	return p, true, nil
}

func TestClusterInfoCache_GetPod(t *testing.T) {
	workingStore := fakeStore{
		pods: map[string]*v1.Pod{
			"default/test": {},
		},
	}

	errStore := fakeStore{
		pods: map[string]*v1.Pod{
			"default/test": {},
		},
		returnErr: true,
	}

	tests := []struct {
		name    string
		key     string
		store   cache.Store
		wantErr bool
	}{
		{"found", "default/test", workingStore, false},
		{"not found", "custom/missing", workingStore, true},
		{"store error", "default/test", errStore, true},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			informer := &fakeInformer{store: v.store}

			c := &controllerImpl{
				pods: informer,
				env:  test.NewEnv(t),
			}

			got, err := c.GetPod(v.key)
			if err == nil && v.wantErr {
				t.Fatal("Expected error")
			}
			if err != nil && !v.wantErr {
				t.Fatalf("Unexpected error: %v", err)
			}
			if got == nil && !v.wantErr {
				t.Error("Expected non-nil Pod returned")
			}
		})
	}
}

func TestClusterInfoCache_Run(t *testing.T) {
	informer := &fakeInformer{}
	c := &controllerImpl{
		env:           test.NewEnv(t),
		pods:          informer,
		mutationsChan: make(chan resourceMutation),
	}

	stop := make(chan struct{})
	go c.Run(stop)

	c.mutationsChan <- resourceMutation{kind: deletion, obj: v1.Pod{}}

	informer.SetSynced(true)
	c.mutationsChan <- resourceMutation{kind: addition, obj: &v1.Pod{
		ObjectMeta: v1.ObjectMeta{Name: "pod", Namespace: "ns"},
	}}
	c.mutationsChan <- resourceMutation{kind: update, obj: v1.Pod{}}

	close(stop)
	if !informer.RunCalled() {
		t.Error("Expected cache to have been started.")
	}
}
