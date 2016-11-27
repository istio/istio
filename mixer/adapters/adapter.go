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

package adapters

// AdapterConfig is used to configure an adapter.
type AdapterConfig interface{}

// Adapter represents the handle that the mixer has on an individual adapter. The mixer holds on
// to one of these per logical adapter, which the mixer uses to control the lifecycle of both
// adapters and adapter instances.
type Adapter interface {
	// Name returns the official name of this adapter for use in diagnostics and in config
	Name() string

	// Description returns a user-friendly description of this adapter.
	Description() string

	// DefaultConfig returns a default configuration struct for this adapter.
	// This will be used by the configuration system to establish the shape of a block
	// of configuration state passed to the Activate function.
	DefaultConfig() AdapterConfig

	// Activate the adapter with the given configuration. Once an adapter is active,
	// the mixer can start calling the newInstance function to instantiate the adapter
	Activate(config AdapterConfig) error

	// Deactivate the adapter, allowing it to clean up any resources it might be holding.
	// Once this function is called, the mixer may no longer call the newInstance function.
	Deactivate()

	// DefaultInstanceConfig returns a default configuration struct for instances
	// of this adapter. This will be used by the configuration system to establish
	// the shape of a block of configuration state passed to the Activate function
	DefaultInstanceConfig() InstanceConfig

	// newInstance creates a single instance of the adapter based on the supplied configuration.
	NewInstance(config InstanceConfig) (Instance, error)
}
