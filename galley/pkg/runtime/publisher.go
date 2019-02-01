//  Copyright 2019 Istio Authors
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
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/publish"
	"istio.io/istio/galley/pkg/runtime/resource"
)

type publisher struct {
	// name to use when publishing a snapshot
	name string

	// snapshotter is used for building a snapshot
	snapshotter processing.Snapshotter

	// distributor distributes snapshots
	distributor Distributor

	// strategy for publishing the snapshot
	strategy *publish.Strategy

	distribute bool
}

var _ processing.Listener = &publisher{}

// newPublisher returns a new processing.Publisher
func newPublisher(name string, snapshotter processing.Snapshotter, distributor Distributor, s *publish.Strategy) *publisher {
	return &publisher{
		name:        name,
		snapshotter: snapshotter,
		distributor: distributor,
		strategy:    s,
	}
}

// CollectionChanged implements runtime.Listener
func (p *publisher) CollectionChanged(c resource.Collection) {
	if p.distribute {
		p.strategy.OnChange()
	}
}

func (p *publisher) channel() <-chan struct{} {
	return p.strategy.Publish
}

func (p *publisher) publish() {
	sn := p.snapshotter.Snapshot()
	p.distributor.SetSnapshot(p.name, sn)
}

// start implements processing.Publisher
func (p *publisher) start() {
	p.distribute = true
	p.strategy.OnChange()
}

// Close implements io.Closer
func (p *publisher) Close() error {
	p.strategy.Reset()
	return nil
}
