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

package runtime

import (
	"errors"
	"sync"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/dispatcher"
	"istio.io/istio/mixer/pkg/runtime/handler"
	"istio.io/istio/mixer/pkg/runtime/routing"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
	"istio.io/pkg/probe"
)

var errNotListening = errors.New("runtime is not listening to the store")

const watchFlushDuration = time.Second

// Runtime is the main entry point to the Mixer runtime environment. It listens to configuration, instantiates handler
// instances, creates the dispatch machinery and handles incoming requests.
type Runtime struct {
	defaultConfigNamespace string

	ephemeral *config.Ephemeral

	snapshot *config.Snapshot

	handlers *handler.Table

	dispatcher *dispatcher.Impl

	store store.Store

	handlerPool *pool.GoroutinePool

	*probe.Probe

	namespaces []string

	stateLock            sync.Mutex
	shutdown             chan struct{}
	waitQuiesceListening sync.WaitGroup
}

// New returns a new instance of Runtime.
func New(
	s store.Store,
	templates map[string]*template.Info,
	adapters map[string]*adapter.Info,
	defaultConfigNamespace string,
	executorPool *pool.GoroutinePool,
	handlerPool *pool.GoroutinePool,
	enableTracing bool,
	namespaces []string) *Runtime {

	// Ignoring the errors for bad configuration that has already made it to the store.
	// during snapshot creation the bad configuration errors are already logged.
	e := config.NewEphemeral(templates, adapters)
	rt := &Runtime{
		defaultConfigNamespace: defaultConfigNamespace,
		ephemeral:              e,
		snapshot:               config.Empty(),
		handlers:               handler.Empty(),
		dispatcher:             dispatcher.New(executorPool, enableTracing),
		handlerPool:            handlerPool,
		Probe:                  probe.NewProbe(),
		store:                  s,
		namespaces:             namespaces,
	}

	// Make sure we have a stable state.
	rt.processNewConfig()

	rt.Probe.SetAvailable(errNotListening)

	return rt
}

// Dispatcher returns the dispatcher.Dispatcher that is implemented by this runtime package.
func (c *Runtime) Dispatcher() dispatcher.Dispatcher {
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

	data, watchChan, err := store.StartWatch(c.store)
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
	c.ephemeral.ApplyEvent(events)
	c.processNewConfig()
}

func (c *Runtime) processNewConfig() {
	newSnapshot, err := c.ephemeral.BuildSnapshot()
	log.Infof("Built new config.Snapshot: id='%d'", newSnapshot.ID)
	if err != nil {
		log.Error(err.Error())
	}

	oldHandlers := c.handlers

	newHandlers := handler.NewTable(oldHandlers, newSnapshot, c.handlerPool, c.namespaces)

	newRoutes := routing.BuildTable(
		newHandlers, newSnapshot, c.defaultConfigNamespace, log.DebugEnabled())

	oldContext := c.dispatcher.ChangeRoute(newRoutes)

	c.handlers = newHandlers
	c.snapshot = newSnapshot

	log.Debugf("New routes in effect:\n%s", newRoutes)

	cleanupHandlers(oldContext, oldHandlers, newHandlers, maxCleanupDuration)
}

// maxCleanupDuration is the maximum amount of time cleanup operation will wait
// before resolver ref count goes to 0. It will return after this duration without
// calling Close() on handlers.
const maxCleanupDuration = 10 * time.Second

const cleanupSleepTime = 500 * time.Millisecond

func cleanupHandlers(oldContext *dispatcher.RoutingContext, oldHandlers *handler.Table, currentHandlers *handler.Table, timeout time.Duration) {
	start := time.Now()
	for {
		rc := oldContext.GetRefs()
		if rc <= 0 {
			log.Infof("Cleaning up handler table, with config ID:%d", oldContext.Routes.ID())
			oldHandlers.Cleanup(currentHandlers)
			return
		}

		if time.Since(start) > timeout {
			log.Errorf("unable to cleanup handlers(config id:%d) in %v, %d requests remain", oldContext.Routes.ID(), timeout, rc)
			return
		}

		log.Debugf("Waiting for dispatches using routes with config ID '%d' to finish: %d remaining requests", oldContext.Routes.ID(), rc)
		time.Sleep(cleanupSleepTime)
	}
}
