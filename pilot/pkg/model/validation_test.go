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

package model

import (
	"testing"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
)

var (
	endpoint1 = NetworkEndpoint{
		Address:     "192.168.1.1",
		Port:        10001,
		ServicePort: &Port{Name: "http", Port: 81, Protocol: protocol.HTTP},
	}

	service1 = &Service{
		Hostname: "one.service.com",
		Address:  "192.168.3.1", // VIP
		Ports: PortList{
			&Port{Name: "http", Port: 81, Protocol: protocol.HTTP},
			&Port{Name: "http-alt", Port: 8081, Protocol: protocol.HTTP},
		},
	}
)

func TestServiceInstanceValidate(t *testing.T) {
	cases := []struct {
		name     string
		instance *ServiceInstance
		valid    bool
	}{
		{
			name: "nil service",
			instance: &ServiceInstance{
				Labels:   labels.Instance{},
				Endpoint: endpoint1,
			},
		},
		{
			name: "bad label",
			instance: &ServiceInstance{
				Service:  service1,
				Labels:   labels.Instance{"*": "-"},
				Endpoint: endpoint1,
			},
		},
		{
			name: "invalid service",
			instance: &ServiceInstance{
				Service: &Service{},
			},
		},
		{
			name: "invalid endpoint port and service port",
			instance: &ServiceInstance{
				Service: service1,
				Endpoint: NetworkEndpoint{
					Address: "192.168.1.2",
					Port:    -80,
				},
			},
		},
		{
			name: "endpoint missing service port",
			instance: &ServiceInstance{
				Service: service1,
				Endpoint: NetworkEndpoint{
					Address: "192.168.1.2",
					Port:    service1.Ports[1].Port,
					ServicePort: &Port{
						Name:     service1.Ports[1].Name + "-extra",
						Port:     service1.Ports[1].Port,
						Protocol: service1.Ports[1].Protocol,
					},
				},
			},
		},
		{
			name: "endpoint port and protocol mismatch",
			instance: &ServiceInstance{
				Service: service1,
				Endpoint: NetworkEndpoint{
					Address: "192.168.1.2",
					Port:    service1.Ports[1].Port,
					ServicePort: &Port{
						Name:     "http",
						Port:     service1.Ports[1].Port + 1,
						Protocol: protocol.GRPC,
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Log("running case " + c.name)
		if got := c.instance.Validate(); (got == nil) != c.valid {
			t.Errorf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestServiceValidate(t *testing.T) {
	ports := PortList{
		{Name: "http", Port: 80, Protocol: protocol.HTTP},
		{Name: "http-alt", Port: 8080, Protocol: protocol.HTTP},
	}
	badPorts := PortList{
		{Port: 80, Protocol: protocol.HTTP},
		{Name: "http-alt^", Port: 8080, Protocol: protocol.HTTP},
		{Name: "http", Port: -80, Protocol: protocol.HTTP},
	}

	address := "192.168.1.1"

	cases := []struct {
		name    string
		service *Service
		valid   bool
	}{
		{
			name:    "empty hostname",
			service: &Service{Hostname: "", Address: address, Ports: ports},
		},
		{
			name:    "invalid hostname",
			service: &Service{Hostname: "hostname.^.com", Address: address, Ports: ports},
		},
		{
			name:    "empty ports",
			service: &Service{Hostname: "hostname", Address: address},
		},
		{
			name:    "bad ports",
			service: &Service{Hostname: "hostname", Address: address, Ports: badPorts},
		},
	}
	for _, c := range cases {
		if got := c.service.Validate(); (got == nil) != c.valid {
			t.Errorf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateNetworkEndpointAddress(t *testing.T) {
	testCases := []struct {
		name  string
		ne    *NetworkEndpoint
		valid bool
	}{
		{
			"Unix OK",
			&NetworkEndpoint{Family: AddressFamilyUnix, Address: "/absolute/path"},
			true,
		},
		{
			"IP OK",
			&NetworkEndpoint{Address: "12.3.4.5", Port: 76},
			true,
		},
		{
			"Unix not absolute",
			&NetworkEndpoint{Family: AddressFamilyUnix, Address: "./socket"},
			false,
		},
		{
			"IP invalid",
			&NetworkEndpoint{Address: "260.3.4.5", Port: 76},
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNetworkEndpointAddress(tc.ne)
			if tc.valid && err != nil {
				t.Fatalf("ValidateAddress() => want error nil got %v", err)
			} else if !tc.valid && err == nil {
				t.Fatalf("ValidateAddress() => want error got nil")
			}
		})
	}
}
