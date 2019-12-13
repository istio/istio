package validation

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/coreos/etcd/pkg/cpuutil"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	"net"
	"syscall"
)

// <arpa/inet.h>
func ntohs(n16 uint16) uint16 {
	var bytes [2]byte
	binary.BigEndian.PutUint16(bytes[:], n16)
	return cpuutil.ByteOrder().Uint16(bytes[:])
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
		err = errors.New("neither ipv6 nor ipv4 original addr: " + ip.String())
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
				constants.SO_ORIGINAL_DST)
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
			constants.SO_ORIGINAL_DST)

		if err != nil {
			fmt.Println("error ipv6 getsockopt")
			return
		}
		// See struct sockaddr_in6
		daddr = addr.Addr.Addr[:]
	}
	// See sockaddr_in6 and sockaddr_in
	dport = ntohs(addr.Addr.Port)

	fmt.Println("local addr ", conn.LocalAddr())
	fmt.Println("original addr ", ip, ":", dport)
	return
}
