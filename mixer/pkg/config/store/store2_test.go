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

type testStore struct {
	memstore
	initErr  error
	watchErr error
}

func (t *testStore) Init(ctx context.Context, kinds []string) error {
	if t.initErr != nil {
		return t.initErr
	}
	return t.memstore.Init(ctx, kinds)
}

func (t *testStore) Watch(ctx context.Context) (<-chan BackendEvent, error) {
	if t.watchErr != nil {
		return nil, t.watchErr
	}
	return t.memstore.Watch(ctx)
}

func registerTestStore(builders map[string]Store2Builder) {
	builders["test"] = func(u *url.URL) (Store2Backend, error) {
		return &testStore{memstore: *createMemstore(u)}, nil
	}
}

func TestStore2(t *testing.T) {
	r := NewRegistry2(registerTestStore)
	u := "memstore://" + t.Name()
	s, err := r.NewStore2(u)
	if err != nil {
		t.Fatal(err)
	}
	m := GetMemstoreWriter(u)
	kinds := map[string]proto.Message{"Handler": &cfg.Handler{}}
	if err = s.Init(context.Background(), kinds); err != nil {
		t.Fatal(err)
	}
	k := Key{Kind: "Handler", Name: "name", Namespace: "ns"}
	h1 := &cfg.Handler{}
	if err = s.Get(k, h1); err != ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
	m.Put(k, &BackEndResource{Spec: map[string]interface{}{"name": "default", "adapter": "noop"}})
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
	m.Put(k, &BackEndResource{Spec: map[string]interface{}{"name": "default", "adapter": "noop"}})
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

func TestStore2WatchMultiple(t *testing.T) {
	r := NewRegistry2(registerTestStore)
	s, err := r.NewStore2("memstore://" + t.Name())
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
	r := NewRegistry2(registerTestStore)
	s, err := r.NewStore2("test://" + t.Name())
	if err != nil {
		t.Fatal(err)
	}
	ts := s.(*store2).backend.(*testStore)
	kinds := map[string]proto.Message{"Handler": &cfg.Handler{}}
	ts.initErr = errors.New("dummy")
	if err = s.Init(context.Background(), kinds); err.Error() != "dummy" {
		t.Errorf("Got %v, Want dummy error", err)
	}
	ts.initErr = nil
	if err = s.Init(context.Background(), kinds); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}

	ts.watchErr = errors.New("watch error")
	if _, err := s.Watch(context.Background()); err.Error() != "watch error" {
		t.Errorf("Got %v, Want watch error", err)
	}

	ts.Put(Key{Kind: "Handler", Name: "name", Namespace: "ns"}, &BackEndResource{Spec: map[string]interface{}{
		"foo": 1,
	}})
	ts.Put(Key{Kind: "Unknown", Name: "unknown", Namespace: "ns"}, &BackEndResource{Spec: map[string]interface{}{
		"unknown": "unknown",
	}})
	if lst := s.List(); len(lst) != 0 {
		t.Errorf("Got %v, Want empty", lst)
	}
}

func TestRegistry2(t *testing.T) {
	r := NewRegistry2(registerTestStore)
	for _, c := range []struct {
		u  string
		ok bool
	}{
		{"memstore://", true},
		{"mem://", false},
		{"fs:///", true},
		{"://", false},
		{"test://", true},
	} {
		_, err := r.NewStore2(c.u)
		ok := err == nil
		if ok != c.ok {
			t.Errorf("Want %v, Got %v, Err %v", c.ok, ok, err)
		}
	}
}
