// Copyright 2018 Istio Authors
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

package v1alpha3

import (
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

var (
	tnow  = time.Now()
	tzero = time.Time{}
	proxy = model.Proxy{
		Type:      model.Sidecar,
		IPAddress: "1.1.1.1",
		ID:        "v0.default",
		Domain:    "default.example.org",
	}
)

func TestOutboundListenerConflict_HTTP(t *testing.T) {
	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService(model.ProtocolHTTP, tnow.Add(1*time.Second)),
		buildService(model.ProtocolTCP, tnow),
		buildService(model.ProtocolHTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_TCP(t *testing.T) {
	// The oldest service port is HTTP.  We should encounter conflicts when attempting to add the TCP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService(model.ProtocolTCP, tnow.Add(1*time.Second)),
		buildService(model.ProtocolHTTP, tnow),
		buildService(model.ProtocolTCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_Unordered(t *testing.T) {
	// Ensure that the order is preserved when all the times match. The first service in the list wins.
	testOutboundListenerConflict(t,
		buildService(model.ProtocolHTTP, tzero),
		buildService(model.ProtocolTCP, tzero),
		buildService(model.ProtocolTCP, tzero))
}

func testOutboundListenerConflict(t *testing.T, services ...*model.Service) {
	t.Helper()

	p := &fakePlugin{}
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnv()

	var oldestService *model.Service
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service: s,
		}
		if oldestService == nil || s.CreationTime.Before(oldestService.CreationTime) {
			oldestService = s
		}
	}
	listeners := configgen.buildSidecarOutboundListeners(env, proxy, instances, services)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != model.ProtocolHTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == model.ProtocolHTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}

	if len(p.outboundListenerParams) != 1 {
		t.Fatalf("expected %d listener params, found %d", 1, len(p.outboundListenerParams))
	}

	if p.outboundListenerParams[0].Service != oldestService {
		t.Fatalf("listener conflict failed to preserve listener for the oldest service")
	}
}

type fakePlugin struct {
	outboundListenerParams []*plugin.InputParams
}

func (p *fakePlugin) OnOutboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	p.outboundListenerParams = append(p.outboundListenerParams, in)
	return nil
}

func (p *fakePlugin) OnInboundListener(in *plugin.InputParams, mutable *plugin.MutableObjects) error {
	return nil
}

func (p *fakePlugin) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
	cluster *xdsapi.Cluster) {
}

func (p *fakePlugin) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
	cluster *xdsapi.Cluster) {
}

func (p *fakePlugin) OnOutboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

func (p *fakePlugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

func isHTTPListener(listener *xdsapi.Listener) bool {
	if len(listener.FilterChains) > 0 && len(listener.FilterChains[0].Filters) > 0 {
		return listener.FilterChains[0].Filters[0].Name == "envoy.http_connection_manager"
	}
	return false
}

func buildService(protocol model.Protocol, creationTime time.Time) *model.Service {
	return &model.Service{
		CreationTime: creationTime,
		Hostname:     "*.example.org",
		Address:      "1.1.1.1",
		ClusterVIPs:  make(map[string]string),
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     8080,
				Protocol: protocol,
			},
		},
		Resolution: model.Passthrough,
	}
}

func buildListenerEnv() model.Environment {
	serviceDiscovery := &fakes.ServiceDiscovery{}

	configStore := &fakes.IstioConfigStore{}

	mesh := model.DefaultMeshConfig()
	env := model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ServiceAccounts:  &fakes.ServiceAccounts{},
		IstioConfigStore: configStore,
		Mesh:             &mesh,
		MixerSAN:         []string{},
	}

	return env
}
