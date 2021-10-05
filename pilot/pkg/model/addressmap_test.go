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

package model_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
)

const (
	c1ID = "cluster-1"
	c2ID = "cluster-2"
)

var (
	c1Addresses = []string{"1.1.1.1", "1.1.1.2"}
	c2Addresses = []string{"2.1.1.1", "2.1.1.2"}
)

func TestAddressMapIsEmpty(t *testing.T) {
	cases := []struct {
		name     string
		newMap   func() *model.AddressMap
		expected bool
	}{
		{
			name: "created empty",
			newMap: func() *model.AddressMap {
				return &model.AddressMap{}
			},
			expected: true,
		},
		{
			name: "set nil addresses",
			newMap: func() *model.AddressMap {
				m := model.AddressMap{}
				m.SetAddressesFor(c1ID, nil)
				return &m
			},
			expected: true,
		},
		{
			name: "set empty addresses",
			newMap: func() *model.AddressMap {
				m := model.AddressMap{}
				m.SetAddressesFor(c1ID, make([]string, 0))
				return &m
			},
			expected: true,
		},
		{
			name: "set addresses",
			newMap: func() *model.AddressMap {
				m := model.AddressMap{}
				m.SetAddressesFor(c1ID, c1Addresses)
				return &m
			},
			expected: false,
		},
		{
			name: "add nil addresses",
			newMap: func() *model.AddressMap {
				m := model.AddressMap{}
				m.AddAddressesFor(c1ID, nil)
				return &m
			},
			expected: true,
		},
		{
			name: "add empty addresses",
			newMap: func() *model.AddressMap {
				m := model.AddressMap{}
				m.AddAddressesFor(c1ID, make([]string, 0))
				return &m
			},
			expected: true,
		},
		{
			name: "add addresses",
			newMap: func() *model.AddressMap {
				m := model.AddressMap{}
				m.AddAddressesFor(c1ID, c1Addresses)
				return &m
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(c.newMap().IsEmpty()).To(Equal(c.expected))
		})
	}
}

func TestAddressMapGetAddressesFor(t *testing.T) {
	g := NewWithT(t)

	m := model.AddressMap{
		Addresses: map[cluster.ID][]string{
			c1ID: c1Addresses,
			c2ID: c2Addresses,
		},
	}

	g.Expect(m.GetAddressesFor(c1ID)).To(Equal(c1Addresses))
	g.Expect(m.GetAddressesFor(c2ID)).To(Equal(c2Addresses))
}

func TestAddressMapSetAddressesFor(t *testing.T) {
	g := NewWithT(t)

	m := model.AddressMap{}
	m.SetAddressesFor(c1ID, c1Addresses)
	m.SetAddressesFor(c2ID, c2Addresses)

	g.Expect(m.GetAddressesFor(c1ID)).To(Equal(c1Addresses))
	g.Expect(m.GetAddressesFor(c2ID)).To(Equal(c2Addresses))
}

func TestAddressMapAddAddressesFor(t *testing.T) {
	g := NewWithT(t)

	m := model.AddressMap{}
	m.SetAddressesFor(c1ID, c1Addresses)
	m.AddAddressesFor(c1ID, []string{"1.1.1.3", "1.1.1.4"})

	g.Expect(m.GetAddressesFor(c1ID)).To(Equal([]string{"1.1.1.1", "1.1.1.2", "1.1.1.3", "1.1.1.4"}))
}

func TestAddressMapForEach(t *testing.T) {
	g := NewWithT(t)

	m := model.AddressMap{
		Addresses: map[cluster.ID][]string{
			c1ID: c1Addresses,
			c2ID: c2Addresses,
		},
	}

	found := make(map[cluster.ID][]string)
	m.ForEach(func(id cluster.ID, addrs []string) {
		found[id] = addrs
	})

	g.Expect(found[c1ID]).To(Equal(c1Addresses))
	g.Expect(found[c2ID]).To(Equal(c2Addresses))
}
