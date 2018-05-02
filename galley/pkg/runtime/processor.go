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
	"istio.io/istio/galley/pkg/model/resource"
	"istio.io/istio/galley/pkg/model/provider"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/galley/pkg/runtime/state"

	"istio.io/istio/pkg/log"
)

type Processor struct {
	stateLock sync.Mutex

	source      provider.Interface
	distributor distributor.Interface

	ch   chan provider.Event
	done chan struct{}

	processEnd sync.WaitGroup

	st        *state.Instance
	watermark distributor.BundleVersion
}

func New(source provider.Interface, distributor distributor.Interface) *Processor {
	return &Processor{
		source:      source,
		distributor: distributor,
	}
}

func (in *Processor) Start() error {
	log.Infof("Starting runtime...")
	in.stateLock.Lock()
	defer in.stateLock.Unlock()

	if in.ch != nil {
		log.Warnf("Already started")
		return errors.New("already started")
	}

	in.st = state.New()
	in.watermark = 0

	err := in.distributor.Initialize()
	if err != nil {
		return err
	}

	ch, err := in.source.Start()
	if err != nil {
		in.distributor.Shutdown()
		return err
	}

	in.ch = ch

	in.done = make(chan struct{})
	in.processEnd.Add(1)
	go in.process()

	return nil
}

func (in *Processor) Stop() {
	log.Infof("Stopping runtime...")
	in.stateLock.Lock()
	defer in.stateLock.Unlock()

	if in.ch == nil {
		log.Warnf("Already stopped")
		return
	}

	in.source.Stop()

	close(in.done)
	in.processEnd.Wait()

	in.distributor.Shutdown()

	close(in.ch)

	in.ch = nil
	in.done = nil
	in.st = nil
}

func (in *Processor) process() {
loop:
	for {
		select {
		case e := <-in.ch:
			in.processEvent(e)
		case <-in.done:
			break loop
		}
	}

	log.Debugf("Exiting process loop")
	in.processEnd.Done()
}

func (in *Processor) processEvent(e provider.Event) {
	log.Debugf("=== Incoming event: %v", e)
	if e.Id.Kind != resource.Known.ProducerService.Kind {
		log.Debugf("Skipping unknown resource kind: '%v'", e.Id.Kind)
		return
	}

	switch e.Kind {
	case provider.Added, provider.Updated:
		res, err := in.source.Get(e.Id.Key)
		if err != nil {
			log.Errorf("get error: %v", err)
			// TODO: Handle error case
			panic(err)
		}
		// TODO: Support other types
		ps := res.Item.(*dev.ProducerService)
		in.st.ApplyProducerService(e.Id, ps)

	case provider.Deleted:
		in.st.RemoveProducerService(e.Id)

	case provider.FullSync:
		log.Debugf("starting distribution")
		in.distributor.Start()
		return

	default:
		log.Warnf("Unknown event: %s (%v)", e.Kind, e)
	}

	bundles, version := in.st.GetNewBundles(in.watermark)
	for _, b := range bundles {
		in.distributor.Distribute(b)
	}
	in.watermark = version
}
