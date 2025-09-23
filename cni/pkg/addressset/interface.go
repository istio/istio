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

package addressset

import (
	"net/netip"
)

// AddressSetManager defines the interface for managing host-level IP sets
// for health check support in Ambient mode. This abstraction allows switching
// between ipset and nftables set implementations.
type AddressSetManager interface {
	// AddIP adds an IP address to the set with the specified protocol, comment etc
	AddIP(ip netip.Addr, ipProto uint8, comment string, replace bool) error

	// DeleteIP removes an IP address with the specified protocol from the set
	DeleteIP(ip netip.Addr, ipProto uint8) error

	// Flush clears all entries from the set
	Flush() error

	// DestroySet completely destroys the set
	DestroySet() error

	// ClearEntriesWithComment removes all entries with the specified comment
	ClearEntriesWithComment(comment string) error

	// ClearEntriesWithIP removes all entries with the specified IP address
	ClearEntriesWithIP(ip netip.Addr) error

	// ClearEntriesWithIPAndComment removes entries with both IP and comment match
	// Returns the mismatched comment if IP exists but comment doesn't match
	ClearEntriesWithIPAndComment(ip netip.Addr, comment string) (string, error)

	// ListEntriesByIP returns all IP addresses currently in the set
	ListEntriesByIP() ([]netip.Addr, error)

	// GetPrefix returns the set name prefix for logging purposes
	GetPrefix() string
}
