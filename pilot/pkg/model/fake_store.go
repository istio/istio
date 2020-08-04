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
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"

	"time"

	"istio.io/pkg/ledger"
)

type FakeStore struct {
	store  map[resource.GroupVersionKind]map[string][]Config
	ledger ledger.Ledger
}

func NewFakeStore() *FakeStore {
	f := FakeStore{
		store:  make(map[resource.GroupVersionKind]map[string][]Config),
		ledger: ledger.Make(time.Minute),
	}
	return &f
}

var _ ConfigStore = (*FakeStore)(nil)

func (s *FakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func (*FakeStore) Get(typ resource.GroupVersionKind, name, namespace string) *Config { return nil }

func (s *FakeStore) List(typ resource.GroupVersionKind, namespace string) ([]Config, error) {
	nsConfigs := s.store[typ]
	if nsConfigs == nil {
		return nil, nil
	}
	var res []Config
	if namespace == NamespaceAll {
		for _, configs := range nsConfigs {
			res = append(res, configs...)
		}
		return res, nil
	}
	return nsConfigs[namespace], nil
}

func (s *FakeStore) Create(config Config) (revision string, err error) {
	configs := s.store[config.GroupVersionKind]
	if configs == nil {
		configs = make(map[string][]Config)
	}
	configs[config.Namespace] = append(configs[config.Namespace], config)
	s.store[config.GroupVersionKind] = configs
	return "", nil
}

func (*FakeStore) Update(config Config) (newRevision string, err error) { return "", nil }

func (*FakeStore) Delete(typ resource.GroupVersionKind, name, namespace string) error { return nil }

func (s *FakeStore) Version() string {
	return s.ledger.RootHash()
}
func (s *FakeStore) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return s.ledger.GetPreviousValue(version, key)
}

func (s *FakeStore) GetLedger() ledger.Ledger {
	return s.ledger
}

func (s *FakeStore) SetLedger(l ledger.Ledger) error {
	s.ledger = l
	return nil
}
