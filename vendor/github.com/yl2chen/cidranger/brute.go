package cidranger

import (
	"net"

	rnet "github.com/yl2chen/cidranger/net"
)

// bruteRanger is a brute force implementation of Ranger.  Insertion and
// deletion of networks is performed on an internal storage in the form of
// map[string]net.IPNet (constant time operations).  However, inclusion tests are
// always performed linearly at no guaranteed traversal order of recorded networks,
// so one can assume a worst case performance of O(N).  The performance can be
// boosted many ways, e.g. changing usage of net.IPNet.Contains() to using masked
// bits equality checking, but the main purpose of this implementation is for
// testing because the correctness of this implementation can be easily guaranteed,
// and used as the ground truth when running a wider range of 'random' tests on
// other more sophisticated implementations.
type bruteRanger struct {
	ipV4Entries map[string]RangerEntry
	ipV6Entries map[string]RangerEntry
}

// newBruteRanger returns a new Ranger.
func newBruteRanger() Ranger {
	return &bruteRanger{
		ipV4Entries: make(map[string]RangerEntry),
		ipV6Entries: make(map[string]RangerEntry),
	}
}

// Insert inserts a RangerEntry into ranger.
func (b *bruteRanger) Insert(entry RangerEntry) error {
	network := entry.Network()
	key := network.String()
	if _, found := b.ipV4Entries[key]; !found {
		entries, err := b.getEntriesByVersion(entry.Network().IP)
		if err != nil {
			return err
		}
		entries[key] = entry
	}
	return nil
}

// Remove removes a RangerEntry identified by given network from ranger.
func (b *bruteRanger) Remove(network net.IPNet) (RangerEntry, error) {
	networks, err := b.getEntriesByVersion(network.IP)
	if err != nil {
		return nil, err
	}
	key := network.String()
	if networkToDelete, found := networks[key]; found {
		delete(networks, key)
		return networkToDelete, nil
	}
	return nil, nil
}

// Contains returns bool indicating whether given ip is contained by any
// network in ranger.
func (b *bruteRanger) Contains(ip net.IP) (bool, error) {
	entries, err := b.getEntriesByVersion(ip)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		network := entry.Network()
		if network.Contains(ip) {
			return true, nil
		}
	}
	return false, nil
}

// ContainingNetworks returns all RangerEntry(s) that given ip contained in.
func (b *bruteRanger) ContainingNetworks(ip net.IP) ([]RangerEntry, error) {
	entries, err := b.getEntriesByVersion(ip)
	if err != nil {
		return nil, err
	}
	results := []RangerEntry{}
	for _, entry := range entries {
		network := entry.Network()
		if network.Contains(ip) {
			results = append(results, entry)
		}
	}
	return results, nil
}

// CoveredNetworks returns the list of RangerEntry(s) the given ipnet
// covers.  That is, the networks that are completely subsumed by the
// specified network.
func (b *bruteRanger) CoveredNetworks(network net.IPNet) ([]RangerEntry, error) {
	entries, err := b.getEntriesByVersion(network.IP)
	if err != nil {
		return nil, err
	}
	var results []RangerEntry
	testNetwork := rnet.NewNetwork(network)
	for _, entry := range entries {
		entryNetwork := rnet.NewNetwork(entry.Network())
		if testNetwork.Covers(entryNetwork) {
			results = append(results, entry)
		}
	}
	return results, nil
}

func (b *bruteRanger) getEntriesByVersion(ip net.IP) (map[string]RangerEntry, error) {
	if ip.To4() != nil {
		return b.ipV4Entries, nil
	}
	if ip.To16() != nil {
		return b.ipV6Entries, nil
	}
	return nil, ErrInvalidNetworkInput
}
