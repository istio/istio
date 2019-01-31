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
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"reflect"
	"time"
)

// creationProcessor is used by Manager to resolve creation order for components.
type creationProcessor struct {
	scope lifecycle.Scope
	mgr   *Manager

	// The entries that need to be processed.
	required map[namedId]*reqEntry
}

// newCreationProcessor creates a new creation processor for the Manager.
func newCreationProcessor(mgr *Manager, scope lifecycle.Scope) *creationProcessor {
	return &creationProcessor{
		scope:    scope,
		mgr:      mgr,
		required: make(map[namedId]*reqEntry),
	}
}

// A struct representing a named ID. This is used as the key to what requirements need to be
// created, as the Variant is not important, just the ID and Name.
type namedId struct {
	Name string
	ID   component.ID
}

// Create a named ID for the given name and ID.
func namedIdFor(name string, ID component.ID) namedId {
	return namedId{
		Name: name,
		ID:   ID,
	}
}

// A struct representing a single parsed requirement.
type reqEntry struct {
	id     namedId
	desc   *component.Descriptor
	config component.Configuration

	// Child entries that need to be processed before this entry can be created.
	children map[namedId]bool
}

func (p *creationProcessor) ProcessRequirements(reqs []component.Requirement) component.RequirementError {
	for _, req := range reqs {
		entry, err := parseRequirement(req)
		if err != nil {
			return err
		}
		if err := p.addRequirement(entry); err != nil {
			return err
		}
	}
	return nil
}

// Parses a requirement into a requirement entry. This takes care of unwrapping the requirement
// envelope into a single flat type that we don't need to reflect over.
func parseRequirement(req component.Requirement) (r *reqEntry, err component.RequirementError) {
	if c, ok := req.(*component.ConfiguredRequirement); ok {
		if r, err = parseRequirement(c.GetRequirement()); err != nil {
			return
		}
		r.id.Name = c.GetName()
		r.config = c.GetConfiguration()
		return
	}
	if id, ok := req.(*component.ID); ok {
		r = &reqEntry{
			id:       namedIdFor("", *id),
			children: make(map[namedId]bool),
		}
		return
	}
	if d, ok := req.(*component.Descriptor); ok {
		r = &reqEntry{
			id:       namedIdFor("", d.ID),
			desc:     d,
			children: make(map[namedId]bool),
		}
		return
	}
	err = resolutionError(fmt.Errorf("unsupported requirement type: %v", req))
	return
}

// Adds a requirement to our map of requirements. This verifies the requirement is not overwriting
// a requirement of the same key with mismatched contents. We allow more specific overwrites.
func (p *creationProcessor) addRequirement(entry *reqEntry) component.RequirementError {
	// First load up the children into the entry if it has a descriptor.
	if err := p.loadChildren(entry); err != nil {
		return err
	}

	// Now check if there is an existing entry, if so we need to validate that they match or this
	// entry should replace the previous one (since it is more specific)
	if oldEntry, ok := p.required[entry.id]; ok {
		if reflect.DeepEqual(oldEntry, entry) {
			// This is the same entry we already have in our list, ignore it.
			return nil
		}
		// If the old entry does not have a descriptor, but it did have config and that config does not match the new config, error.
		if oldEntry.desc == nil {
			if oldEntry.config != nil && !reflect.DeepEqual(oldEntry.config, entry.config) {
				return resolutionError(fmt.Errorf("required mismatched configuration for %v: %v, %v", entry.id, oldEntry, entry))
			}
		} else {
			if entry.desc == nil {
				// The previous entry was more specific, keep it.
				return nil
			}
			// Both have descriptors, compare them.
			if !reflect.DeepEqual(oldEntry.desc, entry.desc) {
				return resolutionError(fmt.Errorf("required mismatched descriptors for %v: %v, %v", entry.id, oldEntry.desc, entry.desc))
			}
			// Descriptors match, next check config.
			if oldEntry.config != nil {
				if entry.config == nil {
					// The previous entry was more specific, keep it.
					return nil
				}
				if !reflect.DeepEqual(oldEntry.config, entry.config) {
					return resolutionError(fmt.Errorf("required mismatched configuration for %v: %v, %v", entry.id, oldEntry.config, entry.config))
				}
				// This shouldn't be possible, we should have had a match. Report an error.
				return resolutionError(fmt.Errorf("resolution is confused about entries for %v: %v, %v", entry.id, oldEntry, entry))
			}
		}
	}
	p.required[entry.id] = entry

	// If the entry has a descriptor, process all of the child requirements.
	if entry.desc != nil {
		return p.ProcessRequirements(entry.desc.Requires)
	}
	return nil
}

func (p *creationProcessor) loadChildren(entry *reqEntry) component.RequirementError {
	if entry.desc == nil {
		return nil
	}
	for _, childReq := range entry.desc.Requires {
		child, err := parseRequirement(childReq)
		if err != nil {
			return err
		}
		entry.children[child.id] = true
	}
	return nil
}

// For any required entry that does not have a descriptor, find a default and add that as a
// requirement. This will replace the entry with just an ID with one with a descriptor, as well as
// adding any child requirements.
func (p *creationProcessor) ApplyDefaults() component.RequirementError {
	done := false
	var toProcess []component.Requirement = nil
	for !done {
		for _, entry := range p.required {
			if entry.desc == nil {
				desc, err := p.mgr.GetDefaultDescriptor(entry.id.ID)
				if err != nil {
					return resolutionError(err)
				}
				toProcess = append(toProcess, component.NewNamedRequirement(entry.id.Name, &desc))
			}
		}
		done = len(toProcess) == 0
		if !done {
			err := p.ProcessRequirements(toProcess)
			if err != nil {
				return err
			}
			toProcess = nil
		}
		time.Sleep(time.Second)
	}
	return nil
}

// CreateComponents contained in this processor in the appropriate order.
func (p *creationProcessor) CreateComponents() component.RequirementError {
	for len(p.required) > 0 {
		progress := false
		for _, entry := range p.required {
			// Remove requirements for any components that have been created.
			for childId := range entry.children {
				if p.mgr.GetComponent(childId.Name, childId.ID) != nil {
					delete(entry.children, childId)
				}
			}

			// If all the requirements have been satisified, create the component.
			if len(entry.children) == 0 {
				progress = true

				// Mark this requirement as satisfied.
				delete(p.required, entry.id)

				// Create the component.
				if _, err := p.mgr.requireComponent(entry.id.Name, *entry.desc, p.scope); err != nil {
					return err
				}
			}
		}

		// If we failed to make process on any of the required entries, report an error.
		if !progress {
			return resolutionError(fmt.Errorf("unable to determine creation order for required components, remaining requirements: %v", p.required))
		}
	}
	return nil
}
