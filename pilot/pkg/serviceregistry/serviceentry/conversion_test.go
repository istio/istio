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
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/spiffe"
)

var GlobalTime = time.Now()
var httpNone = &model.Config{
	ConfigMeta: model.ConfigMeta{
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

var tcpNone = &model.Config{
	ConfigMeta: model.ConfigMeta{
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

var httpStatic = &model.Config{
	ConfigMeta: model.ConfigMeta{
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
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "3.3.3.3",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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
var httpStaticOverlay = &model.Config{
	ConfigMeta: model.ConfigMeta{
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

var httpDNSnoEndpoints = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpDNSnoEndpoints",
		Namespace:         "httpDNSnoEndpoints",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"google.com", "www.wikipedia.org"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
	},
}

var httpDNS = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpDNS",
		Namespace:         "httpDNS",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "uk.google.com",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "de.google.com",
				Labels:  map[string]string{"foo": "bar", label.TLSMode: model.IstioMutualTLSModeLabel},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
	},
}

var tcpDNS = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "tcpDNS",
		Namespace:         "tcpDNS",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"tcpdns.com"},
		Ports: []*networking.Port{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{
				Address: "lon.google.com",
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "in.google.com",
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
	},
}

var tcpStatic = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "tcpStatic",
		Namespace:         "tcpStatic",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
			},
			{
				Address: "2.2.2.2",
				Labels:  map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
			},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
	},
}

var httpNoneInternal = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "httpNoneInternal",
		Namespace:         "httpNoneInternal",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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

var tcpNoneInternal = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "tcpNoneInternal",
		Namespace:         "tcpNoneInternal",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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

var multiAddrInternal = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "multiAddrInternal",
		Namespace:         "multiAddrInternal",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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

var udsLocal = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "udsLocal",
		Namespace:         "udsLocal",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"uds.cluster.local"},
		Ports: []*networking.Port{
			{Number: 6553, Name: "grpc-1", Protocol: "grpc"},
		},
		Endpoints: []*networking.WorkloadEntry{
			{Address: "unix:///test/sock", Labels: map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel}},
		},
		Resolution: networking.ServiceEntry_STATIC,
	},
}

// ServiceEntry with a selector
var selector = &model.Config{
	ConfigMeta: model.ConfigMeta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "selector",
		Namespace:         "selector",
		CreationTimestamp: GlobalTime,
		Labels:            map[string]string{label.TLSMode: model.IstioMutualTLSModeLabel},
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

func createWorkloadEntry(name, namespace string, spec *networking.WorkloadEntry) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
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
	external bool, resolution model.Resolution) *model.Service {

	svc := &model.Service{
		CreationTime: GlobalTime,
		Hostname:     hostname,
		Address:      address,
		MeshExternal: external,
		Resolution:   resolution,
		Attributes: model.ServiceAttributes{
			ServiceRegistry: serviceregistry.External,
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
func makeInstanceWithServiceAccount(cfg *model.Config, address string, port int,
	svcPort *networking.Port, svcLabels map[string]string, serviceAccount string) *model.ServiceInstance {
	i := makeInstance(cfg, address, port, svcPort, svcLabels, MTLSUnlabelled)
	i.Endpoint.ServiceAccount = spiffe.MustGenSpiffeURI(i.Service.Attributes.Namespace, serviceAccount)
	return i
}

// nolint: unparam
func makeInstance(cfg *model.Config, address string, port int,
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
		svcLabels[label.TLSMode] = model.IstioMutualTLSModeLabel
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
		externalSvc *model.Config
		services    []*model.Service
	}{
		{
			// service entry http
			externalSvc: httpNone,
			services: []*model.Service{makeService("*.google.com", "httpNone", constants.UnspecifiedIP,
				map[string]int{"http-number": 80, "http2-number": 8080}, true, model.Passthrough),
			},
		},
		{
			// service entry tcp
			externalSvc: tcpNone,
			services: []*model.Service{makeService("tcpnone.com", "tcpNone", "172.217.0.0/16",
				map[string]int{"tcp-444": 444}, true, model.Passthrough),
			},
		},
		{
			// service entry http  static
			externalSvc: httpStatic,
			services: []*model.Service{makeService("*.google.com", "httpStatic", constants.UnspecifiedIP,
				map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.ClientSideLB),
			},
		},
		{
			// service entry DNS with no endpoints
			externalSvc: httpDNSnoEndpoints,
			services: []*model.Service{
				makeService("google.com", "httpDNSnoEndpoints", constants.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
				makeService("www.wikipedia.org", "httpDNSnoEndpoints", constants.UnspecifiedIP,
					map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
			},
		},
		{
			// service entry dns
			externalSvc: httpDNS,
			services: []*model.Service{makeService("*.google.com", "httpDNS", constants.UnspecifiedIP,
				map[string]int{"http-port": 80, "http-alt-port": 8080}, true, model.DNSLB),
			},
		},
		{
			// service entry tcp DNS
			externalSvc: tcpDNS,
			services: []*model.Service{makeService("tcpdns.com", "tcpDNS", constants.UnspecifiedIP,
				map[string]int{"tcp-444": 444}, true, model.DNSLB),
			},
		},
		{
			// service entry tcp static
			externalSvc: tcpStatic,
			services: []*model.Service{makeService("tcpstatic.com", "tcpStatic", "172.217.0.1",
				map[string]int{"tcp-444": 444}, true, model.ClientSideLB),
			},
		},
		{
			// service entry http internal
			externalSvc: httpNoneInternal,
			services: []*model.Service{makeService("*.google.com", "httpNoneInternal", constants.UnspecifiedIP,
				map[string]int{"http-number": 80, "http2-number": 8080}, false, model.Passthrough),
			},
		},
		{
			// service entry tcp internal
			externalSvc: tcpNoneInternal,
			services: []*model.Service{makeService("tcpinternal.com", "tcpNoneInternal", "172.217.0.0/16",
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
		externalSvc *model.Config
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
		externalSvc *model.Config
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
			instances := convertInstances(*tt.externalSvc, nil)
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
		name string
		wle  *networking.WorkloadEntry
		se   *model.Config
		out  []*model.ServiceInstance
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
	}

	for _, tt := range serviceInstanceTests {
		t.Run(tt.name, func(t *testing.T) {
			services := convertServices(*tt.se)
			instances := convertWorkloadEntryToServiceInstances(tt.wle, services, tt.se.Spec.(*networking.ServiceEntry))
			sortServiceInstances(instances)
			sortServiceInstances(tt.out)
			if err := compare(t, instances, tt.out); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConvertWorkloadEntryToWorkloadInstance(t *testing.T) {
	labels := map[string]string{
		"app": "wle",
	}
	workloadInstanceTests := []struct {
		name string
		wle  model.Config
		out  *model.WorkloadInstance
	}{
		{
			name: "simple",
			wle: model.Config{
				ConfigMeta: model.ConfigMeta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address: "1.1.1.1",
					Labels:  labels,
					Ports: map[string]uint32{
						"http": 80,
					},
					ServiceAccount: "scooby",
				}},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Endpoint: &model.IstioEndpoint{
					Labels:         labels,
					Address:        "1.1.1.1",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "istio",
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
		{
			name: "simple - tls mode disabled",
			wle: model.Config{
				ConfigMeta: model.ConfigMeta{
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
				}},
			out: &model.WorkloadInstance{
				Namespace: "ns1",
				Endpoint: &model.IstioEndpoint{
					Labels: map[string]string{
						"security.istio.io/tlsMode": "disabled",
					},
					Address:        "1.1.1.1",
					ServiceAccount: "spiffe://cluster.local/ns/ns1/sa/scooby",
					TLSMode:        "disabled",
				},
				PortMap: map[string]uint32{
					"http": 80,
				},
			},
		},
		{
			name: "unix domain socket",
			wle: model.Config{
				ConfigMeta: model.ConfigMeta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address:        "unix://foo/bar",
					ServiceAccount: "scooby",
				}},
			out: nil,
		},
		{
			name: "DNS address",
			wle: model.Config{
				ConfigMeta: model.ConfigMeta{
					Namespace: "ns1",
				},
				Spec: &networking.WorkloadEntry{
					Address:        "scooby.com",
					ServiceAccount: "scooby",
				}},
			out: nil,
		},
	}

	for _, tt := range workloadInstanceTests {
		t.Run(tt.name, func(t *testing.T) {
			instance := convertWorkloadEntryToWorkloadInstance(tt.wle)
			if err := compare(t, instance, tt.out); err != nil {
				t.Fatal(err)
			}
		})
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
