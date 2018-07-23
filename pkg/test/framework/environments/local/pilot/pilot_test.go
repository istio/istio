//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"fmt"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	envoy_proxy_v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/test/framework/environment"
)

func TestLocalPilot(t *testing.T) {
	// Create and start the pilot discovery service.
	pilot, stopFunc := newPilot(t)
	defer stopFunc()

	// Add a service entry.
	_, err := pilot.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "some-service-entry",
			Namespace: "istio-system",
			Type:      model.ServiceEntry.Type,
		},
		Spec: &v1alpha3.ServiceEntry{
			Hosts: []string{
				"fakehost.istio-system.svc.cluster.local",
			},
			Addresses: []string{
				"127.0.0.1/24",
			},
			Ports: []*v1alpha3.Port{
				{
					Name:     "http",
					Protocol: "HTTP",
					Number:   123,
				},
			},
			Location: v1alpha3.ServiceEntry_MESH_INTERNAL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Query pilot for the listeners.
	res, err := pilot.CallDiscovery(&xdsapi.DiscoveryRequest{
		// Just use a dummy node as the origin of this request.
		Node: &envoy_api_v2_core1.Node{
			Id: "sidecar~127.0.0.1~mysidecar.istio-system~istio-system.svc.cluster.local",
		},
		TypeUrl: envoy_proxy_v2.ListenerType,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Now look through the listeners for our service entry.
	for _, resource := range res.Resources {
		l := xdsapi.Listener{}
		l.Unmarshal(resource.Value)
		if l.Address.GetSocketAddress().GetPortValue() == 123 {
			return
		}
	}
	t.Fatal(fmt.Errorf("service entry not found in pilot discovery service"))
}

func newPilot(t *testing.T) (environment.DeployedPilot, func()) {
	t.Helper()
	mesh := model.DefaultMeshConfig()
	options := envoy.DiscoveryServiceOptions{
		HTTPAddr:       ":0",
		MonitoringAddr: ":0",
		GrpcAddr:       ":0",
		SecureGrpcAddr: ":0",
	}

	pilot, err := NewPilot(Args{
		Namespace: "istio-system",
		Mesh:      &mesh,
		Options:   options,
	})

	if err != nil {
		t.Fatal(err)
	}

	stopFunc := func() {
		pilot.(*deployedPilot).Stop()
	}

	return pilot, stopFunc
}
