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

package nftables

import (
	"context"
	"fmt"
	"net/netip"

	"sigs.k8s.io/knftables"

	"istio.io/istio/tools/istio-nftables/pkg/builder"
)

// HostNftSetManager implements AddressSetManager using nftables sets
type HostNftSetManager struct {
	v4SetName   string
	v6SetName   string
	prefix      string
	enableIPv6  bool
	nftProvider NftProviderFunc
}

// NewHostSetManager creates a new nftables-based host set manager
func NewHostSetManager(setPrefix string, enableIPv6 bool) (*HostNftSetManager, error) {
	// Lets use the same naming convention as ipset
	v4SetName := fmt.Sprintf("%s-v4", setPrefix)
	v6SetName := fmt.Sprintf("%s-v6", setPrefix)

	// Use the real implementation when the package level variable is not set
	nftProvider := nftProviderVar
	if nftProvider == nil {
		nftProvider = func(family knftables.Family, table string) (builder.NftablesAPI, error) {
			return builder.NewNftImpl(family, table)
		}
	}

	manager := &HostNftSetManager{
		v4SetName:   v4SetName,
		v6SetName:   v6SetName,
		prefix:      setPrefix,
		enableIPv6:  enableIPv6,
		nftProvider: nftProvider,
	}

	// Initialize the sets
	if err := manager.initializeSets(); err != nil {
		return nil, err
	}

	return manager, nil
}

// initializeSets creates the nftables table and sets
func (h *HostNftSetManager) initializeSets() error {
	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return err
	}

	tx := nft.NewTransaction()

	// Create table
	tx.Add(&knftables.Table{})

	// Create IPv4 set
	tx.Add(&knftables.Set{
		Name:    h.v4SetName,
		Type:    "ipv4_addr",
		Comment: knftables.PtrTo("UUID of the pods that are part of ambient mesh"),
	})

	// Create IPv6 set if enabled
	if h.enableIPv6 {
		tx.Add(&knftables.Set{
			Name:    h.v6SetName,
			Type:    "ipv6_addr",
			Comment: knftables.PtrTo("UUID of the pods that are part of ambient mesh"),
		})
	}

	if err := nft.Run(context.TODO(), tx); err != nil {
		return fmt.Errorf("failed to initialize nftables sets: %w", err)
	}

	builder.LogNftRules(tx)
	return nil
}

// AddIP adds an IP address to the appropriate set (v4 or v6)
func (h *HostNftSetManager) AddIP(ip netip.Addr, ipProto uint8, comment string, replace bool) error {
	ipToInsert := ip.Unmap()
	setName := h.v4SetName
	if ipToInsert.Is6() {
		if !h.enableIPv6 {
			log.Debugf("IPv6 is not enabled. Skipping the addition of IP: %v", ip)
			return nil
		}
		setName = h.v6SetName
	}

	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return err
	}

	tx := nft.NewTransaction()

	// Add element to the set
	tx.Add(&knftables.Element{
		Set:     setName,
		Key:     []string{ipToInsert.String()},
		Comment: knftables.PtrTo(comment),
	})

	if err := nft.Run(context.TODO(), tx); err != nil {
		return fmt.Errorf("failed to add IP %s to nftables set %s: %w", ip, setName, err)
	}

	builder.LogNftRules(tx)
	return nil
}

// DeleteIP removes an IP address from the appropriate set
func (h *HostNftSetManager) DeleteIP(ip netip.Addr, ipProto uint8) error {
	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return err
	}

	ipToDel := ip.Unmap()
	setName := h.v4SetName
	if ipToDel.Is6() {
		if !h.enableIPv6 {
			log.Debugf("IPv6 is not enabled. Skipping the deletion of IP: %v", ip)
			return nil
		}
		setName = h.v6SetName
	}

	tx := nft.NewTransaction()

	// Delete element from the set
	tx.Delete(&knftables.Element{
		Set: setName,
		Key: []string{ipToDel.String()},
	})

	if err := nft.Run(context.TODO(), tx); err != nil {
		return fmt.Errorf("failed to delete IP %s from the nftables set %s: %w", ip, setName, err)
	}

	builder.LogNftRules(tx)
	return nil
}

// Flush clears all entries from both sets
func (h *HostNftSetManager) Flush() error {
	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return err
	}

	tx := nft.NewTransaction()

	// Flush IPv4 set
	tx.Flush(&knftables.Set{
		Name: h.v4SetName,
	})

	// Flush IPv6 set if enabled
	if h.enableIPv6 {
		tx.Flush(&knftables.Set{
			Name: h.v6SetName,
		})
	}

	if err := nft.Run(context.TODO(), tx); err != nil {
		return fmt.Errorf("failed to flush nftables sets: %w", err)
	}

	builder.LogNftRules(tx)
	return nil
}

// DestroySet completely destroys both sets and the table
func (h *HostNftSetManager) DestroySet() error {
	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return err
	}

	tx := nft.NewTransaction()

	// Delete the entire table (which will delete sets and rules)
	tx.Delete(&knftables.Table{})

	if err := nft.Run(context.TODO(), tx); err != nil {
		return fmt.Errorf("failed to destroy nftables table %s: %w", AmbientNatTable, err)
	}

	builder.LogNftRules(tx)
	return nil
}

// ClearEntriesWithComment removes all entries with the specified comment
func (h *HostNftSetManager) ClearEntriesWithComment(comment string) error {
	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return err
	}

	// A pod can have multiple IPs, so we have to clear it from both v4 and v6 sets.
	if err := h.clearEntriesFromSetWithComment(nft, h.v4SetName, comment); err != nil {
		return fmt.Errorf("failed to clear entries from IPv4 address set: %w", err)
	}

	// Clear from IPv6 set if enabled
	if h.enableIPv6 {
		if err := h.clearEntriesFromSetWithComment(nft, h.v6SetName, comment); err != nil {
			return fmt.Errorf("failed to clear entries from IPv6 address set: %w", err)
		}
	}

	return nil
}

// ClearEntriesWithIP removes all entries with the specified IP address
func (h *HostNftSetManager) ClearEntriesWithIP(ip netip.Addr) error {
	return h.DeleteIP(ip, 0)
}

// clearEntriesWithIPAndComment takes an IP and a comment string and deletes any entries where both match the entry.
//
// Returns a non-nil error if listing or deletion fails.
// For the first matching IP found in the list, *only* removes the entry if *both* the IP and comment match.
// If the IP matches but the comment does not, returns the actual comment found to the caller, and does not remove any entries.
// Otherwise, returns an empty string.
func (h *HostNftSetManager) ClearEntriesWithIPAndComment(ip netip.Addr, comment string) (string, error) {
	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return "", err
	}

	ipToCheck := ip.Unmap()
	setName := h.v4SetName
	if ipToCheck.Is6() {
		if !h.enableIPv6 {
			return "", fmt.Errorf("request to delete %s from the set, but ipv6 is not enabled", ipToCheck.String())
		}
		setName = h.v6SetName
	}

	elements, err := nft.ListElements(context.TODO(), "set", setName)
	if err != nil {
		return "", fmt.Errorf("failed to list elements from set %s: %w", setName, err)
	}

	// Find the element with matching IP
	for _, elem := range elements {
		if len(elem.Key) > 0 && elem.Key[0] == ipToCheck.String() {
			// Element exists, but we have to look at the comment as well
			if elem.Comment == nil {
				log.Errorf("element %s exists in set %s but has no comment", ipToCheck.String(), setName)
				continue
			}

			if *elem.Comment != comment {
				// pod ip matches with an element from the set, but the comment does not match.
				return *elem.Comment, nil
			}

			// Both the pod IP and comment matches, we can now delete the element
			tx := nft.NewTransaction()
			tx.Delete(&knftables.Element{
				Set: setName,
				Key: elem.Key,
			})

			if err := nft.Run(context.TODO(), tx); err != nil {
				return "", fmt.Errorf("failed to delete element with IP %s: %w", ip, err)
			}

			builder.LogNftRules(tx)
			return "", nil
		}
	}

	return "", fmt.Errorf("element with IP %s not found in set %s", ip, setName)
}

// ListEntriesByIP returns all IP addresses currently in both sets
func (h *HostNftSetManager) ListEntriesByIP() ([]netip.Addr, error) {
	nft, err := h.nftProvider(knftables.InetFamily, AmbientNatTable)
	if err != nil {
		return nil, err
	}

	var allIPs []netip.Addr

	// List elements from IPv4 set
	v4Elements, err := nft.ListElements(context.TODO(), "set", h.v4SetName)
	if err != nil {
		return nil, fmt.Errorf("failed to list elements from IPv4 set %s: %w", h.v4SetName, err)
	}

	for _, elem := range v4Elements {
		if len(elem.Key) > 0 {
			if ip, err := netip.ParseAddr(elem.Key[0]); err == nil {
				allIPs = append(allIPs, ip)
			} else {
				log.Warnf("Failed to parse IPv4 address %s: %v", elem.Key[0], err)
			}
		}
	}

	// List elements from IPv6 set if enabled
	if h.enableIPv6 {
		v6Elements, err := nft.ListElements(context.TODO(), "set", h.v6SetName)
		if err != nil {
			return nil, fmt.Errorf("failed to list elements from IPv6 set %s: %w", h.v6SetName, err)
		}

		for _, elem := range v6Elements {
			if len(elem.Key) > 0 {
				if ip, err := netip.ParseAddr(elem.Key[0]); err == nil {
					allIPs = append(allIPs, ip)
				} else {
					log.Warnf("Failed to parse IPv6 address %s: %v", elem.Key[0], err)
				}
			}
		}
	}

	return allIPs, nil
}

// clearEntriesFromSetWithComment is a helper function to clear entries with a specific comment from a set
func (h *HostNftSetManager) clearEntriesFromSetWithComment(nft builder.NftablesAPI, setName, comment string) error {
	elements, err := nft.ListElements(context.TODO(), "set", setName)
	if err != nil {
		return fmt.Errorf("failed to list elements from set %s: %w", setName, err)
	}

	// Collect elements to delete in a single transaction
	tx := nft.NewTransaction()
	elementsToDelete := 0

	for _, elem := range elements {
		if elem.Comment != nil && *elem.Comment == comment {
			tx.Delete(&knftables.Element{
				Set: setName,
				Key: elem.Key,
			})
			elementsToDelete++
		}
	}

	// Only run the transaction if there are elements to delete
	if elementsToDelete > 0 {
		if err := nft.Run(context.TODO(), tx); err != nil {
			return fmt.Errorf("failed to delete %d elements from set %s: %w", elementsToDelete, setName, err)
		}
		builder.LogNftRules(tx)
	}

	return nil
}

// GetPrefix returns the set name prefix for logging purposes
func (h *HostNftSetManager) GetPrefix() string {
	return h.prefix
}
