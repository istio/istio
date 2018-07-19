// Copyright 2017 Istio Authors
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

package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"istio.io/istio/pkg/log"
)

// ErrResolveNoAddress error occurs when IP address resolution is attempted,
// but no address was provided.
var ErrResolveNoAddress = errors.New("no address specified")

// ResolveAddr resolves an authority address to an IP address. Incoming
// addr can be an IP address or hostname. If addr is an IPv6 address, the IP
// part must be enclosed in square brackets.
//
// TODO: LookupIPAddr() may return multiple IP addresses, of which this function
// returns the first entry. We're assuming that IPv4 addresses will be listed
// first, meaning it has priority over IPv6. If this does not hold, then
// additional logic will be needed to enforce an IP mode priority.
//
// To use this function in an IPv6 only environment, either provide an IPv6
// address or ensure the hostname resolves to only IPv6 addresses.
func ResolveAddr(addr string) (string, error) {
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
	addrs, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
	if lookupErr != nil || len(addrs) == 0 {
		return "", fmt.Errorf("lookup failed for IP address: %v", lookupErr)
	}
	var resolvedAddr string
	ip := addrs[0].IP
	if ip.To4() == nil {
		resolvedAddr = fmt.Sprintf("[%s]:%s", ip, port)
	} else {
		resolvedAddr = fmt.Sprintf("%s:%s", ip, port)
	}
	log.Infof("Addr resolved to: %s", resolvedAddr)
	return resolvedAddr, nil
}
