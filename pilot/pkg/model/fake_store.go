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
	"istio.io/istio/pkg/kube/krt"
)

type FakeStore struct {
	store map[config.GroupVersionKind]nsStore
	stop  chan struct{}
}

type nsStore struct {
	collection krt.StaticCollection[config.Config]
	index      krt.Index[string, config.Config]
}

func NewFakeStore() *FakeStore {
	stop := make(chan struct{})
	f := FakeStore{
		store: make(map[config.GroupVersionKind]nsStore),
		stop:  stop,
	}
	return &f
}

var _ ConfigStore = (*FakeStore)(nil)

func (s *FakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func (s *FakeStore) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	nsConfigs, ok := s.store[typ]
	if !ok {
		return nil
	}

	key := name
	if namespace != "" {
		key = namespace + "/" + name
	}
	return nsConfigs.collection.GetKey(key)
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
	nsConfigs, ok := s.store[cfg.GroupVersionKind]
	if !ok {
		opts := krt.NewOptionsBuilder(s.stop, "fake-store", krt.GlobalDebugHandler)
		collection := krt.NewStaticCollection[config.Config](nil, nil, opts.WithName(cfg.GroupVersionKind.Kind)...)
		nsConfigs = nsStore{
			collection: collection,
			index:      krt.NewNamespaceIndex(collection),
		}
		s.store[cfg.GroupVersionKind] = nsConfigs
	}

	nsConfigs.collection.UpdateObject(cfg)
	return "", nil
}

func (s *FakeStore) Update(cfg config.Config) (newRevision string, err error) {
	nsConfigs, ok := s.store[cfg.GroupVersionKind]
	if ok {
		if obj := nsConfigs.collection.GetKey(krt.GetKey(cfg)); obj != nil {
			nsConfigs.collection.UpdateObject(cfg)
			return "", nil
		}
	}

	return "", errors.New("config not found")
}

func (*FakeStore) UpdateStatus(config config.Config) (string, error) { return "", nil }

func (*FakeStore) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "", nil
}

func (s *FakeStore) Delete(typ config.GroupVersionKind, name, namespace string, rv *string) error {
	nsConfigs, ok := s.store[typ]
	if !ok {
		return nil
	}

	key := name
	if namespace != "" {
		key = namespace + "/" + name
	}
	nsConfigs.collection.DeleteObject(key)
	return nil
}

func (s *FakeStore) RegisterEventHandler(kind config.GroupVersionKind, handler EventHandler) {

}

func (s *FakeStore) Run(stop <-chan struct{}) {
	<-stop
	close(s.stop)
}

func (s *FakeStore) HasSynced() bool {
	return true
}

func (s *FakeStore) KrtCollection(typ config.GroupVersionKind) krt.Collection[config.Config] {
	return s.store[typ].collection
}
