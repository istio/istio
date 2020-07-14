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

package validation

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

var nativeByteOrder binary.ByteOrder

func init() {
	var x uint16 = 0x0102
	var lowerByte = *(*byte)(unsafe.Pointer(&x))
	switch lowerByte {
	case 0x01:
		nativeByteOrder = binary.BigEndian
	case 0x02:
		nativeByteOrder = binary.LittleEndian
	default:
		panic("Could not determine native byte order.")
	}
}

// <arpa/inet.h>
func ntohs(n16 uint16) uint16 {
	if nativeByteOrder == binary.BigEndian {
		return n16
	}
	return (n16&0xff00)>>8 | (n16&0xff)<<8
}

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
	var addr *syscall.IPv6MTUInfo
	if isIpv4 {
		addr, err =
			syscall.GetsockoptIPv6MTUInfo(
				int(fd),
				syscall.IPPROTO_IP,
				constants.SoOriginalDst)
		if err != nil {
			fmt.Println("error ipv4 getsockopt")
			return
		}
		// See struct sockaddr_in
		daddr = net.IPv4(
			addr.Addr.Addr[0], addr.Addr.Addr[1], addr.Addr.Addr[2], addr.Addr.Addr[3])
	} else {
		addr, err = syscall.GetsockoptIPv6MTUInfo(
			int(fd), syscall.IPPROTO_IPV6,
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
		err := syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, 2 /*syscall.SO_REUSEADDR*/, 1)
		if err != nil {
			fmt.Printf("fail to set fd %d SO_REUSEADDR with error %v\n", descriptor, err)
		}
		err = syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, 15 /*syscall.SO_REUSEPORT*/, 1)
		if err != nil {
			fmt.Printf("fail to set fd %d SO_REUSEPORT with error %v\n", descriptor, err)
		}
	})
}
