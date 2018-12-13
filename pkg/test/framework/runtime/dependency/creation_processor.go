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

package dependency

import (
	"fmt"
	"reflect"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/key"
)

// creationProcessor is used by Manager to resolve creation order for components.
type creationProcessor struct {
	scope    lifecycle.Scope
	mgr      *Manager
	provided map[component.ID]component.Descriptor
	required map[key.Instance]*requiredEntry
}

// newCreationProcessor creates a new creation processor for the Manager.
func newCreationProcessor(mgr *Manager, scope lifecycle.Scope) *creationProcessor {
	return &creationProcessor{
		scope:    scope,
		mgr:      mgr,
		provided: make(map[component.ID]component.Descriptor),
		required: make(map[key.Instance]*requiredEntry),
	}
}

type requiredEntry struct {
	desc component.Descriptor
	ids  map[component.ID]bool
}

func newRequiredEntry(d component.Descriptor) *requiredEntry {
	return &requiredEntry{
		desc: d,
		ids:  make(map[component.ID]bool),
	}
}

// Add a new component to be created by this processor.
func (p *creationProcessor) Add(desc component.Descriptor) component.RequirementError {
	// Check if the component was already created.
	if c := p.mgr.GetComponent(desc.ID); c != nil {
		if !reflect.DeepEqual(c.Descriptor(), desc) {
			return resolutionError(fmt.Errorf("cannot add component `%s`, already running with `%s`", desc.FriendlyName(), c.Descriptor().FriendlyName()))
		}
		if c.Scope().IsLower(p.scope) {
			return resolutionError(fmt.Errorf("component `%s` already exists with lower lifecycle scope: %s", desc.FriendlyName(), c.Scope()))
		}

		// The same component was already created with the same scope.
		return nil
	}

	// Check if we've already processed this component.
	if oldDesc, ok := p.provided[desc.ID]; ok {
		if reflect.DeepEqual(oldDesc, desc) {
			// The same deployment was required multiple times - just ignore it.
			return nil
		}
		return resolutionError(fmt.Errorf("required duplicate components: %v, %v", oldDesc, desc))
	}

	p.provided[desc.ID] = desc

	// Add the requirements for this component.
	k := key.For(desc)
	entry := newRequiredEntry(desc)
	for _, req := range desc.Requires {
		if reqDesc, ok := req.(*component.Descriptor); ok {
			if err := p.Add(*reqDesc); err != nil {
				return err
			}
			// Indicate it's required.
			entry.ids[reqDesc.ID] = true
		} else if cid, ok := req.(*component.ID); ok {
			if c := p.mgr.GetComponent(*cid); c != nil {
				if c.Scope().IsLower(p.scope) {
					return resolutionError(
						fmt.Errorf("previously created component %s (required by %s) has insufficient scope: %s, required: %s",
							req, desc.ID, c.Scope(), p.scope))
				}
			} else {
				// Indicate it's required.
				entry.ids[*cid] = true
			}
		}
	}
	p.required[k] = entry
	return nil
}

// CreateComponents contained in this processor in the appropriate order.
func (p *creationProcessor) CreateComponents() component.RequirementError {
	// Apply appropriate defaults for any missing components.
	if err := p.applyDefaults(); err != nil {
		return err
	}

	for len(p.required) > 0 {
		progress := false
		for desc, entry := range p.required {
			// Remove requirements for any components that have been created.
			for id := range entry.ids {
				if p.mgr.GetComponent(id) != nil {
					delete(entry.ids, id)
				}
			}

			// If all the requirements have been satisified, create the component.
			if len(entry.ids) == 0 {
				progress = true

				// Mark this requirement as satisfied.
				delete(p.required, desc)

				// Create the component.
				if _, err := p.mgr.requireComponent(entry.desc, p.scope); err != nil {
					return err
				}
			}
		}

		if !progress {
			return resolutionError(fmt.Errorf("unable to determine creation order for required components"))
		}
	}
	return nil
}

func (p *creationProcessor) applyDefaults() component.RequirementError {
	done := false
	for !done {
		done = true
		for _, entry := range p.required {
			for compID := range entry.ids {
				if _, ok := p.provided[compID]; !ok {
					done = false

					// Not found... Use a default.
					desc, err := p.mgr.GetDefaultDescriptor(compID)
					if err != nil {
						return resolutionError(err)
					}

					if err := p.Add(desc); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
