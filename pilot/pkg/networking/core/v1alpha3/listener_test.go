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
	"fmt"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"

	networking "istio.io/api/networking/v1alpha3"
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
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: map[string]string{
			model.NodeMetadataConfigNamespace: "not-default",
			"ISTIO_PROXY_VERSION":             "1.1",
		},
		ConfigNamespace: "not-default",
	}
	proxyInstances = []*model.ServiceInstance{
		{
			Service: &model.Service{
				Hostname:     "v0.default.example.org",
				Address:      "9.9.9.9",
				CreationTime: tnow,
				Attributes: model.ServiceAttributes{
					Namespace: "not-default",
				},
			},
			Labels: nil,
		},
	}
)

func TestOutboundListenerConflict_HTTPWithCurrentTCP(t *testing.T) {
	// The oldest service port is TCP.  We should encounter conflicts when attempting to add the HTTP ports. Purposely
	// storing the services out of time order to test that it's being sorted properly.
	testOutboundListenerConflict(t,
		buildService("test1.com", wildcardIP, model.ProtocolHTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, model.ProtocolTCP, tnow),
		buildService("test3.com", wildcardIP, model.ProtocolHTTP, tnow.Add(2*time.Second)))
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
	listeners := buildOutboundListeners(p, nil, services...)
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

func TestInboundListenerConfig_HTTP(t *testing.T) {
	// Add a service and verify it's config
	testInboundListenerConfig(t,
		buildService("test.com", wildcardIP, model.ProtocolHTTP, tnow))
	testInboundListenerConfigWithoutServices(t)
	testInboundListenerConfigWithSidecar(t,
		buildService("test.com", wildcardIP, model.ProtocolHTTP, tnow))
	testInboundListenerConfigWithSidecarWithoutServices(t)
}

func TestOutboundListenerConfig_WithSidecar(t *testing.T) {
	// Add a service and verify it's config
	services := []*model.Service{
		buildService("test1.com", wildcardIP, model.ProtocolHTTP, tnow.Add(1*time.Second)),
		buildService("test2.com", wildcardIP, model.ProtocolTCP, tnow),
		buildService("test3.com", wildcardIP, model.ProtocolHTTP, tnow.Add(2*time.Second))}
	testOutboundListenerConfigWithSidecar(t, services...)
	testOutboundListenerConfigWithSidecarWithCaptureModeNone(t, services...)
}

func TestGetActualWildcardAndLocalHost(t *testing.T) {
	tests := []struct {
		name     string
		proxy    *model.Proxy
		expected [2]string
	}{
		{
			name: "ipv4 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1.1.1.1", "127.0.0.1", "2.2.2.2"},
			},
			expected: [2]string{WildcardAddress, LocalhostAddress},
		},
		{
			name: "ipv6 only",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "2222:3333::1"},
			},
			expected: [2]string{WildcardIPv6Address, LocalhostIPv6Address},
		},
		{
			name: "mixed ipv4 and ipv6",
			proxy: &model.Proxy{
				IPAddresses: []string{"1111:2222::1", "::1", "127.0.0.1", "2.2.2.2", "2222:3333::1"},
			},
			expected: [2]string{WildcardAddress, LocalhostAddress},
		},
	}
	for _, tt := range tests {
		wm, lh := getActualWildcardAndLocalHost(tt.proxy)
		if wm != tt.expected[0] && lh != tt.expected[1] {
			t.Errorf("Test %s failed, expected: %s / %s got: %s / %s", tt.name, tt.expected[0], tt.expected[1], wm, lh)
		}
	}
}

func testOutboundListenerConflict(t *testing.T, services ...*model.Service) {
	t.Helper()

	oldestService := getOldestService(services...)

	p := &fakePlugin{}
	listeners := buildOutboundListeners(p, nil, services...)
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

func testInboundListenerConfig(t *testing.T, services ...*model.Service) {
	t.Helper()
	oldestService := getOldestService(services...)
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, nil, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	oldestProtocol := oldestService.Ports[0].Protocol
	if oldestProtocol != model.ProtocolHTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if oldestProtocol == model.ProtocolHTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}
	verifyInboundHTTPListenerServerName(t, listeners[0])
	if isHTTPListener(listeners[0]) {
		verifyInboundHTTPListenerCertDetails(t, listeners[0])
	}
}

func testInboundListenerConfigWithoutServices(t *testing.T) {
	t.Helper()
	p := &fakePlugin{}
	listeners := buildInboundListeners(p, nil)
	if expected := 0; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}
}

func testInboundListenerConfigWithSidecar(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Number:   8080,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:80",
				},
			},
		},
	}
	listeners := buildInboundListeners(p, sidecarConfig, services...)
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}

	if !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}
}

func testInboundListenerConfigWithSidecarWithoutServices(t *testing.T) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo-without-service",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Number:   8080,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:            "1.1.1.1",
					DefaultEndpoint: "127.0.0.1:80",
				},
			},
		},
	}
	listeners := buildInboundListeners(p, sidecarConfig)
	if expected := 0; len(listeners) != expected {
		t.Fatalf("expected %d listeners, found %d", expected, len(listeners))
	}
}

func testOutboundListenerConfigWithSidecar(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   9000,
						Protocol: "HTTP",
						Name:     "uds",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/*"},
				},
				{
					Hosts: []string{"*/*"},
				},
			},
		},
	}
	listeners := buildOutboundListeners(p, sidecarConfig, services...)
	if len(listeners) != 2 {
		t.Fatalf("expected %d listeners, found %d", 2, len(listeners))
	}

	if isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener on port 8080, found HTTP")
	}
	if !isHTTPListener(listeners[1]) {
		t.Fatal("expected HTTP listener on port 9000, found TCP")
	}
}

func testOutboundListenerConfigWithSidecarWithCaptureModeNone(t *testing.T, services ...*model.Service) {
	t.Helper()
	p := &fakePlugin{}
	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					// Bind + Port
					CaptureMode: networking.CaptureMode_NONE,
					Port: &networking.Port{
						Number:   9090,
						Protocol: "HTTP",
						Name:     "grpc",
					},
					Bind:  "127.1.1.2",
					Hosts: []string{"*/*"},
				},
				{
					// Bind Only
					CaptureMode: networking.CaptureMode_NONE,
					Bind:        "127.1.1.2",
					Hosts:       []string{"*/*"},
				},
				{
					// Port Only
					CaptureMode: networking.CaptureMode_NONE,
					Port: &networking.Port{
						Number:   9090,
						Protocol: "HTTP",
						Name:     "grpc",
					},
					Hosts: []string{"*/*"},
				},
				{
					// None
					CaptureMode: networking.CaptureMode_NONE,
					Hosts:       []string{"*/*"},
				},
			},
		},
	}
	listeners := buildOutboundListeners(p, sidecarConfig, services...)
	if len(listeners) != 4 {
		t.Fatalf("expected %d listeners, found %d", 4, len(listeners))
	}

	expectedListeners := map[string]string{
		"127.1.1.2_9090": "HTTP",
		"127.1.1.2_8080": "TCP",
		"127.0.0.1_9090": "HTTP",
		"127.0.0.1_8080": "TCP",
	}

	for _, listener := range listeners {
		listenerName := listener.Name
		expectedListenerType := expectedListeners[listenerName]
		if expectedListenerType == "" {
			t.Fatalf("listener %s not expected", listenerName)
		}
		if expectedListenerType == "TCP" && isHTTPListener(listener) {
			t.Fatalf("expected TCP listener %s, but found HTTP", listenerName)
		}
		if expectedListenerType == "HTTP" && !isHTTPListener(listener) {
			t.Fatalf("expected HTTP listener %s, but found TCP", listenerName)
		}
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
	config, _ := xdsutil.MessageToStruct(f.GetTypedConfig())
	statPrefix := config.Fields["stat_prefix"].GetStringValue()
	if statPrefix != expectedStatPrefix {
		t.Fatalf("expected listener to contain stat_prefix %s, found %s", expectedStatPrefix, statPrefix)
	}
}

func verifyInboundHTTPListenerServerName(t *testing.T, l *xdsapi.Listener) {
	t.Helper()
	if len(l.FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
	}
	f := fc.Filters[0]
	expectedServerName := "istio-envoy"
	config, _ := xdsutil.MessageToStruct(f.GetTypedConfig())
	serverName := config.Fields["server_name"].GetStringValue()
	if serverName != expectedServerName {
		t.Fatalf("expected listener to contain server_name %s, found %s", expectedServerName, serverName)
	}
}

func verifyInboundHTTPListenerCertDetails(t *testing.T, l *xdsapi.Listener) {
	t.Helper()
	if len(l.FilterChains) != 1 {
		t.Fatalf("expected %d filter chains, found %d", 1, len(l.FilterChains))
	}
	fc := l.FilterChains[0]
	if len(fc.Filters) != 1 {
		t.Fatalf("expected %d filters, found %d", 1, len(fc.Filters))
	}
	f := fc.Filters[0]
	config, _ := xdsutil.MessageToStruct(f.GetTypedConfig())
	forwardDetails, expected := config.Fields["forward_client_cert_details"].GetStringValue(), "APPEND_FORWARD"
	if forwardDetails != expected {
		t.Fatalf("expected listener to contain forward_client_cert_details %s, found %s", expected, forwardDetails)
	}
	setDetails := config.Fields["set_current_client_cert_details"].GetStructValue()
	subject := setDetails.Fields["subject"].GetBoolValue()
	dns := setDetails.Fields["dns"].GetBoolValue()
	uri := setDetails.Fields["uri"].GetBoolValue()
	if !subject || !dns || !uri {
		t.Fatalf("expected listener to contain set_current_client_cert_details (subject: true, dns: true, uri: true), "+
			"found (subject: %t, dns: %t, uri %t)", subject, dns, uri)
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

func buildOutboundListeners(p plugin.Plugin, sidecarConfig *model.Config, services ...*model.Service) []*xdsapi.Listener {
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnv(services)

	if err := env.PushContext.InitContext(&env); err != nil {
		return nil
	}

	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}
	return configgen.buildSidecarOutboundListeners(&env, &proxy, env.PushContext, proxyInstances)
}

func buildInboundListeners(p plugin.Plugin, sidecarConfig *model.Config, services ...*model.Service) []*xdsapi.Listener {
	configgen := NewConfigGenerator([]plugin.Plugin{p})
	env := buildListenerEnv(services)
	if err := env.PushContext.InitContext(&env); err != nil {
		return nil
	}
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service:  s,
			Endpoint: buildEndpoint(s),
		}
	}
	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}
	return configgen.buildSidecarInboundListeners(&env, &proxy, env.PushContext, instances)
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

func (p *fakePlugin) OnOutboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
}

func (p *fakePlugin) OnInboundCluster(in *plugin.InputParams, cluster *xdsapi.Cluster) {
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
		Attributes: model.ServiceAttributes{
			Namespace: "default",
		},
	}
}

func buildEndpoint(service *model.Service) model.NetworkEndpoint {
	return model.NetworkEndpoint{
		ServicePort: service.Ports[0],
		Port:        8080,
	}
}

func buildListenerEnv(services []*model.Service) model.Environment {
	serviceDiscovery := new(fakes.ServiceDiscovery)
	serviceDiscovery.ServicesReturns(services, nil)

	configStore := &fakes.IstioConfigStore{}

	mesh := model.DefaultMeshConfig()
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             &mesh,
	}

	return env
}
