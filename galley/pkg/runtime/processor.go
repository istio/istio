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

	serviceconfig "istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/model"
	"istio.io/istio/galley/pkg/model/generator"
	"istio.io/istio/pkg/log"
)

type Processor struct {
	stateLock sync.Mutex

	source      Source
	distributor Distributor

	ch   chan Event
	done chan struct{}

	processEnd sync.WaitGroup

	state *MixerConfigState
}

func New(source Source, distributor Distributor) *Processor {
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

	in.state = newMixerConfigState()

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
	in.state = nil
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

func (in *Processor) processEvent(e Event) {
	log.Debugf("=== Incoming event: %v", e)
	if e.Id.Kind != model.Info.ServiceConfig.Kind {
		return
	}

	switch e.Kind {
	case Added, Updated:
		if in.state.versions[e.Id] == e.Version {
			// Already known, skip.
			log.Debugf("resource/version is already known, skipping: %v/%v", e.Id, e.Version)
			return
		}

		in.state.versions[e.Id] = e.Version
		in.generatePart(e.Id)

	case Deleted:
		if _, ok := in.state.versions[e.Id]; !ok {
			log.Debugf("resource/version is already deleted, skipping: %v/%v", e.Id, e.Version)
			// Already deleted, skip.
			return
		}
		delete(in.state.versions, e.Id)
		delete(in.state.parts, e.Id)

	case FullSync:
		log.Debugf("starting distribution")
		in.distributor.Start()
		return

	default:
		log.Warnf("Unknown event: %s (%v)", e.Kind, e)
	}

	in.distributor.Dispatch(in.state.Generate())
}

func (in *Processor) generatePart(key model.ResourceKey) {
	res, err := in.source.Get(key)
	if err != nil {
		log.Errorf("get error: %v", err)
		// TODO: Handle error case
		panic(err)
	}
	instances, rules := generator.Generate(res.Item.(*serviceconfig.ProducerService), in.state.names)
	in.state.parts[key] = &PartialMixerConfigState{
		Rules:     rules,
		Instances: instances,
	}
}
