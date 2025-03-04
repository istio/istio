//go:build !unix
// +build !unix

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package validation

import (
	"fmt"
	"net"
	"syscall"
)

func GetOriginalDestination(conn net.Conn) (daddr net.IP, dport uint16, err error) {
	return nil, 0, fmt.Errorf("GetOriginalDestination is not supported on this platform")
}

func reuseAddr(network, address string, conn syscall.RawConn) error {
	return fmt.Errorf("reuseAddr is not supported on this platform")
}
