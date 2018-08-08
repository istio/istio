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

package pilot_test

import (
	"fmt"
	"io"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	envoy_proxy_v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

const (
	localCIDR = "127.0.0.1/32"
)

func TestLocalPilot(t *testing.T) {
	// Create and start the pilot discovery service.
	p, err := pilot.NewLocalPilot("istio-system")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.(io.Closer).Close()
	}()

	// Add a service entry.
	configStore := p.(model.ConfigStore)
	_, err = configStore.Create(model.Config{
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
				localCIDR,
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
	res, err := p.CallDiscovery(&xdsapi.DiscoveryRequest{
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

/*
TODO: uncomment and modify to test against an active kube environment.
func TestKubePilot(t *testing.T) {
	p, err := pilot.NewKubePilot("", "istio-system", "istio-pilot-6c5c6b586c-9gwn5")
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.CallDiscovery(&xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: "sidecar~127.0.0.1~mysidecar.istio-system~istio-system.svc.cluster.local",
		},
		TypeUrl: envoy_proxy_v2.ListenerType,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := jsonpb.Marshaler{
		Indent: "  ",
	}
	str, err := m.MarshalToString(res)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(str)
}
*/
