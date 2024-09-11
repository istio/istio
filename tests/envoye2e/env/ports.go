// Copyright 2019 Istio Authors
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

package env

import (
	"log"
)

// Dynamic port allocation scheme
// In order to run the tests in parallel. Each test should use unique ports
// Each test has a unique test_name, its ports will be allocated based on that name

const (
	portBase uint16 = 20000
	// Maximum number of ports used in each test.
	portNum       uint16 = 20
	portBaseShift uint16 = 3000
)

// Ports stores all used ports
type Ports struct {
	BackendPort      uint16
	ClientAdmin      uint16
	ClientPort       uint16
	ServerPort       uint16
	ServerAdmin      uint16
	XDSPort          uint16
	ServerTunnelPort uint16
	Max              uint16
}

func allocPortBase(name uint16) uint16 {
	base := portBase + name*portNum
	for i := 0; i < 10; i++ {
		if allPortFree(base, portNum) {
			return base
		}
		// Shift base port if there is collision.
		base += portBaseShift
	}
	log.Println("could not find free ports, continue the test...")
	return base
}

func allPortFree(base uint16, ports uint16) bool {
	for port := base; port < base+ports; port++ {
		if IsPortUsed(port) {
			log.Println("port is used ", port)
			return false
		}
	}
	return true
}

// NewPorts allocate all ports based on test id.
func NewPorts(name uint16) *Ports {
	base := allocPortBase(name)
	return &Ports{
		BackendPort:      base,
		ClientAdmin:      base + 1,
		ClientPort:       base + 2,
		ServerPort:       base + 3,
		ServerAdmin:      base + 4,
		XDSPort:          base + 5,
		ServerTunnelPort: base + 6,
		Max:              base + 6,
	}
}
