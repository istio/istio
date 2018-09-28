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
	"github.com/pkg/errors"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/snapshot"
)

var scope = log.RegisterScope("runtime", "Galley runtime", 0)

// Processor is the main control-loop for processing incoming config events and translating them into
// component configuration
type Processor struct {
	// source interface for retrieving the events from.
	source Source

	// distributor interface for publishing config snapshots to.
	distributor Distributor

	// The heuristic publishing strategy
	strategy *publishingStrategy

	schema *resource.Schema

	// events channel that was obtained from source
	events chan resource.Event

	// channel that gets closed during Shutdown.
	done chan struct{}

	// indicates that the State is built-up enough to warrant distribution.
	distribute bool

	// channel that signals the background process as being stopped.
	stopped chan struct{}

	// The current in-memory configuration State
	state *State

	// hook that gets called after each event processing. Useful for testing.
	postProcessHook postProcessHookFn
}

type postProcessHookFn func()

// NewProcessor returns a new instance of a Processor
func NewProcessor(src Source, distributor Distributor) *Processor {
	return newProcessor(src, distributor, newPublishingStrategyWithDefaults(), metadata.Types, nil)
}

func newProcessor(
	src Source,
	distributor Distributor,
	strategy *publishingStrategy,
	schema *resource.Schema,
	postProcessHook postProcessHookFn) *Processor {

	return &Processor{
		source:          src,
		distributor:     distributor,
		strategy:        strategy,
		schema:          schema,
		postProcessHook: postProcessHook,
	}
}

// Start the processor. This will cause processor to listen to incoming events from the provider
// and publish component configuration via the Distributor.
func (p *Processor) Start() error {
	scope.Info("Starting processor...")

	if p.events != nil {
		scope.Warn("Processor has already started")
		return errors.New("already started")
	}

	p.distribute = false

	events, err := p.source.Start()
	if err != nil {
		scope.Warnf("Unable to Start source: %v", err)
		return err
	}

	p.events = events
	p.state = newState(p.schema)

	p.done = make(chan struct{})
	p.stopped = make(chan struct{})
	go p.process()

	return nil
}

// Stop the processor.
func (p *Processor) Stop() {
	scope.Info("Stopping processor...")

	if p.events == nil {
		scope.Warnf("Processor has already stopped")
		return
	}

	p.source.Stop()

	close(p.done)
	<-p.stopped

	p.events = nil
	p.done = nil
	p.state = nil
	p.distribute = false
}

func (p *Processor) process() {
	scope.Debugf("Starting process loop")

loop:
	for {
		select {

		// Incoming events are received through p.events
		case e := <-p.events:
			scope.Debugf("Processor.process: event: %v", e)
			if p.processEvent(e) {
				scope.Debugf("Processor.process: event: %v, signaling onChange", e)
				p.strategy.onChange()
			}

		case <-p.strategy.publish:
			scope.Debug("Processor.process: publish")
			p.publish()

		// p.done signals the graceful Shutdown of the processor.
		case <-p.done:
			scope.Debug("Processor.process: done")
			break loop
		}

		if p.postProcessHook != nil {
			p.postProcessHook()
		}
	}

	p.strategy.reset()
	close(p.stopped)
	scope.Debugf("Process.process: Exiting process loop")
}

func (p *Processor) processEvent(e resource.Event) bool {
	scope.Debugf("Incoming source event: %v", e)

	if e.Kind == resource.FullSync {
		scope.Infof("Synchronization is complete, starting distribution.")
		p.distribute = true
		return true
	}

	return p.state.apply(e) && p.distribute
}

func (p *Processor) publish() {
	sn := p.state.buildSnapshot()

	// TODO: Set the appropriate name for publishing
	p.distributor.SetSnapshot(snapshot.DefaultGroup, sn)
}
