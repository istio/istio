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
	"istio.io/istio/pkg/maps"
)

type FakeStore struct {
	store map[config.GroupVersionKind]map[string]map[string]config.Config
}

func NewFakeStore() *FakeStore {
	f := FakeStore{
		store: make(map[config.GroupVersionKind]map[string]map[string]config.Config),
	}
	return &f
}

var _ ConfigStore = (*FakeStore)(nil)

func (s *FakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func (s *FakeStore) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	nsConfigs := s.store[typ]
	if nsConfigs == nil {
		return nil
	}

	configs := nsConfigs[namespace]
	if configs == nil {
		return nil
	}

	if config, f := configs[name]; f {
		return &config
	}

	return nil
}

func (s *FakeStore) List(typ config.GroupVersionKind, namespace string) []config.Config {
	nsConfigs := s.store[typ]
	if nsConfigs == nil {
		return nil
	}
	var res []config.Config
	if namespace == NamespaceAll {
		for _, configs := range nsConfigs {
			for _, cfg := range configs {
				res = append(res, cfg)
			}
		}
		return res
	}

	return maps.Values(nsConfigs[namespace])
}

func (s *FakeStore) Create(cfg config.Config) (revision string, err error) {
	nsConfigs := s.store[cfg.GroupVersionKind]
	if nsConfigs == nil {
		nsConfigs = make(map[string]map[string]config.Config)
		s.store[cfg.GroupVersionKind] = nsConfigs
	}

	configs := nsConfigs[cfg.Namespace]
	if configs == nil {
		configs = make(map[string]config.Config)
		nsConfigs[cfg.Namespace] = configs
	}

	configs[cfg.Name] = cfg
	return "", nil
}

func (s *FakeStore) Update(cfg config.Config) (newRevision string, err error) {
	nsConfigs := s.store[cfg.GroupVersionKind]
	if nsConfigs != nil {
		configs := nsConfigs[cfg.Namespace]
		if configs != nil {
			if _, f := configs[cfg.Name]; f {
				configs[cfg.Name] = cfg
				return "", nil
			}
		}
	}

	return "", errors.New("config not found")
}

func (*FakeStore) UpdateStatus(config config.Config) (string, error) { return "", nil }

func (s *FakeStore) Delete(typ config.GroupVersionKind, name, namespace string, rv *string) error {
	nsConfigs := s.store[typ]
	if nsConfigs == nil {
		return nil
	}

	configs := nsConfigs[namespace]
	if configs == nil {
		return nil
	}

	delete(configs, name)
	return nil
}
