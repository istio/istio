// Copyright 2018 Istio Authors
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
	"errors"
	"fmt"
	"sync"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/dispatcher"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
)

var errNotListening = errors.New("runtime is not listening to the store")

const watchFlushDuration = time.Second

// Runtime is the main entry point to the Mixer runtime environment. It listens to configuration, instantiates handler
// instances, creates the dispatch machinery and handles incoming requests.
type Runtime struct {
	identityAttribute string

	defaultConfigNamespace string

	ephemeral *config.Ephemeral

	snapshot *config.Snapshot

	handlers *handler.Table

	dispatcher *dispatcher.Dispatcher

	store store.Store

	handlerPool *pool.GoroutinePool

	*probe.Probe

	stateLock            sync.Mutex
	shutdown             chan struct{}
	waitQuiesceListening sync.WaitGroup
}

// New returns a new instance of Runtime.
func New(
	s store.Store,
	templates map[string]*template.Info,
	adapters map[string]*adapter.Info,
	identityAttribute string,
	defaultConfigNamespace string,
	executorPool *pool.GoroutinePool,
	handlerPool *pool.GoroutinePool,
	enableTracing bool) *Runtime {

	rt := &Runtime{
		identityAttribute:      identityAttribute,
		defaultConfigNamespace: defaultConfigNamespace,
		ephemeral:              config.NewEphemeral(templates, adapters),
		snapshot:               config.Empty(),
		handlers:               handler.Empty(),
		dispatcher:             dispatcher.New(identityAttribute, executorPool, enableTracing),
		handlerPool:            handlerPool,
		Probe:                  probe.NewProbe(),
		store:                  s,
	}

	// Make sure we have a stable state.
	rt.processNewConfig()

	rt.Probe.SetAvailable(errNotListening)

	return rt
}

// Dispatcher returns the runtime.Dispatcher that is implemented by this runtime package.
func (c *Runtime) Dispatcher() runtime.Dispatcher {
	return c.dispatcher
}

// StartListening directs Runtime to start listening to configuration changes. As config changes, runtime processes
// the confguration and creates a dispatcher.
func (c *Runtime) StartListening() error {
	c.stateLock.Lock()
	defer c.stateLock.Unlock()

	if c.shutdown != nil {
		return errors.New("already listening")
	}

	kinds := config.KindMap(c.snapshot.Adapters, c.snapshot.Templates)

	data, watchChan, err := store.StartWatch(c.store, kinds)
	if err != nil {
		return err
	}

	c.ephemeral.SetState(data)
	c.processNewConfig()

	c.shutdown = make(chan struct{})
	c.waitQuiesceListening.Add(1)
	go func() {
		store.WatchChanges(watchChan, c.shutdown, watchFlushDuration, c.onConfigChange)
		c.waitQuiesceListening.Done()
	}()

	c.Probe.SetAvailable(nil)

	return nil
}

// StopListening directs Runtime to stop listening to configuration changes. It will not unload the current
// configuration, or close the existing adapters.
func (c *Runtime) StopListening() {
	c.stateLock.Lock()
	defer c.stateLock.Unlock()

	if c.shutdown != nil {
		c.shutdown <- struct{}{}
		c.shutdown = nil
		c.waitQuiesceListening.Wait()
		c.store.Stop()

		c.Probe.SetAvailable(errNotListening)
	}
}

func (c *Runtime) onConfigChange(events []*store.Event) {
	for _, e := range events {
		c.ephemeral.ApplyEvent(e)
	}
	c.processNewConfig()
}

func (c *Runtime) processNewConfig() {
	newSnapshot := c.ephemeral.BuildSnapshot()

	oldHandlers := c.handlers

	newHandlers := handler.NewTable(oldHandlers, newSnapshot, c.handlerPool)

	builder := compiled.NewBuilder(newSnapshot.Attributes)
	newRoutes := routing.BuildTable(
		newHandlers, newSnapshot, builder, c.defaultConfigNamespace, log.DebugEnabled())

	oldContext := c.dispatcher.ChangeRoute(newRoutes)

	c.handlers = newHandlers
	c.snapshot = newSnapshot

	log.Debugf("New routes in effect:\n%s", newRoutes)

	if err := cleanupHandlers(oldContext, oldHandlers, newHandlers, maxCleanupDuration); err != nil {
		log.Errorf("Failed to cleanup handlers: %v", err)
	}
}

// maxCleanupDuration is the maximum amount of time cleanup operation will wait
// before resolver ref count goes to 0. It will return after this duration without
// calling Close() on handlers.
var maxCleanupDuration = 10 * time.Second

var cleanupSleepTime = 500 * time.Millisecond

func cleanupHandlers(oldContext *dispatcher.RoutingContext, oldHandlers *handler.Table, currentHandlers *handler.Table, timeout time.Duration) error {
	start := time.Now()
	for {
		rc := oldContext.GetRefs()
		if rc > 0 {
			if time.Since(start) > timeout {
				return fmt.Errorf("unable to cleanup handlers(config id:%d) in %v time: %d requests remain", oldContext.Routes.ID(), timeout, rc)
			}

			log.Warnf("Waiting for dispatches using routes with config ID '%d' to finish: %d remaining requests", oldContext.Routes.ID(), rc)

			time.Sleep(cleanupSleepTime)
			continue
		}
		break
	}

	log.Infof("Cleaning up handler table, with config ID:%d", oldContext.Routes.ID())

	oldHandlers.Cleanup(currentHandlers)
	return nil
}
