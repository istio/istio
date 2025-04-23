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
	"sync/atomic"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/kube"
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
	monitor   Monitor
	hasSynced func() bool
	synced    *atomic.Bool
	store     *Store

	stop chan struct{}
}

// NewController return an implementation of ConfigStoreController
// This is a client-side monitor that dispatches events as the changes are being
// made on the client.

func NewController(schemas collection.Schemas) *Controller {
	return NewControllerOptions(schemas, Options{})
}

// NewControllerOptions return an implementation of ConfigStoreController
// This is a client-side monitor that dispatches events as the changes are being
// made on the client.
func NewControllerOptions(schemas collection.Schemas, options Options) *Controller {
	stop := make(chan struct{})

	out := &Controller{
		stop:   stop,
		synced: &atomic.Bool{},
	}
	if options.Sync {
		out.monitor = NewSyncMonitor(schemas)
	} else {
		out.monitor = NewMonitor(schemas)
		out.synced.Store(true)
	}

	out.store = newStore(
		schemas,
		options.SkipValidation,
		out,
		stop,
		options.KrtDebugger,
	)

	return out
}

func (c *Controller) RegisterHasSyncedHandler(cb func() bool) {
	c.hasSynced = cb
}

func (c *Controller) MarkSynced() {
	c.synced.Store(true)
}

func (c *Controller) WaitUntilSynced(stop <-chan struct{}) bool {
	return kube.WaitForCacheSync("memory", stop, c.HasSynced)
}

func (c *Controller) RegisterEventHandler(kind config.GroupVersionKind, f model.EventHandler) {
	c.monitor.AppendEventHandler(kind, f)
}

// HasSynced return whether store has synced
// It can be controlled externally (such as by the data source),
// otherwise it'll always consider synced.
func (c *Controller) HasSynced() bool {
	if c.hasSynced != nil {
		return c.hasSynced()
	}

	return c.synced.Load()
}

func (c *Controller) Run(stop <-chan struct{}) {
	go c.monitor.Run(stop)
	<-stop
	close(c.stop)
}

func (c *Controller) Schemas() collection.Schemas {
	return c.store.schemas
}

func (c *Controller) Get(kind config.GroupVersionKind, key, namespace string) *config.Config {
	return c.store.Get(kind, key, namespace)
}

func (c *Controller) Create(cfg config.Config) (revision string, err error) {
	if revision, err = c.store.Create(cfg); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			config: cfg,
			event:  model.EventAdd,
		})
		return cfg.ResourceVersion, nil
	}

	return
}

func (c *Controller) Update(cfg config.Config) (newRevision string, err error) {
	oldconfig := c.store.Get(cfg.GroupVersionKind, cfg.Name, cfg.Namespace)
	if newRevision, err = c.store.Update(cfg); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    *oldconfig,
			config: cfg,
			event:  model.EventUpdate,
		})
	}

	return
}

func (c *Controller) UpdateStatus(cfg config.Config) (newRevision string, err error) {
	oldconfig := c.store.Get(cfg.GroupVersionKind, cfg.Name, cfg.Namespace)
	if newRevision, err = c.store.UpdateStatus(cfg); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    *oldconfig,
			config: cfg,
			event:  model.EventUpdate,
		})
	}

	return
}

func (c *Controller) Patch(orig config.Config, patchFn config.PatchFunc) (newRevision string, err error) {
	cfg, typ := patchFn(orig.DeepCopy())
	switch typ {
	case types.MergePatchType:
	case types.JSONPatchType:
	default:
		return "", fmt.Errorf("unsupported merge type: %s", typ)
	}

	if newRevision, err = c.store.Patch(cfg, patchFn); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    orig,
			config: cfg,
			event:  model.EventUpdate,
		})
	}

	return
}

func (c *Controller) Delete(kind config.GroupVersionKind, key, namespace string, resourceVersion *string) error {
	if config := c.Get(kind, key, namespace); config != nil {
		if err := c.store.Delete(kind, key, namespace, resourceVersion); err != nil {
			return err
		}
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			config: *config,
			event:  model.EventDelete,
		})
		return nil
	}

	return fmt.Errorf("delete: config %v/%v/%v does not exist", kind, namespace, key)
}

func (c *Controller) List(kind config.GroupVersionKind, namespace string) []config.Config {
	return c.store.List(kind, namespace)
}
