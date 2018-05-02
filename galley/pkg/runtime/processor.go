//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package runtime

import (
	"sync"

	"github.com/pkg/errors"
	"istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/galley/pkg/model/provider"
	"istio.io/istio/galley/pkg/model/resource"
	st "istio.io/istio/galley/pkg/runtime/state"

	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("runtime", "Galley runtime", 0)

// Processor is the main control-loop for processing incoming config events and translating them into
// component configuration
type Processor struct {
	stateLock sync.Mutex

	source      provider.Interface
	distributor distributor.Interface

	ch         chan provider.Event
	done       chan struct{}
	distribute bool

	processEnd sync.WaitGroup

	// The current in-memory configuration state
	state     *st.Instance
	watermark distributor.BundleVersion
}

// New returns a new instance of a Processor
func New(source provider.Interface, distributor distributor.Interface) *Processor {
	return &Processor{
		source:      source,
		distributor: distributor,
	}
}

// Start the processor. This will cause processor to listen to incoming events from the provider
// and publish component configuration via the distributor.
func (p *Processor) Start() error {
	scope.Info("Starting processor...")

	p.stateLock.Lock()
	defer p.stateLock.Unlock()

	if p.ch != nil {
		scope.Warn("Processor has already started")
		return errors.New("already started")
	}

	p.state = st.New()
	p.watermark = 0
	p.distribute = false

	err := p.distributor.Initialize()
	if err != nil {
		scope.Warnf("Unable to initialize distributor: %v", err)
		return err
	}

	ch, err := p.source.Start()
	if err != nil {
		scope.Warnf("Unable to initialize provider: %v", err)
		p.distributor.Shutdown()
		return err
	}

	p.ch = ch

	p.done = make(chan struct{})
	p.processEnd.Add(1)
	go p.process()

	return nil
}

// Stop the processor.
func (p *Processor) Stop() {
	scope.Info("Stopping processor...")
	p.stateLock.Lock()
	defer p.stateLock.Unlock()

	if p.ch == nil {
		scope.Warnf("Processoer has already stopped")
		return
	}

	p.source.Stop()

	close(p.done)
	p.processEnd.Wait()

	p.distributor.Shutdown()

	close(p.ch)

	p.ch = nil
	p.done = nil
	p.state = nil
	p.distribute = false
}

func (p *Processor) process() {
	scope.Debugf("Starting process loop")
loop:
	for {
		select {
		case e := <-p.ch:
			p.processEvent(e)
		case <-p.done:
			break loop
		}
	}

	scope.Debugf("Exiting process loop")
	p.processEnd.Done()
}

func (p *Processor) processEvent(e provider.Event) {
	scope.Debugf("Incoming source event: %v", e)

	if e.Id.Kind != resource.ProducerServiceKind {
		scope.Debugf("Skipping unknown resource kind: '%v'", e.Id.Kind)
		return
	}

	switch e.Kind {
	case provider.Added, provider.Updated:
		res, err := p.source.Get(e.Id.Key)
		if err != nil {
			scope.Errorf("get error: %v", err)
			// TODO: Handle error case
			panic(err)
		}
		// TODO: Support other types
		ps := res.Item.(*dev.ProducerService)
		p.state.ApplyProducerService(e.Id, ps)

	case provider.Deleted:
		p.state.RemoveProducerService(e.Id)

	case provider.FullSync:
		scope.Debugf("starting distribution after full sync")
		p.distribute = true

	default:
		scope.Warnf("Unknown provider event: %s (%v)", e.Kind, e)
	}

	if p.distribute {
		bundles, version := p.state.GetNewBundles(p.watermark)
		for _, b := range bundles {
			p.distributor.Distribute(b)
		}
		p.watermark = version
	}
}
