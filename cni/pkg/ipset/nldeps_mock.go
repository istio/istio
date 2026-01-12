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

	"github.com/stretchr/testify/mock"
)

type MockedIpsetDeps struct {
	mock.Mock
}

func FakeNLDeps() *MockedIpsetDeps {
	return &MockedIpsetDeps{}
}

func (m *MockedIpsetDeps) ipsetIPHashCreate(name string, v6 bool) error {
	args := m.Called(name, v6)
	return args.Error(0)
}

func (m *MockedIpsetDeps) destroySet(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockedIpsetDeps) addIP(name string, ip netip.Addr, ipProto uint8, comment string, replace bool) error {
	args := m.Called(name, ip, ipProto, comment, replace)
	return args.Error(0)
}

func (m *MockedIpsetDeps) deleteIP(name string, ip netip.Addr, ipProto uint8) error {
	args := m.Called(name, ip, ipProto)
	return args.Error(0)
}

func (m *MockedIpsetDeps) flush(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockedIpsetDeps) clearEntriesWithComment(name, comment string) error {
	args := m.Called(name, comment)
	return args.Error(0)
}

func (m *MockedIpsetDeps) clearEntriesWithIP(name string, ip netip.Addr) error {
	args := m.Called(name, ip)
	return args.Error(0)
}

func (m *MockedIpsetDeps) clearEntriesWithIPAndComment(name string, ip netip.Addr, comment string) (string, error) {
	args := m.Called(name, ip, comment)
	return args.Get(0).(string), args.Error(1)
}

func (m *MockedIpsetDeps) listEntriesByIP(name string) ([]netip.Addr, error) {
	args := m.Called(name)
	return args.Get(0).([]netip.Addr), args.Error(1)
}
