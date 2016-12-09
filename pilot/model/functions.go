// Copyright 2016 Google Inc.
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

package model

import multierror "github.com/hashicorp/go-multierror"

// ConfigOutput holds the target configuration output
type ConfigOutput struct {
	// Sources identify the source mapping from the Registry
	// This will grow into finer grained references
	Sources []*ConfigKey
	// Content of the configuration
	Content []byte
}

// Generator renders the config output from the registry
// The generic interface allows the generator to operate on arbitrary
// kinded config objects and generate arbitrary many configuration outputs
type Generator interface {
	// Render outputs configuration artifacts for the target components
	Render(reg Registry) ([]*ConfigOutput, error)
}

// TODO: Differential computation:
// - generator and distributor should have the notion of the registry delta

// TODO: Configuration dataflow:
// - end-to-end push config to output reload
// - association betweeen generated outputs and where they go is the
//   responsibility of the individual consumers
// - diffing with source references
// - status

// ConfigConsumer registers a component to receive configuration output
type ConfigConsumer interface {
	Name() string
	// Generators produce configuration for the component
	Generators() []Generator
	// Distribute pushes configuration to the component
	Distribute([]*ConfigOutput) error
}

// Manager defines the data-flow of the configuration artifacts
type Manager struct {
	Registry        Registry
	ConfigConsumers []ConfigConsumer
}

// PushAll the entire registry
func (m *Manager) PushAll() error {
	var out error
	for _, consumer := range m.ConfigConsumers {
		for _, gen := range consumer.Generators() {
			outputs, err := gen.Render(m.Registry)
			if err != nil {
				out = multierror.Append(out, err)
			} else {
				err = consumer.Distribute(outputs)
				if err != nil {
					out = multierror.Append(out, err)
				}
			}
		}
	}
	return out
}
