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
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/golang/glog"
)

// ResolveAddr resolves an authority address to an IP address
func ResolveAddr(addr string) (string, error) {
	if addr == "" {
		return "", nil
	}
	colon := strings.Index(addr, ":")
	host := addr[:colon]
	port := addr[colon:]
	glog.Infof("Attempting to lookup address: %s", host)
	defer glog.Infof("Finished lookup of address: %s", host)
	// lookup the udp address with a timeout of 15 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	addrs, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
	if lookupErr != nil {
		return "", fmt.Errorf("lookup failed for udp address: %v", lookupErr)
	}
	resolvedAddr := fmt.Sprintf("%s%s", addrs[0].IP, port)
	glog.Infof("Addr resolved to: %s", resolvedAddr)
	return resolvedAddr, nil
}
