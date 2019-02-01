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
	"time"

	"github.com/pkg/errors"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/monitoring"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/publish"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/log"
	sn "istio.io/istio/pkg/mcp/snapshot"
)

var scope = log.RegisterScope("runtime", "Galley runtime", 0)

// Processor is the main control-loop for processing incoming config events and translating them into
// component configuration
type Processor struct {
	// source interface for retrieving the events from.
	source Source

	// distributor for distributing snapshots.
	distributor Distributor

	// events channel that was obtained from source
	events chan resource.Event

	// handler for events.
	handler processing.Handler

	// channel that gets closed during Shutdown.
	done chan struct{}

	// channel that signals the background process as being stopped.
	stopped chan struct{}

	// publishers that the processor is controlling.
	// At this time, we know the exact number of publishers, so this is an array with a well-known size.
	// This is important, as the select method below uses this.
	// TODO: Fixed-size array should disappear once processing pipeline can process all events concurrently.
	publishers [1]*publisher

	// hook that gets called after each event processing. Useful for testing.
	postProcessHook postProcessHookFn

	// lastEventTime records the last time an event was received.
	lastEventTime time.Time
}

type postProcessHookFn func()

// NewProcessor returns a new instance of a Processor
func NewProcessor(src Source, distributor Distributor, cfg *Config) *Processor {
	return newProcessor(src, distributor, cfg, nil)
}

func newProcessor(
	src Source,
	distributor Distributor,
	cfg *Config,
	postProcessHook postProcessHookFn) *Processor {

	now := time.Now()

	cfgHandler := newConfigHandler(metadata.Types, cfg)
	cfgPublisher := newPublisher(sn.DefaultGroup, cfgHandler, distributor, publish.NewStrategyWithDefaults())
	cfgHandler.SetListener(cfgPublisher)

	b := processing.NewDispatcherBuilder()
	cfgHandler.registerHandlers(b)
	dispatcher := b.Build()

	publishers := [1]*publisher{
		cfgPublisher,
	}

	return &Processor{
		source:          src,
		distributor:     distributor,
		handler:         dispatcher,
		publishers:      publishers,
		postProcessHook: postProcessHook,
		done:            make(chan struct{}),
		stopped:         make(chan struct{}),
		lastEventTime:   now,
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

	events := make(chan resource.Event, 1024)
	err := p.source.Start(func(e resource.Event) {
		events <- e
	})
	if err != nil {
		scope.Warnf("Unable to Start source: %v", err)
		return err
	}

	p.events = events

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
	close(p.events)

	p.events = nil
	p.done = nil
}

func (p *Processor) process() {
	scope.Debugf("Starting process loop")

loop:
	for {
		select {

		// Incoming events are received through p.events
		case e := <-p.events:
			p.processEvent(e)

		case <-p.publishers[0].channel():
			scope.Debug("Processor.process: publish")
			p.publishers[0].publish()

		// p.done signals the graceful Shutdown of the processor.
		case <-p.done:
			scope.Debug("Processor.process: done")
			break loop
		}

		if p.postProcessHook != nil {
			p.postProcessHook()
		}
	}

	for _, publisher := range p.publishers {
		if err := publisher.Close(); err != nil {
			scope.Errorf("Error closing publisher: %v", err)
		}
	}

	close(p.stopped)
	scope.Debugf("Process.process: Exiting process loop")
}

func (p *Processor) processEvent(e resource.Event) {
	scope.Debugf("Incoming source event: %v", e)
	p.recordEvent()

	if e.Kind == resource.FullSync {
		scope.Infof("Synchronization is complete, starting distribution.")
		for _, publisher := range p.publishers {
			publisher.start()
		}
		return
	}

	p.handler.Handle(e)
}

func (p *Processor) recordEvent() {
	now := time.Now()
	monitoring.RecordProcessorEventProcessed(now.Sub(p.lastEventTime))
	p.lastEventTime = now
}
