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
	"net/netip"
)

type IPSet struct {
	V4Name string
	V6Name string
	Prefix string
	Deps   NetlinkIpsetDeps
}

const (
	V4Name = "%s-v4"
	V6Name = "%s-v6"
)

type NetlinkIpsetDeps interface {
	ipsetIPHashCreate(name string, v6 bool) error
	destroySet(name string) error
	addIP(name string, ip netip.Addr, ipProto uint8, comment string, replace bool) error
	deleteIP(name string, ip netip.Addr, ipProto uint8) error
	flush(name string) error
	clearEntriesWithComment(name string, comment string) error
	clearEntriesWithIPAndComment(name string, ip netip.Addr, comment string) (string, error)
	clearEntriesWithIP(name string, ip netip.Addr) error
	listEntriesByIP(name string) ([]netip.Addr, error)
}

// TODO this should actually create v6 and v6 subsets of type `hash:ip`, add them both to a
// superset of type `list:set` - we can then query the superset directly in iptables (with the same rule),
// and iptables will be smart enough to pick the correct underlying set (v4 or v6, based on context),
// reducing the # of rules we need.
//
// BUT netlink lib doesn't support adding things to `list:set` types yet, and current tagged release
// doesn't support creating `list:set` types yet (is in main branch tho).
// So this will actually create 2 underlying ipsets, one for v4 and one for v6
func NewIPSet(name string, v6 bool, deps NetlinkIpsetDeps) (IPSet, error) {
	var err error
	set := IPSet{
		V4Name: fmt.Sprintf(V4Name, name),
		Deps:   deps,
		Prefix: name,
	}
	err = deps.ipsetIPHashCreate(set.V4Name, false)
	if v6 {
		set.V6Name = fmt.Sprintf(V6Name, name)
		v6err := deps.ipsetIPHashCreate(set.V6Name, true)
		err = errors.Join(err, v6err)
	}
	return set, err
}

func (m *IPSet) DestroySet() error {
	var err error
	err = m.Deps.destroySet(m.V4Name)

	if m.V6Name != "" {
		v6err := m.Deps.destroySet(m.V6Name)
		err = errors.Join(err, v6err)
	}
	return err
}

func (m *IPSet) AddIP(ip netip.Addr, ipProto uint8, comment string, replace bool) error {
	ipToInsert := ip.Unmap()

	// We have already Unmap'd, so we can do a simple IsV6 y/n check now
	if ipToInsert.Is6() {
		return m.Deps.addIP(m.V6Name, ipToInsert, ipProto, comment, replace)
	}
	return m.Deps.addIP(m.V4Name, ipToInsert, ipProto, comment, replace)
}

func (m *IPSet) DeleteIP(ip netip.Addr, ipProto uint8) error {
	ipToDel := ip.Unmap()

	// We have already Unmap'd, so we can do a simple IsV6 y/n check now
	if ipToDel.Is6() {
		return m.Deps.deleteIP(m.V6Name, ipToDel, ipProto)
	}
	return m.Deps.deleteIP(m.V4Name, ipToDel, ipProto)
}

func (m *IPSet) Flush() error {
	var err error
	err = m.Deps.flush(m.V4Name)

	if m.V6Name != "" {
		v6err := m.Deps.flush(m.V6Name)
		err = errors.Join(err, v6err)
	}
	return err
}

func (m *IPSet) ClearEntriesWithComment(comment string) error {
	var err error
	err = m.Deps.clearEntriesWithComment(m.V4Name, comment)

	if m.V6Name != "" {
		v6err := m.Deps.clearEntriesWithComment(m.V6Name, comment)
		err = errors.Join(err, v6err)
	}
	return err
}

func (m *IPSet) ClearEntriesWithIP(ip netip.Addr) error {
	ipToClear := ip.Unmap()

	if ipToClear.Is6() {
		return m.Deps.clearEntriesWithIP(m.V6Name, ipToClear)
	}
	return m.Deps.clearEntriesWithIP(m.V4Name, ipToClear)
}

func (m *IPSet) ClearEntriesWithIPAndComment(ip netip.Addr, comment string) (string, error) {
	ipToClear := ip.Unmap()

	if ipToClear.Is6() {
		return m.Deps.clearEntriesWithIPAndComment(m.V6Name, ipToClear, comment)
	}
	return m.Deps.clearEntriesWithIPAndComment(m.V4Name, ipToClear, comment)
}

func (m *IPSet) ListEntriesByIP() ([]netip.Addr, error) {
	var err error
	var set []netip.Addr
	set, err = m.Deps.listEntriesByIP(m.V4Name)

	if m.V6Name != "" {
		v6set, v6err := m.Deps.listEntriesByIP(m.V6Name)
		err = errors.Join(err, v6err)
		set = append(set, v6set...)
	}
	return set, err
}
