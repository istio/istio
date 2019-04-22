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

package model

import (
	"fmt"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
)

func TestMergeGateways(t *testing.T) {
	configGw1 := makeConfig("foo1", "not-default", "foo.bar.com", "name1", "http", 7, "ingressgateway")
	configGw2 := makeConfig("foo2", "not-default", "*", "name2", "http", 7, "ingressgateway2")
	configGw3 := makeConfig("foo3", "not-default", "*", "name3", "http", 8, "ingressgateway")
	configGw4 := makeConfig("foo4", "not-default-2", "*", "name4", "tcp", 8, "ingressgateway")

	tests := []struct {
		name        string
		gwConfig    []Config
		serversNum  int
		routesNum   int
		gatewaysNum int
	}{
		{
			"single-server-config",
			[]Config{configGw1},
			1,
			1,
			1,
		},
		{
			"same-server-config",
			[]Config{configGw1, configGw2},
			1,
			1,
			2,
		},
		{
			"multi-server-config",
			[]Config{configGw1, configGw2, configGw3},
			2,
			2,
			3,
		},
		{
			"http-tcp-server-config",
			[]Config{configGw1, configGw4},
			2,
			1,
			2,
		},
		{
			"tcp-tcp-server-config",
			[]Config{configGw4, configGw3},
			1,
			0,
			2,
		},
		{
			"tcp-tcp-server-config",
			[]Config{configGw3, configGw4}, //order matters
			1,
			1,
			2,
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			mgw := MergeGateways(tt.gwConfig...)
			if len(mgw.Servers) != tt.serversNum {
				t.Errorf("Incorrect number of servers. Expected: %v Got: %d", tt.serversNum, len(mgw.Servers))
			}
			if len(mgw.ServersByRouteName) != tt.routesNum {
				t.Errorf("Incorrect number of routes. Expected: %v Got: %d", tt.routesNum, len(mgw.ServersByRouteName))
			}
			if len(mgw.GatewayNameForServer) != tt.gatewaysNum {
				t.Errorf("Incorrect number of gateways. Expected: %v Got: %d", tt.gatewaysNum, len(mgw.GatewayNameForServer))
			}
		})
	}
}

func makeConfig(name, namespace, host, portName, portProtocol string, portNumber uint32, gw string) Config {
	c := Config{
		ConfigMeta: ConfigMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": gw},
			Servers: []*networking.Server{
				{
					Hosts: []string{host},
					Port:  &networking.Port{Name: portName, Number: portNumber, Protocol: portProtocol},
				},
			},
		},
	}
	return c
}
