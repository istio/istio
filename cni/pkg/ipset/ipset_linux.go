package ipset

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

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

type IPSet struct {
	// the name of the ipset to use
	Name string
}

func (m *IPSet) CreateSet() error {
	err := netlink.IpsetCreate(m.Name, "hash:ip", netlink.IpsetCreateOptions{Comments: true})
	if ipsetErr, ok := err.(nl.IPSetError); ok && ipsetErr == nl.IPSET_ERR_EXIST {
		return nil
	}
	return err
}

func (m *IPSet) DestroySet() error {
	err := netlink.IpsetDestroy(m.Name)
	return err
}

func (m *IPSet) AddIP(ip net.IP, comment string) error {
	err := netlink.IpsetAdd(m.Name, &netlink.IPSetEntry{
		Comment: comment,
		IP:      ip,
	})
	if err != nil {
		return fmt.Errorf("failed to add IP %s to ipset %s: %w", ip, m.Name, err)
	}
	return nil
}

func (m *IPSet) Flush() error {
	err := netlink.IpsetFlush(m.Name)
	if err != nil {
		return fmt.Errorf("failed to flush ipset %s: %w", m.Name, err)
	}
	return nil
}

func (m *IPSet) List() ([]netlink.IPSetEntry, error) {
	res, err := netlink.IpsetList(m.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list ipset %s: %w", m.Name, err)
	}
	return res.Entries, nil
}

func (m *IPSet) DeleteIP(ip net.IP) error {
	err := netlink.IpsetDel(m.Name, &netlink.IPSetEntry{
		IP: ip,
	})
	if err != nil {
		return fmt.Errorf("failed to delete IP %s from ipset %s: %w", ip, m.Name, err)
	}
	return nil
}

// This is only supported in kernel module from revision 2 or 4, so may not be present
func (m *IPSet) ClearEntriesWithComment(comment string) error {
	res, err := netlink.IpsetList(m.Name)
	if err != nil {
		return fmt.Errorf("failed to list ipset %s: %w", m.Name, err)
	}
	for _, entry := range res.Entries {
		if entry.Comment == comment {
			err := netlink.IpsetDel(m.Name, &entry)
			if err != nil {
				return fmt.Errorf("failed to delete IP %s from ipset %s: %w", entry.IP, m.Name, err)
			}
		}
	}
	return nil
}
