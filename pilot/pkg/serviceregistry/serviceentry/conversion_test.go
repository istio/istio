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

package serviceentry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/spiffe"
)

var (
	GlobalTime = time.Now()
	httpNone   = &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  gvk.ServiceEntry,
			Name:              "httpNone",
			Namespace:         "httpNone",
			Domain:            "svc.cluster.local",
			CreationTimestamp: GlobalTime,
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-number", Protocol: "http"},
				{Number: 8080, Name: "http2-number", Protocol: "http2"},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_NONE,
		},
	}
)

var tcpNone = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "tcpNone",
		Namespace:         "tcpNone",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts:     []string{"tcpnone.com"},
		Addresses: []string{"172.217.0.0/16"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_NONE,
	},
}

var httpStatic = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpStatic",
		Namespace:         "httpStatic",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"*.google.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "2.2.2.2",
				Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "3.3.3.3",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "4.4.4.4",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{"foo": "bar"},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
	},
}

// Shares the same host as httpStatic, but adds some endpoints. We expect these to be merge
var httpStaticOverlay = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpStaticOverlay",
		Namespace:         "httpStatic",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"*.google.com"},
		Ports: []*networking.Port{
			{Number: 4567, Name: "http-port", Protocol: "http"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "5.5.5.5",
				Labels:  map[string]string{"overlay": "bar"},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
	},
}

var httpDNSnoEndpoints = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpDNSnoEndpoints",
		Namespace:         "httpDNSnoEndpoints",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"google.com", "www.wikipedia.org"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Location:        networking.ServiceEntry_MESH_EXTERNAL,
		Resolution:      networking.ServiceEntry_DNS,
		SubjectAltNames: []string{"google.com"},
	},
}

var httpDNSRRnoEndpoints = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpDNSRRnoEndpoints",
		Namespace:         "httpDNSRRnoEndpoints",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"api.istio.io"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Location:        networking.ServiceEntry_MESH_EXTERNAL,
		Resolution:      networking.ServiceEntry_DNS_ROUND_ROBIN,
		SubjectAltNames: []string{"api.istio.io"},
	},
}

var dnsTargetPort = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "dnsTargetPort",
		Namespace:         "dnsTargetPort",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"google.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http", TargetPort: 8080},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
	},
}

var httpDNS = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpDNS",
		Namespace:         "httpDNS",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"*.google.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "us.google.com",
				Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "uk.google.com",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "de.google.com",
				Labels:  map[string]string{"foo": "bar", label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
	},
}

var httpDNSRR = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpDNSRR",
		Namespace:         "httpDNSRR",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"*.istio.io"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "api-v1.istio.io",
				Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "api-v2.istio.io",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "api-v3.istio.io",
				Labels:  map[string]string{"foo": "bar", label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS_ROUND_ROBIN,
	},
}

var tcpDNS = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "tcpDNS",
		Namespace:         "tcpDNS",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"tcpdns.com"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "lon.google.com",
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "in.google.com",
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
	},
}

var tcpStatic = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "tcpStatic",
		Namespace:         "tcpStatic",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts:     []string{"tcpstatic.com"},
		Addresses: []string{"172.217.0.1"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "1.1.1.1",
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "2.2.2.2",
				Labels:  map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
	},
}

var httpNoneInternal = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpNoneInternal",
		Namespace:         "httpNoneInternal",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"*.google.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-number", Protocol: "http"},
			{Number: 8080, Name: "http2-number", Protocol: "http2"},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_NONE,
	},
}

var tcpNoneInternal = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "tcpNoneInternal",
		Namespace:         "tcpNoneInternal",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts:     []string{"tcpinternal.com"},
		Addresses: []string{"172.217.0.0/16"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_NONE,
	},
}

var multiAddrInternal = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "multiAddrInternal",
		Namespace:         "multiAddrInternal",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts:     []string{"tcp1.com", "tcp2.com"},
		Addresses: []string{"1.1.1.0/16", "2.2.2.0/16"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_NONE,
	},
}

var udsLocal = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "udsLocal",
		Namespace:         "udsLocal",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"uds.cluster.local"},
		Ports: []*networking.Port{
			{Number: 6553, Name: "grpc-1", Protocol: "grpc"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{Address: "unix:///test/sock", Labels: map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel}},
		},
		Resolution: networking.ServiceEntry_STATIC,
	},
}

// ServiceEntry DNS with a selector
var selectorDNS = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "selector",
		Namespace:         "selector",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"selector.com"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
			{Number: 445, Name: "http-445", Protocol: "http"},
		},
		WorkloadSelector: &networking.WorkloadSelector{
			Labels: map[string]string{"app": "wle"},
		},
		Resolution: networking.ServiceEntry_DNS,
	},
}

// ServiceEntry with a selector
var selector = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "selector",
		Namespace:         "selector",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"selector.com"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
			{Number: 445, Name: "http-445", Protocol: "http"},
		},
		WorkloadSelector: &networking.WorkloadSelector{
			Labels: map[string]string{"app": "wle"},
		},
		Resolution: networking.ServiceEntry_STATIC,
	},
}

// DNS ServiceEntry with a selector
var dnsSelector = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "dns-selector",
		Namespace:         "dns-selector",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel},
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"dns.selector.com"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
			{Number: 445, Name: "http-445", Protocol: "http"},
		},
		WorkloadSelector: &networking.WorkloadSelector{
			Labels: map[string]string{"app": "dns-wle"},
		},
		Resolution: networking.ServiceEntry_DNS,
	},
}

func createWorkloadEntry(name, namespace string, spec *networking.WorkloadEntry) *config.Config {
	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  gvk.WorkloadEntry,
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: GlobalTime,
		},
		Spec: spec,
	}
}

func convertPortNameToProtocol(name string) protocol.Instance {
	prefix := name
	i := strings.Index(name, "-")
	if i >= 0 {
		prefix = name[:i]
	}
	return protocol.Parse(prefix)
}

func makeService(hostname host.Name, configNamespace, address string, ports map[string]int,
	external bool, resolution model.Resolution, serviceAccounts ...string) *model.Service {
	svc := &model.Service{
		CreationTime:    GlobalTime,
		Hostname:        hostname,
		DefaultAddress:  address,
		MeshExternal:    external,
		Resolution:      resolution,
		ServiceAccounts: serviceAccounts,
		Attributes: model.ServiceAttributes{
			ServiceRegistry: provider.External,
			Name:            string(hostname),
			Namespace:       configNamespace,
		},
	}

	svcPorts := make(model.PortList, 0, len(ports))
	for name, port := range ports {
		svcPort := &model.Port{
			Name:     name,
			Port:     port,
			Protocol: convertPortNameToProtocol(name),
		}
		svcPorts = append(svcPorts, svcPort)
	}

	sortPorts(svcPorts)
	svc.Ports = svcPorts
	return svc
}

// MTLSMode is the expected instance mtls settings. This is for test setup only
type MTLSMode int

const (
	MTLS = iota
	// Like mTLS, but no label is defined on the endpoint
	MTLSUnlabelled
	PlainText
)

// nolint: unparam
func makeInstanceWithServiceAccount(cfg *config.Config, address string, port int,
	svcPort *networking.Port, svcLabels map[string]string, serviceAccount string) *model.ServiceInstance {
	i := makeInstance(cfg, address, port, svcPort, svcLabels, MTLSUnlabelled)
	i.Endpoint.ServiceAccount = spiffe.MustGenSpiffeURI(i.Service.Attributes.Namespace, serviceAccount)
	return i
}

// nolint: unparam
func makeInstance(cfg *config.Config, address string, port int,
	svcPort *networking.Port, svcLabels map[string]string, mtlsMode MTLSMode) *model.ServiceInstance {
	services := convertServices(*cfg)
	svc := services[0] // default
	for _, s := range services {
		if string(s.Hostname) == address {
			svc = s
			break
		}
	}
	tlsMode := model.DisabledTLSModeLabel
	if mtlsMode == MTLS || mtlsMode == MTLSUnlabelled {
		tlsMode = model.IstioMutualTLSModeLabel
	}
	if mtlsMode == MTLS {
		if svcLabels == nil {
			svcLabels = map[string]string{}
		}
		svcLabels[label.SecurityTlsMode.Name] = model.IstioMutualTLSModeLabel
	}
	return &model.ServiceInstance{
		Service: svc,
		Endpoint: &model.IstioEndpoint{
			Address:         address,
			EndpointPort:    uint32(port),
			ServicePortName: svcPort.Name,
			Labels:          svcLabels,
			TLSMode:         tlsMode,
		},
		ServicePort: &model.Port{
			Name:     svcPort.Name,
			Port:     int(svcPort.Number),
			Protocol: protocol.Parse(svcPort.Protocol),
		},
	}
}

func TestConvertService(t *testing.T) {
	serviceTests := []struct {
		externalSvc *config.Config
		services    []*model.Service
	}{
		{
			// service entry http
			externalSvc: httpNone,
			services: []*model.Service{
				makeService("*.google.com", "httpNone", constants.UnspecifiedIP,
					map[string]int{"http-number": 80, "http2-number": 8080}, true, model.Passthrough),
			},
		},
		{
			// service entry tcp
			externalSvc: tcpNone,
			services: []*model.Service{
				makeService("tcpnone.com", "tcpNone", "172.217.0.0/16",
					map[string]int{"tcp-444": 444}, true, model.Passthrough),
			},
		},
		{
			// service entry http  static
			externalSvc: httpStatic,
			services: []*model.Service{
				makeService("*.google.com", "httpStatic", constants.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.ClientSideLB),
			},
		},
		{
			// service entry DNS with no endpoints
			externalSvc: httpDNSnoEndpoints,
			services: []*model.Service{
				makeService("google.com", "httpDNSnoEndpoints", constants.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, "google.com"),
				makeService("www.wikipedia.org", "httpDNSnoEndpoints", constants.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB, "google.com"),
			},
		},
		{
			// service entry dns
			externalSvc: httpDNS,
			services: []*model.Service{
				makeService("*.google.com", "httpDNS", constants.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
			},
		},
		{
			// service entry dns with target port
			externalSvc: dnsTargetPort,
			services: []*model.Service{
				makeService("google.com", "dnsTargetPort", constants.UnspecifiedIP,
					map[string]int{"http-port": 80}, true, model.DNSLB),
			},
		},
		{
			// service entry tcp DNS
			externalSvc: tcpDNS,
			services: []*model.Service{
				makeService("tcpdns.com", "tcpDNS", constants.UnspecifiedIP,
					map[string]int{"tcp-444": 444}, true, model.DNSLB),
			},
		},
		{
			// service entry tcp static
			externalSvc: tcpStatic,
			services: []*model.Service{
				makeService("tcpstatic.com", "tcpStatic", "172.217.0.1",
					map[string]int{"tcp-444": 444}, true, model.ClientSideLB),
			},
		},
		{
			// service entry http internal
			externalSvc: httpNoneInternal,
			services: []*model.Service{
				makeService("*.google.com", "httpNoneInternal", constants.UnspecifiedIP,
					map[string]int{"http-number": 80, "http2-number": 8080}, false, model.Passthrough),
			},
		},
		{
			// service entry tcp internal
			externalSvc: tcpNoneInternal,
			services: []*model.Service{
				makeService("tcpinternal.com", "tcpNoneInternal", "172.217.0.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough),
			},
		},
		{
			// service entry multiAddrInternal
			externalSvc: multiAddrInternal,
			services: []*model.Service{
				makeService("tcp1.com", "multiAddrInternal", "1.1.1.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough),
				makeService("tcp1.com", "multiAddrInternal", "2.2.2.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough),
				makeService("tcp2.com", "multiAddrInternal", "1.1.1.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough),
				makeService("tcp2.com", "multiAddrInternal", "2.2.2.0/16",
					map[string]int{"tcp-444": 444}, false, model.Passthrough),
			},
		},
	}

	selectorSvc := makeService("selector.com", "selector", "0.0.0.0",
		map[string]int{"tcp-444": 444, "http-445": 445}, true, model.ClientSideLB)
	selectorSvc.Attributes.LabelSelectors = map[string]string{"app": "wle"}

	serviceTests = append(serviceTests, struct {
		externalSvc *config.Config
		services    []*model.Service
	}{
		externalSvc: selector,
		services:    []*model.Service{selectorSvc},
	})

	for _, tt := range serviceTests {
		services := convertServices(*tt.externalSvc)
		if err := compare(t, services, tt.services); err != nil {
			t.Errorf("testcase: %v\n%v ", tt.externalSvc.Name, err)
		}
	}
}

func TestConvertInstances(t *testing.T) {
	serviceInstanceTests := []struct {
		externalSvc *config.Config
		out         []*model.ServiceInstance
	}{
		{
			// single instance with multiple ports
			externalSvc: httpNone,
			// DNS type none means service should not have a registered instance
			out: []*model.ServiceInstance{},
		},
		{
			// service entry tcp
			externalSvc: tcpNone,
			// DNS type none means service should not have a registered instance
			out: []*model.ServiceInstance{},
		},
		{
			// service entry static
			externalSvc: httpStatic,
			out: []*model.ServiceInstance{
				makeInstance(httpStatic, "2.2.2.2", 7080, httpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
				makeInstance(httpStatic, "2.2.2.2", 18080, httpStatic.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
				makeInstance(httpStatic, "3.3.3.3", 1080, httpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
				makeInstance(httpStatic, "3.3.3.3", 8080, httpStatic.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
				makeInstance(httpStatic, "4.4.4.4", 1080, httpStatic.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, PlainText),
				makeInstance(httpStatic, "4.4.4.4", 8080, httpStatic.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"foo": "bar"}, PlainText),
			},
		},
		{
			// service entry DNS with no endpoints
			externalSvc: httpDNSnoEndpoints,
			out: []*model.ServiceInstance{
				makeInstance(httpDNSnoEndpoints, "google.com", 80, httpDNSnoEndpoints.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
				makeInstance(httpDNSnoEndpoints, "google.com", 8080, httpDNSnoEndpoints.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
				makeInstance(httpDNSnoEndpoints, "www.wikipedia.org", 80, httpDNSnoEndpoints.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
				makeInstance(httpDNSnoEndpoints, "www.wikipedia.org", 8080, httpDNSnoEndpoints.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
			},
		},
		{
			// service entry DNS with no endpoints using round robin
			externalSvc: httpDNSRRnoEndpoints,
			out: []*model.ServiceInstance{
				makeInstance(httpDNSRRnoEndpoints, "api.istio.io", 80, httpDNSnoEndpoints.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
				makeInstance(httpDNSRRnoEndpoints, "api.istio.io", 8080, httpDNSnoEndpoints.Spec.(*networking.ServiceEntry).Ports[1], nil, PlainText),
			},
		},
		{
			// service entry DNS with workload selector and no endpoints
			externalSvc: selectorDNS,
			out:         []*model.ServiceInstance{},
		},
		{
			// service entry dns
			externalSvc: httpDNS,
			out: []*model.ServiceInstance{
				makeInstance(httpDNS, "us.google.com", 7080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
				makeInstance(httpDNS, "us.google.com", 18080, httpDNS.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
				makeInstance(httpDNS, "uk.google.com", 1080, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
				makeInstance(httpDNS, "uk.google.com", 8080, httpDNS.Spec.(*networking.ServiceEntry).Ports[1], nil, MTLS),
				makeInstance(httpDNS, "de.google.com", 80, httpDNS.Spec.(*networking.ServiceEntry).Ports[0], map[string]string{"foo": "bar"}, MTLS),
				makeInstance(httpDNS, "de.google.com", 8080, httpDNS.Spec.(*networking.ServiceEntry).Ports[1], map[string]string{"foo": "bar"}, MTLS),
			},
		},
		{
			// service entry dns with target port
			externalSvc: dnsTargetPort,
			out: []*model.ServiceInstance{
				makeInstance(dnsTargetPort, "google.com", 8080, dnsTargetPort.Spec.(*networking.ServiceEntry).Ports[0], nil, PlainText),
			},
		},
		{
			// service entry tcp DNS
			externalSvc: tcpDNS,
			out: []*model.ServiceInstance{
				makeInstance(tcpDNS, "lon.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
				makeInstance(tcpDNS, "in.google.com", 444, tcpDNS.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
			},
		},
		{
			// service entry tcp static
			externalSvc: tcpStatic,
			out: []*model.ServiceInstance{
				makeInstance(tcpStatic, "1.1.1.1", 444, tcpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
				makeInstance(tcpStatic, "2.2.2.2", 444, tcpStatic.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
			},
		},
		{
			// service entry unix domain socket static
			externalSvc: udsLocal,
			out: []*model.ServiceInstance{
				makeInstance(udsLocal, "/test/sock", 0, udsLocal.Spec.(*networking.ServiceEntry).Ports[0], nil, MTLS),
			},
		},
	}

	for _, tt := range serviceInstanceTests {
		t.Run(strings.Join(tt.externalSvc.Spec.(*networking.ServiceEntry).Hosts, "_"), func(t *testing.T) {
			s := &Controller{}
			instances := s.convertServiceEntryToInstances(*tt.externalSvc, nil)
			sortServiceInstances(instances)
			sortServiceInstances(tt.out)
			if err := compare(t, instances, tt.out); err != nil {
				t.Fatalf("testcase: %v\n%v", tt.externalSvc.Name, err)
			}
		})
	}
}

func TestConvertWorkloadEntryToServiceInstances(t *testing.T) {
	labels := map[string]string{
		"app": "wle",
	}
	serviceInstanceTests := []struct {
		name      string
		wle       *networking.WorkloadEntry
		se        *config.Config
		clusterID cluster.ID
		out       []*model.ServiceInstance
	}{
		{
			name: "simple",
			wle: &networking.WorkloadEntry{
				Address: "1.1.1.1",
				Labels:  labels,
			},
			se: selector,
			out: []*model.ServiceInstance{
				makeInstance(selector, "1.1.1.1", 444, selector.Spec.(*networking.ServiceEntry).Ports[0], labels, PlainText),
				makeInstance(selector, "1.1.1.1", 445, selector.Spec.(*networking.ServiceEntry).Ports[1], labels, PlainText),
			},
		},
		{
			name: "mtls",
			wle: &networking.WorkloadEntry{
				Address:        "1.1.1.1",
				Labels:         labels,
				ServiceAccount: "default",
			},
			se: selector,
			out: []*model.ServiceInstance{
				makeInstanceWithServiceAccount(selector, "1.1.1.1", 444, selector.Spec.(*networking.ServiceEntry).Ports[0], labels, "default"),
				makeInstanceWithServiceAccount(selector, "1.1.1.1", 445, selector.Spec.(*networking.ServiceEntry).Ports[1], labels, "default"),
			},
		},
		{
			name: "replace-port",
			wle: &networking.WorkloadEntry{
				Address: "1.1.1.1",
				Labels:  labels,
				Ports: map[string]uint32{
					"http-445": 8080,
				},
			},
			se: selector,
			out: []*model.ServiceInstance{
				makeInstance(selector, "1.1.1.1", 444, selector.Spec.(*networking.ServiceEntry).Ports[0], labels, PlainText),
				makeInstance(selector, "1.1.1.1", 8080, selector.Spec.(*networking.ServiceEntry).Ports[1], labels, PlainText),
			},
		},
		{
			name: "augment label",
			wle: &networking.WorkloadEntry{
				Address:        "1.1.1.1",
				Labels:         labels,
				Locality:       "region1/zone1/sunzone1",
				Network:        "network1",
				ServiceAccount: "default",
			},
			se:        selector,
			clusterID: "fakeCluster",
			out: []*model.ServiceInstance{
				makeInstanceWithServiceAccount(selector, "1.1.1.1", 444, selector.Spec.(*networking.ServiceEntry).Ports[0], labels, "default"),
				makeInstanceWithServiceAccount(selector, "1.1.1.1", 445, selector.Spec.(*networking.ServiceEntry).Ports[1], labels, "default"),
			},
		},
	}

	for _, tt := range serviceInstanceTests {
		t.Run(tt.name, func(t *testing.T) {
			services := convertServices(*tt.se)
			s := &Controller{}
			instances := s.convertWorkloadEntryToServiceInstances(tt.wle, services, tt.se.Spec.(*networking.ServiceEntry), &configKey{}, tt.clusterID)
			sortServiceInstances(instances)
			sortServiceInstances(tt.out)

			if tt.wle.Locality != "" || tt.clusterID != "" || tt.wle.Network != "" {
				for _, serviceInstance := range tt.out {
					serviceInstance.Endpoint.Locality = model.Locality{
						Label:     tt.wle.Locality,
						ClusterID: tt.clusterID,
					}
					serviceInstance.Endpoint.Network = network.ID(tt.wle.Network)
					serviceInstance.Endpoint.Labels = labelutil.AugmentLabels(serviceInstance.Endpoint.Labels,
						tt.clusterID, tt.wle.Locality, network.ID(tt.wle.Network))
				}
			}

			if err := compare(t, instances, tt.out); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConvertWorkloadEntryToWorkloadInstance(t *testing.T) {
	workloadLabel := map[string]string{
		"app": "wle",
	}

	clusterID := "fakeCluster"
	expectedLabel := map[string]string{
		"app":                       "wle",
		"topology.istio.io/cluster": clusterID,
	}

	workloadInstanceTests := []struct {
		name           string
		getNetworkIDCb func(IP string, labels labels.Instance) network.ID
		wle            config.Config
		out            *model.WorkloadInstance
	}{
		{
			name: "simple",
			wle: config.Config{
				Meta: config.Meta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address: "1.1.1.1",
					Labels:  workloadLabel,
					Ports: map[string]uint32{
						"http": 80,
					},
					ServiceAccount: "scooby",
				},
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels:         expectedLabel,
					Address:        "1.1.1.1",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
					Namespace:      "ns1",
					Locality: model.Locality{
						ClusterID: cluster.ID(clusterID),
					},
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
		{
			name: "simple - tls mode disabled",
			wle: config.Config{
				Meta: config.Meta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address: "1.1.1.1",
					Labels: map[string]string{
						"security.istio.io/tlsMode": "disabled",
					},
					Ports: map[string]uint32{
						"http": 80,
					},
					ServiceAccount: "scooby",
				},
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels: map[string]string{
						"security.istio.io/tlsMode": "disabled",
						"topology.istio.io/cluster": clusterID,
					},
					Address:        "1.1.1.1",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "disabled",
					Namespace:      "ns1",
					Locality: model.Locality{
						ClusterID: cluster.ID(clusterID),
					},
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
		{
			name: "unix domain socket",
			wle: config.Config{
				Meta: config.Meta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address:        "unix://foo/bar",
					ServiceAccount: "scooby",
				},
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels: map[string]string{
						"topology.istio.io/cluster": clusterID,
					},
					Address:        "unix://foo/bar",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
					Namespace:      "ns1",
					Locality: model.Locality{
						ClusterID: cluster.ID(clusterID),
					},
				},
				DNSServiceEntryOnly: true,
			},
		},
		{
			name: "DNS address",
			wle: config.Config{
				Meta: config.Meta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address:        "scooby.com",
					ServiceAccount: "scooby",
				},
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels: map[string]string{
						"topology.istio.io/cluster": clusterID,
					},
					Address:        "scooby.com",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
					Namespace:      "ns1",
					Locality: model.Locality{
						ClusterID: cluster.ID(clusterID),
					},
				},
				DNSServiceEntryOnly: true,
			},
		},
		{
			name: "metadata labels only",
			wle: config.Config{
				Meta: config.Meta{
					Labels:    workloadLabel,
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address: "1.1.1.1",
					Ports: map[string]uint32{
						"http": 80,
					},
					ServiceAccount: "scooby",
				},
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels:         expectedLabel,
					Address:        "1.1.1.1",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
					Namespace:      "ns1",
					Locality: model.Locality{
						ClusterID: cluster.ID(clusterID),
					},
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
		{
			name: "labels merge",
			wle: config.Config{
				Meta: config.Meta{
					Namespace: "ns1",
					Labels: map[string]string{
						"my-label": "bar",
					},
				},
				Spec: &networking.WorkloadEntry{
					Address: "1.1.1.1",
					Labels:  workloadLabel,
					Ports: map[string]uint32{
						"http": 80,
					},
					ServiceAccount: "scooby",
				},
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels: map[string]string{
						"my-label":                  "bar",
						"app":                       "wle",
						"topology.istio.io/cluster": clusterID,
					},
					Address:        "1.1.1.1",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
					Namespace:      "ns1",
					Locality: model.Locality{
						ClusterID: cluster.ID(clusterID),
					},
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
		{
			name: "augment labels",
			wle: config.Config{
				Meta: config.Meta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address: "1.1.1.1",
					Labels:  workloadLabel,
					Ports: map[string]uint32{
						"http": 80,
					},
					Locality:       "region1/zone1/subzone1",
					Network:        "network1",
					ServiceAccount: "scooby",
				},
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels: map[string]string{
						"app":                           "wle",
						"topology.kubernetes.io/region": "region1",
						"topology.kubernetes.io/zone":   "zone1",
						"topology.istio.io/subzone":     "subzone1",
						"topology.istio.io/network":     "network1",
						"topology.istio.io/cluster":     clusterID,
					},
					Address: "1.1.1.1",
					Network: "network1",
					Locality: model.Locality{
						Label:     "region1/zone1/subzone1",
						ClusterID: cluster.ID(clusterID),
					},
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
					Namespace:      "ns1",
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
		{
			name: "augment labels: networkID get from cb",
			wle: config.Config{
				Meta: config.Meta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address: "1.1.1.1",
					Labels:  workloadLabel,
					Ports: map[string]uint32{
						"http": 80,
					},
					Locality:       "region1/zone1/subzone1",
					ServiceAccount: "scooby",
				},
			},
			getNetworkIDCb: func(IP string, labels labels.Instance) network.ID {
				return "cb-network1"
			},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Kind:      model.WorkloadEntryKind,
				Endpoint: &model.IstioEndpoint{
					Labels: map[string]string{
						"app":                           "wle",
						"topology.kubernetes.io/region": "region1",
						"topology.kubernetes.io/zone":   "zone1",
						"topology.istio.io/subzone":     "subzone1",
						"topology.istio.io/network":     "cb-network1",
						"topology.istio.io/cluster":     clusterID,
					},
					Address: "1.1.1.1",
					Locality: model.Locality{
						Label:     "region1/zone1/subzone1",
						ClusterID: cluster.ID(clusterID),
					},
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
					Namespace:      "ns1",
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
	}

	for _, tt := range workloadInstanceTests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Controller{networkIDCallback: tt.getNetworkIDCb}
			instance := s.convertWorkloadEntryToWorkloadInstance(tt.wle, cluster.ID(clusterID))
			if err := compare(t, instance, tt.out); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func compare(t testing.TB, actual, expected interface{}) error {
	return util.Compare(jsonBytes(t, actual), jsonBytes(t, expected))
}

func jsonBytes(t testing.TB, v interface{}) []byte {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(t)
	}
	return data
}
