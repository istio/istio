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
	"sync"

	"google.golang.org/protobuf/proto"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/util/sets"
)

// ConfigTypeWatcher is to watch config types that has changed during the analysis interval
type configTypeWatcher struct {
	schemas collection.Schemas

	lock         sync.Mutex
	changedTypes sets.Set[config.GroupVersionKind]
}

func newConfigTypeWatcher(store model.ConfigStoreController) *configTypeWatcher {
	w := &configTypeWatcher{
		schemas:      store.Schemas(),
		changedTypes: sets.New[config.GroupVersionKind](),
	}
	for _, sch := range w.schemas.All() {
		// add all types to analyze for the first time
		w.changedTypes.Insert(sch.Resource().GroupVersionKind())
		store.RegisterEventHandler(sch.Resource().GroupVersionKind(), w.watchTypeChange)
	}
	return w
}

func (a *configTypeWatcher) TypesChange() sets.Set[config.GroupVersionKind] {
	a.lock.Lock()
	ct := a.changedTypes
	// Empty the store for the next analysis run
	a.changedTypes = sets.New[config.GroupVersionKind]()
	a.lock.Unlock()
	return ct
}

func (a *configTypeWatcher) watchTypeChange(prev config.Config, curr config.Config, event model.Event) {
	switch event {
	case model.EventUpdate:
		if !needsReAnalyze(prev, curr) {
			return
		}
	}
	a.lock.Lock()
	a.changedTypes.Insert(curr.GroupVersionKind)
	a.lock.Unlock()
}

func needsReAnalyze(prev config.Config, curr config.Config) bool {
	if prev.GroupVersionKind != curr.GroupVersionKind {
		// This should never happen.
		return true
	}
	if !strings.HasSuffix(prev.GroupVersionKind.Group, "istio.io") {
		return true
	}
	prevSpecProto, okProtoP := prev.Spec.(proto.Message)
	currSpecProto, okProtoC := curr.Spec.(proto.Message)
	if okProtoP && okProtoC {
		return !proto.Equal(prevSpecProto, currSpecProto)
	}
	return true
}
