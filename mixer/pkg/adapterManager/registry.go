// Copyright 2017 Google Inc.
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

package adapterManager

import (
	"sync"

	"fmt"

	"istio.io/mixer/pkg/adapter"
)

// Registry is a simple implementation of pkg/registry.Registrar and pkg/aspect/uber.RegistryQuerier which requires
// that all registered adapters have a unique adapter name.
type Registry struct {
	sync.Mutex
	adaptersByName map[string]adapter.Adapter
}

// NewRegistry returns a registry whose implementation requires that all adapters have a globally unique name
// (not just unique per aspect). Registering two adapters with the same name results in a runtime panic.
func NewRegistry() *Registry {
	return &Registry{adaptersByName: make(map[string]adapter.Adapter)}
}

// ByImpl returns the implementation with adapter.Adapter.Name() == adapterName.
func (r *Registry) ByImpl(adapterName string) (adapter.Adapter, bool) {
	r.Lock()
	adapter, ok := r.adaptersByName[adapterName]
	r.Unlock()
	return adapter, ok // yet `return r.adaptersByName[adapterName]` doesn't typecheck.
}

// RegisterListChecker registers adapters implementing the listChecker aspect.
func (r *Registry) RegisterListChecker(list adapter.ListCheckerAdapter) error {
	r.insert(list)
	return nil
}

// RegisterDenyChecker registers adapters implementing the denyChecker aspect.
func (r *Registry) RegisterDenyChecker(deny adapter.DenyCheckerAdapter) error {
	r.insert(deny)
	return nil
}

// RegisterLogger registers adapters implementing the logger aspect.
func (r *Registry) RegisterLogger(logger adapter.LoggerAdapter) error {
	r.insert(logger)
	return nil
}

// RegisterQuota registers adapters implementing the quota aspect.
func (r *Registry) RegisterQuota(quota adapter.QuotaAdapter) error {
	r.insert(quota)
	return nil
}

func (r *Registry) insert(a adapter.Adapter) {
	r.Lock()
	if _, exists := r.adaptersByName[a.Name()]; exists {
		r.Unlock()
		panic(fmt.Errorf("attempting to register an adapter with a name already in the registry: %s", a.Name()))
	}
	r.adaptersByName[a.Name()] = a
	r.Unlock()
}
