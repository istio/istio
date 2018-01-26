// Copyright 2017 Istio Authors. All Rights Reserved.
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

// All tests should be listed here to get their test ids
const (
	CheckCacheTest uint16 = iota
	CheckReportAttributesTest
	CheckReportDisableTest
	DisableCheckCacheTest
	DisableTCPCheckCallsTest
	FailedRequestTest
	FaultInjectTest
	JWTAuthTest
	MixerInternalFailTest
	NetworkFailureTest
	ReportBatchTest
	StressEnvoyTest
	TCPMixerFilterTest
	QuotaCacheTest
	QuotaCallTest

	// The number of total tests. has to be the last one.
	maxTestNum
)

const (
	portBase uint16 = 20000
	// Maximum number of ports used in each test.
	portNum uint16 = 6
)

// Ports stores all used ports
type Ports struct {
	ClientProxyPort uint16
	ServerProxyPort uint16
	TCPProxyPort    uint16
	MixerPort       uint16
	BackendPort     uint16
	AdminPort       uint16
}

func allocPortBase(name uint16) uint16 {
	base := portBase + name*portNum
	for i := 0; i < 10; i++ {
		if allPortFree(base) {
			return base
		}
		base += maxTestNum * portNum
	}
	log.Println("could not find free ports, continue the test...")
	return base
}

func allPortFree(base uint16) bool {
	for port := base; port < base+portNum; port++ {
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
		ClientProxyPort: base,
		ServerProxyPort: base + 1,
		TCPProxyPort:    base + 2,
		MixerPort:       base + 3,
		BackendPort:     base + 4,
		AdminPort:       base + 5,
	}
}
