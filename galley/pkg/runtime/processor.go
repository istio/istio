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
	"time"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/groups"
	"istio.io/istio/galley/pkg/runtime/monitoring"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/publish"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/util"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("runtime", "Galley runtime", 0)

// Processor is the main control-loop for processing incoming config events and translating them into
// component configuration
type Processor struct {
	// source interface for retrieving the events from.
	source Source

	// handler for events.
	handler processing.Handler

	eventCh chan resource.Event

	// The current in-memory configuration State
	state *State

	// hook that gets called after each event processing. Useful for testing.
	postProcessHook postProcessHookFn

	// lastEventTime records the last time an event was received.
	lastEventTime time.Time

	worker *util.Worker
}

type postProcessHookFn func()

// NewProcessor returns a new instance of a Processor
func NewProcessor(src Source, distributor publish.Distributor, cfg *Config) *Processor {
	state := newState(groups.Default, metadata.Types, cfg, publish.NewStrategyWithDefaults(), distributor)
	return newProcessor(state, src, nil)
}

func newProcessor(
	state *State,
	src Source,
	postProcessHook postProcessHookFn) *Processor {

	now := time.Now()
	return &Processor{
		handler:         buildDispatcher(state),
		state:           state,
		source:          src,
		eventCh:         make(chan resource.Event, 1024),
		postProcessHook: postProcessHook,
		worker:          util.NewWorker("runtime processor", scope),
		lastEventTime:   now,
	}
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
			close(p.eventCh)
			p.state.close()
		}()

		defer p.source.Stop()

		scope.Debug("Starting process loop")

		for {
			select {
			case <-ctx.Done():
				// Graceful termination.
				scope.Debug("Processor.process: done")
				return
			case e := <-p.eventCh:
				p.processEvent(e)
			case <-p.state.strategy.Publish:
				scope.Debug("Processor.process: publish")
				p.state.publish()
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

func (p *Processor) processEvent(e resource.Event) {
	if scope.DebugEnabled() {
		scope.Debugf("Incoming source event: %v", e)
	}
	p.recordEvent()

	if e.Kind == resource.FullSync {
		scope.Infof("Synchronization is complete, starting distribution.")
		p.state.onFullSync()
		return
	}

	p.handler.Handle(e)
}

func (p *Processor) recordEvent() {
	now := time.Now()
	monitoring.RecordProcessorEventProcessed(now.Sub(p.lastEventTime))
	p.lastEventTime = now
}

func buildDispatcher(states ...*State) *processing.Dispatcher {
	b := processing.NewDispatcherBuilder()
	for _, state := range states {
		for _, spec := range state.schema.All() {
			b.Add(spec.Collection, state)
		}
	}
	return b.Build()
}
