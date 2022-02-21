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

package server_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	dnsProto "istio.io/istio/pkg/dns/proto"
	dnsServer "istio.io/istio/pkg/dns/server"
)

// nolint
func makeServiceInstances(proxy *model.Proxy, service *model.Service, hostname, subdomain string) map[int][]*model.ServiceInstance {
	instances := make(map[int][]*model.ServiceInstance)
	for _, port := range service.Ports {
		instances[port.Port] = makeInstances(proxy, service, port.Port, port.Port)
		instances[port.Port][0].Endpoint.HostName = hostname
		instances[port.Port][0].Endpoint.SubDomain = subdomain
		instances[port.Port][0].Endpoint.Network = proxy.Metadata.Network
		instances[port.Port][0].Endpoint.Locality.ClusterID = proxy.Metadata.ClusterID
	}
	return instances
}

func TestNameTable(t *testing.T) {
	mesh := &meshconfig.MeshConfig{RootNamespace: "istio-system"}
	proxy := &model.Proxy{
		IPAddresses: []string{"9.9.9.9"},
		Metadata:    &model.NodeMetadata{},
		Type:        model.SidecarProxy,
		DNSDomain:   "testns.svc.cluster.local",
	}
	nw1proxy := &model.Proxy{
		IPAddresses: []string{"9.9.9.9"},
		Metadata:    &model.NodeMetadata{Network: "nw1"},
		Type:        model.SidecarProxy,
		DNSDomain:   "testns.svc.cluster.local",
	}
	cl1proxy := &model.Proxy{
		IPAddresses: []string{"9.9.9.9"},
		Metadata:    &model.NodeMetadata{ClusterID: "cl1"},
		Type:        model.SidecarProxy,
		DNSDomain:   "testns.svc.cluster.local",
	}

	pod1 := &model.Proxy{
		IPAddresses: []string{"1.2.3.4"},
		Metadata:    &model.NodeMetadata{},
		Type:        model.SidecarProxy,
		DNSDomain:   "testns.svc.cluster.local",
	}
	pod2 := &model.Proxy{
		IPAddresses: []string{"9.6.7.8"},
		Metadata:    &model.NodeMetadata{Network: "nw2", ClusterID: "cl2"},
		Type:        model.SidecarProxy,
		DNSDomain:   "testns.svc.cluster.local",
	}
	pod3 := &model.Proxy{
		IPAddresses: []string{"19.6.7.8"},
		Metadata:    &model.NodeMetadata{Network: "nw1"},
		Type:        model.SidecarProxy,
		DNSDomain:   "testns.svc.cluster.local",
	}
	pod4 := &model.Proxy{
		IPAddresses: []string{"9.16.7.8"},
		Metadata:    &model.NodeMetadata{ClusterID: "cl1"},
		Type:        model.SidecarProxy,
		DNSDomain:   "testns.svc.cluster.local",
	}

	headlessService := &model.Service{
		Hostname:       host.Name("headless-svc.testns.svc.cluster.local"),
		DefaultAddress: constants.UnspecifiedIP,
		Ports: model.PortList{&model.Port{
			Name:     "tcp-port",
			Port:     9000,
			Protocol: protocol.TCP,
		}},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Name:            "headless-svc",
			Namespace:       "testns",
			ServiceRegistry: provider.Kubernetes,
		},
	}

	headlessServiceForServiceEntry := &model.Service{
		Hostname:       host.Name("foo.bar.com"),
		DefaultAddress: constants.UnspecifiedIP,
		Ports: model.PortList{&model.Port{
			Name:     "tcp-port",
			Port:     9000,
			Protocol: protocol.TCP,
		}},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Name:            "foo.bar.com",
			Namespace:       "testns",
			ServiceRegistry: provider.External,
			LabelSelectors:  map[string]string{"wl": "headless-foobar"},
		},
	}

	wildcardService := &model.Service{
		Hostname:       host.Name("*.testns.svc.cluster.local"),
		DefaultAddress: "172.10.10.10",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     9000,
				Protocol: protocol.TCP,
			},
			&model.Port{
				Name:     "http-port",
				Port:     8000,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Name:            "wildcard-svc",
			Namespace:       "testns",
			ServiceRegistry: provider.Kubernetes,
		},
	}

	cidrService := &model.Service{
		Hostname:       host.Name("*.testns.svc.cluster.local"),
		DefaultAddress: "172.217.0.0/16",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     9000,
				Protocol: protocol.TCP,
			},
			&model.Port{
				Name:     "http-port",
				Port:     8000,
				Protocol: protocol.HTTP,
			},
		},
		Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{
			Name:            "cidr-svc",
			Namespace:       "testns",
			ServiceRegistry: provider.Kubernetes,
		},
	}

	push := model.NewPushContext()
	push.Mesh = mesh
	push.AddPublicServices([]*model.Service{headlessService})
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod1, headlessService, "pod1", "headless-svc"))
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod2, headlessService, "pod2", "headless-svc"))
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod3, headlessService, "pod3", "headless-svc"))
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod4, headlessService, "pod4", "headless-svc"))

	wpush := model.NewPushContext()
	wpush.Mesh = mesh
	wpush.AddPublicServices([]*model.Service{wildcardService})

	cpush := model.NewPushContext()
	cpush.Mesh = mesh
	wpush.AddPublicServices([]*model.Service{cidrService})

	sepush := model.NewPushContext()
	sepush.Mesh = mesh
	sepush.AddPublicServices([]*model.Service{headlessServiceForServiceEntry})
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod1, headlessServiceForServiceEntry, "", ""))
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod2, headlessServiceForServiceEntry, "", ""))
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod3, headlessServiceForServiceEntry, "", ""))
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod4, headlessServiceForServiceEntry, "", ""))

	cases := []struct {
		name                       string
		proxy                      *model.Proxy
		push                       *model.PushContext
		enableMultiClusterHeadless bool
		expectedNameTable          *dnsProto.NameTable
	}{
		{
			name:  "headless service pods",
			proxy: proxy,
			push:  push,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"pod1.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4"},
						Registry:  "Kubernetes",
						Shortname: "pod1.headless-svc",
						Namespace: "testns",
					},
					"pod2.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"9.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod2.headless-svc",
						Namespace: "testns",
					},
					"pod3.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"19.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod3.headless-svc",
						Namespace: "testns",
					},
					"pod4.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod4.headless-svc",
						Namespace: "testns",
					},
					"headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4", "9.6.7.8", "19.6.7.8", "9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "headless-svc",
						Namespace: "testns",
					},
				},
			},
		},
		{
			name:  "headless service pods with network isolation",
			proxy: nw1proxy,
			push:  push,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"pod1.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4"},
						Registry:  "Kubernetes",
						Shortname: "pod1.headless-svc",
						Namespace: "testns",
					},
					"pod3.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"19.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod3.headless-svc",
						Namespace: "testns",
					},
					"pod4.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod4.headless-svc",
						Namespace: "testns",
					},
					"headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4", "19.6.7.8", "9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "headless-svc",
						Namespace: "testns",
					},
				},
			},
		},
		{
			name:  "multi cluster headless service pods",
			proxy: cl1proxy,
			push:  push,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"pod1.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4"},
						Registry:  "Kubernetes",
						Shortname: "pod1.headless-svc",
						Namespace: "testns",
					},
					"pod2.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"9.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod2.headless-svc",
						Namespace: "testns",
					},
					"pod3.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"19.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod3.headless-svc",
						Namespace: "testns",
					},
					"pod4.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod4.headless-svc",
						Namespace: "testns",
					},
					"headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4", "19.6.7.8", "9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "headless-svc",
						Namespace: "testns",
					},
				},
			},
		},
		{
			name:                       "multi cluster headless service pods with multi cluster enabled",
			proxy:                      cl1proxy,
			push:                       push,
			enableMultiClusterHeadless: true,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"pod1.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4"},
						Registry:  "Kubernetes",
						Shortname: "pod1.headless-svc",
						Namespace: "testns",
					},
					"pod2.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"9.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod2.headless-svc",
						Namespace: "testns",
					},
					"pod3.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"19.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod3.headless-svc",
						Namespace: "testns",
					},
					"pod4.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "pod4.headless-svc",
						Namespace: "testns",
					},
					"headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4", "9.6.7.8", "19.6.7.8", "9.16.7.8"},
						Registry:  "Kubernetes",
						Shortname: "headless-svc",
						Namespace: "testns",
					},
				},
			},
		},
		{
			name:  "wildcard service pods",
			proxy: proxy,
			push:  wpush,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"*.testns.svc.cluster.local": {
						Ips:       []string{"172.10.10.10"},
						Registry:  "Kubernetes",
						Shortname: "wildcard-svc",
						Namespace: "testns",
					},
				},
			},
		},
		{
			name:  "cidr service",
			proxy: proxy,
			push:  cpush,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{},
			},
		},
		{
			name:  "service entry with resolution = NONE",
			proxy: proxy,
			push:  sepush,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"foo.bar.com": {
						Ips:      []string{"1.2.3.4", "9.6.7.8", "19.6.7.8", "9.16.7.8"},
						Registry: "External",
					},
				},
			},
		},
		{
			name:  "service entry with resolution = NONE with network isolation",
			proxy: nw1proxy,
			push:  sepush,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"foo.bar.com": {
						Ips:      []string{"1.2.3.4", "19.6.7.8", "9.16.7.8"},
						Registry: "External",
					},
				},
			},
		},
		{
			name:  "multi cluster service entry with resolution = NONE",
			proxy: cl1proxy,
			push:  sepush,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"foo.bar.com": {
						Ips:      []string{"1.2.3.4", "19.6.7.8", "9.16.7.8"},
						Registry: "External",
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.proxy.SidecarScope = model.ConvertToSidecarScope(tt.push, nil, "default")
			if diff := cmp.Diff(dnsServer.BuildNameTable(dnsServer.Config{
				Node:                        tt.proxy,
				Push:                        tt.push,
				MulticlusterHeadlessEnabled: tt.enableMultiClusterHeadless,
			}), tt.expectedNameTable, protocmp.Transform()); diff != "" {
				t.Fatalf("got diff: %v", diff)
			}
		})
	}
}

func makeInstances(proxy *model.Proxy, svc *model.Service, servicePort int, targetPort int) []*model.ServiceInstance {
	ret := make([]*model.ServiceInstance, 0)
	for _, p := range svc.Ports {
		if p.Port != servicePort {
			continue
		}
		ret = append(ret, &model.ServiceInstance{
			Service:     svc,
			ServicePort: p,
			Endpoint: &model.IstioEndpoint{
				Address:         proxy.IPAddresses[0],
				ServicePortName: p.Name,
				EndpointPort:    uint32(targetPort),
			},
		})
	}
	return ret
}
