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

package runtime2

import (
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/dispatcher"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/mixer/pkg/template"
)

// Temporary type. Will change this later.
type Controller struct {
	ephemeral *config.Ephemeral

	snapshot *config.Snapshot

	handlers *handler.Table

	dispatcher *dispatcher.Dispatcher
}

func NewController(
	templates map[string]template.Info,
	adapters map[string]*adapter.Info,
	initialConfig map[store.Key]*store.Resource,
	dispatcher *dispatcher.Dispatcher) *Controller {
	return &Controller{
		ephemeral:  config.NewEphemeral(templates, adapters, initialConfig),
		snapshot:   config.Empty(),
		handlers:   handler.Empty(),
		dispatcher: dispatcher,
	}
}

func (c *Controller) onConfigChange(events []*store.Event) {
	c.ephemeral.ApplyEvents(events)
	c.applyNewConfig()
}

func (c *Controller) applyNewConfig() {
	newSnapshot := c.ephemeral.BuildSnapshot()

	oldHandlers := c.handlers

	newHandlers := handler.Instantiate(oldHandlers, newSnapshot)

	builder := compiled.NewBuilder(newSnapshot.Attributes())
	newRoutes := routing.BuildTable(newHandlers, newSnapshot, builder)

	oldRoutes := c.dispatcher.ChangeRoute(newRoutes)

	c.handlers = newHandlers
	c.snapshot = newSnapshot

	cleanupHandlers(oldRoutes, oldHandlers, newHandlers)
}

func cleanupHandlers(oldRoutes *routing.Table, oldHandlers *handler.Table, currentHandlers *handler.Table) {
	// TODO
}
