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
	"net/netip"
)

type IPSet struct {
	Name string
	Deps NetlinkIpsetDeps
}

type NetlinkIpsetDeps interface {
	ipsetIPPortCreate(name string) error
	destroySet(name string) error
	addIP(name string, ip netip.Addr, ipProto uint8, comment string, replace bool) error
	deleteIP(name string, ip netip.Addr, ipProto uint8) error
	flush(name string) error
	clearEntriesWithComment(name, comment string) error
	clearEntriesWithIP(name string, ip netip.Addr) error
	listEntriesByIP(name string) ([]netip.Addr, error)
}

func NewIPSet(name string, deps NetlinkIpsetDeps) (IPSet, error) {
	set := IPSet{
		Name: name,
		Deps: deps,
	}
	err := deps.ipsetIPPortCreate(name)
	return set, err
}

func (m *IPSet) DestroySet() error {
	return m.Deps.destroySet(m.Name)
}

func (m *IPSet) AddIP(ip netip.Addr, ipProto uint8, comment string, replace bool) error {
	return m.Deps.addIP(m.Name, ip, ipProto, comment, replace)
}

func (m *IPSet) DeleteIP(ip netip.Addr, ipProto uint8) error {
	return m.Deps.deleteIP(m.Name, ip, ipProto)
}

func (m *IPSet) Flush() error {
	return m.Deps.flush(m.Name)
}

func (m *IPSet) ClearEntriesWithComment(comment string) error {
	return m.Deps.clearEntriesWithComment(m.Name, comment)
}

func (m *IPSet) ClearEntriesWithIP(ip netip.Addr) error {
	return m.Deps.clearEntriesWithIP(m.Name, ip)
}

func (m *IPSet) ListEntriesByIP() ([]netip.Addr, error) {
	return m.Deps.listEntriesByIP(m.Name)
}
