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

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package validation

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// Recover the original address from redirect socket. Supposed to work for tcp over ipv4 and ipv6.
func GetOriginalDestination(conn net.Conn) (daddr net.IP, dport uint16, err error) {
	// obtain os fd from Conn
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		err = errors.New("socket is not tcp")
		return daddr, dport, err
	}
	file, err := tcp.File()
	if err != nil {
		return daddr, dport, err
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
		return daddr, dport, err
	}

	// golang doesn't provide a struct sockaddr_storage
	// IPv6MTUInfo is chosen because
	// 1. it is no smaller than sockaddr_storage,
	// 2. it is provide the port field value
	var addr *unix.IPv6MTUInfo
	if isIpv4 {
		addr, err = unix.GetsockoptIPv6MTUInfo(
			int(fd),
			unix.IPPROTO_IP,
			constants.SoOriginalDst)
		if err != nil {
			log.Errorf("Error ipv4 getsockopt: %v", err)
			return daddr, dport, err
		}
		// See struct sockaddr_in
		daddr = net.IPv4(
			addr.Addr.Addr[0], addr.Addr.Addr[1], addr.Addr.Addr[2], addr.Addr.Addr[3])
	} else {
		addr, err = unix.GetsockoptIPv6MTUInfo(
			int(fd), unix.IPPROTO_IPV6,
			constants.SoOriginalDst)
		if err != nil {
			log.Errorf("Error to ipv6 getsockopt: %v", err)
			return daddr, dport, err
		}
		// See struct sockaddr_in6
		daddr = addr.Addr.Addr[:]
	}
	// See sockaddr_in6 and sockaddr_in
	dport = ntohs(addr.Addr.Port)

	log.Infof("Local addr %s", conn.LocalAddr())
	log.Infof("Original addr %s: %d", ip, dport)
	return daddr, dport, err
}

// Setup reuse address to run the validation server more robustly
func reuseAddr(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(descriptor uintptr) {
		err := unix.SetsockoptInt(int(descriptor), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if err != nil {
			log.Errorf("Fail to set fd %d SO_REUSEADDR with error %v", descriptor, err)
		}
		err = unix.SetsockoptInt(int(descriptor), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		if err != nil {
			log.Errorf("Fail to set fd %d SO_REUSEPORT with error %v", descriptor, err)
		}
	})
}
