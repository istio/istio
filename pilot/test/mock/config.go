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

package mock

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/model/test"
	"istio.io/pilot/test/util"
)

var (
	// Types defines the mock config descriptor
	Types = model.ConfigDescriptor{model.MockConfig}

	// ExampleRouteRule is an example route rule
	ExampleRouteRule = &proxyconfig.RouteRule{
		Name:        "sample-rule",
		Destination: WorldService.Hostname,
		Route: []*proxyconfig.DestinationWeight{
			{Weight: 80, Tags: map[string]string{"version": "v1"}},
			{Weight: 20, Tags: map[string]string{"version": "v2"}},
		},
	}

	// ExampleIngressRule is an example ingress rule
	ExampleIngressRule = &proxyconfig.IngressRule{
		Name:                   "sample-ingress",
		Port:                   80,
		Destination:            WorldService.Hostname,
		DestinationServicePort: &proxyconfig.IngressRule_DestinationPort{DestinationPort: 80},
	}

	// ExampleDestinationPolicy is an example destination policy
	ExampleDestinationPolicy = &proxyconfig.DestinationPolicy{
		Destination: WorldService.Hostname,
		Policy: []*proxyconfig.DestinationVersionPolicy{
			{LoadBalancing: &proxyconfig.LoadBalancing{
				LbPolicy: &proxyconfig.LoadBalancing_Name{Name: proxyconfig.LoadBalancing_RANDOM},
			}},
		},
	}
)

// Make creates a mock config indexed by a number
func Make(i int) *test.MockConfig {
	return &test.MockConfig{
		Key: fmt.Sprintf("%s%d", "mock-config", i),
		Pairs: []*test.ConfigPair{
			{Key: "key", Value: strconv.Itoa(i)},
		},
	}
}

// CheckMapInvariant validates operational invariants of an empty config registry
func CheckMapInvariant(r model.ConfigStore, t *testing.T, n int) {
	// check that the config descriptor is the mock config descriptor
	_, contains := r.ConfigDescriptor().GetByType(model.MockConfig.Type)
	if !contains {
		t.Error("expected config mock types")
	}

	// create configuration objects
	elts := make(map[int]*test.MockConfig)
	for i := 0; i < n; i++ {
		elts[i] = Make(i)
	}

	// post all elements
	for _, elt := range elts {
		if _, err := r.Post(elt); err != nil {
			t.Error(err)
		}
	}

	revs := make(map[int]string)

	// check that elements are stored
	for i, elt := range elts {
		if v1, ok, rev := r.Get(model.MockConfig.Type, elts[i].Key); !ok || !reflect.DeepEqual(v1, elt) {
			t.Errorf("wanted %v, got %v", elt, v1)
		} else {
			revs[i] = rev
		}
	}

	if _, err := r.Post(elts[0]); err == nil {
		t.Error("expected error posting twice")
	}

	if _, err := r.Post(nil); err == nil {
		t.Error("expected error posting invalid object")
	}

	if _, err := r.Post(&test.MockConfig{}); err == nil {
		t.Error("expected error posting invalid object")
	}

	if _, err := r.Put(nil, revs[0]); err == nil {
		t.Error("expected error putting invalid object")
	}

	if _, err := r.Put(&test.MockConfig{}, revs[0]); err == nil {
		t.Error("expected error putting invalid object")
	}

	if _, err := r.Put(&test.MockConfig{Key: "missing"}, revs[0]); err == nil {
		t.Error("expected error putting missing object with a missing key")
	}

	if _, err := r.Put(elts[0], ""); err == nil {
		t.Error("expected error putting object without revision")
	}

	if _, err := r.Put(elts[0], "missing"); err == nil {
		t.Error("expected error putting object with a bad revision")
	}

	// check for missing type
	if l, _ := r.List("missing"); len(l) > 0 {
		t.Errorf("unexpected objects for missing type")
	}

	// check for missing element
	if _, ok, _ := r.Get(model.MockConfig.Type, "missing"); ok {
		t.Error("unexpected configuration object found")
	}

	// check for missing element
	if _, ok, _ := r.Get("missing", "missing"); ok {
		t.Error("unexpected configuration object found")
	}

	// delete missing elements
	if err := r.Delete("missing", "missing"); err == nil {
		t.Error("expected error on deletion of missing type")
	}

	// delete missing elements
	if err := r.Delete(model.MockConfig.Type, "missing"); err == nil {
		t.Error("expected error on deletion of missing element")
	}

	// list elements
	l, err := r.List(model.MockConfig.Type)
	if err != nil {
		t.Errorf("List error %#v, %v", l, err)
	}
	if len(l) != n {
		t.Errorf("wanted %d element(s), got %d in %v", n, len(l), l)
	}

	// update all elements
	for i := 0; i < n; i++ {
		elts[i] = Make(i)
		elts[i].Pairs[0].Value += "(updated)"
		if _, err = r.Put(elts[i], revs[i]); err != nil {
			t.Error(err)
		}
	}

	// check that elements are stored
	for i, elt := range elts {
		if v1, ok, _ := r.Get(model.MockConfig.Type, elts[i].Key); !ok || !reflect.DeepEqual(v1, elt) {
			t.Errorf("wanted %v, got %v", elt, v1)
		}
	}

	// delete all elements
	for i := range elts {
		if err = r.Delete(model.MockConfig.Type, elts[i].Key); err != nil {
			t.Error(err)
		}
	}

	l, err = r.List(model.MockConfig.Type)
	if err != nil {
		t.Error(err)
	}
	if len(l) != 0 {
		t.Errorf("wanted 0 element(s), got %d in %v", len(l), l)
	}
}

// CheckIstioConfigTypes validates that an empty store can ingest Istio config objects
func CheckIstioConfigTypes(store model.ConfigStore, t *testing.T) {
	if _, err := store.Post(ExampleRouteRule); err != nil {
		t.Errorf("Post(RouteRule) => got %v", err)
	}
	if _, err := store.Post(ExampleIngressRule); err != nil {
		t.Errorf("Post(IngressRule) => got %v", err)
	}
	if _, err := store.Post(ExampleDestinationPolicy); err != nil {
		t.Errorf("Post(DestinationPolicy) => got %v", err)
	}

	registry := model.MakeIstioStore(store)
	rules := registry.RouteRules()
	if len(rules) != 1 || !reflect.DeepEqual(rules[ExampleRouteRule.Name], ExampleRouteRule) {
		t.Errorf("RouteRules() => %v, want %v", rules, ExampleRouteRule)
	}
}

// CheckCacheEvents validates operational invariants of a cache
func CheckCacheEvents(store model.ConfigStore, cache model.ConfigStoreCache, n int, t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)

	added, deleted := 0, 0
	cache.RegisterEventHandler(model.MockConfig.Type, func(c model.Config, ev model.Event) {
		switch ev {
		case model.EventAdd:
			if deleted != 0 {
				t.Errorf("Events are not serialized (add)")
			}
			added++
		case model.EventDelete:
			if added != n {
				t.Errorf("Events are not serialized (delete)")
			}
			deleted++
		}
		glog.Infof("Added %d, deleted %d", added, deleted)
	})
	go cache.Run(stop)

	// run map invariant sequence
	CheckMapInvariant(store, t, n)

	glog.Infof("Waiting till all events are received")
	util.Eventually(func() bool { return added == n && deleted == n }, t)
}

// CheckCacheFreshness validates operational invariants of a cache
func CheckCacheFreshness(cache model.ConfigStoreCache, t *testing.T) {
	stop := make(chan struct{})
	var doneMu sync.Mutex
	done := false

	// validate cache consistency
	cache.RegisterEventHandler(model.MockConfig.Type, func(config model.Config, ev model.Event) {
		elts, _ := cache.List(model.MockConfig.Type)
		switch ev {
		case model.EventAdd:
			if len(elts) != 1 {
				t.Errorf("Got %#v, expected %d element(s) on ADD event", elts, 1)
			}
			glog.Infof("Calling Delete(%#v)", config.Key)
			if err := cache.Delete(model.MockConfig.Type, config.Key); err != nil {
				t.Error(err)
			}
		case model.EventDelete:
			if len(elts) != 0 {
				t.Errorf("Got %#v, expected zero elements on DELETE event", elts)
			}
			glog.Infof("Stopping channel for (%#v)", config.Key)
			close(stop)
			doneMu.Lock()
			done = true
			doneMu.Unlock()
		}
	})

	go cache.Run(stop)
	o := Make(0)

	// add and remove
	glog.Infof("Calling Post(%#v)", o)
	if _, err := cache.Post(o); err != nil {
		t.Error(err)
	}

	util.Eventually(func() bool {
		doneMu.Lock()
		defer doneMu.Unlock()
		return done
	}, t)
}

// CheckCacheSync validates operational invariants of a cache
func CheckCacheSync(store model.ConfigStore, cache model.ConfigStoreCache, n int, t *testing.T) {
	keys := make(map[int]*test.MockConfig)
	// add elements directly through client
	for i := 0; i < n; i++ {
		keys[i] = Make(i)
		if _, err := store.Post(keys[i]); err != nil {
			t.Error(err)
		}
	}

	// check in the controller cache
	stop := make(chan struct{})
	defer close(stop)
	go cache.Run(stop)
	util.Eventually(func() bool { return cache.HasSynced() }, t)
	os, _ := cache.List(model.MockConfig.Type)
	if len(os) != n {
		t.Errorf("cache.List => Got %d, expected %d", len(os), n)
	}

	// remove elements directly through client
	for i := 0; i < n; i++ {
		if err := store.Delete(model.MockConfig.Type, keys[i].Key); err != nil {
			t.Error(err)
		}
	}

	// check again in the controller cache
	util.Eventually(func() bool {
		os, _ = cache.List(model.MockConfig.Type)
		glog.Infof("cache.List => Got %d, expected %d", len(os), 0)
		return len(os) == 0
	}, t)

	// now add through the controller
	for i := 0; i < n; i++ {
		if _, err := cache.Post(Make(i)); err != nil {
			t.Error(err)
		}
	}

	// check directly through the client
	util.Eventually(func() bool {
		cs, _ := cache.List(model.MockConfig.Type)
		os, _ := store.List(model.MockConfig.Type)
		glog.Infof("cache.List => Got %d, expected %d", len(cs), n)
		glog.Infof("store.List => Got %d, expected %d", len(os), n)
		return len(os) == n && len(cs) == n
	}, t)

	// remove elements directly through the client
	for i := 0; i < n; i++ {
		if err := store.Delete(model.MockConfig.Type, keys[i].Key); err != nil {
			t.Error(err)
		}
	}
}
