// Copyright 2017 Istio Authors
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

package framework

// Component is a interface of a test component
type Component interface {
	// GetName returns component name
	GetName() string

	// GetConfig returns the config of this component for outside use
	// It can be called anytime during the whole component lifetime
	// It's recommended to use sync.Mutex to lock data while read
	// in case component itself is accessing config
	GetConfig() Config

	// SetConfig sets a config into this component
	// Initial config is set when during initialization
	// But config can be updated at runtime using SetConfig
	// Component needs to implement extra functions to apply new configs
	// It's recommended to use sync.Mutex to lock data while write
	// in case component itself is accessing config
	SetConfig(config Config) error

	// GetStatus returns the status of this component for outside use
	// It can be called anytime during the whole component lifetime
	// It's recommended to use sync.Mutex to lock data while read
	// in case component itself is updating status
	GetStatus() Status

	// Start sets up for this component
	// Start is being called in framework.StartUp()
	Start() error

	// Stop stops this component and clean it up
	// Stop is being called in framework.TearDown()
	Stop() error

	// IsAlive checks if component is alive/running
	IsAlive() (bool, error)
}
