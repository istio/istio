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

package local

import (
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
)

// analyzableResourceCache is the cache to get resources that needs to be analyzed
type analyzableResourcesStore struct {
	schemas        collection.Schemas
	resourcesStore model.ConfigStore
}

func newAnalyzableResourcesCache(initializedStore model.ConfigStoreController) *analyzableResourcesStore {
	a := &analyzableResourcesStore{
		schemas: initializedStore.Schemas(),
	}
	a.resourcesStore = memory.Make(a.schemas)
	for _, sch := range initializedStore.Schemas().All() {
		initializedStore.RegisterEventHandler(sch.Resource().GroupVersionKind(), a.processConfigChanges)
	}
	return a
}

func (a *analyzableResourcesStore) getStore() model.ConfigStoreController {
	store := a.resourcesStore

	// empty the store for the next analysis run
	a.resourcesStore = memory.Make(a.schemas)

	return &dfCache{
		ConfigStore: store,
	}
}

func (a *analyzableResourcesStore) processConfigChanges(old config.Config, new config.Config, event model.Event) {
	switch event {
	case model.EventAdd, model.EventUpdate:
		a.resourcesStore.Get(new.GroupVersionKind, new.Name, new.Namespace)
	case model.EventDelete:
		// do not analyze deleted resources
	}
}
