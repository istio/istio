// Copyright 2018 Istio Authors
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

package runtime2

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pkg/probe"
)

func TestStartWatch_Basic(t *testing.T) {
	s := &mockStore{
		initErrorToReturn:    nil,
		watchChannelToReturn: make(chan store.Event),
		watchErrorToReturn:   nil,
		listResultToReturn:   make(map[store.Key]*store.Resource),
	}
	s.listResultToReturn[store.Key{Name: "Foo"}] = &store.Resource{Metadata: store.ResourceMeta{Name: "Bar"}}

	kinds := map[string]proto.Message{
		"foo": &mockProto{},
	}

	initialResources, _, err := startWatch(s, kinds)

	if !s.initCalled {
		t.Fatal("Init should have been called")
	}

	if !reflect.DeepEqual(s.initKinds, kinds) {
		t.Fatalf("kind mismatch. Got: %v\nwanted: %v\n", s.initKinds, kinds)
	}

	if !s.watchCalled {
		t.Fatal("Watch should have been called")
	}

	if !s.listCalled {
		t.Fatalf("list should have been called")
	}

	if err != nil {
		t.Fatalf("Unexpected error : %v", err)
	}

	if !reflect.DeepEqual(initialResources, s.listResultToReturn) {
		t.Fatalf("Expected initial resource mismatch. Got: %v\nwanted: %v\n", initialResources, s.listResultToReturn)
	}
}

func TestStartWatch_InitFailure(t *testing.T) {
	s := &mockStore{
		initErrorToReturn: errors.New("cannot init"),
	}

	kinds := map[string]proto.Message{
		"foo": &mockProto{},
	}

	_, _, err := startWatch(s, kinds)
	if err != s.initErrorToReturn {
		t.Fatalf("Expected error was not returned: %v", err)
	}
}

func TestStartWatch_WatchFailure(t *testing.T) {
	s := &mockStore{
		watchErrorToReturn: errors.New("cannot watch"),
	}

	kinds := map[string]proto.Message{
		"foo": &mockProto{},
	}

	_, _, err := startWatch(s, kinds)
	if err != s.watchErrorToReturn {
		t.Fatalf("Expected error was not returned: %v", err)
	}
}

func TestWatchChanges(t *testing.T) {
	wch := make(chan store.Event)
	sch := make(chan struct{})

	evt := make(chan store.Event)
	fn := func(events []*store.Event) {
		for _, e := range events {
			evt <- *e
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		watchChanges(wch, sch, fn)
		wg.Done()
	}()

	expected := store.Event{Type: store.Update, Value: &store.Resource{Metadata: store.ResourceMeta{Name: "FOO"}}}
	wch <- expected

	actual := <-evt

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Expected event was not received")
	}

	sch <- struct{}{}

	wg.Wait()
}

type mockStore struct {
	// Init method related fields
	initCalled        bool
	initKinds         map[string]proto.Message
	initErrorToReturn error

	// Watch method related fields
	watchCalled          bool
	watchChannelToReturn chan store.Event
	watchErrorToReturn   error

	// List method related fields
	listCalled         bool
	listResultToReturn map[store.Key]*store.Resource
}

var _ store.Store = &mockStore{}

func (m *mockStore) Stop() {
}

func (m *mockStore) Init(kinds map[string]proto.Message) error {
	m.initCalled = true
	m.initKinds = kinds

	return m.initErrorToReturn
}

// Watch creates a channel to receive the events. A store can conduct a single
// watch channel at the same time. Multiple calls lead to an error.
func (m *mockStore) Watch() (<-chan store.Event, error) {
	m.watchCalled = true

	return m.watchChannelToReturn, m.watchErrorToReturn
}

// Get returns a resource's spec to the key.
func (m *mockStore) Get(key store.Key, spec proto.Message) error {
	return nil
}

// List returns the whole mapping from key to resource specs in the store.
func (m *mockStore) List() map[store.Key]*store.Resource {
	m.listCalled = true
	return m.listResultToReturn
}

func (m *mockStore) RegisterProbe(c probe.Controller, name string) {

}

type mockProto struct {
}

var _ proto.Message = &mockProto{}

func (m *mockProto) Reset()         {}
func (m *mockProto) String() string { return "" }
func (m *mockProto) ProtoMessage()  {}
