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

package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"

	"istio.io/pkg/log"
)

// Network-related utility functions
const (
	waitInterval = 100 * time.Millisecond
	waitTimeout  = 2 * time.Minute
)

type lookupIPAddrType = func(ctx context.Context, addr string) ([]net.IPAddr, error)

// ErrResolveNoAddress error occurs when IP address resolution is attempted,
// but no address was provided.
var ErrResolveNoAddress = errors.New("no address specified")

// GetPrivateIPs blocks until private IP addresses are available, or a timeout is reached.
func GetPrivateIPs(ctx context.Context) ([]string, bool) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, waitTimeout)
		defer cancel()
	}

	for {
		select {
		case <-ctx.Done():
			return getPrivateIPsIfAvailable()
		default:
			addr, ok := getPrivateIPsIfAvailable()
			if ok {
				return addr, true
			}
			time.Sleep(waitInterval)
		}
	}
}

// Returns all the private IP addresses
func getPrivateIPsIfAvailable() ([]string, bool) {
	ok := true
	ipAddresses := make([]string, 0)

	ifaces, _ := net.Interfaces()

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, _ := iface.Addrs()

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip.IsUnspecified() {
				ok = false
				continue
			}
			ipAddresses = append(ipAddresses, ip.String())
		}
	}
	return ipAddresses, ok
}

// ResolveAddr resolves an authority address to an IP address. Incoming
// addr can be an IP address or hostname. If addr is an IPv6 address, the IP
// part must be enclosed in square brackets.
//
// LookupIPAddr() may return multiple IP addresses, of which this function returns
// the first IPv4 entry. To use this function in an IPv6 only environment, either
// provide an IPv6 address or ensure the hostname resolves to only IPv6 addresses.
func ResolveAddr(addr string, lookupIPAddr ...lookupIPAddrType) (string, error) {
	if addr == "" {
		return "", ErrResolveNoAddress
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	log.Infof("Attempting to lookup address: %s", host)
	defer log.Infof("Finished lookup of address: %s", host)
	// lookup the udp address with a timeout of 15 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var addrs []net.IPAddr
	var lookupErr error
	if (len(lookupIPAddr) > 0) && (lookupIPAddr[0] != nil) {
		// if there are more than one lookup function, ignore all but first
		addrs, lookupErr = lookupIPAddr[0](ctx, host)
	} else {
		addrs, lookupErr = net.DefaultResolver.LookupIPAddr(ctx, host)
	}

	if lookupErr != nil || len(addrs) == 0 {
		return "", errors.WithMessage(lookupErr, "lookup failed for IP address")
	}
	var resolvedAddr string

	for _, address := range addrs {
		ip := address.IP
		if ip.To4() == nil {
			resolvedAddr = fmt.Sprintf("[%s]:%s", ip, port)
		} else {
			resolvedAddr = fmt.Sprintf("%s:%s", ip, port)
			break
		}
	}
	log.Infof("Addr resolved to: %s", resolvedAddr)
	return resolvedAddr, nil
}
