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

// GetPrivateIP blocks until a private IP address is available, or a timeout is reached.
func GetPrivateIP(ctx context.Context) (net.IP, bool) {
	if _, ok := ctx.Deadline(); !ok {
		context.WithTimeout(ctx, waitTimeout)
	}

	for {
		select {
		case <-ctx.Done():
			return net.IPv4zero, false
		default:
			addr := getPrivateIPIfAvailable()
			if !addr.IsUnspecified() {
				return addr, true
			}
			time.Sleep(waitInterval)
		}
	}
}

// Returns a private IP address, or unspecified IP (0.0.0.0) if no IP is available
func getPrivateIPIfAvailable() net.IP {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		if !ip.IsLoopback() {
			return ip
		}
	}
	return net.IPv4zero
}
