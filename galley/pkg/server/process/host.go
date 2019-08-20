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

import (
	"errors"
	"sync"
)

// Host is a process host that takes care of start/stop of sub-components.
type Host struct {
	mu         sync.Mutex
	started    bool
	components []Component
}

// Add a new component to this host. It panics if the host is already started.
func (h *Host) Add(c Component) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		panic("Host.Add: host is already started")
	}
	h.components = append(h.components, c)
}

// Start the process.Host. If any of the components starts to fail, then an error is returned. If there is an error
// then all successfully started components will be stopped before control is returned.
func (h *Host) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return errors.New("process.Host: already started")
	}

	for i, c := range h.components {
		if err := c.Start(); err != nil {
			// Component startup failed. Stop already started components.
			for j := i - 1; j >= 0; j-- {
				h.components[j].Stop()
			}
			return err
		}
	}
	h.started = true

	return nil
}

// Stop the Host. All components will be stopped, before the control is returned.
func (h *Host) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for j := len(h.components) - 1; j >= 0; j-- {
		h.components[j].Stop()
	}
}
