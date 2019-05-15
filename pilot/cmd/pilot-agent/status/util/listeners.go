// Copyright 2018 Istio Authors
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

package util

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
)

var (
	ipPrefixes = getLocalIPPrefixes()
)

// GetInboundListeningPorts returns a map of inbound ports for which Envoy has active listeners.
func GetInboundListeningPorts(localHostAddr string, adminPort uint16) (map[uint16]bool, string, error) {
	buf, err := doHTTPGet(fmt.Sprintf("http://%s:%d/listeners", localHostAddr, adminPort))
	if err != nil {
		return nil, "", multierror.Prefix(err, "failed retrieving Envoy listeners:")
	}

	// Clean the input string to remove surrounding brackets
	input := strings.Trim(strings.TrimSpace(buf.String()), "[]")

	// Get the individual listener strings.
	listeners := strings.Split(input, ",")
	ports := make(map[uint16]bool)
	for _, l := range listeners {
		// Remove quotes around the string
		l = strings.Trim(strings.TrimSpace(l), "\"")

		ipAddrParts := strings.Split(l, ":")
		if len(ipAddrParts) < 2 {
			return nil, "", fmt.Errorf("failed parsing Envoy listener: %s", l)
		}
		// Before checking if listener is local, removing port portion of the address
		ipAddr := strings.TrimSuffix(l, ":"+ipAddrParts[len(ipAddrParts)-1])
		if !isLocalListener(ipAddr) {
			continue
		}

		portStr := ipAddrParts[len(ipAddrParts)-1]
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil, "", multierror.Prefix(err, fmt.Sprintf("failed parsing port for Envoy listener: %s", l))
		}

		ports[uint16(port)] = true
	}

	return ports, input, nil
}

func isLocalListener(l string) bool {
	for _, ipPrefix := range ipPrefixes {
		// In case of IPv6 address, it always comes in "[]", remove them so HasPrefix would work
		if strings.HasPrefix(strings.Trim(l, "[]"), ipPrefix) {
			return true
		}
	}
	return false
}

func getLocalIPPrefixes() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err.Error())
	}

	prefixes := make([]string, 0, len(ifaces))

	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			prefixes = append(prefixes, ip.String())
		}
	}
	return prefixes
}
