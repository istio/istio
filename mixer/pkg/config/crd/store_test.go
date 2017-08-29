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

package crd

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"istio.io/mixer/pkg/config/store"
)

// The "retryTimeout" used by the test.
const testingRetryTimeout = 10 * time.Millisecond

func createFakeDiscovery(*rest.Config) (discovery.DiscoveryInterface, error) {
	return &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{
					GroupVersion: apiGroupVersion,
					APIResources: []metav1.APIResource{
						{Name: "handlers", SingularName: "handler", Kind: "Handler", Namespaced: true},
						{Name: "actions", SingularName: "action", Kind: "Action", Namespaced: true},
					},
				},
			},
		},
	}, nil
}

type dummyListerWatcherBuilder struct {
	mu       sync.RWMutex
	data     map[store.Key]*unstructured.Unstructured
	watchers map[string]*watch.FakeWatcher
}

func (d *dummyListerWatcherBuilder) build(res metav1.APIResource) cache.ListerWatcher {
	return &cache.ListWatch{
		ListFunc: func(metav1.ListOptions) (runtime.Object, error) {
			d.mu.RLock()
			defer d.mu.RUnlock()
			list := &unstructured.UnstructuredList{}
			for k, v := range d.data {
				if k.Kind == res.Kind {
					list.Items = append(list.Items, *v)
				}
			}
			return list, nil
		},
		WatchFunc: func(metav1.ListOptions) (watch.Interface, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			w := watch.NewFake()
			d.watchers[res.Kind] = w
			return w, nil
		},
	}
}

func (d *dummyListerWatcherBuilder) put(key store.Key, spec map[string]interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	res := &unstructured.Unstructured{}
	res.SetKind(key.Kind)
	res.SetAPIVersion(apiGroupVersion)
	res.SetName(key.Name)
	res.SetNamespace(key.Namespace)
	res.Object["spec"] = spec
	_, existed := d.data[key]
	d.data[key] = res
	w, ok := d.watchers[key.Kind]
	if !ok {
		return nil
	}
	if existed {
		w.Modify(res)
	} else {
		w.Add(res)
	}
	return nil
}

func (d *dummyListerWatcherBuilder) delete(key store.Key) {
	d.mu.Lock()
	defer d.mu.Unlock()
	value, ok := d.data[key]
	if !ok {
		return
	}
	delete(d.data, key)
	w, ok := d.watchers[key.Kind]
	if !ok {
		return
	}
	w.Delete(value)
}

func getTempClient() (*Store, string, *dummyListerWatcherBuilder) {
	ns := "istio-mixer-testing"
	lw := &dummyListerWatcherBuilder{
		data:     map[store.Key]*unstructured.Unstructured{},
		watchers: map[string]*watch.FakeWatcher{},
	}
	client := &Store{
		conf:             &rest.Config{},
		retryTimeout:     testingRetryTimeout,
		discoveryBuilder: createFakeDiscovery,
		listerWatcherBuilder: func(*rest.Config) (listerWatcherBuilderInterface, error) {
			return lw, nil
		},
	}
	return client, ns, lw
}

func waitFor(wch <-chan store.BackendEvent, ct store.ChangeType, key store.Key) {
	for ev := range wch {
		if ev.Key == key && ev.Type == ct {
			return
		}
	}
}

func TestStore(t *testing.T) {
	s, ns, lw := getTempClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Init(ctx, []string{"Handler", "Action"}); err != nil {
		t.Fatal(err.Error())
	}

	wch, err := s.Watch(ctx)
	if err != nil {
		t.Fatal(err.Error())
	}
	k := store.Key{Kind: "Handler", Namespace: ns, Name: "default"}
	if _, err = s.Get(k); err != store.ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
	h := map[string]interface{}{"name": "default", "adapter": "noop"}
	if err = lw.put(k, h); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	waitFor(wch, store.Update, k)
	h2, err := s.Get(k)
	if err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if !reflect.DeepEqual(h, h2) {
		t.Errorf("Got %+v, Want %+v", h2, h)
	}
	want := map[store.Key]map[string]interface{}{k: h2}
	if lst := s.List(); !reflect.DeepEqual(lst, want) {
		t.Errorf("Got %+v, Want %+v", lst, want)
	}
	h["adapter"] = "noop2"
	if err = lw.put(k, h); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	h2, err = s.Get(k)
	if err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if !reflect.DeepEqual(h, h2) {
		t.Errorf("Got %+v, Want %+v", h2, h)
	}
	lw.delete(k)
	waitFor(wch, store.Delete, k)
	if _, err := s.Get(k); err != store.ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
}

func TestStoreWrongKind(t *testing.T) {
	s, ns, lw := getTempClient()
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Init(ctx, []string{"Action"}); err != nil {
		t.Fatal(err.Error())
	}
	defer cancel()

	k := store.Key{Kind: "Handler", Namespace: ns, Name: "default"}
	h := map[string]interface{}{"name": "default", "adapter": "noop"}
	if err := lw.put(k, h); err != nil {
		t.Error("Got nil, Want error")
	}

	if _, err := s.Get(k); err == nil {
		t.Errorf("Got nil, Want error")
	}
}

func TestStoreFailToInit(t *testing.T) {
	s, _, _ := getTempClient()
	ctx := context.Background()
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return nil, errors.New("dummy")
	}
	if err := s.Init(ctx, []string{"Handler", "Action"}); err.Error() != "dummy" {
		t.Errorf("Got %v, Want dummy error", err)
	}
	s.discoveryBuilder = createFakeDiscovery
	s.listerWatcherBuilder = func(*rest.Config) (listerWatcherBuilderInterface, error) {
		return nil, errors.New("dummy2")
	}
	if err := s.Init(ctx, []string{"Handler", "Action"}); err.Error() != "dummy2" {
		t.Errorf("Got %v, Want dummy2 error", err)
	}
}

func TestCrdsAreNotReady(t *testing.T) {
	emptyDiscovery := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}}
	s, _, _ := getTempClient()
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return emptyDiscovery, nil
	}
	start := time.Now()
	err := s.Init(context.Background(), []string{"Handler", "Action"})
	d := time.Since(start)
	if err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if d < testingRetryTimeout {
		t.Errorf("Duration for Init %v is too short, maybe not retrying", d)
	}
}

func TestCrdsRetryMakeSucceed(t *testing.T) {
	fakeDiscovery := &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{GroupVersion: apiGroupVersion},
			},
		},
	}
	callCount := 0
	// Gradually increase the number of API resources.
	fakeDiscovery.AddReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		callCount++
		if callCount == 2 {
			fakeDiscovery.Resources[0].APIResources = append(
				fakeDiscovery.Resources[0].APIResources,
				metav1.APIResource{Name: "handlers", SingularName: "handler", Kind: "Handler", Namespaced: true},
			)
		} else if callCount == 3 {
			fakeDiscovery.Resources[0].APIResources = append(
				fakeDiscovery.Resources[0].APIResources,
				metav1.APIResource{Name: "actions", SingularName: "action", Kind: "Action", Namespaced: true},
			)
		}
		return true, nil, nil
	})

	s, _, _ := getTempClient()
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return fakeDiscovery, nil
	}
	// Should set a longer timeout to avoid early quitting retry loop due to lack of computational power.
	s.retryTimeout = 2 * time.Second
	err := s.Init(context.Background(), []string{"Handler", "Action"})
	if err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if callCount != 3 {
		t.Errorf("Got %d, Want 3", callCount)
	}
}
