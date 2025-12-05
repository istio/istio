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
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	dnsProto "istio.io/istio/pkg/dns/proto"
	dnsServer "istio.io/istio/pkg/dns/server"
	"istio.io/istio/pkg/test"
)

func TestNameTable(t *testing.T) {
	test.SetForTest(t, &features.EnableDualStack, true)
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

	cl1DualStackproxy := &model.Proxy{
		IPAddresses: []string{"9.9.9.9", "2001:2::2001"},
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

	headlessServiceWithPublishNonReadyEnabled := &model.Service{
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
			K8sAttributes:   model.K8sAttributes{PublishNotReadyAddresses: true},
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

	serviceWithVIP1 := &model.Service{
		Hostname:       host.Name("mysql.foo.bar"),
		DefaultAddress: "10.0.0.5",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp",
				Port:     3306,
				Protocol: protocol.TCP,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Name:            "mysql-svc",
			Namespace:       "testns",
			ServiceRegistry: provider.External,
		},
	}
	serviceWithVIP2 := serviceWithVIP1.DeepCopy()
	serviceWithVIP2.DefaultAddress = "10.0.0.6"

	decoratedService := serviceWithVIP1.DeepCopy()
	decoratedService.DefaultAddress = "10.0.0.7"
	decoratedService.Attributes.ServiceRegistry = provider.Kubernetes

	dualStackService := &model.Service{
		Hostname:       host.Name("dual.foo.bar"),
		DefaultAddress: "10.0.0.8",
		ClusterVIPs: model.AddressMap{
			Addresses: map[cluster.ID][]string{
				"cl1": {"2001:2::", "10.0.0.8"},
			},
		},
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp",
				Port:     3306,
				Protocol: protocol.TCP,
			},
		},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Name:            "mysql-svc",
			Namespace:       "testns",
			ServiceRegistry: provider.External,
		},
	}

	push := model.NewPushContext()
	push.Mesh = mesh
	push.AddPublicServices([]*model.Service{headlessService})
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod1, headlessService, "pod1", "headless-svc", model.Healthy))
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod2, headlessService, "pod2", "headless-svc", model.Healthy))
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod3, headlessService, "pod3", "headless-svc", model.Healthy))
	push.AddServiceInstances(headlessService,
		makeServiceInstances(pod4, headlessService, "pod4", "headless-svc", model.Healthy))

	uhpush := model.NewPushContext()
	uhpush.Mesh = mesh
	uhpush.AddPublicServices([]*model.Service{headlessService})
	uhpush.AddServiceInstances(headlessService,
		makeServiceInstances(pod1, headlessService, "pod1", "headless-svc", model.Healthy))
	uhpush.AddServiceInstances(headlessService,
		makeServiceInstances(pod2, headlessService, "pod2", "headless-svc", model.UnHealthy))
	uhpush.AddServiceInstances(headlessService,
		makeServiceInstances(pod3, headlessService, "pod3", "headless-svc", model.Terminating))

	uhreadypush := model.NewPushContext()
	uhreadypush.Mesh = mesh
	uhreadypush.AddPublicServices([]*model.Service{headlessServiceWithPublishNonReadyEnabled})
	uhreadypush.AddServiceInstances(headlessService,
		makeServiceInstances(pod1, headlessService, "pod1", "headless-svc", model.Healthy))
	uhreadypush.AddServiceInstances(headlessService,
		makeServiceInstances(pod2, headlessService, "pod2", "headless-svc", model.UnHealthy))
	uhreadypush.AddServiceInstances(headlessService,
		makeServiceInstances(pod3, headlessService, "pod3", "headless-svc", model.Terminating))

	wpush := model.NewPushContext()
	wpush.Mesh = mesh
	wpush.AddPublicServices([]*model.Service{wildcardService})

	cpush := model.NewPushContext()
	cpush.Mesh = mesh
	cpush.AddPublicServices([]*model.Service{cidrService})

	sepush := model.NewPushContext()
	sepush.Mesh = mesh
	sepush.AddPublicServices([]*model.Service{headlessServiceForServiceEntry})
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod1, headlessServiceForServiceEntry, "", "", model.Healthy))
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod2, headlessServiceForServiceEntry, "", "", model.Healthy))
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod3, headlessServiceForServiceEntry, "", "", model.Healthy))
	sepush.AddServiceInstances(headlessServiceForServiceEntry,
		makeServiceInstances(pod4, headlessServiceForServiceEntry, "", "", model.Healthy))

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
			name:                       "headless service with unhealthy pods",
			proxy:                      cl1proxy,
			push:                       uhpush,
			enableMultiClusterHeadless: true,
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"pod1.headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4"},
						Registry:  "Kubernetes",
						Shortname: "pod1.headless-svc",
						Namespace: "testns",
					},
					"headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4"},
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
			name:                       "publish unready headless service with unhealthy pods",
			proxy:                      cl1proxy,
			push:                       uhreadypush,
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
					"headless-svc.testns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4", "9.6.7.8", "19.6.7.8"},
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
			name:  "dual stack",
			proxy: cl1DualStackproxy,
			push: func() *model.PushContext {
				push := model.NewPushContext()
				push.Mesh = mesh
				push.AddPublicServices([]*model.Service{dualStackService})
				return push
			}(),
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					"dual.foo.bar": {
						Ips:      []string{"2001:2::", "10.0.0.8"},
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
		{
			name:  "service entry with multiple VIPs",
			proxy: proxy,
			push: func() *model.PushContext {
				push := model.NewPushContext()
				push.Mesh = mesh
				push.AddPublicServices([]*model.Service{serviceWithVIP1, serviceWithVIP2})
				return push
			}(),
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					serviceWithVIP1.Hostname.String(): {
						Ips:      []string{serviceWithVIP1.DefaultAddress, serviceWithVIP2.DefaultAddress},
						Registry: provider.External.String(),
					},
				},
			},
		},
		{
			name:  "service entry as a decorator(created before k8s service)",
			proxy: proxy,
			push: func() *model.PushContext {
				push := model.NewPushContext()
				push.Mesh = mesh
				push.AddPublicServices([]*model.Service{serviceWithVIP1, decoratedService})
				return push
			}(),
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					serviceWithVIP1.Hostname.String(): {
						Ips:       []string{decoratedService.DefaultAddress},
						Registry:  provider.Kubernetes.String(),
						Shortname: decoratedService.Attributes.Name,
						Namespace: decoratedService.Attributes.Namespace,
					},
				},
			},
		},
		{
			name:  "service entry as a decorator(created after k8s service)",
			proxy: proxy,
			push: func() *model.PushContext {
				push := model.NewPushContext()
				push.Mesh = mesh
				push.AddPublicServices([]*model.Service{decoratedService, serviceWithVIP2})
				return push
			}(),
			expectedNameTable: &dnsProto.NameTable{
				Table: map[string]*dnsProto.NameTable_NameInfo{
					serviceWithVIP1.Hostname.String(): {
						Ips:       []string{decoratedService.DefaultAddress},
						Registry:  provider.Kubernetes.String(),
						Shortname: decoratedService.Attributes.Name,
						Namespace: decoratedService.Attributes.Namespace,
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.proxy.SetSidecarScope(tt.push)
			tt.proxy.DiscoverIPMode()
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

// nolint
func makeServiceInstances(proxy *model.Proxy, service *model.Service, hostname, subdomain string, healthStatus model.HealthStatus) map[int][]*model.IstioEndpoint {
	instances := make(map[int][]*model.IstioEndpoint)
	for _, port := range service.Ports {
		instances[port.Port] = makeInstances(proxy, service, port.Port, port.Port, healthStatus)
		instances[port.Port][0].HostName = hostname
		instances[port.Port][0].SubDomain = subdomain
		instances[port.Port][0].Network = proxy.Metadata.Network
		instances[port.Port][0].Locality.ClusterID = proxy.Metadata.ClusterID
	}
	return instances
}

func makeInstances(proxy *model.Proxy, svc *model.Service, servicePort int, targetPort int,
	healthStatus model.HealthStatus,
) []*model.IstioEndpoint {
	ret := make([]*model.IstioEndpoint, 0)
	for _, p := range svc.Ports {
		if p.Port != servicePort {
			continue
		}
		ret = append(ret, &model.IstioEndpoint{
			Addresses:       proxy.IPAddresses,
			ServicePortName: p.Name,
			EndpointPort:    uint32(targetPort),
			HealthStatus:    healthStatus,
		})
	}
	return ret
}

func TestPodNameTableLocalRemoteAddresses(t *testing.T) {
	mesh := &meshconfig.MeshConfig{RootNamespace: "istio-system"}

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

	cases := []struct {
		name          string
		proxyCluster  cluster.ID
		instances     []*model.Proxy
		expectedEntry string
		expectedIPs   []string
	}{
		{
			name:         "cumulative IPs for same pod in local cluster",
			proxyCluster: "cluster1",
			instances: []*model.Proxy{
				{IPAddresses: []string{"10.0.0.1"}, Metadata: &model.NodeMetadata{ClusterID: "cluster1"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
				{IPAddresses: []string{"10.0.0.2"}, Metadata: &model.NodeMetadata{ClusterID: "cluster1"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
			},
			expectedEntry: "mysql-0.headless-svc.testns.svc.cluster.local",
			expectedIPs:   []string{"10.0.0.1", "10.0.0.2"},
		},
		{
			name:         "cumulative IPs for same pod in remote cluster",
			proxyCluster: "cluster1",
			instances: []*model.Proxy{
				{IPAddresses: []string{"10.0.0.3"}, Metadata: &model.NodeMetadata{ClusterID: "cluster2"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
				{IPAddresses: []string{"10.0.0.4"}, Metadata: &model.NodeMetadata{ClusterID: "cluster2"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
			},
			expectedEntry: "mysql-0.headless-svc.testns.svc.cluster.local",
			expectedIPs:   []string{"10.0.0.3", "10.0.0.4"},
		},
		{
			name:         "local cluster preferred over remote",
			proxyCluster: "cluster1",
			instances: []*model.Proxy{
				{IPAddresses: []string{"10.0.0.1"}, Metadata: &model.NodeMetadata{ClusterID: "cluster1"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
				{IPAddresses: []string{"10.0.0.2"}, Metadata: &model.NodeMetadata{ClusterID: "cluster2"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
			},
			expectedEntry: "mysql-0.headless-svc.testns.svc.cluster.local",
			expectedIPs:   []string{"10.0.0.1"},
		},
		{
			name:         "multiple local instances preferred over multiple remote",
			proxyCluster: "cluster1",
			instances: []*model.Proxy{
				{IPAddresses: []string{"10.0.0.1"}, Metadata: &model.NodeMetadata{ClusterID: "cluster1"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
				{IPAddresses: []string{"10.0.0.2"}, Metadata: &model.NodeMetadata{ClusterID: "cluster1"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
				{IPAddresses: []string{"10.0.0.3"}, Metadata: &model.NodeMetadata{ClusterID: "cluster2"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
				{IPAddresses: []string{"10.0.0.4"}, Metadata: &model.NodeMetadata{ClusterID: "cluster2"}, Type: model.SidecarProxy, DNSDomain: "testns.svc.cluster.local"},
			},
			expectedEntry: "mysql-0.headless-svc.testns.svc.cluster.local",
			expectedIPs:   []string{"10.0.0.1", "10.0.0.2"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			push := model.NewPushContext()
			push.Mesh = mesh
			push.AddPublicServices([]*model.Service{headlessService})
			for _, inst := range tt.instances {
				push.AddServiceInstances(headlessService,
					makeServiceInstances(inst, headlessService, "mysql-0", "headless-svc", model.Healthy))
			}

			proxy := &model.Proxy{
				IPAddresses: []string{"9.9.9.9"},
				Metadata:    &model.NodeMetadata{ClusterID: tt.proxyCluster},
				Type:        model.SidecarProxy,
				DNSDomain:   "testns.svc.cluster.local",
			}
			proxy.SetSidecarScope(push)
			proxy.DiscoverIPMode()

			nameTable := dnsServer.BuildNameTable(dnsServer.Config{
				Node:                        proxy,
				Push:                        push,
				MulticlusterHeadlessEnabled: true,
			})

			entry, found := nameTable.Table[tt.expectedEntry]
			if !found {
				t.Fatalf("Expected entry %s not found", tt.expectedEntry)
			}
			if diff := cmp.Diff(entry.Ips, tt.expectedIPs); diff != "" {
				t.Errorf("IPs mismatch (-got +want):\n%s", diff)
			}
		})
	}
}
