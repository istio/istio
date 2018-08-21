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

	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

const (
	wildcardIP = "0.0.0.0"
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

func TestOutboundListenerConflict_HTTPWithCurrentTCP(t *testing.T) {
	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1,com", wildcardIP, model.ProtocolHTTP, tnow.Add(1*time.Second)),
		buildService("test2,com", wildcardIP, model.ProtocolTCP, tnow),
		buildService("test3,com", wildcardIP, model.ProtocolHTTP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_TCPWithCurrentHTTP(t *testing.T) {
	// The oldest service port is HTTP.  We should encounter conflicts when attempting to add the TCP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, model.ProtocolTCP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, model.ProtocolHTTP, tnow),
		buildService("test3.com", wildcardIP, model.ProtocolTCP, tnow.Add(2*time.Second)))
}

func TestOutboundListenerConflict_Unordered(t *testing.T) {
	// Ensure that the order is preserved when all the times match. The first service in the list wins.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, model.ProtocolHTTP, tzero),
		buildService("test2.com", wildcardIP, model.ProtocolTCP, tzero),
		buildService("test3.com", wildcardIP, model.ProtocolTCP, tzero))
}

func TestOutboundListenerConflict_TCPWithCurrentTCP(t *testing.T) {
	services := []*model.Service{
		buildService("test1.com", "1.2.3.4", model.ProtocolTCP, tnow.Add(1*time.Second)),
		buildService("test2.com", "1.2.3.4", model.ProtocolTCP, tnow),
		buildService("test3.com", "1.2.3.4", model.ProtocolTCP, tnow.Add(2*time.Second)),
	}
	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	// The filter chains should all be merged into one.
	if len(listeners[0].FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(listeners[0].FilterChains))
	}
	verifyOutboundTCPListenerHostname(t, listeners[0], "test2.com")

	oldestService := getOldestService(services...)
	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != model.ProtocolHTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == model.ProtocolHTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}

	if p.outboundListenerParams[0].Service != oldestService {
		t.Fatalf("listener conflict failed to preserve listener for the oldest service")
	}
}

func testOutboundListenerConflict(t *testing.T, services ...*model.Service) {
	t.Helper()

	oldestService := getOldestService(services...)

	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, services...)
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

func verifyOutboundTCPListenerHostname(t *testing.T, l *xdsapi.Listener, hostname model.Hostname) {
	t.Helper()
	if len(l.FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
	}
	f := fc.Filters[0]
	expectedStatPrefix := fmt.Sprintf("outbound|8080||%s", hostname)
	statPrefix := f.Config.Fields["stat_prefix"].GetStringValue()
	if statPrefix != expectedStatPrefix {
		t.Fatalf("expected listener to contain stat_prefix %s, found %s", expectedStatPrefix, statPrefix)
	}
}

func getOldestService(services ...*model.Service) *model.Service {
	var oldestService *model.Service
	for _, s := range services {
		if oldestService == nil || s.CreationTime.Before(oldestService.CreationTime) {
			oldestService = s
		}
	}
	return oldestService
}

func buildOutboundListeners(p plugin.Plugin, services ...*model.Service) []*xdsapi.Listener {
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnv(services)

	if err := env.PushContext.InitContext(&env); err != nil {
		return nil
	}

	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service: s,
		}
	}
	return configgen.buildSidecarOutboundListeners(&env, &proxy, env.PushContext, instances, services)
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

func (p *fakePlugin) OnOutboundCluster(env *model.Environment, node *model.Proxy, push *model.PushContext, service *model.Service, servicePort *model.Port,
	cluster *xdsapi.Cluster) {
}

func (p *fakePlugin) OnInboundCluster(env *model.Environment, node *model.Proxy, push *model.PushContext, service *model.Service, servicePort *model.Port,
	cluster *xdsapi.Cluster) {
}

func (p *fakePlugin) OnOutboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

func (p *fakePlugin) OnInboundRouteConfiguration(in *plugin.InputParams, routeConfiguration *xdsapi.RouteConfiguration) {
}

func (p *fakePlugin) OnInboundFilterChains(in *plugin.InputParams) []plugin.FilterChain {
	return nil
}

func isHTTPListener(listener *xdsapi.Listener) bool {
	if len(listener.FilterChains) > 0 && len(listener.FilterChains[0].Filters) > 0 {
		return listener.FilterChains[0].Filters[0].Name == "envoy.http_connection_manager"
	}
	return false
}

func buildService(hostname string, ip string, protocol model.Protocol, creationTime time.Time) *model.Service {
	return &model.Service{
		CreationTime: creationTime,
		Hostname:     model.Hostname(hostname),
		Address:      ip,
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

func buildListenerEnv(services []*model.Service) model.Environment {
	serviceDiscovery := new(fakes.ServiceDiscovery)
	serviceDiscovery.ServicesReturns(services, nil)

	configStore := &fakes.IstioConfigStore{}

	mesh := model.DefaultMeshConfig()
	env := model.Environment{
		PushContext:      model.NewStatus(),
		ServiceDiscovery: serviceDiscovery,
		ServiceAccounts:  &fakes.ServiceAccounts{},
		IstioConfigStore: configStore,
		Mesh:             &mesh,
		MixerSAN:         []string{},
	}

	return env
}
