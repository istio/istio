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

package memory

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
)

type Options struct {
	// processes events synchronously and requires manual sync
	Sync           bool
	SkipValidation bool
	KrtDebugger    *krt.DebugHandler
}

// Controller is an implementation of ConfigStoreController.
type Controller struct {
	hasSynced func() bool
	store     *Store
	handlers  []krt.HandlerRegistration
}

// NewController return an implementation of ConfigStoreController
func NewController(store *Store) *Controller {
	fmt.Println("new memory controller")
	return &Controller{
		store: store,
	}
}

func (c *Controller) RegisterHasSyncedHandler(cb func() bool) {
	c.hasSynced = cb
}

func (c *Controller) WaitUntilSynced(stop <-chan struct{}) bool {
	return kube.WaitForCacheSync("memory", stop, c.HasSynced)
}

func (c *Controller) RegisterEventHandler(kind config.GroupVersionKind, f model.EventHandler) {
	c.store.mutex.Lock()
	defer c.store.mutex.Unlock()
	if kindStore, ok := c.store.data[kind]; ok {
		c.handlers = append(c.handlers, kindStore.collection.RegisterBatch(func(o []krt.Event[config.Config]) {
			for _, event := range o {
				switch event.Event {
				case controllers.EventAdd:
					f(config.Config{}, *event.New, model.Event(event.Event))
				case controllers.EventUpdate:
					f(*event.Old, *event.New, model.Event(event.Event))
				case controllers.EventDelete:
					f(config.Config{}, *event.Old, model.Event(event.Event))
				}
			}
		}, false))
	}
}

// HasSynced return whether store has synced
// It can be controlled externally (such as by the data source),
// otherwise it'll always consider synced.
func (c *Controller) HasSynced() bool {
	if c.hasSynced != nil {
		return c.hasSynced()
	}

	if !c.store.hasSynced() {
		fmt.Println("syncer not synced")
		return false
	}

	for _, handler := range c.handlers {
		if !handler.HasSynced() {
			fmt.Println("handler not synced")
			return false
		}
	}

	return true
}

func (c *Controller) Run(stop <-chan struct{}) {
	fmt.Println("memory controller run")
	// marked as synced on run to allow clients to store data before the store is marked as synced
	c.store.syncer.MarkSynced()
	<-stop
	close(c.store.stop)
}

func (c *Controller) KrtCollection(kind config.GroupVersionKind) krt.Collection[config.Config] {
	if data, ok := c.store.data[kind]; ok {
		return data.collection
	}

	return nil
}

func (c *Controller) Schemas() collection.Schemas {
	return c.store.schemas
}

func (c *Controller) Get(kind config.GroupVersionKind, key, namespace string) *config.Config {
	return c.store.Get(kind, key, namespace)
}

func (c *Controller) Create(cfg config.Config) (revision string, err error) {
	return c.store.Create(cfg)
}

func (c *Controller) Update(cfg config.Config) (newRevision string, err error) {
	return c.store.Update(cfg)
}

func (c *Controller) UpdateStatus(cfg config.Config) (newRevision string, err error) {
	return c.store.UpdateStatus(cfg)
}

func (c *Controller) Patch(orig config.Config, patchFn config.PatchFunc) (newRevision string, err error) {
	cfg, typ := patchFn(orig.DeepCopy())
	switch typ {
	case types.MergePatchType:
	case types.JSONPatchType:
	default:
		return "", fmt.Errorf("unsupported merge type: %s", typ)
	}

	return c.store.Patch(cfg, patchFn)
}

func (c *Controller) Delete(kind config.GroupVersionKind, key, namespace string, resourceVersion *string) error {
	return c.store.Delete(kind, key, namespace, resourceVersion)
}

func (c *Controller) List(kind config.GroupVersionKind, namespace string) []config.Config {
	return c.store.List(kind, namespace)
}
