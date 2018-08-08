//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package mcp

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/galley/pkg/metadata/kube"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/galley/pkg/mcp/snapshot"
	"istio.io/istio/galley/pkg/mcp/testing"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
)

var (
	// Some test instances

	r1 = &v1beta1.Rule{
		Match: "foo",
	}
	r2 = &v1beta1.Rule{
		Match: "bar",
	}
	r3 = &v1beta1.Rule{
		Match: "baz",
	}
)

func typeURLOf(nonLegacyKind string) string {
	for _, u := range kube.Types.All() {
		if u.Kind == nonLegacyKind {
			return u.Target.TypeURL.String()
		}
	}

	panic(fmt.Sprintf("nonLegacyKind not found: %s", nonLegacyKind))
}

type testState struct {
	mapping *mapping
	server  *mcptest.Server
	backend store.Backend

	// updateWg is used to synchronize between w.r.t. to the Updater.Update call.
	// updateWg.Done() will be called each time Updater.Update call completes successfully.
	// Test authors need to call add on this, before sending updates through the server.
	updateWg sync.WaitGroup
}

func createState(t *testing.T) *testState {
	st := &testState{}

	var typeUrls []string
	var kinds []string
	m, err := constructMapping([]string{}, kube.Types)
	if err != nil {
		t.Fatal(err)
	}
	st.mapping = m
	for t, k := range st.mapping.typeURLsToKinds {
		typeUrls = append(typeUrls, t)
		kinds = append(kinds, k)
	}

	if st.server, err = mcptest.NewServer(0, typeUrls); err != nil {
		t.Fatal(err)
	}

	hookFn := func() {
		scope.Warnf("Update hook called")
		st.updateWg.Done()
	}

	if st.backend, err = newStore(st.server.URL, hookFn); err != nil {
		t.Fatal(err)
	}

	err = st.backend.Init(kinds)
	if err != nil {
		t.Fatal(err)
	}

	return st
}

func (s *testState) close(t *testing.T) {
	s.backend.Stop()

	err := s.server.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBackend_List(t *testing.T) {
	st := createState(t)
	defer st.close(t)

	b := snapshot.NewInMemoryBuilder()
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "ns1/e1", r1)
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "ns2/e2", r2)
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "e3", r3)
	b.SetVersion(typeURLOf(constant.RulesKind), "v1")
	sn := b.Build()

	st.updateWg.Add(1)
	st.server.Cache.SetSnapshot(mixerNodeID, sn)
	st.updateWg.Wait()

	actual := st.backend.List()

	expected := map[store.Key]*store.BackEndResource{
		store.Key{
			Name:      "e1",
			Namespace: "ns1",
			Kind:      constant.RulesKind,
		}: {
			Kind: constant.RulesKind,
			Metadata: store.ResourceMeta{
				Name:      "e1",
				Namespace: "ns1",
				Revision:  "v1",
			},
			Spec: map[string]interface{}{"match": "foo"},
		},
		store.Key{
			Name:      "e2",
			Namespace: "ns2",
			Kind:      constant.RulesKind,
		}: {
			Kind: constant.RulesKind,
			Metadata: store.ResourceMeta{
				Name:      "e2",
				Namespace: "ns2",
				Revision:  "v1",
			},
			Spec: map[string]interface{}{"match": "bar"},
		},
		store.Key{
			Name:      "e3",
			Namespace: "",
			Kind:      constant.RulesKind,
		}: {
			Kind: constant.RulesKind,
			Metadata: store.ResourceMeta{
				Name:      "e3",
				Namespace: "",
				Revision:  "v1",
			},
			Spec: map[string]interface{}{"match": "baz"},
		},
	}

	checkEqual(t, actual, expected)
}

func TestBackend_Get(t *testing.T) {
	st := createState(t)
	defer st.close(t)

	b := snapshot.NewInMemoryBuilder()

	_ = b.SetEntry(typeURLOf(constant.RulesKind), "ns1/e1", r1)
	b.SetVersion(typeURLOf(constant.RulesKind), "v1")
	sn := b.Build()

	st.updateWg.Add(1)
	st.server.Cache.SetSnapshot(mixerNodeID, sn)
	st.updateWg.Wait()

	actual, err := st.backend.Get(store.Key{Kind: constant.RulesKind, Namespace: "ns1", Name: "e1"})
	if err != nil {
		t.Fatal(err)
	}

	expected := &store.BackEndResource{
		Kind: constant.RulesKind,
		Metadata: store.ResourceMeta{
			Name:      "e1",
			Namespace: "ns1",
			Revision:  "v1",
		},
		Spec: map[string]interface{}{"match": "foo"},
	}
	checkEqual(t, actual, expected)

	_, err = st.backend.Get(store.Key{Kind: constant.RulesKind, Namespace: "not exists", Name: "e1"})
	if err == nil {
		t.Fatal("expected error not found")
	}

	_, err = st.backend.Get(store.Key{Kind: constant.RulesKind, Namespace: "ns1", Name: "not exists"})
	if err == nil {
		t.Fatal("expected error not found")
	}

	_, err = st.backend.Get(store.Key{Kind: "unknown", Namespace: "ns1", Name: "e1"})
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestBackend_Watch(t *testing.T) {
	st := createState(t)
	defer st.close(t)

	ch, err := st.backend.Watch()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	b := snapshot.NewInMemoryBuilder()
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "ns1/e1", r1)
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "ns2/e2", r2)
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "e3", r3)
	b.SetVersion(typeURLOf(constant.RulesKind), "v1")

	sn := b.Build()

	st.updateWg.Add(1)
	st.server.Cache.SetSnapshot(mixerNodeID, sn)
	st.updateWg.Wait()

	var actual []store.BackendEvent
loop:
	for {
		select {
		case e := <-ch:
			actual = append(actual, e)
		default:
			break loop
		}
	}
	// Order events by name. Changing order of events in a single batch doesn't change semantics.
	sort.Slice(actual, func(i, j int) bool {
		return strings.Compare(actual[i].Name, actual[j].Name) < 0
	})

	expected := []store.BackendEvent{
		{
			Type: store.Update,
			Key:  store.Key{Kind: constant.RulesKind, Namespace: "ns1", Name: "e1"},
			Value: &store.BackEndResource{
				Kind: constant.RulesKind,
				Metadata: store.ResourceMeta{
					Name:      "e1",
					Namespace: "ns1",
					Revision:  "v1",
				},
				Spec: map[string]interface{}{"match": "foo"},
			},
		},
		{
			Type: store.Update,
			Key:  store.Key{Kind: constant.RulesKind, Namespace: "ns2", Name: "e2"},
			Value: &store.BackEndResource{
				Kind: constant.RulesKind,
				Metadata: store.ResourceMeta{
					Name:      "e2",
					Namespace: "ns2",
					Revision:  "v1",
				},
				Spec: map[string]interface{}{"match": "bar"},
			},
		},
		{
			Type: store.Update,
			Key:  store.Key{Kind: constant.RulesKind, Name: "e3"},
			Value: &store.BackEndResource{
				Kind: constant.RulesKind,
				Metadata: store.ResourceMeta{
					Name:     "e3",
					Revision: "v1",
				},
				Spec: map[string]interface{}{"match": "baz"},
			},
		},
	}

	checkEqual(t, actual, expected)

	// Now check delete, update, stay same
	b = snapshot.NewInMemoryBuilder()

	// delete ns1/e1
	// update ns2/e2 (r2 -> r1)
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "ns2/e2", r1)
	// e3 doesn't change
	_ = b.SetEntry(typeURLOf(constant.RulesKind), "e3", r3)
	b.SetVersion(typeURLOf(constant.RulesKind), "v2")

	sn = b.Build()

	st.updateWg.Add(1)
	st.server.Cache.SetSnapshot(mixerNodeID, sn)
	st.updateWg.Wait()

	actual = []store.BackendEvent{}
loop2:
	for {
		select {
		case e := <-ch:
			actual = append(actual, e)
		default:
			break loop2
		}
	}
	sort.Slice(actual, func(i, j int) bool {
		return strings.Compare(actual[i].Name, actual[j].Name) < 0
	})

	expected = []store.BackendEvent{
		{
			Type: store.Delete,
			Key:  store.Key{Kind: constant.RulesKind, Namespace: "ns1", Name: "e1"},
		},
		{
			Type: store.Update,
			Key:  store.Key{Kind: constant.RulesKind, Namespace: "ns2", Name: "e2"},
			Value: &store.BackEndResource{
				Kind: constant.RulesKind,
				Metadata: store.ResourceMeta{
					Name:      "e2",
					Namespace: "ns2",
					Revision:  "v2",
				},
				Spec: map[string]interface{}{"match": "foo"}, // r1's contents
			},
		},
		// We still get an event for this as we don't do deep equality comparison.
		{
			Type: store.Update,
			Key:  store.Key{Kind: constant.RulesKind, Name: "e3"},
			Value: &store.BackEndResource{
				Kind: constant.RulesKind,
				Metadata: store.ResourceMeta{
					Name:     "e3",
					Revision: "v2",
				},
				Spec: map[string]interface{}{"match": "baz"},
			},
		},
	}

	checkEqual(t, actual, expected)

}

func checkEqual(t *testing.T, actual, expected interface{}) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Mismatch. got:\n%v\n wanted:\n%v\n", spew.Sdump(actual), spew.Sdump(expected))
	}

}
