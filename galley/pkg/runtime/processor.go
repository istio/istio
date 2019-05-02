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

package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/groups"
	"istio.io/istio/galley/pkg/runtime/monitoring"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry"
	"istio.io/istio/galley/pkg/runtime/publish"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/util"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/wait"
)

var scope = log.RegisterScope("runtime", "Galley runtime", 0)

// Processor is the main control-loop for processing incoming config events and translating them into
// component configuration
type Processor struct {
	// source interface for retrieving the events from.
	source Source

	distribute bool

	// eventCh channel that was obtained from source
	eventCh chan resource.Event

	// handler for events.
	handler processing.Handler

	// The current in-memory configuration State
	state         *State
	stateStrategy *publish.Strategy

	// Handler that generates synthetic ServiceEntry projections.
	serviceEntryHandler *serviceentry.Handler

	distributor Distributor

	// hook that gets called after each event processing. Useful for testing.
	postProcessHook postProcessHookFn

	// lastEventTime records the last time an event was received.
	lastEventTime time.Time

	// worker handles the lifecycle of the processing worker thread.
	worker *util.Worker

	// Condition used to notify callers of AwaitFullSync that the full sync has occurred.
	fullSyncCond *sync.Cond
}

type postProcessHookFn func()

// NewProcessor returns a new instance of a Processor
func NewProcessor(src Source, distributor Distributor, cfg *Config) *Processor {
	stateStrategy := publish.NewStrategyWithDefaults()

	return newProcessor(src, cfg, metadata.Types, stateStrategy, distributor, nil)
}

func newProcessor(
	src Source,
	cfg *Config,
	schema *resource.Schema,
	stateStrategy *publish.Strategy,
	distributor Distributor,
	postProcessHook postProcessHookFn) *Processor {
	now := time.Now()

	p := &Processor{
		stateStrategy:   stateStrategy,
		distributor:     distributor,
		source:          src,
		eventCh:         make(chan resource.Event, 1024),
		postProcessHook: postProcessHook,
		worker:          util.NewWorker("runtime processor", scope),
		lastEventTime:   now,
		fullSyncCond:    sync.NewCond(&sync.Mutex{}),
	}
	stateListener := processing.ListenerFromFn(func(c resource.Collection) {
		// When the state indicates a change occurred, update the publishing strategy
		if p.distribute {
			stateStrategy.OnChange()
		}
	})
	p.state = newState(schema, cfg, stateListener)

	// Publish ServiceEntry events as soon as they occur.
	p.serviceEntryHandler = serviceentry.NewHandler(cfg.DomainSuffix, processing.ListenerFromFn(func(_ resource.Collection) {
		scope.Debug("Processor.process: publish serviceEntry")
		s := p.serviceEntryHandler.BuildSnapshot()
		p.distributor.SetSnapshot(groups.SyntheticServiceEntry, s)
	}))

	p.handler = buildDispatcher(p.state, p.serviceEntryHandler)
	return p
}

// Start the processor. This will cause processor to listen to incoming events from the provider
// and publish component configuration via the Distributor.
func (p *Processor) Start() error {
	setupFn := func() error {
		err := p.source.Start(func(e resource.Event) {
			p.eventCh <- e
		})
		if err != nil {
			return fmt.Errorf("runtime unable to Start source: %v", err)
		}
		return nil
	}

	runFn := func(ctx context.Context) {
		scope.Info("Starting processor...")
		defer func() {
			scope.Debugf("Process.process: Exiting worker thread")
			p.source.Stop()
			close(p.eventCh)
			p.stateStrategy.Close()
		}()

		scope.Debug("Starting process loop")

		for {
			select {
			case <-ctx.Done():
				// Graceful termination.
				scope.Debug("Processor.process: done")
				return
			case e := <-p.eventCh:
				p.processEvent(e)
			case <-p.stateStrategy.Publish:
				scope.Debug("Processor.process: publish")
				s := p.state.buildSnapshot()
				p.distributor.SetSnapshot(groups.Default, s)
			}

			if p.postProcessHook != nil {
				p.postProcessHook()
			}
		}
	}

	return p.worker.Start(setupFn, runFn)
}

// Stop the processor.
func (p *Processor) Stop() {
	scope.Info("Stopping processor...")
	p.worker.Stop()
}

// AwaitFullSync waits until the full sync event is received from the source. For testing purposes only.
func (p *Processor) AwaitFullSync(timeout time.Duration) error {
	return wait.WithTimeout(func() {
		p.fullSyncCond.L.Lock()
		defer p.fullSyncCond.L.Unlock()
		if p.distribute {
			// Already synced. Nothing to do.
			return
		}

		p.fullSyncCond.Wait()
	}, timeout)
}

func (p *Processor) processEvent(e resource.Event) {
	if scope.DebugEnabled() {
		scope.Debugf("Incoming source event: %v", e)
	}
	p.recordEvent()

	if e.Kind == resource.FullSync {
		scope.Infof("Synchronization is complete, starting distribution.")

		p.fullSyncCond.L.Lock()
		p.distribute = true
		p.fullSyncCond.Broadcast()
		p.fullSyncCond.L.Unlock()

		p.stateStrategy.OnChange()
		return
	}

	p.handler.Handle(e)
}

func (p *Processor) recordEvent() {
	now := time.Now()
	monitoring.RecordProcessorEventProcessed(now.Sub(p.lastEventTime))
	p.lastEventTime = now
}

func buildDispatcher(state *State, serviceEntryHandler processing.Handler) *processing.Dispatcher {
	b := processing.NewDispatcherBuilder()

	// Route all types to the state, except for those required by the serviceEntryHandler.
	stateSchema := resource.NewSchemaBuilder().RegisterSchema(state.schema).UnregisterSchema(serviceentry.Schema).Build()
	for _, spec := range stateSchema.All() {
		b.Add(spec.Collection, state)
	}

	// Route all other types to the serviceEntryHandler
	for _, spec := range serviceentry.Schema.All() {
		b.Add(spec.Collection, serviceEntryHandler)
	}

	return b.Build()
}
