//go:build !linux
// +build !linux

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
	"net"
)

var ErrNotImplemented = errors.New("not implemented")

type IPSet struct {
	// the name of the ipset to use
	Name string
}

func (m *IPSet) CreateSet() error {
	return ErrNotImplemented
}

func (m *IPSet) DestroySet() error {
	return ErrNotImplemented
}

func (m *IPSet) AddIP(ip net.IP, comment string) error {
	return ErrNotImplemented
}

func (m *IPSet) Flush() error {
	return ErrNotImplemented
}

func (m *IPSet) DeleteIP(ip net.IP) error {
	return ErrNotImplemented
}

func (m *IPSet) ClearEntriesWithComment(comment string) error {
	return ErrNotImplemented
}
