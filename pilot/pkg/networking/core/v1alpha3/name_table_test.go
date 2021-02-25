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

package v1alpha3_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	nds "istio.io/istio/pilot/pkg/proto"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
)

func makeServiceInstances(proxy *model.Proxy, service *model.Service, labels labels.Instance) map[int][]*model.ServiceInstance {
	instances := make(map[int][]*model.ServiceInstance)
	for _, port := range service.Ports {
		instances[port.Port] = makeInstances(proxy, service, port.Port, port.Port)
		instances[port.Port][0].Endpoint.Labels = labels
	}
	return instances
}

func TestNameTable(t *testing.T) {
	proxy := &model.Proxy{
		IPAddresses: []string{"9.9.9.9"},
		Metadata:    &model.NodeMetadata{},
		Type:        model.SidecarProxy,
		DNSDomain:   "stsns.svc.cluster.local",
	}

	stpod1 := &model.Proxy{
		IPAddresses: []string{"1.2.3.4"},
		Metadata:    &model.NodeMetadata{},
		Type:        model.SidecarProxy,
		DNSDomain:   "stsns.svc.cluster.local",
	}
	stpod2 := &model.Proxy{
		IPAddresses: []string{"9.6.7.8"},
		Metadata:    &model.NodeMetadata{},
		Type:        model.SidecarProxy,
		DNSDomain:   "stsns.svc.cluster.local",
	}

	statefulsetService := &model.Service{
		Hostname:    host.Name("stateful.stsns.svc.cluster.local"),
		Address:     constants.UnspecifiedIP,
		ClusterVIPs: make(map[string]string),
		Ports: model.PortList{&model.Port{
			Name:     "tcp-port",
			Port:     9000,
			Protocol: protocol.TCP,
		}},
		Resolution: model.Passthrough,
		Attributes: model.ServiceAttributes{
			Name:            "stateful",
			Namespace:       "stsns",
			ServiceRegistry: string(serviceregistry.Kubernetes),
		},
	}
	statefulPush := &model.PushContext{}
	statefulPush.ServiceIndex.Public = []*model.Service{statefulsetService}
	instancesByPort := make(map[*model.Service]map[int][]*model.ServiceInstance)
	instancesByPort[statefulsetService] = makeServiceInstances(stpod1, statefulsetService, labels.Instance{v1alpha3.StatefulSetPodLabel: "stateful-0"})
	instancesByPort[statefulsetService][9000] = append(instancesByPort[statefulsetService][9000],
		makeServiceInstances(stpod2, statefulsetService, labels.Instance{v1alpha3.StatefulSetPodLabel: "stateful-1"})[9000]...)
	statefulPush.ServiceIndex.InstancesByPort = instancesByPort

	cases := []struct {
		name              string
		proxy             *model.Proxy
		push              *model.PushContext
		expectedNameTable *nds.NameTable
	}{
		{
			name:  "stateful service pods",
			proxy: proxy,
			push:  statefulPush,
			expectedNameTable: &nds.NameTable{
				Table: map[string]*nds.NameTable_NameInfo{
					"stateful-0.stateful.stsns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4"},
						Registry:  "Kubernetes",
						Shortname: "stateful-0.stateful",
						Namespace: "stsns",
					},
					"stateful-1.stateful.stsns.svc.cluster.local": {
						Ips:       []string{"9.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "stateful-1.stateful",
						Namespace: "stsns",
					},
					"stateful.stsns.svc.cluster.local": {
						Ips:       []string{"1.2.3.4", "9.6.7.8"},
						Registry:  "Kubernetes",
						Shortname: "stateful",
						Namespace: "stsns",
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			configgen := core.NewConfigGenerator(nil, model.DisabledCache{})
			if diff := cmp.Diff(configgen.BuildNameTable(tt.proxy, tt.push), tt.expectedNameTable); diff != "" {
				t.Fatalf("got diff: %v", diff)
			}
		})
	}
}
