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

// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package validation

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"

	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// Recover the original address from redirect socket. Supposed to work for tcp over ipv4 and ipv6.
func GetOriginalDestination(conn net.Conn) (daddr net.IP, dport uint16, err error) {
	// obtain os fd from Conn
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		err = errors.New("socket is not tcp")
		return
	}
	file, err := tcp.File()
	if err != nil {
		return
	}
	defer file.Close()
	fd := file.Fd()

	// Detect underlying ip is v4 or v6
	ip := conn.RemoteAddr().(*net.TCPAddr).IP
	isIpv4 := false
	if ip.To4() != nil {
		isIpv4 = true
	} else if ip.To16() != nil {
		isIpv4 = false
	} else {
		err = fmt.Errorf("neither ipv6 nor ipv4 original addr: %s", ip)
		return
	}

	// golang doesn't provide a struct sockaddr_storage
	// IPv6MTUInfo is chosen because
	// 1. it is no smaller than sockaddr_storage,
	// 2. it is provide the port field value
	var addr *unix.IPv6MTUInfo
	if isIpv4 {
		addr, err =
			unix.GetsockoptIPv6MTUInfo(
				int(fd),
				unix.IPPROTO_IP,
				constants.SoOriginalDst)
		if err != nil {
			fmt.Println("error ipv4 getsockopt")
			return
		}
		// See struct sockaddr_in
		daddr = net.IPv4(
			addr.Addr.Addr[0], addr.Addr.Addr[1], addr.Addr.Addr[2], addr.Addr.Addr[3])
	} else {
		addr, err = unix.GetsockoptIPv6MTUInfo(
			int(fd), unix.IPPROTO_IPV6,
			constants.SoOriginalDst)

		if err != nil {
			fmt.Println("error ipv6 getsockopt")
			return
		}
		// See struct sockaddr_in6
		daddr = addr.Addr.Addr[:]
	}
	// See sockaddr_in6 and sockaddr_in
	dport = ntohs(addr.Addr.Port)

	fmt.Printf("local addr %s\n", conn.LocalAddr())
	fmt.Printf("original addr %s:%d\n", ip, dport)
	return
}

// Setup reuse address to run the validation server more robustly
func reuseAddr(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(descriptor uintptr) {
		err := unix.SetsockoptInt(int(descriptor), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if err != nil {
			fmt.Printf("fail to set fd %d SO_REUSEADDR with error %v\n", descriptor, err)
		}
		err = unix.SetsockoptInt(int(descriptor), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		if err != nil {
			fmt.Printf("fail to set fd %d SO_REUSEPORT with error %v\n", descriptor, err)
		}
	})
}
