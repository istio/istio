// Copyright Istio Authors
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
	endpoint1 = IstioEndpoint{
		Address:      "192.168.1.1",
		EndpointPort: 10001,
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
	endpointWithLabels := func(lbls labels.Instance) *IstioEndpoint {
		cpy := endpoint1
		cpy.Labels = lbls
		return &cpy
	}

	cases := []struct {
		name     string
		instance *ServiceInstance
		valid    bool
	}{
		{
			name: "nil service",
			instance: &ServiceInstance{
				Endpoint: endpointWithLabels(labels.Instance{}),
			},
		},
		{
			name: "bad label",
			instance: &ServiceInstance{
				Service:  service1,
				Endpoint: endpointWithLabels(labels.Instance{"*": "-"}),
			},
		},
		{
			name: "invalid service",
			instance: &ServiceInstance{
				Service:  &Service{},
				Endpoint: &IstioEndpoint{},
			},
		},
		{
			name: "invalid endpoint port and service port",
			instance: &ServiceInstance{
				Service: service1,
				Endpoint: &IstioEndpoint{
					Address:      "192.168.1.2",
					EndpointPort: 1000000,
				},
			},
		},
		{
			name: "endpoint missing service port",
			instance: &ServiceInstance{
				Service: service1,
				ServicePort: &Port{
					Name:     service1.Ports[1].Name + "-extra",
					Port:     service1.Ports[1].Port,
					Protocol: service1.Ports[1].Protocol,
				},
				Endpoint: &IstioEndpoint{
					Address:      "192.168.1.2",
					EndpointPort: uint32(service1.Ports[1].Port),
				},
			},
		},
		{
			name: "endpoint port and protocol mismatch",
			instance: &ServiceInstance{
				Service: service1,
				ServicePort: &Port{
					Name:     "http",
					Port:     service1.Ports[1].Port + 1,
					Protocol: protocol.GRPC,
				},
				Endpoint: &IstioEndpoint{
					Address:      "192.168.1.2",
					EndpointPort: uint32(service1.Ports[1].Port),
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.instance.Validate(); (got == nil) != c.valid {
				t.Fatalf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
			}
		})
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
