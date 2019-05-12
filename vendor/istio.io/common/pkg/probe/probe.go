// Copyright 2018 Istio Authors
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

// Package probe provides liveness / readiness probe.
package probe

import "errors"

var errUninitialized = errors.New("uninitialized")

// SupportsProbe provides the interface to register itself to a controller.
type SupportsProbe interface {
	RegisterProbe(c Controller, name string)
}

// Probe manages the availability status used by Controller. Probe also
// implements SupportsProbe by itself, therefore a struct which embeds this
// can be registered.
type Probe struct {
	c      Controller
	name   string
	status error
}

// NewProbe creates a new Emitter instance.
func NewProbe() *Probe {
	return &Probe{status: errUninitialized}
}

var _ SupportsProbe = &Probe{}

// RegisterProbe implements SupportsProbe interface.
func (p *Probe) RegisterProbe(c Controller, name string) {
	p.c = c
	p.name = name
	c.register(p, p.IsAvailable())
}

// IsAvailable returns the current status.
func (p *Probe) IsAvailable() error {
	return p.status
}

// SetAvailable sets the new status, and notifies the controller.
func (p *Probe) SetAvailable(newStatus error) {
	if p.status != newStatus {
		p.status = newStatus
		if p.c != nil {
			p.c.onChange(p, newStatus)
		}
	}
}

// String implements fmt.Stringer interface.
func (p *Probe) String() string {
	return p.name
}
