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

package consul

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul/api"

	"istio.io/pilot/model"
)

var (
	protocols = []struct {
		name string
		port int
		out  model.Protocol
	}{
		{"tcp", 80, model.ProtocolTCP},
		{"http", 81, model.ProtocolHTTP},
		{"https", 443, model.ProtocolHTTPS},
		{"http2", 83, model.ProtocolHTTP2},
		{"grpc", 84, model.ProtocolGRPC},
		{"udp", 85, model.ProtocolUDP},
		{"", 86, model.ProtocolHTTP},
	}

	goodLabels = []string{
		"key1|val1",
		"version|v1",
	}

	badLabels = []string{
		"badtag",
		"goodtag|goodvalue",
	}
)

func TestConvertProtocol(t *testing.T) {
	for _, tt := range protocols {
		out := convertPort(tt.port, tt.name)
		if out.Protocol != tt.out {
			t.Errorf("convertProtocol(%v, %q) => %q, want %q", tt.port, tt.name, out, tt.out)
		}
	}
}

func TestConvertLabels(t *testing.T) {
	out := convertLabels(goodLabels)
	if len(out) != len(goodLabels) {
		t.Errorf("convertLabels(%q) => length %v, want %v", goodLabels, len(out), len(goodLabels))
	}

	out = convertLabels(badLabels)
	if len(out) == len(badLabels) {
		t.Errorf("convertLabels(%q) => length %v, want %v", badLabels, len(out), len(badLabels)-1)
	}
}

func TestConvertInstance(t *testing.T) {
	ip := "172.19.0.11"
	port := 9080
	protocol := "udp"
	name := "productpage"
	tagKey1 := "version"
	tagVal1 := "v1"
	tagKey2 := "zone"
	tagVal2 := "prod"
	consulServiceInst := api.CatalogService{
		Node:        "istio-node",
		Address:     "172.19.0.5",
		ServiceID:   "1111-22-3333-444",
		ServiceName: name,
		ServiceTags: []string{
			fmt.Sprintf("%v|%v", tagKey1, tagVal1),
			fmt.Sprintf("%v|%v", tagKey2, tagVal2),
		},
		ServiceAddress: ip,
		ServicePort:    port,
		NodeMeta:       map[string]string{protocolTagName: protocol},
	}

	out := convertInstance(&consulServiceInst)

	if out.Endpoint.ServicePort.Protocol != model.ProtocolUDP {
		t.Errorf("convertInstance() => %v, want %v", out.Endpoint.ServicePort.Protocol, model.ProtocolUDP)
	}

	if out.Endpoint.ServicePort.Name != protocol {
		t.Errorf("convertInstance() => %v, want %v", out.Endpoint.ServicePort.Name, protocol)
	}

	if out.Endpoint.ServicePort.Port != port {
		t.Errorf("convertInstance() => %v, want %v", out.Endpoint.ServicePort.Port, port)
	}

	if out.Endpoint.Address != ip {
		t.Errorf("convertInstance() => %v, want %v", out.Endpoint.Address, ip)
	}

	if len(out.Labels) != 2 {
		t.Errorf("convertInstance() len(Labels) => %v, want %v", len(out.Labels), 2)
	}

	if out.Labels[tagKey1] != tagVal1 || out.Labels[tagKey2] != tagVal2 {
		t.Errorf("convertInstance() => missing or incorrect tag in %q", out.Labels)
	}

	if out.Service.Hostname != serviceHostname(name) {
		t.Errorf("convertInstance() bad service hostname => %q, want %q",
			out.Service.Hostname, serviceHostname(name))
	}

	if out.Service.Address != ip {
		t.Errorf("convertInstance() bad service address => %q, want %q", out.Service.Address, ip)
	}

	if len(out.Service.Ports) != 1 {
		t.Errorf("convertInstance() incorrect # of service ports => %q, want %q", len(out.Service.Ports), 1)
	}

	if out.Service.Ports[0].Port != port || out.Service.Ports[0].Name != protocol {
		t.Errorf("convertInstance() incorrect service port => %q", out.Service.Ports[0])
	}

	if out.Service.External() {
		t.Error("convertInstance() should not be external service")
	}
}

func TestServiceHostname(t *testing.T) {
	out := serviceHostname("productpage")

	if out != "productpage.service.consul" {
		t.Errorf("serviceHostname() => %q, want %q", out, "productpage.service.consul")
	}
}

func TestConvertService(t *testing.T) {
	name := "productpage"
	consulServiceInsts := []*api.CatalogService{
		{
			Node:        "istio-node",
			Address:     "172.19.0.5",
			ServiceID:   "1111-22-3333-444",
			ServiceName: name,
			ServiceTags: []string{
				"version=v1",
				"zone=prod",
			},
			ServiceAddress: "172.19.0.11",
			ServicePort:    9080,
			NodeMeta:       map[string]string{protocolTagName: "udp"},
		},
		{
			Node:        "istio-node",
			Address:     "172.19.0.5",
			ServiceID:   "1111-22-3333-444",
			ServiceName: name,
			ServiceTags: []string{
				"version=v2",
			},
			ServiceAddress: "172.19.0.12",
			ServicePort:    9080,
			NodeMeta:       map[string]string{protocolTagName: "udp"},
		},
	}

	out := convertService(consulServiceInsts)

	if out.Hostname != serviceHostname(name) {
		t.Errorf("convertService() bad hostname => %q, want %q",
			out.Hostname, serviceHostname(name))
	}

	if out.External() {
		t.Error("convertService() should not be an external service")
	}

	if len(out.Ports) != 1 {
		t.Errorf("convertService() incorrect # of ports => %v, want %v",
			len(out.Ports), 1)
	}
}
