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
	"net"
	"time"
)

// Network-related utility functions

const (
	waitInterval = 100 * time.Millisecond
	waitTimeout  = 2 * time.Minute
)

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
