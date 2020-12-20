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

// +build !aix,!darwin,!dragonfly,!freebsd,!linux,!netbsd,!openbsd,!solaris

package validation

import (
	"errors"
	"net"
	"syscall"
)

// Recover the original address from redirect socket. Supposed to work for tcp over ipv4 and ipv6.
func GetOriginalDestination(_ net.Conn) (net.IP, uint16, error) {
	return nil, 0, errors.New("not supported on the current platform")
}

// Setup reuse address to run the validation server more robustly
func reuseAddr(_, _ string, _ syscall.RawConn) error {
	return errors.New("not supported on the current platform")
}
