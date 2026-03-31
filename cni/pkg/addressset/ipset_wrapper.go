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

	"istio.io/istio/cni/pkg/ipset"
)

// IPSetWrapper wraps the existing ipset.IPSet to implement AddressSetManager
type IPSetWrapper struct {
	ipset ipset.IPSet
}

var _ AddressSetManager = &IPSetWrapper{}

// NewIPSetWrapper creates a new IPSetWrapper with the given ipset
func NewIPSetWrapper(ipsetInstance ipset.IPSet) *IPSetWrapper {
	return &IPSetWrapper{ipset: ipsetInstance}
}

// AddIP adds an IP address to the ipset
func (w *IPSetWrapper) AddIP(ip netip.Addr, ipProto uint8, comment string, replace bool) error {
	return w.ipset.AddIP(ip, ipProto, comment, replace)
}

// DeleteIP removes an IP address from the ipset
func (w *IPSetWrapper) DeleteIP(ip netip.Addr, ipProto uint8) error {
	return w.ipset.DeleteIP(ip, ipProto)
}

// Flush clears all entries from the ipset
func (w *IPSetWrapper) Flush() error {
	return w.ipset.Flush()
}

// DestroySet completely destroys the ipset
func (w *IPSetWrapper) DestroySet() error {
	return w.ipset.DestroySet()
}

// ClearEntriesWithComment removes all entries with the specified comment
func (w *IPSetWrapper) ClearEntriesWithComment(comment string) error {
	return w.ipset.ClearEntriesWithComment(comment)
}

// ClearEntriesWithIP removes all entries with the specified IP address
func (w *IPSetWrapper) ClearEntriesWithIP(ip netip.Addr) error {
	return w.ipset.ClearEntriesWithIP(ip)
}

// ClearEntriesWithIPAndComment removes entries with both IP and comment match
func (w *IPSetWrapper) ClearEntriesWithIPAndComment(ip netip.Addr, comment string) (string, error) {
	return w.ipset.ClearEntriesWithIPAndComment(ip, comment)
}

// ListEntriesByIP returns all IP addresses currently in the ipset
func (w *IPSetWrapper) ListEntriesByIP() ([]netip.Addr, error) {
	return w.ipset.ListEntriesByIP()
}

// GetPrefix returns the ipset name prefix for logging purposes
func (w *IPSetWrapper) GetPrefix() string {
	return w.ipset.Prefix
}
