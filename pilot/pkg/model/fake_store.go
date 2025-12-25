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

package model

import (
	"errors"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
)

type syncer struct {
	synced chan struct{}
}

func (c *syncer) WaitUntilSynced(stop <-chan struct{}) bool {
	select {
	case <-c.synced:
		return true
	case <-stop:
		return false
	}
}

func (c *syncer) HasSynced() bool {
	select {
	case <-c.synced:
		return true
	default:
		return false
	}
}

func (c *syncer) markSynced() {
	close(c.synced)
}

type FakeStore struct {
	store    map[config.GroupVersionKind]kindStore
	stop     chan struct{}
	syncer   *syncer
	handlers []krt.HandlerRegistration
}

type kindStore struct {
	collection krt.StaticCollection[config.Config]
	index      krt.Index[string, config.Config]
}

func NewFakeStore() *FakeStore {
	stop := make(chan struct{})
	f := &FakeStore{
		store:  make(map[config.GroupVersionKind]kindStore),
		stop:   stop,
		syncer: &syncer{make(chan struct{})},
	}

	for _, s := range collections.Pilot.All() {
		opts := krt.NewOptionsBuilder(f.stop, "fake-store", krt.GlobalDebugHandler)
		collection := krt.NewStaticCollection[config.Config](f.syncer, nil, opts.WithName(s.Kind())...)
		kindStore := kindStore{
			collection: collection,
			index:      krt.NewNamespaceIndex(collection),
		}
		f.store[s.GroupVersionKind()] = kindStore
	}

	return f
}

var _ ConfigStoreController = (*FakeStore)(nil)

func (s *FakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func (s *FakeStore) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	kindStore, ok := s.store[typ]
	if !ok {
		return nil
	}

	key := name
	if len(namespace) > 0 {
		key = namespace + "/" + name
	}
	return kindStore.collection.GetKey(key)
}

func (s *FakeStore) List(typ config.GroupVersionKind, namespace string) []config.Config {
	data, exists := s.store[typ]
	if !exists {
		return nil
	}

	if namespace == NamespaceAll {
		return data.collection.List()
	}

	return data.index.Lookup(namespace)
}

func (s *FakeStore) Create(cfg config.Config) (revision string, err error) {
	kindStore, ok := s.store[cfg.GroupVersionKind]
	if ok {
		kindStore.collection.UpdateObject(cfg)
	}

	return "", nil
}

func (s *FakeStore) Update(cfg config.Config) (newRevision string, err error) {
	kindStore, ok := s.store[cfg.GroupVersionKind]
	if ok {
		if obj := kindStore.collection.GetKey(krt.GetKey(cfg)); obj != nil {
			kindStore.collection.UpdateObject(cfg)
			return "", nil
		}
	}

	return "", errors.New("config not found")
}

func (*FakeStore) UpdateStatus(config config.Config) (string, error) { return "", nil }

func (s *FakeStore) Delete(typ config.GroupVersionKind, name, namespace string, rv *string) error {
	kindStore, ok := s.store[typ]
	if !ok {
		return nil
	}

	key := name
	if len(namespace) > 0 {
		key = namespace + "/" + name
	}
	kindStore.collection.DeleteObject(key)
	return nil
}

func (s *FakeStore) RegisterEventHandler(kind config.GroupVersionKind, handler EventHandler) {
	if kindStore, ok := s.store[kind]; ok {
		s.handlers = append(
			s.handlers,
			kindStore.collection.RegisterBatch(func(evs []krt.Event[config.Config]) {
				for _, event := range evs {
					switch event.Event {
					case controllers.EventAdd:
						handler(config.Config{}, *event.New, EventAdd)
					case controllers.EventUpdate:
						handler(*event.Old, *event.New, EventUpdate)
					case controllers.EventDelete:
						handler(config.Config{}, *event.Old, EventDelete)
					}
				}
			}, false),
		)
	}
}

func (s *FakeStore) Run(stop <-chan struct{}) {
	s.syncer.markSynced()
	<-stop
	close(s.stop)
}

func (s *FakeStore) HasSynced() bool {
	if !s.syncer.HasSynced() {
		return false
	}

	for _, kindStore := range s.store {
		if !kindStore.collection.HasSynced() {
			return false
		}
	}

	for _, handler := range s.handlers {
		if !handler.HasSynced() {
			return false
		}
	}

	return true
}

func (s *FakeStore) KrtCollection(typ config.GroupVersionKind) krt.Collection[config.Config] {
	if kindStore, ok := s.store[typ]; ok {
		return kindStore.collection
	}

	return nil
}
