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
	"time"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

// creationProcessor is used by Manager to resolve creation order for components.
type creationProcessor struct {
	scope lifecycle.Scope
	mgr   *Manager

	// The entries that need to be processed.
	required map[component.Key]*reqEntry

	// The IDs that are being provided by the required entries. This is used when processing variants.
	provided map[component.ID]component.Key
}

// newCreationProcessor creates a new creation processor for the Manager.
func newCreationProcessor(mgr *Manager, scope lifecycle.Scope) *creationProcessor {
	return &creationProcessor{
		scope:    scope,
		mgr:      mgr,
		required: make(map[component.Key]*reqEntry),
		provided: make(map[component.ID]component.Key),
	}
}

// A struct representing a single requirement that needs to be created.
type reqEntry struct {
	key  component.Key
	desc *component.Descriptor

	// Child entries that still need to be processed before this entry can be created.
	children map[component.Key]bool
}

func (e reqEntry) String() string {
	result := "Entry{\n"

	result += fmt.Sprintf("Key:          %v\n", e.key)
	if e.desc != nil {
		result += fmt.Sprintf("Descriptor:   %v\n", e.desc)
	}
	if len(e.children) > 0 {
		result += fmt.Sprintf("Children:     %v\n", e.children)
	}
	result += "}\n"

	return result
}

func (p *creationProcessor) ProcessRequirements(reqs []component.Requirement) component.RequirementError {
	for _, req := range reqs {
		entry := createEntry(req)
		if err := p.addRequirement(entry); err != nil {
			return err
		}
	}
	return nil
}

// CreateEntry wraps the requirement in an entry object that can track the state of child
// dependencies that need to be resolved.
func createEntry(req component.Requirement) *reqEntry {
	r := &reqEntry{
		key:      req.GetKey(),
		children: make(map[component.Key]bool),
	}
	if d, ok := req.(*component.Descriptor); ok {
		r.desc = d
	}
	return r
}

// Adds a requirement to our map of requirements. This verifies the requirement is not overwriting
// a requirement of the same key with mismatched contents. We allow more specific overwrites.
func (p *creationProcessor) addRequirement(entry *reqEntry) component.RequirementError {
	// First load up the children into the entry if it has a descriptor.
	if err := p.loadChildren(entry); err != nil {
		return err
	}

	// Now check if there is an existing entry, and if so compare them.
	if oldEntry, ok := p.required[entry.key]; ok {
		override, err := compareEntries(oldEntry, entry)
		if err != nil {
			return err
		}
		if !override {
			return nil
		}
	}

	// If this entry was from an ID and not a descriptor, check if there is an existing descriptor
	// providing this ID and if so we can just return.
	_, ok := p.provided[entry.key.ID]
	if entry.desc == nil && ok {
		return nil
	}

	// If this entry was a descriptor and there is not already a provider for the ID, register it as the provider.
	// TODO(sven): If there are multiple descriptors for an ID a random one will become default, fix that.
	if entry.desc != nil && !ok {
		p.provided[entry.key.ID] = entry.key
	}

	p.required[entry.key] = entry

	// If the entry has a descriptor, process all of the child requirements.
	if entry.desc != nil {
		return p.ProcessRequirements(entry.desc.Requires)
	}
	return nil
}

// CompareEntries compares two request entries, returning true if the new entry should override the old one,
// or an error if they are incompatible.
func compareEntries(oldEntry *reqEntry, entry *reqEntry) (override bool, err component.RequirementError) {
	override = false
	if reflect.DeepEqual(oldEntry, entry) {
		return
	}

	// First compare descriptors, and check if we need to merge or override the descriptor.
	if oldEntry.desc == nil {
		if entry.desc != nil {
			override = true
		}
	} else if entry.desc == nil {
		entry.desc = oldEntry.desc
	} else if !reflect.DeepEqual(oldEntry.desc, entry.desc) {
		return compareDescriptors(oldEntry.desc, entry.desc)
	}
	return
}

func compareDescriptors(oldDesc *component.Descriptor, desc *component.Descriptor) (override bool, err component.RequirementError) {
	if oldDesc.Configuration == nil {
		if desc.Configuration != nil {
			override = true
		}
	} else if desc.Configuration == nil {
		desc.Configuration = oldDesc.Configuration
	} else if !reflect.DeepEqual(oldDesc.Configuration, desc.Configuration) {
		err = resolutionError(fmt.Errorf("required mismatched descriptors for %v: %v, %v", desc.Key, oldDesc, desc))
	}

	if len(oldDesc.Requires) == 0 {
		if len(desc.Requires) > 0 {
			override = true
		}
	} else if len(desc.Requires) == 0 {
		desc.Requires = oldDesc.Requires
	} else if !reflect.DeepEqual(oldDesc.Requires, desc.Requires) {
		err = resolutionError(fmt.Errorf("required mismatched descriptors for %v: %v, %v", desc.Key, oldDesc, desc))
	}

	return
}

// LoadChildren loads up the given entry's children set with all the required child keys.
func (p *creationProcessor) loadChildren(entry *reqEntry) component.RequirementError {
	if entry.desc == nil {
		return nil
	}
	for _, childReq := range entry.desc.Requires {
		child := createEntry(childReq)
		entry.children[child.key] = true
	}
	return nil
}

// For any required entry that does not have a descriptor, find a default and add that as a
// requirement. This will replace the entry with just an ID with one with a descriptor, as well as
// adding any child requirements.
func (p *creationProcessor) ApplyDefaults() component.RequirementError {
	done := false
	var toProcess []component.Requirement
	var toRemove []component.Key
	for !done {
		for _, entry := range p.required {
			if entry.desc == nil {
				// First check if we have a descriptor providing this ID, if so just use that.
				if _, ok := p.provided[entry.key.ID]; ok {
					toRemove = append(toRemove, entry.key)
					continue
				}
				// No descriptor for the ID, lookup the default.
				desc, err := p.mgr.GetDefaultDescriptor(entry.key.ID)
				if err != nil {
					return resolutionError(err)
				}
				toProcess = append(toProcess, &desc)
			}
		}
		// Remove any items marked for removal.
		if len(toRemove) > 0 {
			for _, k := range toRemove {
				delete(p.required, k)
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
			for childID := range entry.children {
				if p.mgr.getComponentByKey(childID) != nil {
					delete(entry.children, childID)
				}
			}

			// If all the requirements have been satisified, create the component.
			if len(entry.children) == 0 {
				progress = true

				// Mark this requirement as satisfied.
				delete(p.required, entry.key)

				// Create the component.
				if _, err := p.mgr.requireComponent(entry, p.scope); err != nil {
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
