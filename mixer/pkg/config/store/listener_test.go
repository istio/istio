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

package store

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"

	"istio.io/pkg/probe"
)

const watchFlushDuration = time.Millisecond

func TestStartWatch_Basic(t *testing.T) {
	s := &mockStore{
		initErrorToReturn:    nil,
		watchChannelToReturn: make(chan Event),
		watchErrorToReturn:   nil,
		listResultToReturn:   make(map[Key]*Resource),
	}
	s.listResultToReturn[Key{Name: "Foo"}] = &Resource{Metadata: ResourceMeta{Name: "Bar"}}

	kinds := map[string]proto.Message{
		"foo": &mockProto{},
	}

	if err := s.Init(kinds); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	initialResources, _, err := StartWatch(s)

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

func TestStartWatch_WatchFailure(t *testing.T) {
	s := &mockStore{
		watchErrorToReturn: errors.New("cannot watch"),
	}

	_, _, err := StartWatch(s)
	if err != s.watchErrorToReturn {
		t.Fatalf("Expected error was not returned: %v", err)
	}
}

func TestWatchChanges(t *testing.T) {
	wch := make(chan Event)
	sch := make(chan struct{})

	evt := make(chan Event)
	fn := func(events []*Event) {
		for _, e := range events {
			evt <- *e
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		WatchChanges(wch, sch, watchFlushDuration, fn)
		wg.Done()
	}()

	expected := Event{Type: Update, Value: &Resource{Metadata: ResourceMeta{Name: "FOO"}}}
	wch <- expected

	actual := <-evt

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Expected event was not received")
	}

	sch <- struct{}{}

	wg.Wait()
}

type mockStore struct {
	initKinds            map[string]proto.Message
	initErrorToReturn    error
	watchChannelToReturn chan Event
	watchErrorToReturn   error
	listResultToReturn   map[Key]*Resource

	initCalled  bool
	watchCalled bool
	listCalled  bool
}

var _ Store = &mockStore{}

func (m *mockStore) Stop() {
}

func (m *mockStore) Init(kinds map[string]proto.Message) error {
	m.initCalled = true
	m.initKinds = kinds

	return m.initErrorToReturn
}

func (m *mockStore) WaitForSynced(time.Duration) error {
	return nil
}

// Watch creates a channel to receive the events. A store can conduct a single
// watch channel at the same time. Multiple calls lead to an error.
func (m *mockStore) Watch() (<-chan Event, error) {
	m.watchCalled = true

	return m.watchChannelToReturn, m.watchErrorToReturn
}

// Get returns a resource's spec to the key.
func (m *mockStore) Get(key Key) (*Resource, error) {
	return nil, nil
}

// List returns the whole mapping from key to resource specs in the store.
func (m *mockStore) List() map[Key]*Resource {
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
