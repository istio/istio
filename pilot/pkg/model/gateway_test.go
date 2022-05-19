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
	"fmt"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
)

// nolint lll
func TestMergeGateways(t *testing.T) {
	gwHTTPFoo := makeConfig("foo1", "not-default", "foo.bar.com", "name1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE)
	gwHTTPbar := makeConfig("bar1", "not-default", "bar.foo.com", "bname1", "http", 7, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE)
	gwHTTPlocalbar := makeConfig("lcoalbar1", "not-default", "localbar.foo.com", "bname1", "http", 7, "ingressgateway", "127.0.0.1", networking.ServerTLSSettings_SIMPLE)
	gwHTTP2Wildcard := makeConfig("foo5", "not-default", "*", "name5", "http2", 8, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE)
	gwHTTPWildcard := makeConfig("foo3", "not-default", "*", "name3", "http", 8, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE)
	gwTCPWildcard := makeConfig("foo4", "not-default-2", "*", "name4", "tcp", 8, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE)

	gwHTTPWildcardAlternate := makeConfig("foo2", "not-default", "*", "name2", "http", 7, "ingressgateway2", "", networking.ServerTLSSettings_SIMPLE)

	gwSimple := makeConfig("foo-simple", "not-default-2", "*.example.com", "https", "HTTPS", 443, "ingressgateway", "", networking.ServerTLSSettings_SIMPLE)
	gwPassthrough := makeConfig("foo-passthrough", "not-default-2", "foo.example.com", "tls-foo", "TLS", 443, "ingressgateway", "", networking.ServerTLSSettings_PASSTHROUGH)

	// TODO(ramaraochavali): Add more test cases here.
	tests := []struct {
		name               string
		gwConfig           []config.Config
		mergedServersNum   int
		serverNum          int
		serversForRouteNum map[string]int
		gatewaysNum        int
	}{
		{
			"single-server-config",
			[]config.Config{gwHTTPFoo},
			1,
			1,
			map[string]int{"http.7": 1},
			1,
		},
		{
			"two servers on the same port",
			[]config.Config{gwHTTPFoo, gwHTTPbar},
			1,
			2,
			map[string]int{"http.7": 2},
			2,
		},
		{
			"two servers on the same port with different bind",
			[]config.Config{gwHTTPbar, gwHTTPlocalbar},
			2,
			2,
			map[string]int{"http.7": 1, "http.7.127.0.0.1": 1},
			2,
		},
		{
			"same-server-config",
			[]config.Config{gwHTTPFoo, gwHTTPWildcardAlternate},
			1,
			2,
			map[string]int{"http.7": 2},
			2,
		},
		{
			"multi-server-config",
			[]config.Config{gwHTTPFoo, gwHTTPWildcardAlternate, gwHTTPWildcard},
			2,
			3,
			map[string]int{"http.7": 2, "http.8": 1},
			3,
		},
		{
			"http-tcp-wildcard-server-config",
			[]config.Config{gwHTTPFoo, gwTCPWildcard},
			2,
			2,
			map[string]int{"http.7": 1},
			2,
		},
		{
			"tcp-http-server-config",
			[]config.Config{gwTCPWildcard, gwHTTPWildcard},
			1,
			1,
			map[string]int{},
			2,
		},
		{
			"tcp-tcp-server-config",
			[]config.Config{gwHTTPWildcard, gwTCPWildcard}, // order matters
			1,
			1,
			map[string]int{"http.8": 1},
			2,
		},
		{
			"http-http2-server-config",
			[]config.Config{gwHTTPWildcard, gwHTTP2Wildcard},
			1,
			1,
			// http and http2 both present
			map[string]int{"http.8": 1},
			2,
		},
		{
			"simple-passthrough",
			[]config.Config{gwSimple, gwPassthrough},
			2,
			2,
			map[string]int{"https.443.https.foo-simple.not-default-2": 1},
			2,
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			instances := []gatewayWithInstances{}
			for _, c := range tt.gwConfig {
				instances = append(instances, gatewayWithInstances{c, true, nil})
			}
			mgw := MergeGateways(instances, &Proxy{}, nil)
			if len(mgw.MergedServers) != tt.mergedServersNum {
				t.Errorf("Incorrect number of merged servers. Expected: %v Got: %d", tt.mergedServersNum, len(mgw.MergedServers))
			}
			if len(mgw.ServersByRouteName) != len(tt.serversForRouteNum) {
				t.Errorf("Incorrect number of routes. Expected: %v Got: %d", len(tt.serversForRouteNum), len(mgw.ServersByRouteName))
			}
			for k, v := range mgw.ServersByRouteName {
				if tt.serversForRouteNum[k] != len(v) {
					t.Errorf("for route %v expected %v servers got %v", k, tt.serversForRouteNum[k], len(v))
				}
			}
			ns := 0
			for _, ms := range mgw.MergedServers {
				ns += len(ms.Servers)
			}
			if ns != tt.serverNum {
				t.Errorf("Incorrect number of total servers. Expected: %v Got: %d", tt.serverNum, ns)
			}
			if len(mgw.GatewayNameForServer) != tt.gatewaysNum {
				t.Errorf("Incorrect number of gateways. Expected: %v Got: %d", tt.gatewaysNum, len(mgw.GatewayNameForServer))
			}
		})
	}
}

func makeConfig(name, namespace, host, portName, portProtocol string, portNumber uint32, gw string, bind string,
	mode networking.ServerTLSSettings_TLSmode,
) config.Config {
	c := config.Config{
		Meta: config.Meta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &networking.Gateway{
			Selector: map[string]string{"istio": gw},
			Servers: []*networking.Server{
				{
					Hosts: []string{host},
					Port:  &networking.Port{Name: portName, Number: portNumber, Protocol: portProtocol},
					Bind:  bind,
					Tls:   &networking.ServerTLSSettings{Mode: mode},
				},
			},
		},
	}
	return c
}

func TestParseGatewayRDSRouteName(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name           string
		args           args
		wantPortNumber int
		wantPortName   string
		wantGateway    string
	}{
		{
			name:           "invalid rds name",
			args:           args{"https.scooby.dooby.doo"},
			wantPortNumber: 0,
			wantPortName:   "",
			wantGateway:    "",
		},
		{
			name:           "gateway http rds name",
			args:           args{"http.80"},
			wantPortNumber: 80,
			wantPortName:   "",
			wantGateway:    "",
		},
		{
			name:           "https rds name",
			args:           args{"https.443.app1.gw1.ns1"},
			wantPortNumber: 443,
			wantPortName:   "app1",
			wantGateway:    "ns1/gw1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPortNumber, gotPortName, gotGateway := ParseGatewayRDSRouteName(tt.args.name)
			if gotPortNumber != tt.wantPortNumber {
				t.Errorf("ParseGatewayRDSRouteName() gotPortNumber = %v, want %v", gotPortNumber, tt.wantPortNumber)
			}
			if gotPortName != tt.wantPortName {
				t.Errorf("ParseGatewayRDSRouteName() gotPortName = %v, want %v", gotPortName, tt.wantPortName)
			}
			if gotGateway != tt.wantGateway {
				t.Errorf("ParseGatewayRDSRouteName() gotGateway = %v, want %v", gotGateway, tt.wantGateway)
			}
		})
	}
}
