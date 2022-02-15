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
)

// AddressMap provides a thread-safe mapping of addresses for each Kubernetes cluster.
type AddressMap struct {
	// Addresses hold the underlying map. Most code should only access this through the available methods.
	// Should only be used by tests and construction/initialization logic, where there is no concern
	// for race conditions.
	Addresses map[cluster.ID][]string
}

func (m *AddressMap) IsEmpty() bool {
	if m == nil {
		return true
	}
	return len(m.Addresses) == 0
}

func (m *AddressMap) DeepCopy() AddressMap {
	return AddressMap{
		Addresses: m.GetAddresses(),
	}
}

// GetAddresses returns the mapping of clusters to addresses.
func (m *AddressMap) GetAddresses() map[cluster.ID][]string {
	if m == nil {
		return nil
	}

	if m.Addresses == nil {
		return nil
	}

	out := make(map[cluster.ID][]string)
	for k, v := range m.Addresses {
		out[k] = append([]string{}, v...)
	}
	return out
}

func (m *AddressMap) GetAddressesFor(c cluster.ID) []string {
	if m == nil {
		return nil
	}

	if m.Addresses == nil {
		return nil
	}

	// Copy the Addresses array.
	return append([]string{}, m.Addresses[c]...)
}

func (m *AddressMap) SetAddressesFor(c cluster.ID, addresses []string) *AddressMap {
	if len(addresses) == 0 {
		// Setting an empty array for the cluster. Remove the entry for the cluster if it exists.
		if m.Addresses != nil {
			delete(m.Addresses, c)

			// Delete the map if there's nothing left.
			if len(m.Addresses) == 0 {
				m.Addresses = nil
			}
		}
	} else {
		// Create the map if if doesn't already exist.
		if m.Addresses == nil {
			m.Addresses = make(map[cluster.ID][]string)
		}
		m.Addresses[c] = addresses
	}
	return m
}

func (m *AddressMap) AddAddressesFor(c cluster.ID, addresses []string) *AddressMap {
	if len(addresses) == 0 {
		return m
	}

	// Create the map if nil.
	if m.Addresses == nil {
		m.Addresses = make(map[cluster.ID][]string)
	}

	m.Addresses[c] = append(m.Addresses[c], addresses...)
	return m
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
