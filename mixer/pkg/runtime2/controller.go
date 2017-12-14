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
	"fmt"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/log"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/dispatcher"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/runtime2/legacy"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/mixer/pkg/template"
)

// Temporary type. Will change this later.
type Controller struct {
	ephemeral *config.Ephemeral

	snapshot *config.Snapshot

	handlers *handler.Table

	dispatcher *dispatcher.Dispatcher

	// handlerGoRoutinePool is the goroutine pool used by handlers.
	handlerGoRoutinePool *pool.GoroutinePool
}

func NewController(
	templates map[string]template.Info,
	adapters map[string]*adapter.Info,
	initialConfig map[store.Key]*store.Resource,
	dispatcher *dispatcher.Dispatcher,
	handlerPool *pool.GoroutinePool) *Controller {
	return &Controller{
		ephemeral:            config.NewEphemeral(templates, adapters, initialConfig),
		snapshot:             config.Empty(),
		handlers:             handler.Empty(),
		dispatcher:           dispatcher,
		handlerGoRoutinePool: handlerPool,
	}
}

func (c *Controller) onConfigChange(events []*store.Event) {
	c.ephemeral.ApplyEvents(events)
	c.applyNewConfig()
}

func (c *Controller) applyNewConfig() {
	newSnapshot := c.ephemeral.BuildSnapshot()

	oldHandlers := c.handlers

	newHandlers := handler.Instantiate(oldHandlers, newSnapshot, legacy.NewEnv("", c.handlerGoRoutinePool))

	builder := compiled.NewBuilder(newSnapshot.Attributes)
	newRoutes := routing.BuildTable(newHandlers, newSnapshot, builder)

	oldRoutes := c.dispatcher.ChangeRoute(newRoutes)

	c.handlers = newHandlers
	c.snapshot = newSnapshot

	cleanupHandlers(oldRoutes, oldHandlers, newHandlers, maxCleanupDuration)
}

// maxCleanupDuration is the maximum amount of time cleanup operation will wait
// before resolver ref count does to 0. It will return after this duration without
// calling Close() on handlers.
var maxCleanupDuration = 10 * time.Second

var cleanupSleepTime = 500 * time.Millisecond

func cleanupHandlers(oldRoutes *routing.Table, oldHandlers *handler.Table, currentHandlers *handler.Table, timeout time.Duration) error {
	start := time.Now()
	for {
		rc := oldRoutes.GetRefs()
		if rc > 0 {
			if time.Since(start) > timeout {
				return fmt.Errorf("unable to cleanup resolver in %v time. %d requests remain", timeout, rc)
			}

			log.Warnf("Waiting for resolver %d to finish %d remaining requests", oldRoutes.ID(), rc)

			time.Sleep(cleanupSleepTime)
			continue
		}
	}

	log.Infof("cleanupResolver[%d] handler table has %d entries", oldRoutes.ID())

	handler.Cleanup(currentHandlers, oldHandlers)
	return nil
}
