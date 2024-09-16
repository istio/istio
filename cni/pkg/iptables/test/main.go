//go:build windows
// +build windows

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Microsoft/hcsshim/hcn"
	"istio.io/istio/cni/pkg/iptables"
)

// just here for testing
func main() {
	hostSNATIPv4 := "10.244.1.21"
	policySetting := hcn.L4WfpProxyPolicySetting{
		OutboundProxyPort: strconv.Itoa(iptables.ZtunnelInboundPlaintextPort),
		UserSID:           "S-1-5-18", // user local sid
		FilterTuple: hcn.FiveTuple{
			RemoteAddresses: hostSNATIPv4,
			Protocols:       "6",
			Priority:        1,
		},
		InboundExceptions: hcn.ProxyExceptions{
			IpAddressExceptions: []string{"127.0.0.1"},
			PortExceptions:      []string{strconv.Itoa(iptables.ZtunnelInboundPort)},
		},
	}

	data, _ := json.Marshal(&policySetting)

	endpointPolicy := hcn.EndpointPolicy{
		Type:     hcn.L4WFPPROXY,
		Settings: data,
	}

	request := hcn.PolicyEndpointRequest{
		Policies: []hcn.EndpointPolicy{endpointPolicy},
	}

	endpoint, err := hcn.GetEndpointByID("0bfa334b-56de-4e91-bbf5-e576fda7c8b8")
	if err != nil {
		panic(err)
	}

	err = endpoint.ApplyPolicy(hcn.RequestTypeAdd, request)

	if err != nil {
		panic(err)
	}

	endpoint, err = hcn.GetEndpointByID("0bfa334b-56de-4e91-bbf5-e576fda7c8b8")
	if err != nil {
		panic(err)
	}

	for _, policy := range endpoint.Policies {
		fmt.Printf("%v\n", string(policy.Settings))
	}

}
