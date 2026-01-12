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

package ipset

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func RealNlDeps() NetlinkIpsetDeps {
	return &realDeps{}
}

type realDeps struct{}

func (m *realDeps) ipsetIPHashCreate(name string, v6 bool) error {
	var family uint8

	if v6 {
		family = unix.AF_INET6
	} else {
		family = unix.AF_INET
	}

	err := netlink.IpsetCreate(name, "hash:ip", netlink.IpsetCreateOptions{Comments: true, Replace: true, Family: family})
	// Note there appears to be a bug in vishvananda/netlink here:
	// https://github.com/vishvananda/netlink/issues/992
	//
	// The "right" way to do this is:
	// if err == nl.IPSetError(nl.IPSET_ERR_EXIST) {
	// 	log.Debugf("ignoring ipset err")
	// 	return nil
	// }
	// but that doesn't actually work, so strings.Contains the error.
	if err != nil && strings.Contains(err.Error(), "exists") {
		return nil
	}
	return err
}

func (m *realDeps) destroySet(name string) error {
	err := netlink.IpsetDestroy(name)
	return err
}

func (m *realDeps) addIP(name string, ip netip.Addr, ipProto uint8, comment string, replace bool) error {
	err := netlink.IpsetAdd(name, &netlink.IPSetEntry{
		Comment:  comment,
		IP:       net.IP(ip.AsSlice()),
		Protocol: &ipProto,
		Replace:  replace,
	})
	if err != nil {
		return fmt.Errorf("failed to add IP %s to ipset %s: %w", ip, name, err)
	}
	return nil
}

func (m *realDeps) deleteIP(name string, ip netip.Addr, ipProto uint8) error {
	err := netlink.IpsetDel(name, &netlink.IPSetEntry{
		IP:       net.IP(ip.AsSlice()),
		Protocol: &ipProto,
	})
	if err != nil {
		return fmt.Errorf("failed to delete IP %s from ipset %s: %w", ip, name, err)
	}
	return nil
}

func (m *realDeps) flush(name string) error {
	err := netlink.IpsetFlush(name)
	if err != nil {
		return fmt.Errorf("failed to flush ipset %s: %w", name, err)
	}
	return nil
}

// Alpine and some distros struggles with this - ipset CLI utilities support this, but
// the kernel can be out of sync with the CLI utility, leading to errors like:
//
// ipset v7.10: Argument `comment' is supported in the kernel module of the set type hash:ip
// starting from the revision 3 and you have installed revision 1 only.
// Your kernel is behind your ipset utility.
//
// This happens with kernels as recent as Fedora38, e.g: 6.4.11-200.fc38.aarch64
func (m *realDeps) clearEntriesWithComment(name, comment string) error {
	res, err := netlink.IpsetList(name)
	if err != nil {
		return fmt.Errorf("failed to list ipset %s: %w", name, err)
	}
	for _, entry := range res.Entries {
		if entry.Comment == comment {
			err := netlink.IpsetDel(name, &entry)
			if err != nil {
				return fmt.Errorf("failed to delete IP %s from ipset %s: %w", entry.IP, name, err)
			}
		}
	}
	return nil
}

// clearEntriesWithIPAndComment takes an IP and a comment string and deletes any ipset entries where
// both match the entry.
//
// Returns a non-nil error if listing or deletion fails.
// For the first matching IP found in the list, *only* removes the entry if *both* the IP and comment match.
// If the IP matches but the comment does not, returns the actual comment found to the caller, and does not
// remove any entries.
//
// Otherwise, returns an empty string.
func (m *realDeps) clearEntriesWithIPAndComment(name string, ip netip.Addr, comment string) (string, error) {
	delIP := net.IP(ip.AsSlice())
	res, err := netlink.IpsetList(name)
	if err != nil {
		return "", fmt.Errorf("failed to list ipset %s: %w", name, err)
	}
	for _, entry := range res.Entries {
		if entry.IP.Equal(delIP) {
			if entry.Comment == comment {
				err := netlink.IpsetDel(name, &entry)
				if err != nil {
					return "", fmt.Errorf("failed to delete IP %s from ipset %s: %w", entry.IP, name, err)
				}
			} else {
				return entry.Comment, nil
			}
		}
	}
	return "", nil
}

func (m *realDeps) clearEntriesWithIP(name string, ip netip.Addr) error {
	delIP := net.IP(ip.AsSlice())
	res, err := netlink.IpsetList(name)
	if err != nil {
		return fmt.Errorf("failed to list ipset %s: %w", name, err)
	}

	var delErrs []error

	for _, entry := range res.Entries {
		if entry.IP.Equal(delIP) {
			err := netlink.IpsetDel(name, &entry)
			if err != nil {
				delErrs = append(delErrs, fmt.Errorf("failed to delete IP %s from ipset %s: %w", entry.IP, name, err))
			}
		}
	}

	return errors.Join(delErrs...)
}

func (m *realDeps) listEntriesByIP(name string) ([]netip.Addr, error) {
	var ipList []netip.Addr

	res, err := netlink.IpsetList(name)
	if err != nil {
		return ipList, fmt.Errorf("failed to list ipset %s: %w", name, err)
	}

	for _, entry := range res.Entries {
		addr, _ := netip.AddrFromSlice(entry.IP)
		ipList = append(ipList, addr)
	}

	return ipList, nil
}
