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

package store

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"

	cfg "istio.io/api/mixer/v1/config"
)

type testStore struct {
	initErr      error
	watchErr     error
	watchCh      chan BackendEvent
	watchCount   int
	calledKey    Key
	getResponse  *BackEndResource
	getError     error
	listResponse map[Key]*BackEndResource
	listCount    int
}

func (t *testStore) Init(ctx context.Context, kinds []string) error {
	return t.initErr
}

func (t *testStore) Get(key Key) (*BackEndResource, error) {
	t.calledKey = key
	return t.getResponse, t.getError
}

func (t *testStore) List() map[Key]*BackEndResource {
	t.listCount++
	return t.listResponse
}

func (t *testStore) Watch(ctx context.Context) (<-chan BackendEvent, error) {
	t.watchCount++
	if t.watchErr != nil {
		return nil, t.watchErr
	}
	ch := make(chan BackendEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	t.watchCh = ch
	return t.watchCh, nil
}

func newTestBackend() *testStore {
	return &testStore{listResponse: map[Key]*BackEndResource{}}
}

func registerTestStore(builders map[string]Builder) {
	builders["test"] = func(u *url.URL) (Backend, error) {
		return newTestBackend(), nil
	}
}

func TestStore(t *testing.T) {
	b := newTestBackend()
	s := WithBackend(b)
	kinds := map[string]proto.Message{"Handler": &cfg.Handler{}}
	if err := s.Init(context.Background(), kinds); err != nil {
		t.Fatal(err)
	}

	k := Key{Kind: "Handler", Name: "name", Namespace: "ns"}
	b.getError = ErrNotFound
	h1 := &cfg.Handler{}
	if err := s.Get(k, h1); err != ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
	if b.calledKey != k {
		t.Errorf("calledKey %s should be %s", b.calledKey, k)
	}

	b.getError = nil
	bres := &BackEndResource{
		Kind:     k.Kind,
		Metadata: ResourceMeta{Name: k.Name, Namespace: k.Namespace},
		Spec:     map[string]interface{}{"name": "default", "adapter": "noop"},
	}
	b.getResponse = bres
	if err := s.Get(k, h1); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	want := &cfg.Handler{Name: "default", Adapter: "noop"}
	if !reflect.DeepEqual(h1, want) {
		t.Errorf("Got %v, Want %v", h1, want)
	}

	b.listResponse[k] = bres
	if b.listCount != 0 {
		t.Errorf("List is called %d times already", b.listCount)
	}

	wantList := map[Key]*Resource{k: {Metadata: ResourceMeta{Name: k.Name, Namespace: k.Namespace}, Spec: want}}
	if lst := s.List(); !reflect.DeepEqual(lst, wantList) {
		t.Errorf("Got %+v, Want %+v", lst, wantList)
	}
	if b.listCount != 1 {
		t.Errorf("Got %d, Want 1", b.listCount)
	}

	if b.watchCount != 0 {
		t.Errorf("Watch is called %d times already", b.watchCount)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := s.Watch(ctx)
	if err != nil {
		t.Error(err)
	}
	if b.watchCount != 1 {
		t.Errorf("Got %d, Want 1", b.watchCount)
	}
	b.watchCh <- BackendEvent{Type: Update, Key: k, Value: bres}
	wantEv := Event{Key: k, Type: Update,
		Value: &Resource{
			Metadata: ResourceMeta{
				Name:      k.Name,
				Namespace: k.Namespace,
			},
			Spec: want,
		},
	}

	if ev := <-ch; !reflect.DeepEqual(ev, wantEv) {
		t.Errorf("Got %+v, Want %+v", ev.Value, wantEv.Value)
	}
}

func TestStoreWatchMultiple(t *testing.T) {
	b := newTestBackend()
	s := WithBackend(b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Init(ctx, map[string]proto.Message{"Handler": &cfg.Handler{}}); err != nil {
		t.Fatal(err)
	}
	wctx, wcancel := context.WithCancel(ctx)
	if b.watchCount != 0 {
		t.Errorf("Watch is called %d times already", b.watchCount)
	}
	if _, err := s.Watch(wctx); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if b.watchCount != 1 {
		t.Errorf("Got %d, Want 1", b.watchCount)
	}
	if _, err := s.Watch(wctx); err != ErrWatchAlreadyExists {
		t.Errorf("Got %v, Want %v", err, ErrWatchAlreadyExists)
	}
	if b.watchCount != 1 {
		t.Errorf("Got %d, Want 1", b.watchCount)
	}
	wcancel()
	// short sleep to make sure the goroutine for canceling watch status in store.
	time.Sleep(time.Millisecond * 5)
	wctx2, wcancel2 := context.WithCancel(ctx)
	defer wcancel2()
	if _, err := s.Watch(wctx2); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if b.watchCount != 2 {
		t.Errorf("Got %d, Want 2", b.watchCount)
	}
}

func TestStoreFail(t *testing.T) {
	b := newTestBackend()
	s := WithBackend(b)
	kinds := map[string]proto.Message{"Handler": &cfg.Handler{}}
	b.initErr = errors.New("dummy")
	if err := s.Init(context.Background(), kinds); err.Error() != "dummy" {
		t.Errorf("Got %v, Want dummy error", err)
	}
	b.initErr = nil
	if err := s.Init(context.Background(), kinds); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}

	b.watchErr = errors.New("watch error")
	if _, err := s.Watch(context.Background()); err.Error() != "watch error" {
		t.Errorf("Got %v, Want watch error", err)
	}

	b.listResponse[Key{Kind: "Handler", Name: "name", Namespace: "ns"}] = &BackEndResource{Spec: map[string]interface{}{
		"foo": 1,
	}}
	b.listResponse[Key{Kind: "Unknown", Name: "unknown", Namespace: "ns"}] = &BackEndResource{Spec: map[string]interface{}{
		"unknown": "unknown",
	}}
	if lst := s.List(); len(lst) != 0 {
		t.Errorf("Got %v, Want empty", lst)
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry(registerTestStore)
	for _, c := range []struct {
		u  string
		ok bool
	}{
		{"memstore://", false},
		{"mem://", false},
		{"fs:///", true},
		{"://", false},
		{"test://", true},
	} {
		_, err := r.NewStore(c.u)
		ok := err == nil
		if ok != c.ok {
			t.Errorf("Want %v, Got %v, Err %v", c.ok, ok, err)
		}
	}
}
