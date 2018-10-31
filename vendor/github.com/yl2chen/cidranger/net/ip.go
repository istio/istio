/*
Package net provides utility functions for working with IPs (net.IP).
*/
package net

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
)

// IPVersion is version of IP address.
type IPVersion string

// Helper constants.
const (
	IPv4Uint32Count = 1
	IPv6Uint32Count = 4

	BitsPerUint32 = 32
	BytePerUint32 = 4

	IPv4 IPVersion = "IPv4"
	IPv6 IPVersion = "IPv6"
)

// ErrInvalidBitPosition is returned when bits requested is not valid.
var ErrInvalidBitPosition = fmt.Errorf("bit position not valid")

// ErrVersionMismatch is returned upon mismatch in network input versions.
var ErrVersionMismatch = fmt.Errorf("Network input version mismatch")

// ErrNoGreatestCommonBit is an error returned when no greatest common bit
// exists for the cidr ranges.
var ErrNoGreatestCommonBit = fmt.Errorf("No greatest common bit")

// NetworkNumber represents an IP address using uint32 as internal storage.
// IPv4 usings 1 uint32, while IPv6 uses 4 uint32.
type NetworkNumber []uint32

// NewNetworkNumber returns a equivalent NetworkNumber to given IP address,
// return nil if ip is neither IPv4 nor IPv6.
func NewNetworkNumber(ip net.IP) NetworkNumber {
	if ip == nil {
		return nil
	}
	coercedIP := ip.To4()
	parts := 1
	if coercedIP == nil {
		coercedIP = ip.To16()
		parts = 4
	}
	if coercedIP == nil {
		return nil
	}
	nn := make(NetworkNumber, parts)
	for i := 0; i < parts; i++ {
		idx := i * net.IPv4len
		nn[i] = binary.BigEndian.Uint32(coercedIP[idx : idx+net.IPv4len])
	}
	return nn
}

// ToV4 returns ip address if ip is IPv4, returns nil otherwise.
func (n NetworkNumber) ToV4() NetworkNumber {
	if len(n) != IPv4Uint32Count {
		return nil
	}
	return n
}

// ToV6 returns ip address if ip is IPv6, returns nil otherwise.
func (n NetworkNumber) ToV6() NetworkNumber {
	if len(n) != IPv6Uint32Count {
		return nil
	}
	return n
}

// ToIP returns equivalent net.IP.
func (n NetworkNumber) ToIP() net.IP {
	ip := make(net.IP, len(n)*BytePerUint32)
	for i := 0; i < len(n); i++ {
		idx := i * net.IPv4len
		binary.BigEndian.PutUint32(ip[idx:idx+net.IPv4len], n[i])
	}
	if len(ip) == net.IPv4len {
		ip = net.IPv4(ip[0], ip[1], ip[2], ip[3])
	}
	return ip
}

// Equal is the equality test for 2 network numbers.
func (n NetworkNumber) Equal(n1 NetworkNumber) bool {
	if len(n) != len(n1) {
		return false
	}
	if n[0] != n1[0] {
		return false
	}
	if len(n) == IPv6Uint32Count {
		return n[1] == n1[1] && n[2] == n1[2] && n[3] == n1[3]
	}
	return true
}

// Next returns the next logical network number.
func (n NetworkNumber) Next() NetworkNumber {
	newIP := make(NetworkNumber, len(n))
	copy(newIP, n)
	for i := len(newIP) - 1; i >= 0; i-- {
		newIP[i]++
		if newIP[i] > 0 {
			break
		}
	}
	return newIP
}

// Previous returns the previous logical network number.
func (n NetworkNumber) Previous() NetworkNumber {
	newIP := make(NetworkNumber, len(n))
	copy(newIP, n)
	for i := len(newIP) - 1; i >= 0; i-- {
		newIP[i]--
		if newIP[i] < math.MaxUint32 {
			break
		}
	}
	return newIP
}

// Bit returns uint32 representing the bit value at given position, e.g.,
// "128.0.0.0" has bit value of 1 at position 31, and 0 for positions 30 to 0.
func (n NetworkNumber) Bit(position uint) (uint32, error) {
	if int(position) > len(n)*BitsPerUint32-1 {
		return 0, ErrInvalidBitPosition
	}
	idx := len(n) - 1 - int(position/BitsPerUint32)
	// Mod 31 to get array index.
	rShift := position & (BitsPerUint32 - 1)
	return (n[idx] >> rShift) & 1, nil
}

// LeastCommonBitPosition returns the smallest position of the preceding common
// bits of the 2 network numbers, and returns an error ErrNoGreatestCommonBit
// if the two network number diverges from the first bit.
// e.g., if the network number diverges after the 1st bit, it returns 131 for
// IPv6 and 31 for IPv4 .
func (n NetworkNumber) LeastCommonBitPosition(n1 NetworkNumber) (uint, error) {
	if len(n) != len(n1) {
		return 0, ErrVersionMismatch
	}
	for i := 0; i < len(n); i++ {
		mask := uint32(1) << 31
		pos := uint(31)
		for ; mask > 0; mask >>= 1 {
			if n[i]&mask != n1[i]&mask {
				if i == 0 && pos == 31 {
					return 0, ErrNoGreatestCommonBit
				}
				return (pos + 1) + uint(BitsPerUint32)*uint(len(n)-i-1), nil
			}
			pos--
		}
	}
	return 0, nil
}

// Network represents a block of network numbers, also known as CIDR.
type Network struct {
	net.IPNet
	Number NetworkNumber
	Mask   NetworkNumberMask
}

// NewNetwork returns Network built using given net.IPNet.
func NewNetwork(ipNet net.IPNet) Network {
	return Network{
		IPNet:  ipNet,
		Number: NewNetworkNumber(ipNet.IP),
		Mask:   NetworkNumberMask(NewNetworkNumber(net.IP(ipNet.Mask))),
	}
}

// Masked returns a new network conforming to new mask.
func (n Network) Masked(ones int) Network {
	mask := net.CIDRMask(ones, len(n.Number)*BitsPerUint32)
	return NewNetwork(net.IPNet{
		IP:   n.IP.Mask(mask),
		Mask: mask,
	})
}

// Contains returns true if NetworkNumber is in range of Network, false
// otherwise.
func (n Network) Contains(nn NetworkNumber) bool {
	if len(n.Mask) != len(nn) {
		return false
	}
	if nn[0]&n.Mask[0] != n.Number[0] {
		return false
	}
	if len(nn) == IPv6Uint32Count {
		return nn[1]&n.Mask[1] == n.Number[1] && nn[2]&n.Mask[2] == n.Number[2] && nn[3]&n.Mask[3] == n.Number[3]
	}
	return true
}

// Contains returns true if Network covers o, false otherwise
func (n Network) Covers(o Network) bool {
	if len(n.Number) != len(o.Number) {
		return false
	}
	nMaskSize, _ := n.IPNet.Mask.Size()
	oMaskSize, _ := o.IPNet.Mask.Size()
	return n.Contains(o.Number) && nMaskSize <= oMaskSize
}

// LeastCommonBitPosition returns the smallest position of the preceding common
// bits of the 2 networks, and returns an error ErrNoGreatestCommonBit
// if the two network number diverges from the first bit.
func (n Network) LeastCommonBitPosition(n1 Network) (uint, error) {
	maskSize, _ := n.IPNet.Mask.Size()
	if maskSize1, _ := n1.IPNet.Mask.Size(); maskSize1 < maskSize {
		maskSize = maskSize1
	}
	maskPosition := len(n1.Number)*BitsPerUint32 - maskSize
	lcb, err := n.Number.LeastCommonBitPosition(n1.Number)
	if err != nil {
		return 0, err
	}
	return uint(math.Max(float64(maskPosition), float64(lcb))), nil
}

// Equal is the equality test for 2 networks.
func (n Network) Equal(n1 Network) bool {
	return n.String() == n1.String()
}

func (n Network) String() string {
	return n.IPNet.String()
}

// NetworkNumberMask is an IP address.
type NetworkNumberMask NetworkNumber

// Mask returns a new masked NetworkNumber from given NetworkNumber.
func (m NetworkNumberMask) Mask(n NetworkNumber) (NetworkNumber, error) {
	if len(m) != len(n) {
		return nil, ErrVersionMismatch
	}
	result := make(NetworkNumber, len(m))
	result[0] = m[0] & n[0]
	if len(m) == IPv6Uint32Count {
		result[1] = m[1] & n[1]
		result[2] = m[2] & n[2]
		result[3] = m[3] & n[3]
	}
	return result, nil
}

// NextIP returns the next sequential ip.
func NextIP(ip net.IP) net.IP {
	return NewNetworkNumber(ip).Next().ToIP()
}

// PreviousIP returns the previous sequential ip.
func PreviousIP(ip net.IP) net.IP {
	return NewNetworkNumber(ip).Previous().ToIP()
}
