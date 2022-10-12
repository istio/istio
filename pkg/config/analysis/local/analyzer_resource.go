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
	"strings"

	gogoproto "github.com/gogo/protobuf/proto"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis/scope"
	"istio.io/istio/pkg/config/schema/collection"
)

// analyzableResourcesStore is the cache to get resources that needs to be analyzed
type analyzableResourcesStore struct {
	schemas        collection.Schemas
	resourcesStore model.ConfigStore
}

func newAnalyzableResourcesCache(initializedStore model.ConfigStoreController) *analyzableResourcesStore {
	a := &analyzableResourcesStore{
		schemas: initializedStore.Schemas(),
	}
	a.resourcesStore = &dfCache{ConfigStore: memory.Make(a.schemas)}
	for _, sch := range initializedStore.Schemas().All() {
		initializedStore.RegisterEventHandler(sch.Resource().GroupVersionKind(), a.processConfigChanges)
	}
	return a
}

func (a *analyzableResourcesStore) getStore() model.ConfigStoreController {
	store := a.resourcesStore

	// Empty the store for the next analysis run
	a.resourcesStore = memory.Make(a.schemas)

	return &dfCache{
		ConfigStore: store,
	}
}

func (a *analyzableResourcesStore) processConfigChanges(prev config.Config, curr config.Config, event model.Event) {
	switch event {
	case model.EventAdd, model.EventUpdate:
		if !needsReAnalyze(prev, curr) {
			return
		}
		// We don't care about ResourceVersion, this is to avoid of update error of the in-memory store
		curr.ResourceVersion = ""
		cfg := a.resourcesStore.Get(curr.GroupVersionKind, curr.Name, curr.Namespace)
		if cfg == nil {
			_, err := a.resourcesStore.Create(curr)
			if err != nil {
				scope.Analysis.Errorf("error create config : %v", err)
			}
		} else {
			_, err := a.resourcesStore.Update(curr)
			if err != nil {
				scope.Analysis.Errorf("error update config: %v", err)
			}
		}
	case model.EventDelete:
		// Do not analyze deleted resources
	}
}

func needsReAnalyze(prev config.Config, curr config.Config) bool {
	if prev.GroupVersionKind != curr.GroupVersionKind {
		// This should never happen.
		return true
	}
	if !strings.HasSuffix(prev.GroupVersionKind.Group, "istio.io") {
		return true
	}
	prevspecProto, okProtoP := prev.Spec.(proto.Message)
	currspecProto, okProtoC := curr.Spec.(proto.Message)
	if okProtoP && okProtoC {
		return !proto.Equal(prevspecProto, currspecProto)
	}
	prevspecGogo, okGogoP := prev.Spec.(gogoproto.Message)
	currspecGogo, okGogoC := curr.Spec.(gogoproto.Message)
	if okGogoP && okGogoC {
		return !gogoproto.Equal(prevspecGogo, currspecGogo)
	}
	return true
}
