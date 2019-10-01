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
	"strings"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/golang/protobuf/jsonpb"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
)

var (
	ipPrefixes = getLocalIPPrefixes()
)

// GetInboundListeningPorts returns a map of inbound ports for which Envoy has active listeners.
func GetInboundListeningPorts(localHostAddr string, adminPort uint16, nodeType model.NodeType) (map[uint16]bool, string, error) {
	buf, err := doHTTPGet(fmt.Sprintf("http://%s:%d/listeners?format=json", localHostAddr, adminPort))
	if err != nil {
		return nil, "", multierror.Prefix(err, "failed retrieving Envoy listeners:")
	}

	listeners := &admin.Listeners{}
	if err := jsonpb.Unmarshal(buf, listeners); err != nil {
		return nil, "", fmt.Errorf("failed parsing Envoy listeners %s: %s", buf, err)
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
