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

package registry

import (
	"fmt"
	"sync"

	"istio.io/istio/pkg/log"
)

// Registry is the standard interface for identity registry implementation
type Registry interface {
	Check(string, string) bool
	AddMapping(string, string) error
	DeleteMapping(string, string) error
}

// IdentityRegistry is a naive registry that maintains a mapping between
// identities (as strings): id1 -> id2, id3 -> id4, etc. The method call
// Check(id1, id2) will succeed only if there is a mapping id1 -> id2 stored
// in this registry.
//
// CA can make authorization decisions based on this registry. By creating a
// mapping id1 -> id2, CA will approve CSRs sent only by services running
// as id1 for identity id2.
type IdentityRegistry struct {
	sync.RWMutex
	Map map[string]string
}

// Check checks whether id1 is mapped to id2
func (reg *IdentityRegistry) Check(id1, id2 string) bool {
	reg.RLock()
	mapped, ok := reg.Map[id1]
	reg.RUnlock()
	if !ok || id2 != mapped {
		log.Warnf("Identity %q does not exist or is not mapped to %q", id1, id2)
		return false
	}
	return true
}

// AddMapping adds a mapping id1 -> id2. If id1 is already mapped to
// something else, add fails.
func (reg *IdentityRegistry) AddMapping(id1, id2 string) error {
	reg.Lock()
	defer reg.Unlock()
	oldID, ok := reg.Map[id1]
	if ok && oldID != id2 {
		return fmt.Errorf("identity %q is already mapped to %q", id1, oldID)
	}

	log.Infof("adding registry entry %q -> %q", id1, id2)
	reg.Map[id1] = id2
	return nil
}

// DeleteMapping attempts to delete mapping id1 -> id2. If id1 is already
// mapped to a different identity, deletion fails
func (reg *IdentityRegistry) DeleteMapping(id1, id2 string) error {
	reg.Lock()
	defer reg.Unlock()
	oldID, ok := reg.Map[id1]
	if !ok || oldID != id2 {
		return fmt.Errorf("could not delete nonexistent mapping: %q -> %q", id1, id2)
	}

	log.Infof("deleting registry entry %q -> %q", id1, id2)
	delete(reg.Map, id1)
	return nil
}

var (
	// singleton object of identity registry
	reg Registry
)

// GetIdentityRegistry returns the identity registry object
func GetIdentityRegistry() Registry {
	if reg == nil {
		reg = &IdentityRegistry{
			Map: make(map[string]string),
		}
	}
	return reg
}
