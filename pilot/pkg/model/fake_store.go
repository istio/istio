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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

type FakeStore struct {
	store map[config.GroupVersionKind]map[string][]config.Config
}

func NewFakeStore() *FakeStore {
	f := FakeStore{
		store: make(map[config.GroupVersionKind]map[string][]config.Config),
	}
	return &f
}

var _ ConfigStore = (*FakeStore)(nil)

func (s *FakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func (*FakeStore) Get(typ config.GroupVersionKind, name, namespace string) *config.Config { return nil }

func (s *FakeStore) List(typ config.GroupVersionKind, namespace string) ([]config.Config, error) {
	nsConfigs := s.store[typ]
	if nsConfigs == nil {
		return nil, nil
	}
	var res []config.Config
	if namespace == NamespaceAll {
		for _, configs := range nsConfigs {
			res = append(res, configs...)
		}
		return res, nil
	}
	return nsConfigs[namespace], nil
}

func (s *FakeStore) Create(cfg config.Config) (revision string, err error) {
	configs := s.store[cfg.GroupVersionKind]
	if configs == nil {
		configs = make(map[string][]config.Config)
	}
	configs[cfg.Namespace] = append(configs[cfg.Namespace], cfg)
	s.store[cfg.GroupVersionKind] = configs
	return "", nil
}

func (*FakeStore) Update(config config.Config) (newRevision string, err error) { return "", nil }

func (*FakeStore) UpdateStatus(config config.Config) (string, error) { return "", nil }

func (*FakeStore) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "", nil
}

func (*FakeStore) Delete(typ config.GroupVersionKind, name, namespace string, rv *string) error {
	return nil
}
