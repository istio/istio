// Copyright 2019 Istio Authors
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

package process

// Component of a running process.
type Component interface {
	// Start the component. If the component succeeds in starting, then it should complete its initialization by the
	// time Start returns.
	Start() error

	// Stop the component. When called multiple times, Stop should stop the component idempotently. By the time Stop()
	// returns control, the Component should cease all background operations.
	Stop()
}

type component struct {
	startFn func() error
	stopFn  func()
}

// Start implements Component
func (c *component) Start() error {
	return c.startFn()
}

// Stop implements Component
func (c *component) Stop() {
	c.stopFn()
}

// ComponentFromFns creates a component from functions.
func ComponentFromFns(startFn func() error, stopFn func()) Component {
	return &component{
		startFn: startFn,
		stopFn:  stopFn,
	}
}
