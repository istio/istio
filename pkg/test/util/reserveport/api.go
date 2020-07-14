//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package reserveport

import "testing"

const (
	poolSize = 50
)

// ReservedPort a port reserved by a PortManager
type ReservedPort interface {
	// GetPort returns the bound port number.
	GetPort() uint16
	// Close unbinds this port.
	Close() error
	CloseSilently()
}

// PortManager is responsible for reserving ports for an application.
type PortManager interface {
	// ReservePort reserves a new port. The lifecycle of the returned port is transferred to the caller.
	ReservePort() (ReservedPort, error)
	ReservePortOrFail(t *testing.T) ReservedPort
	ReservePortNumber() (uint16, error)
	ReservePortNumberOrFail(t *testing.T) uint16
	// Close shuts down this manager and frees any associated resources.
	Close() error
	CloseSilently()
}

// NewPortManager allocates a new PortManager
func NewPortManager() (mgr PortManager, err error) {
	return &managerImpl{}, nil
}

// NewPortManagerOrFail calls NewPortManager and fails the test if unsuccessful.
func NewPortManagerOrFail(t *testing.T) PortManager {
	t.Helper()
	mgr, err := NewPortManager()
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}
