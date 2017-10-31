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

package eureka

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"istio.io/pilot/model"
	"istio.io/pilot/test/util"
)

func TestConvertService(t *testing.T) {
	serviceTests := []struct {
		apps     []*application
		services map[string]*model.Service
	}{
		{
			// single instance with multiple ports
			apps: []*application{
				{
					Name: "foo_bar_local",
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.1", 5000, 5443, nil),
					},
				},
			},
			services: map[string]*model.Service{
				"foo.bar.local": makeService("foo.bar.local", []int{5000, 5443}, nil),
			},
		},
		{
			// multi-instance with different IPs
			apps: []*application{
				{
					Name: "foo_bar_local",
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.1", 5000, -1, nil),
						makeInstance("foo.bar.local", "10.0.0.2", 5000, -1, nil),
					},
				},
			},
			services: map[string]*model.Service{
				"foo.bar.local": makeService("foo.bar.local", []int{5000}, nil),
			},
		},
		{
			// multi-instance with different IPs, ports
			apps: []*application{
				{
					Name: "foo_bar_local",
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.1", 5000, -1, nil),
						makeInstance("foo.bar.local", "10.0.0.1", 6000, -1, nil),
					},
				},
			},
			services: map[string]*model.Service{
				"foo.bar.local": makeService("foo.bar.local", []int{5000, 6000}, nil),
			},
		},
		{
			// multi-application with the same hostname
			apps: []*application{
				{
					Name: "foo_bar_local",
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.1", 5000, -1, nil),
					},
				},
				{
					Name: "foo_bar_local2",
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.2", 5000, -1, nil),
					},
				},
			},
			services: map[string]*model.Service{
				"foo.bar.local": makeService("foo.bar.local", []int{5000}, nil),
			},
		},
		{
			// multi-application with different hostnames
			apps: []*application{
				{
					Name: "foo_bar_local",
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.1", 5000, -1, nil),
					},
				},
				{
					Name: "foo_biz_local",
					Instances: []*instance{
						makeInstance("foo.biz.local", "10.0.0.2", 5000, -1, nil),
					},
				},
			},
			services: map[string]*model.Service{
				"foo.bar.local": makeService("foo.bar.local", []int{5000}, nil),
				"foo.biz.local": makeService("foo.biz.local", []int{5000}, nil),
			},
		},
	}

	for _, tt := range serviceTests {
		services := convertServices(tt.apps, nil)
		if err := compare(t, services, tt.services); err != nil {
			t.Error(err)
		}
	}

	hostnameTests := []map[string]bool{
		{"foo.bar.local": true},
		{"foo.biz.local": true},
		{"foo.bar.local": true, "foo.biz.local": true},
	}
	for _, tt := range serviceTests {
		for _, hostnames := range hostnameTests {
			services := convertServices(tt.apps, hostnames)
			for _, service := range services {
				if !hostnames[service.Hostname] {
					t.Errorf("convert services did not filter hostname %q", service.Hostname)
				}
			}
		}
	}
}

func TestConvertServiceInstances(t *testing.T) {
	foobarService := makeService("foo.bar.local", []int{5000, 5443}, nil)

	serviceInstanceTests := []struct {
		services map[string]*model.Service
		apps     []*application
		out      []*model.ServiceInstance
	}{
		{
			services: map[string]*model.Service{
				"foo.bar.local": foobarService,
			},
			apps: []*application{
				{
					Name: "foo_bar_local",
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.1", 5000, 5443, nil),
						makeInstance("foo.bar.local", "10.0.0.2", 5000, -1, nil),
					},
				},
			},
			out: []*model.ServiceInstance{
				makeServiceInstance(foobarService, "10.0.0.1", 5000, nil),
				makeServiceInstance(foobarService, "10.0.0.1", 5443, nil),
				makeServiceInstance(foobarService, "10.0.0.2", 5000, nil),
			},
		},
	}

	for _, tt := range serviceInstanceTests {
		instances := convertServiceInstances(tt.services, tt.apps)
		if err := compare(t, instances, tt.out); err != nil {
			t.Error(err)
		}
	}
}

func TestConvertProtocol(t *testing.T) {
	makeMetadata := func(protocol string) metadata {
		return metadata{
			protocolMetadata: protocol,
			"kit":            "kat",
		}
	}

	protocolTests := []struct {
		in  metadata
		out model.Protocol
	}{
		{in: nil, out: model.ProtocolTCP},
		{in: makeMetadata(""), out: model.ProtocolTCP},
		{in: makeMetadata("HTCPCP"), out: model.ProtocolTCP},
		{in: makeMetadata(metadataUDP), out: model.ProtocolUDP},
		{in: makeMetadata(metadataHTTP), out: model.ProtocolHTTP},
		{in: makeMetadata(metadataHTTP2), out: model.ProtocolHTTP2},
		{in: makeMetadata(metadataHTTPS), out: model.ProtocolHTTPS},
		{in: makeMetadata(metadataGRPC), out: model.ProtocolGRPC},
		{in: makeMetadata(metadataMongo), out: model.ProtocolMongo},
		{in: makeMetadata(metadataRedis), out: model.ProtocolRedis},
	}

	for _, tt := range protocolTests {
		if protocol := convertProtocol(tt.in); protocol != tt.out {
			t.Errorf("convertProtocol(%q) => %q, want %q", tt.in, protocol, tt.out)
		}
	}
}

func TestConvertLabels(t *testing.T) {
	md := metadata{
		"@class":         "java.util.Collections$EmptyMap",
		protocolMetadata: metadataHTTP2,
		"kit":            "kat",
		"spam":           "coolaid",
	}
	labels := convertLabels(md)

	for _, special := range []string{protocolMetadata, "@class"} {
		if _, exists := labels[special]; exists {
			t.Errorf("convertLabels did not filter out special tag %q", special)
		}
	}

	for _, tag := range []string{"kit", "spam"} {
		_, exists := labels[tag]
		if !exists {
			t.Errorf("converted labels has missing key %q", tag)
		} else if labels[tag] != md[tag] {
			t.Errorf("converted labels has mismatch for key %q, %q want %q", tag, labels[tag], md[tag])
		}
	}

	if len(labels) != 2 {
		t.Errorf("converted labels has length %d, want %d", len(labels), 2)
	}
}

// appName returns a debug app name for testing, given a hostname. There is no requirement that the Eureka app name be
// based off of a hostname.
func appName(hostname string) string {
	return strings.ToUpper(strings.Replace(hostname, ".", "_", -1))
}

func makeInstance(hostname, ip string, portNum, securePort int, md metadata) *instance {
	inst := &instance{
		Hostname:  hostname,
		IPAddress: ip,
		Status:    statusUp,
		Port: port{
			Port:    7002,
			Enabled: false,
		},
		SecurePort: port{
			Port:    7002,
			Enabled: false,
		},
		Metadata: md,
	}
	if portNum > 0 {
		inst.Port.Port = portNum
		inst.Port.Enabled = true
	}
	if securePort > 0 {
		inst.SecurePort.Port = securePort
		inst.SecurePort.Enabled = true
	}
	return inst
}

func makeService(hostname string, ports []int, protocols []model.Protocol) *model.Service {
	portList := make(model.PortList, 0, len(ports))
	for i, port := range ports {
		protocol := model.ProtocolTCP
		if i < len(protocols) {
			protocol = protocols[i]
		}

		portList = append(portList, &model.Port{
			Name:     fmt.Sprint(port),
			Port:     port,
			Protocol: protocol,
		})
	}

	return &model.Service{
		Hostname: hostname,
		Ports:    portList,
	}
}

func makeServiceInstance(service *model.Service, ip string, port int, labels model.Labels) *model.ServiceInstance {
	servicePort, _ := service.Ports.GetByPort(port)
	return &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address:     ip,
			Port:        port,
			ServicePort: servicePort,
		},
		Service:          service,
		Labels:           labels,
		AvailabilityZone: "",
	}
}

func compare(t *testing.T, actual, expected interface{}) error {
	return util.Compare(jsonBytes(t, actual), jsonBytes(t, expected))
}

func jsonBytes(t *testing.T, v interface{}) []byte {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(t)
	}
	return data
}
