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

type IPPortSet struct {
	Name string
	Deps NetlinkIpsetDeps
}

type NetlinkIpsetDeps interface {
	ipsetIPPortCreate(name string) error
	destroySet(name string) error
	addIPPort(name string, ip netip.Addr, port uint16, ipProto uint8, comment string, replace bool) error
	deleteIPPort(name string, ip netip.Addr, port uint16, ipProto uint8) error
	flush(name string) error
	clearEntriesWithComment(name, comment string) error
	clearEntriesWithIP(name string, ip netip.Addr) error
	listEntriesByIP(name string) ([]netip.Addr, error)
}

func NewIPPortSet(name string, deps NetlinkIpsetDeps) (IPPortSet, error) {
	set := IPPortSet{
		Name: name,
		Deps: deps,
	}
	err := deps.ipsetIPPortCreate(name)
	return set, err
}

func (m *IPPortSet) DestroySet() error {
	return m.Deps.destroySet(m.Name)
}

func (m *IPPortSet) AddIPPort(ip netip.Addr, port uint16, ipProto uint8, comment string, replace bool) error {
	return m.Deps.addIPPort(m.Name, ip, port, ipProto, comment, replace)
}

func (m *IPPortSet) DeleteIPPort(ip netip.Addr, port uint16, ipProto uint8) error {
	return m.Deps.deleteIPPort(m.Name, ip, port, ipProto)
}

func (m *IPPortSet) Flush() error {
	return m.Deps.flush(m.Name)
}

func (m *IPPortSet) ClearEntriesWithComment(comment string) error {
	return m.Deps.clearEntriesWithComment(m.Name, comment)
}

func (m *IPPortSet) ClearEntriesWithIP(ip netip.Addr) error {
	return m.Deps.clearEntriesWithIP(m.Name, ip)
}

func (m *IPPortSet) ListEntriesByIP() ([]netip.Addr, error) {
	return m.Deps.listEntriesByIP(m.Name)
}
