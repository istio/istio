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
	"net/netip"
)

type realDeps struct{}

func (m *realDeps) ipsetIPHashCreate(name string, v6 bool) error {
	return errors.New("not implemented on this platform")
}

func (m *realDeps) destroySet(name string) error {
	return errors.New("not implemented on this platform")
}

func (m *realDeps) addIP(name string, ip netip.Addr, ipProto uint8, comment string, replace bool) error {
	return errors.New("not implemented on this platform")
}

func (m *realDeps) deleteIP(name string, ip netip.Addr, ipProto uint8) error {
	return errors.New("not implemented on this platform")
}

func (m *realDeps) flush(name string) error {
	return errors.New("not implemented on this platform")
}

func (m *realDeps) clearEntriesWithComment(name, comment string) error {
	return errors.New("not implemented on this platform")
}

func (m *realDeps) clearEntriesWithIP(name string, ip netip.Addr) error {
	return errors.New("not implemented on this platform")
}

func (m *realDeps) listEntriesByIP(name string) ([]netip.Addr, error) {
	return []netip.Addr{}, errors.New("not implemented on this platform")
}
