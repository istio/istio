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

	cfg "istio.io/mixer/pkg/config/proto"
)

type memstore struct {
	data     map[Key]*BackEndResource
	ch       chan BackendEvent
	initErr  error
	watchErr error
}

func (m *memstore) Init(ctx context.Context, kinds []string) error {
	return m.initErr
}

func (m *memstore) Watch(ctx context.Context) (<-chan BackendEvent, error) {
	if m.watchErr != nil {
		return nil, m.watchErr
	}
	m.ch = make(chan BackendEvent)
	return m.ch, nil
}

func (m *memstore) Get(key Key) (*BackEndResource, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (m *memstore) List() map[Key]*BackEndResource {
	return m.data
}

func registerMemstore(builders map[string]Store2Builder) {
	builders["memstore"] = func(*url.URL) (Store2Backend, error) {
		return &memstore{data: map[Key]*BackEndResource{}}, nil
	}
}

func TestStore2(t *testing.T) {
	r := NewRegistry2(registerMemstore)
	s, err := r.NewStore2("memstore://")
	if err != nil {
		t.Fatal(err)
	}
	m := s.(*store2).backend.(*memstore)
	kinds := map[string]proto.Message{"Handler": &cfg.Handler{}}
	if err = s.Init(context.Background(), kinds); err != nil {
		t.Fatal(err)
	}
	k := Key{Kind: "Handler", Name: "name", Namespace: "ns"}
	h1 := &cfg.Handler{}
	if err = s.Get(k, h1); err != ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
	m.data[k] = &BackEndResource{
		Spec: map[string]interface{}{"name": "default", "adapter": "noop"},
	}
	if err = s.Get(k, h1); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	want := &cfg.Handler{Name: "default", Adapter: "noop"}
	if !reflect.DeepEqual(h1, want) {
		t.Errorf("Got %v, Want %v", h1, want)
	}
	wantList := map[Key]proto.Message{k: want}

	for k, v := range s.List() {
		vwant := wantList[k]
		if vwant == nil {
			t.Fatalf("Did not get key for %s", k)
		}
		if !reflect.DeepEqual(v.Spec, vwant) {
			t.Errorf("Got %+v, Want %+v", v, vwant)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := s.Watch(ctx)
	if err != nil {
		t.Error(err)
	}
	m.ch <- BackendEvent{
		Key:  k,
		Type: Update,
		Value: &BackEndResource{
			Spec: map[string]interface{}{"name": "default", "adapter": "noop"},
		},
	}
	wantEv := Event{Key: k, Type: Update,
		Value: &Resource{
			Spec: want,
		},
	}

	if ev := <-ch; !reflect.DeepEqual(ev, wantEv) {
		t.Errorf("Got %+v, Want %+v", ev, wantEv)
	}
}

func TestStore2WatchMultiple(t *testing.T) {
	r := NewRegistry2(registerMemstore)
	s, err := r.NewStore2("memstore://")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Init(context.Background(), map[string]proto.Message{"Handler": &cfg.Handler{}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := s.Watch(ctx); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if _, err := s.Watch(context.Background()); err != ErrWatchAlreadyExists {
		t.Errorf("Got %v, Want %v", err, ErrWatchAlreadyExists)
	}
	cancel()
	// short sleep to make sure the goroutine for canceling watch status in store.
	time.Sleep(time.Millisecond * 5)
	if _, err := s.Watch(context.Background()); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
}

func TestStore2Fail(t *testing.T) {
	r := NewRegistry2(registerMemstore)
	s, err := r.NewStore2("memstore://")
	if err != nil {
		t.Fatal(err)
	}
	m := s.(*store2).backend.(*memstore)
	kinds := map[string]proto.Message{"Handler": &cfg.Handler{}}
	m.initErr = errors.New("dummy")
	if err = s.Init(context.Background(), kinds); err.Error() != "dummy" {
		t.Errorf("Got %v, Want dummy error", err)
	}
	m.initErr = nil
	if err = s.Init(context.Background(), kinds); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}

	m.watchErr = errors.New("watch error")
	if _, err := s.Watch(context.Background()); err.Error() != "watch error" {
		t.Errorf("Got %v, Want watch error", err)
	}

	m.data[Key{Kind: "Handler", Name: "name", Namespace: "ns"}] = &BackEndResource{
		Spec: map[string]interface{}{
			"foo": 1,
		}}
	m.data[Key{Kind: "Unknown", Name: "unknown", Namespace: "ns"}] = &BackEndResource{
		Spec: map[string]interface{}{
			"unknown": "unknown",
		}}
	if lst := s.List(); len(lst) != 0 {
		t.Errorf("Got %v, Want empty", lst)
	}
}

func TestRegistry2(t *testing.T) {
	r := NewRegistry2(registerMemstore)
	for _, c := range []struct {
		u  string
		ok bool
	}{
		{"memstore://", true},
		{"mem://", false},
		{"fs:///", true},
		{"://", false},
	} {
		_, err := r.NewStore2(c.u)
		ok := err == nil
		if ok != c.ok {
			t.Errorf("Want %v, Got %v, Err %v", c.ok, ok, err)
		}
	}
}
