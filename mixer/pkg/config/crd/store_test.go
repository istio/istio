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

package crd

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/pkg/probe"
)

// The "retryTimeout" used by the test.
const testingRetryTimeout = 10 * time.Millisecond

// The timeout for "waitFor" function, waiting for the expected event to come.
const waitForTimeout = time.Second

const apiGroupVersion = ConfigAPIGroup + "/" + ConfigAPIVersion

func createFakeDiscovery(_ *rest.Config) (discovery.DiscoveryInterface, error) {
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
	watchers map[string]*watch.RaceFreeFakeWatcher
}

func (f *fakeDynamicResource) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	f.d.mu.RLock()
	for k, v := range f.d.data {
		if k.Kind == f.res.Kind {
			list.Items = append(list.Items, *v)
		}
	}
	f.d.mu.RUnlock()
	return list, nil
}

func (f *fakeDynamicResource) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return f.w, nil
}

type fakeDynamicResource struct {
	d *dummyListerWatcherBuilder
	dynamic.ResourceInterface
	w   watch.Interface
	res metav1.APIResource
}

func (d *dummyListerWatcherBuilder) build(res metav1.APIResource) dynamic.ResourceInterface {
	w := watch.NewRaceFreeFake()
	d.mu.Lock()
	d.watchers[res.Kind] = w
	d.mu.Unlock()

	return &fakeDynamicResource{d: d, w: w, res: res}
}

// nolint: unparam
func (d *dummyListerWatcherBuilder) put(key store.Key, spec map[string]interface{}) error {
	res := &unstructured.Unstructured{}
	res.SetKind(key.Kind)
	res.SetAPIVersion(apiGroupVersion)
	res.SetName(key.Name)
	res.SetNamespace(key.Namespace)
	res.Object["spec"] = spec

	d.mu.Lock()
	defer d.mu.Unlock()
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
		watchers: map[string]*watch.RaceFreeFakeWatcher{},
	}
	client := &Store{
		conf:             &rest.Config{},
		donec:            make(chan struct{}),
		retryTimeout:     testingRetryTimeout,
		discoveryBuilder: createFakeDiscovery,
		listerWatcherBuilder: func(*rest.Config) (listerWatcherBuilderInterface, error) {
			return lw, nil
		},
		Probe:         probe.NewProbe(),
		retryInterval: 1 * time.Millisecond,
	}
	return client, ns, lw
}

func waitFor(wch <-chan store.BackendEvent, ct store.ChangeType, key store.Key) error {
	timeout := time.After(waitForTimeout)
	for {
		select {
		case ev := <-wch:
			if ev.Key == key && ev.Type == ct {
				return nil
			}
		case <-timeout:
			return context.DeadlineExceeded
		}
	}
}

func TestStore(t *testing.T) {
	s, ns, lw := getTempClient()
	if err := s.Init([]string{"Handler", "Action"}); err != nil {
		t.Fatal(err.Error())
	}
	defer s.Stop()

	wch, err := s.Watch()
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
	if err = waitFor(wch, store.Update, k); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	h2, err := s.Get(k)
	if err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if !reflect.DeepEqual(h, h2.Spec) {
		t.Errorf("Got %+v, Want %+v", h2.Spec, h)
	}
	want := map[store.Key]*store.BackEndResource{k: h2}
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
	if !reflect.DeepEqual(h, h2.Spec) {
		t.Errorf("Got %+v, Want %+v", h2.Spec, h)
	}
	lw.delete(k)
	if err = waitFor(wch, store.Delete, k); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if _, err := s.Get(k); err != store.ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
}

func TestStoreWrongKind(t *testing.T) {
	s, ns, lw := getTempClient()
	if err := s.Init([]string{"Action"}); err != nil {
		t.Fatal(err.Error())
	}
	defer s.Stop()

	k := store.Key{Kind: "Handler", Namespace: ns, Name: "default"}
	h := map[string]interface{}{"name": "default", "adapter": "noop"}
	if err := lw.put(k, h); err != nil {
		t.Error("Got nil, Want error")
	}

	if _, err := s.Get(k); err == nil {
		t.Errorf("Got nil, Want error")
	}
}

func TestStoreNamespaces(t *testing.T) {
	s, ns, lw := getTempClient()
	otherNS := "other-namespace"
	s.ns = map[string]bool{ns: true, otherNS: true}
	if err := s.Init([]string{"Action", "Handler"}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	wch, err := s.Watch()
	if err != nil {
		t.Fatal(err)
	}
	k1 := store.Key{Kind: "Handler", Namespace: ns, Name: "default"}
	k2 := store.Key{Kind: "Handler", Namespace: otherNS, Name: "default"}
	k3 := store.Key{Kind: "Handler", Namespace: "irrelevant-namespace", Name: "default"}
	h := map[string]interface{}{"name": "default", "adapter": "noop"}
	for _, k := range []store.Key{k1, k2, k3} {
		if err = lw.put(k, h); err != nil {
			t.Errorf("Got %v, Want nil", err)
		}
	}
	if err = waitFor(wch, store.Update, k3); err == nil {
		t.Error("Got nil, Want error")
	}
	list := s.List()
	for _, c := range []struct {
		key store.Key
		ok  bool
	}{
		{k1, true},
		{k2, true},
		{k3, false},
	} {
		if _, ok := list[c.key]; ok != c.ok {
			t.Errorf("For key %s, Got %v, Want %v", c.key, ok, c.ok)
		}
		if _, err = s.Get(c.key); (err == nil) != c.ok {
			t.Errorf("For key %s, Got %v error, Want %v", c.key, err, c.ok)
		}
	}
}

func TestStoreFailToInit(t *testing.T) {
	s, _, _ := getTempClient()
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return nil, errors.New("dummy")
	}
	if err := s.Init([]string{"Handler", "Action"}); err.Error() != "dummy" {
		t.Errorf("Got %v, Want dummy error", err)
	}
	s.discoveryBuilder = createFakeDiscovery
	s.listerWatcherBuilder = func(*rest.Config) (listerWatcherBuilderInterface, error) {
		return nil, errors.New("dummy2")
	}
	if err := s.Init([]string{"Handler", "Action"}); err.Error() != "dummy2" {
		t.Errorf("Got %v, Want dummy2 error", err)
	}
	s.Stop()
}

func TestCriticalCrdsAreReady(t *testing.T) {
	fakeDiscovery := &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{GroupVersion: apiGroupVersion},
			},
		},
	}
	var callCount int32
	fakeDiscovery.AddReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&callCount, 1)
		fakeDiscovery.Resources[0].APIResources = append(
			fakeDiscovery.Resources[0].APIResources,
			metav1.APIResource{Name: "handlers", SingularName: "handler", Kind: "Handler", Namespaced: true},
			metav1.APIResource{Name: "actions", SingularName: "action", Kind: "Action", Namespaced: true},
		)
		return true, nil, nil
	})

	s, _, _ := getTempClient()
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return fakeDiscovery, nil
	}
	s.criticalKinds = []string{"Handler", "Action"}
	s.bgRetryInterval = 1 * time.Millisecond
	err := s.Init([]string{"Handler", "Action", "Whatever"})
	if err != nil {
		t.Errorf("Got error %v from Init", err)
	}
	count := atomic.LoadInt32(&callCount)
	if count != 1 {
		t.Errorf("callCount is not expected, got %v wang 1", count)
	}
	s.Stop()
}

func TestCriticalCrdsAreNotReadyRetryTimeout(t *testing.T) {
	fakeDiscovery := &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{GroupVersion: apiGroupVersion},
			},
		},
	}
	var callCount int32
	fakeDiscovery.AddReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&callCount, 1)
		return true, nil, nil
	})

	s, _, _ := getTempClient()
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return fakeDiscovery, nil
	}
	s.criticalKinds = []string{"Handler"}
	s.retryTimeout = 2 * time.Second
	s.retryInterval = time.Second
	err := s.Init([]string{"Handler", "Action"})
	errorMsg := "failed to discover critical kinds: [Handler]"
	if err == nil {
		t.Errorf("got no error from Init, want Init to fail")
	} else if err.Error() != errorMsg {
		t.Errorf("got Init error message %v, want %v", err.Error(), errorMsg)
	}
	count := atomic.LoadInt32(&callCount)
	if count < 1 || count > 3 {
		t.Errorf("got callCount %v, want call count to be more than 1 and less than 3 times", count)
	}
	s.Stop()
}

func TestCriticalCrdsRetryMakeSucceed(t *testing.T) {
	fakeDiscovery := &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{GroupVersion: apiGroupVersion},
			},
		},
	}
	var callCount int32
	// Gradually increase the number of API resources.
	fakeDiscovery.AddReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 2 {
			fakeDiscovery.Resources[0].APIResources = append(
				fakeDiscovery.Resources[0].APIResources,
				metav1.APIResource{Name: "handlers", SingularName: "handler", Kind: "Handler", Namespaced: true},
			)
		} else if count == 3 {
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
	s.retryInterval = 10 * time.Millisecond
	s.criticalKinds = []string{"Handler", "Action"}
	err := s.Init([]string{"Handler", "Action"})
	if err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	count := atomic.LoadInt32(&callCount)
	if count != 3 {
		t.Errorf("Got %d, Want 3", count)
	}
	s.Stop()
}

func TestCrdsRetryAsynchronously(t *testing.T) {
	fakeDiscovery := &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{
					GroupVersion: apiGroupVersion,
					APIResources: []metav1.APIResource{
						{Name: "handlers", SingularName: "handler", Kind: "Handler", Namespaced: true},
					},
				},
			},
		},
	}
	var count int32
	// Gradually increase the number of API resources.
	fakeDiscovery.AddReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		if atomic.LoadInt32(&count) != 0 {
			fakeDiscovery.Resources[0].APIResources = append(
				fakeDiscovery.Resources[0].APIResources,
				metav1.APIResource{Name: "actions", SingularName: "action", Kind: "Action", Namespaced: true},
			)
		}
		return true, nil, nil
	})
	s, ns, lw := getTempClient()
	s.bgRetryInterval = 1 * time.Millisecond
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return fakeDiscovery, nil
	}
	k1 := store.Key{Kind: "Handler", Namespace: ns, Name: "default"}
	if err := lw.put(k1, map[string]interface{}{"adapter": "noop"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Init([]string{"Handler", "Action"}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.cacheMutex.Lock()
	ncaches := len(s.caches)
	s.cacheMutex.Unlock()
	if ncaches != 1 {
		t.Errorf("Has %d caches, Want 1 caches", ncaches)
	}
	wch, err := s.Watch()
	if err != nil {
		t.Fatal(err)
	}
	atomic.StoreInt32(&count, 1)

	after := time.After(time.Second)
	tick := time.Tick(time.Millisecond)
loop:
	for {
		select {
		case <-after:
			break loop
		case <-tick:
			s.cacheMutex.Lock()
			ncaches = len(s.caches)
			s.cacheMutex.Unlock()
			if ncaches > 1 {
				break loop
			}
		}
	}
	if ncaches != 2 {
		t.Fatalf("Has %d caches, Want 2 caches", ncaches)
	}

	k2 := store.Key{Kind: "Action", Namespace: ns, Name: "default"}
	if err = lw.put(k2, map[string]interface{}{"test": "value"}); err != nil {
		t.Error(err)
	}
	if err = waitFor(wch, store.Update, k2); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
}

func TestCrdsRetryAsynchronouslyStoreClose(t *testing.T) {
	fakeDiscovery := &fake.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{GroupVersion: apiGroupVersion},
			},
		},
	}
	callCount := 0
	mutex := sync.RWMutex{}
	fakeDiscovery.AddReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		mutex.Lock()
		callCount++
		mutex.Unlock()
		return true, nil, nil
	})

	s, _, _ := getTempClient()
	s.discoveryBuilder = func(*rest.Config) (discovery.DiscoveryInterface, error) {
		return fakeDiscovery, nil
	}
	s.bgRetryInterval = 10 * time.Millisecond
	s.Init([]string{"Handler", "Action"})

	// Close store, which should shut down the background retry.
	// With 10ms retry interval and 30ms before shutdown, at most 5 discovery calls would be made.
	time.Sleep(30 * time.Millisecond)
	s.Stop()
	time.Sleep(30 * time.Millisecond)
	mutex.RLock()
	if callCount > 5 {
		t.Errorf("got %v, want no more than 5 calls", callCount)
	}
	mutex.RUnlock()
}
