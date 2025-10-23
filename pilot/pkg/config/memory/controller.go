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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/slices"
)

// Controller is an implementation of ConfigStoreController.
type Controller struct {
	monitor     Monitor
	configStore model.ConfigStore
	hasSynced   func() bool

	// If meshConfig.DiscoverySelectors are specified, the namespacesFilter tracks the namespaces this controller watches.
	namespacesFilter func(obj interface{}) bool
}

// NewController return an implementation of ConfigStoreController
// This is a client-side monitor that dispatches events as the changes are being
// made on the client.
func NewController(cs model.ConfigStore) *Controller {
	out := &Controller{
		configStore: cs,
		monitor:     NewMonitor(cs),
	}
	return out
}

// NewSyncController return an implementation of model.ConfigStoreController which processes events synchronously
func NewSyncController(cs model.ConfigStore) *Controller {
	out := &Controller{
		configStore: cs,
		monitor:     NewSyncMonitor(cs),
	}

	return out
}

func (c *Controller) RegisterHasSyncedHandler(cb func() bool) {
	c.hasSynced = cb
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
	return true
}

func (c *Controller) Run(stop <-chan struct{}) {
	c.monitor.Run(stop)
}

func (c *Controller) Schemas() collection.Schemas {
	return c.configStore.Schemas()
}

func (c *Controller) Get(kind config.GroupVersionKind, key, namespace string) *config.Config {
	if c.namespacesFilter != nil && !c.namespacesFilter(namespace) {
		return nil
	}
	return c.configStore.Get(kind, key, namespace)
}

func (c *Controller) Create(config config.Config) (revision string, err error) {
	if revision, err = c.configStore.Create(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			config: config,
			event:  model.EventAdd,
		})
	}
	return revision, err
}

func (c *Controller) Update(config config.Config) (newRevision string, err error) {
	oldconfig := c.configStore.Get(config.GroupVersionKind, config.Name, config.Namespace)
	if newRevision, err = c.configStore.Update(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    *oldconfig,
			config: config,
			event:  model.EventUpdate,
		})
	}
	return newRevision, err
}

func (c *Controller) UpdateStatus(config config.Config) (newRevision string, err error) {
	oldconfig := c.configStore.Get(config.GroupVersionKind, config.Name, config.Namespace)
	if newRevision, err = c.configStore.UpdateStatus(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    *oldconfig,
			config: config,
			event:  model.EventUpdate,
		})
	}
	return newRevision, err
}

func (c *Controller) Delete(kind config.GroupVersionKind, key, namespace string, resourceVersion *string) error {
	if config := c.Get(kind, key, namespace); config != nil {
		if err := c.configStore.Delete(kind, key, namespace, resourceVersion); err != nil {
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
	configs := c.configStore.List(kind, namespace)
	if c.namespacesFilter != nil {
		return slices.Filter(configs, func(config config.Config) bool {
			return c.namespacesFilter(config)
		})
	}
	return configs
}
