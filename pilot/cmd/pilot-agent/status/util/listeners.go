// Copyright 2019 Istio Authors
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

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
)

var (
	ipPrefixes = getLocalIPPrefixes()
)

// convertInboundListeningPortsOutput converts the non-Json listener output to the Listeners admin API.
// This is needed for backward compatibility in case the Pilot-agent is talking to old version Envoy
// that doesn't support to dump its listeners in Json format.
func convertInboundListeningPortsOutput(buf string) (*admin.Listeners, error) {
	// Clean the input string to remove surrounding brackets
	input := strings.Trim(strings.TrimSpace(buf), "[]")
	// Get the individual listener strings.
	listeners := strings.Split(input, ",")
	parsedListeners := &admin.Listeners{}
	for _, l := range listeners {
		// Remove quotes around the string
		l = strings.Trim(strings.TrimSpace(l), "\"")

		ipAddrParts := strings.Split(l, ":")
		if len(ipAddrParts) < 2 {
			return nil, fmt.Errorf("failed parsing address: %s", l)
		}
		// Before checking if listener is local, removing port portion of the address
		ipAddr := strings.TrimSuffix(l, ":"+ipAddrParts[len(ipAddrParts)-1])
		portStr := ipAddrParts[len(ipAddrParts)-1]
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil, multierror.Prefix(err, fmt.Sprintf("failed parsing port: %s", l))
		}

		parsedListeners.ListenerStatuses = append(parsedListeners.ListenerStatuses, &admin.ListenerStatus{
			LocalAddress: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: ipAddr,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: uint32(port),
						},
					},
				},
			},
		})
	}

	return parsedListeners, nil
}

// GetInboundListeningPorts returns a map of inbound ports for which Envoy has active listeners.
func GetInboundListeningPorts(localHostAddr string, adminPort uint16, nodeType model.NodeType) (map[uint16]bool, string, error) {
	buf, err := doHTTPGet(fmt.Sprintf("http://%s:%d/listeners?format=json", localHostAddr, adminPort))
	if err != nil {
		return nil, "", multierror.Prefix(err, "failed retrieving Envoy listeners:")
	}

	listeners := &admin.Listeners{}
	bufStr := buf.String()
	if err := jsonpb.UnmarshalString(bufStr, listeners); err != nil {
		// Try again to convert the output to the Listeners admin API in case there is any version mismatch
		// between pilot-agent and Envoy.
		listeners, err = convertInboundListeningPortsOutput(bufStr)
		if err != nil {
			return nil, "", fmt.Errorf("failed parsing Envoy listeners %s: %s", buf, err)
		}
	}
	ports := make(map[uint16]bool)
	for _, l := range listeners.ListenerStatuses {
		socketAddr := l.LocalAddress.GetSocketAddress()
		if socketAddr == nil {
			// Only care about socket address, ignore pipe.
			continue
		}

		ipAddr := socketAddr.GetAddress()
		switch nodeType {
		// For gateways, we will not listen on a local host, instead on 0.0.0.0
		case model.Router:
			if ipAddr != "0.0.0.0" {
				continue
			}
		default:
			if !isLocalListener(ipAddr) {
				continue
			}
		}

		port := socketAddr.GetPortValue()
		ports[uint16(port)] = true
	}

	return ports, buf.String(), nil
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
