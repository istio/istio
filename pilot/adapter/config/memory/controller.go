// Copyright 2017 Istio Authors
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
	"errors"

	"istio.io/pilot/model"
)

type controller struct {
	monitor     Monitor
	configStore model.ConfigStore
}

// NewController return an implementation of model.ConfigStoreCache
// This is a client-side monitor that dispatches events as the changes are being
// made on the client.
func NewController(cs model.ConfigStore) model.ConfigStoreCache {
	out := &controller{
		configStore: cs,
		monitor:     NewConfigStoreMonitor(cs),
	}
	return out
}

func (c *controller) RegisterEventHandler(typ string, f func(model.Config, model.Event)) {
	c.monitor.AppendEventHandler(typ, f)
}

// Memory implementation is always synchronized with cache
func (c *controller) HasSynced() bool {
	return true
}

func (c *controller) Run(stop <-chan struct{}) {
	c.monitor.Start(stop)
}

func (c *controller) ConfigDescriptor() model.ConfigDescriptor {
	return c.configStore.ConfigDescriptor()
}

func (c *controller) Get(typ, key, namespace string) (*model.Config, bool) {
	return c.configStore.Get(typ, key, namespace)
}

func (c *controller) Create(config model.Config) (revision string, err error) {
	if revision, err = c.configStore.Create(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			config: config,
			event:  model.EventAdd,
		})
	}
	return
}

func (c *controller) Update(config model.Config) (newRevision string, err error) {
	if newRevision, err = c.configStore.Update(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			config: config,
			event:  model.EventUpdate,
		})
	}
	return
}

func (c *controller) Delete(typ, key, namespace string) (err error) {
	if config, exists := c.Get(typ, key, namespace); exists {
		if err = c.configStore.Delete(typ, key, namespace); err == nil {
			c.monitor.ScheduleProcessEvent(ConfigEvent{
				config: *config,
				event:  model.EventDelete,
			})
			return
		}
	}
	return errors.New("Delete failure: config" + key + "does not exist")
}

func (c *controller) List(typ, namespace string) ([]model.Config, error) {
	return c.configStore.List(typ, namespace)
}
