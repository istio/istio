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
		return fmt.Errorf("failed to delete IP %s from nftables set %s: %w", ip, setName, err)
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
	// TODO: For nftables, we need to list elements and delete those with matching comments

	return nil
}

// ClearEntriesWithIP removes all entries with the specified IP address
func (h *HostNftSetManager) ClearEntriesWithIP(ip netip.Addr) error {
	return h.DeleteIP(ip, 0)
}

// ClearEntriesWithIPAndComment removes entries with both IP and comment match
func (h *HostNftSetManager) ClearEntriesWithIPAndComment(ip netip.Addr, comment string) (string, error) {
	// TODO: Implement this method properly. For now, just using DeleteIP()
	err := h.DeleteIP(ip, 0)
	return "", err
}

// ListEntriesByIP returns all IP addresses currently in both sets
func (h *HostNftSetManager) ListEntriesByIP() ([]netip.Addr, error) {
	// TODO: Implement this method properly. For now, just using DeleteIP()
	return []netip.Addr{}, nil
}

// GetPrefix returns the set name prefix for logging purposes
func (h *HostNftSetManager) GetPrefix() string {
	return h.prefix
}
