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
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
)

const (
	ipV4WildcardAddress = "0.0.0.0"
	ipV6WildcardAddress = "::"
)

var (
	localAddresses = getLocalAddresses()
	empty          = struct{}{}
)

// GetListeners retrieves the list of listeners from the Envoy admin port.
func GetListeners(adminPort uint16) (*bytes.Buffer, error) {
	buf, err := doHTTPGet(fmt.Sprintf("http://127.0.0.1:%d/listeners", adminPort))
	if err != nil {
		return nil, multierror.Prefix(err, "failed retrieving Envoy listeners:")
	}
	return buf, nil
}

// ParseInboundListeners parses the given Envoy response (returned from GetListeners) and returns a set of all inbound ports.
func ParseInboundListeners(fromEnvoy string) (map[uint16]struct{}, error) {
	// Clean the input string to remove surrounding brackets
	input := strings.Trim(strings.TrimSpace(fromEnvoy), "[]")

	// Get the individual listener strings.
	listeners := strings.Split(input, ",")
	ports := make(map[uint16]struct{})
	for _, l := range listeners {
		// Remove quotes around the string
		l = strings.Trim(strings.TrimSpace(l), "\"")

		ipAddrParts := strings.Split(l, ":")
		if len(ipAddrParts) < 2 {
			return nil, fmt.Errorf("failed parsing Envoy listener: %s", l)
		}
		// Before checking if listener is local, removing port portion of the address
		ipAddr := strings.TrimSuffix(l, ":"+ipAddrParts[len(ipAddrParts)-1])
		if !isLocalListener(ipAddr) {
			continue
		}

		portStr := ipAddrParts[len(ipAddrParts)-1]
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil, multierror.Prefix(err, fmt.Sprintf("failed parsing port for Envoy listener: %s", l))
		}

		ports[uint16(port)] = empty
	}

	return ports, nil
}

func isLocalListener(l string) bool {
	// If it's an IPv6 address, strip off the surrounding brackets.
	l = strings.Trim(l, "[]")

	ip := net.ParseIP(l)
	if ip == nil {
		// Failed parsing the IP.
		return false
	}

	address := ip.String()
	_, ok := localAddresses[address]
	return ok
}

func getLocalAddresses() map[string]struct{} {
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err.Error())
	}

	addressSet := make(map[string]struct{})

	// Add wildcard addresses for ipv4 and ipv6.
	addressSet[net.ParseIP(ipV4WildcardAddress).String()] = empty
	addressSet[net.ParseIP(ipV6WildcardAddress).String()] = empty

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
			if ip != nil {
				addressSet[ip.String()] = empty
			}
		}
	}

	return addressSet
}
