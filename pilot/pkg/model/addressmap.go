// Copyright Istio Authors
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

package model

import (
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

// AddressMap provides a thread-safe mapping of addresses for each Kubernetes cluster.
type AddressMap struct {
	// Addresses hold the underlying map. Most code should only access this through the available methods.
	// Should only be used by tests and construction/initialization logic, where there is no concern
	// for race conditions.
	Addresses map[cluster.ID][]string
}

func (m *AddressMap) Len() int {
	if m == nil {
		return 0
	}

	return len(m.Addresses)
}

func (m *AddressMap) DeepCopy() *AddressMap {
	if m == nil {
		return nil
	}

	var addresses map[cluster.ID][]string
	if m.Addresses != nil {
		addresses = make(map[cluster.ID][]string, len(m.Addresses))
		for k, v := range m.Addresses {
			addresses[k] = slices.Clone(v)
		}
	}

	return &AddressMap{
		Addresses: addresses,
	}
}

// GetAddresses returns the mapping of clusters to addresses.
func (m *AddressMap) GetAddresses() map[cluster.ID][]string {
	if m == nil {
		return nil
	}

	return m.Addresses
}

func (m *AddressMap) GetAddressesFor(c cluster.ID) []string {
	if m == nil {
		return nil
	}

	if m.Addresses == nil {
		return nil
	}

	return m.Addresses[c]
}

// SetAddressesFor sets the addresses for a cluster,
// users should ensure they have a shallow copy of the AddressMap before calling this method.
func (m *AddressMap) SetAddressesFor(c cluster.ID, addresses []string) {
	if len(addresses) == 0 {
		// Setting an empty array for the cluster. Remove the entry for the cluster if it exists.
		if m.Addresses != nil {
			// Delete the map if there's nothing left.
			if len(m.Addresses) == 1 && m.Addresses[c] != nil {
				m.Addresses = nil
				return
			}

			// ensure we clone the internal map to avoid races with shallow copies
			m.Addresses = maps.Clone(m.Addresses)
			delete(m.Addresses, c)
		}

		return
	}

	// Create the map if it doesn't already exist.
	if m.Addresses == nil {
		m.Addresses = map[cluster.ID][]string{
			c: addresses,
		}

		return
	}

	// ensure we clone the internal map to avoid races with shallow copies
	m.Addresses = maps.Clone(m.Addresses)
	m.Addresses[c] = addresses
}

// AddAddressesFor adds addresses for a cluster,
// users should ensure they have a shallow copy of the AddressMap before calling this method.
func (m *AddressMap) AddAddressesFor(c cluster.ID, addresses []string) {
	if len(addresses) == 0 {
		return
	}

	// Create the map if nil.
	if m.Addresses == nil {
		m.Addresses = map[cluster.ID][]string{
			c: addresses,
		}

		return
	}

	// ensure we clone the internal map and slice to avoid races with shallow copies
	m.Addresses = maps.Clone(m.Addresses)
	m.Addresses[c] = append(slices.Clone(m.Addresses[c]), addresses...)
}

func (m *AddressMap) ForEach(fn func(c cluster.ID, addresses []string)) {
	if m == nil {
		return
	}

	if m.Addresses == nil {
		return
	}

	for c, addresses := range m.Addresses {
		fn(c, addresses)
	}
}
