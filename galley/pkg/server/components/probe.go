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

package components

import (
	"istio.io/pkg/probe"

	"istio.io/istio/galley/pkg/server/process"
)

// Probe component
type Probe struct {
	controller probe.Controller
	running    bool
}

var _ process.Component = &Probe{}

// NewProbe returns a new probe component
func NewProbe(options *probe.Options) *Probe {
	p := &Probe{}

	if options.IsValid() {
		p.controller = probe.NewFileController(options)
	}

	return p
}

// Controller returns the probe.Controller. It maybe nil, if it is not initialized.
func (p *Probe) Controller() probe.Controller {
	return p.controller
}

// Start implements process.Component
func (p *Probe) Start() error {
	if !p.running {
		if p.controller != nil {
			p.controller.Start()
		}
		p.running = true
	}

	return nil
}

// Stop implements process.Component
func (p *Probe) Stop() {
	if p.running {
		if p.controller != nil {
			if err := p.controller.Close(); err != nil {
				scope.Errorf("Probe terminated with error: %v", err)
			}
		}
		p.running = false
	}
}
