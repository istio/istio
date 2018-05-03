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

package state

import (
	"istio.io/istio/galley/pkg/api/service/dev"
	"istio.io/istio/galley/pkg/model/distributor"
	"istio.io/istio/galley/pkg/model/resource"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("runtime", "Galley runtime", 0)

// Global is the total state instance.
type Global struct {
	counter distributor.BundleVersion
	mixers  []*Mixer
}

// New returns a new Global.
func New() *Global {
	// TODO: This currently creates one mixer state. We should come up with a way to per-component states.
	return &Global{
		mixers: []*Mixer{newMixerState("")},
	}
}

// ApplyProducerService applies the producer service to the current configuration state instance.
func (g *Global) ApplyProducerService(key resource.VersionedKey, s *dev.ProducerService) {
	scope.Debugf("Applying producer service: key='%v'", key)

	for _, m := range g.mixers {
		if m.applyProducerService(key, s) {
			g.counter++
			m.version = g.counter
		}
	}
}

// RemoveProducerService removes the producer service from the current configuration state instance.
func (g *Global) RemoveProducerService(key resource.VersionedKey) {
	scope.Debugf("Removing producer service: key='%v'", key)

	for _, m := range g.mixers {
		if m.removeProducerService(key) {
			g.counter++
			m.version = g.counter
		}
	}
}

// GetNewBundles returns new bundles since the supplied since version.
func (g *Global) GetNewBundles(since distributor.BundleVersion) ([]distributor.Bundle, distributor.BundleVersion) {

	var result []distributor.Bundle
	version := since

	for _, m := range g.mixers {
		if m.version <= since {
			continue
		}

		result = append(result, m)

		if version < m.version {
			version = m.version
		}
	}

	return result, version
}
