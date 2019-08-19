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

package v2

import (
	"reflect"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate/mock"
	"istio.io/istio/pkg/config/labels"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
)

func TestSetProxyWorkloadLabels(t *testing.T) {
	aggregator, cancel := mock.NewFakeAggregateControllerForMultiCluster()
	defer cancel()

	environment := &model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: aggregator,
		Mesh:             &meshconfig.MeshConfig{},
	}

	xdsServer := NewDiscoveryServer(environment, nil, aggregate.NewController(), nil, nil)

	ip := "192.168.0.10"

	testCases := []struct {
		name    string
		proxyIP string
		network string
		expect  labels.Collection
	}{
		{
			name:    "should get labels from its own cluster workload",
			proxyIP: ip,
			network: "network1",
			expect:  labels.Collection{{"app": "cluster1"}},
		},
		{
			name:    "should get labels from its own cluster workload",
			proxyIP: ip,
			network: "network2",
			expect:  labels.Collection{{"app": "cluster2"}},
		},
		{
			name:    "can not get a workload from its cluster",
			proxyIP: ip,
			network: "network3",
			expect:  labels.Collection{},
		},
		{
			name:    "can not get a workload from its cluster",
			proxyIP: "not exist ip",
			network: "network1",
			expect:  labels.Collection{},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			proxy := &model.Proxy{
				Metadata:    map[string]string{model.NodeMetadataNetwork: test.network},
				IPAddresses: []string{test.proxyIP},
				Locality: &core.Locality{
					Region: "placeholder",
				},
			}
			connection := &XdsConnection{
				Clusters:     []string{},
				LDSListeners: []*xdsapi.Listener{},
				RouteConfigs: map[string]*xdsapi.RouteConfiguration{},
				modelNode:    proxy,
			}
			push := model.NewPushContext()
			push.Env = environment
			xdsServer.pushConnection(connection, &XdsEvent{push: push})
			workloadlabels := proxy.WorkloadLabels

			if !reflect.DeepEqual(workloadlabels, test.expect) {
				t.Errorf("Expect labels %v != %v", test.expect, workloadlabels)
			}
		})
	}
}
