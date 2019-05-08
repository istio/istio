/*
Package cidranger provides utility to store CIDR blocks and perform ip
inclusion tests against it.

To create a new instance of the path-compressed trie:

			ranger := NewPCTrieRanger()

To insert or remove an entry (any object that satisfies the RangerEntry
interface):

			_, network, _ := net.ParseCIDR("192.168.0.0/24")
			ranger.Insert(NewBasicRangerEntry(*network))
			ranger.Remove(network)

If you desire for any value to be attached to the entry, simply
create custom struct that satisfies the RangerEntry interface:

			type RangerEntry interface {
				Network() net.IPNet
			}

To test whether an IP is contained in the constructed networks ranger:

			// returns bool, error
			containsBool, err := ranger.Contains(net.ParseIP("192.168.0.1"))

To get a list of CIDR blocks in constructed ranger that contains IP:

			// returns []RangerEntry, error
			entries, err := ranger.ContainingNetworks(net.ParseIP("192.168.0.1"))

*/
package cidranger

import (
	"fmt"
	"net"
)

// ErrInvalidNetworkInput is returned upon invalid network input.
var ErrInvalidNetworkInput = fmt.Errorf("Invalid network input")

// ErrInvalidNetworkNumberInput is returned upon invalid network input.
var ErrInvalidNetworkNumberInput = fmt.Errorf("Invalid network number input")

// RangerEntry is an interface for insertable entry into a Ranger.
type RangerEntry interface {
	Network() net.IPNet
}

type basicRangerEntry struct {
	ipNet net.IPNet
}

func (b *basicRangerEntry) Network() net.IPNet {
	return b.ipNet
}

// NewBasicRangerEntry returns a basic RangerEntry that only stores the network
// itself.
func NewBasicRangerEntry(ipNet net.IPNet) RangerEntry {
	return &basicRangerEntry{
		ipNet: ipNet,
	}
}

// Ranger is an interface for cidr block containment lookups.
type Ranger interface {
	Insert(entry RangerEntry) error
	Remove(network net.IPNet) (RangerEntry, error)
	Contains(ip net.IP) (bool, error)
	ContainingNetworks(ip net.IP) ([]RangerEntry, error)
	CoveredNetworks(network net.IPNet) ([]RangerEntry, error)
}

// NewPCTrieRanger returns a versionedRanger that supports both IPv4 and IPv6
// using the path compressed trie implemention.
func NewPCTrieRanger() Ranger {
	return newVersionedRanger(newPrefixTree)
}
