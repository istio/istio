package cidranger

import (
	"net"

	rnet "github.com/yl2chen/cidranger/net"
)

type rangerFactory func(rnet.IPVersion) Ranger

type versionedRanger struct {
	ipV4Ranger Ranger
	ipV6Ranger Ranger
}

func newVersionedRanger(factory rangerFactory) Ranger {
	return &versionedRanger{
		ipV4Ranger: factory(rnet.IPv4),
		ipV6Ranger: factory(rnet.IPv6),
	}
}

func (v *versionedRanger) Insert(entry RangerEntry) error {
	network := entry.Network()
	ranger, err := v.getRangerForIP(network.IP)
	if err != nil {
		return err
	}
	return ranger.Insert(entry)
}

func (v *versionedRanger) Remove(network net.IPNet) (RangerEntry, error) {
	ranger, err := v.getRangerForIP(network.IP)
	if err != nil {
		return nil, err
	}
	return ranger.Remove(network)
}

func (v *versionedRanger) Contains(ip net.IP) (bool, error) {
	ranger, err := v.getRangerForIP(ip)
	if err != nil {
		return false, err
	}
	return ranger.Contains(ip)
}

func (v *versionedRanger) ContainingNetworks(ip net.IP) ([]RangerEntry, error) {
	ranger, err := v.getRangerForIP(ip)
	if err != nil {
		return nil, err
	}
	return ranger.ContainingNetworks(ip)
}

func (v *versionedRanger) CoveredNetworks(network net.IPNet) ([]RangerEntry, error) {
	ranger, err := v.getRangerForIP(network.IP)
	if err != nil {
		return nil, err
	}
	return ranger.CoveredNetworks(network)
}

func (v *versionedRanger) getRangerForIP(ip net.IP) (Ranger, error) {
	if ip.To4() != nil {
		return v.ipV4Ranger, nil
	}
	if ip.To16() != nil {
		return v.ipV6Ranger, nil
	}
	return nil, ErrInvalidNetworkNumberInput
}
