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
	"strings"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

type LdsEnv struct {
	configgen *ConfigGeneratorImpl
}

func getDefaultLdsEnv() *LdsEnv {
	listenerEnv := LdsEnv{
		configgen: NewConfigGenerator([]plugin.Plugin{&fakePlugin{}}),
	}
	return &listenerEnv
}

func getDefaultProxy() model.Proxy {
	return model.Proxy{
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
}

func setNilSidecarOnProxy(proxy *model.Proxy, pushContext *model.PushContext) {
	proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(pushContext, "not-default")
}

func TestListenerBuilder(t *testing.T) {
	// prepare
	t.Helper()
	ldsEnv := getDefaultLdsEnv()
	service := buildService("test.com", wildcardIP, model.ProtocolHTTP, tnow)
	services := []*model.Service{service}

	env := buildListenerEnv(services)

	if err := env.PushContext.InitContext(&env); err != nil {
		t.Fatalf("init push context error: %s", err.Error())
	}
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service:  s,
			Endpoint: buildEndpoint(s),
		}
	}
	proxy := getDefaultProxy()
	setNilSidecarOnProxy(&proxy, env.PushContext)

	builder := NewListenerBuilder(&proxy)
	listeners := builder.buildSidecarInboundListeners(ldsEnv.configgen, &env, &proxy, env.PushContext, instances).
		getListeners()

	// the listener for app
	if len(listeners) != 1 {
		t.Fatalf("expected %d listeners, found %d", 1, len(listeners))
	}
	protocol := service.Ports[0].Protocol
	if protocol != model.ProtocolHTTP && isHTTPListener(listeners[0]) {
		t.Fatal("expected TCP listener, found HTTP")
	} else if protocol == model.ProtocolHTTP && !isHTTPListener(listeners[0]) {
		t.Fatal("expected HTTP listener, found TCP")
	}
	verifyInboundHTTPListenerServerName(t, listeners[0])
	if isHTTPListener(listeners[0]) {
		verifyInboundHTTPListenerCertDetails(t, listeners[0])
	}

	verifyInboundEnvoyListenerNumber(t, listeners[0])
}

func TestVirtualListenerBuilder(t *testing.T) {
	// prepare
	t.Helper()
	ldsEnv := getDefaultLdsEnv()
	service := buildService("test.com", wildcardIP, model.ProtocolHTTP, tnow)
	services := []*model.Service{service}

	env := buildListenerEnv(services)
	if err := env.PushContext.InitContext(&env); err != nil {
		t.Fatalf("init push context error: %s", err.Error())
	}
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service:  s,
			Endpoint: buildEndpoint(s),
		}
	}
	proxy := getDefaultProxy()
	setNilSidecarOnProxy(&proxy, env.PushContext)

	builder := NewListenerBuilder(&proxy)
	listeners := builder.buildSidecarInboundListeners(ldsEnv.configgen, &env, &proxy, env.PushContext, instances).
		buildVirtualListener(&env, &proxy).
		getListeners()

	// app port listener and virtual inbound listener
	if len(listeners) != 2 {
		t.Fatalf("expected %d listeners, found %d", 2, len(listeners))
	}

	// The rest attributes are verified in other tests
	verifyInboundHTTPListenerServerName(t, listeners[0])

	if !strings.HasPrefix(listeners[1].Name, VirtualListenerName) {
		t.Fatalf("expect virtual listener, found %s", listeners[1].Name)
	} else {
		t.Logf("found virtual listener: %s", listeners[1].Name)
	}

}

func setInboundCaptureAllOnThisNode(proxy *model.Proxy) {
	proxy.Metadata[model.NodeMetadataInterceptionMode] = "REDIRECT"
	proxy.Metadata[model.IstioInboundCapturePorts] = model.AllPortsLiteral
}

func TestVirtualInboundListenerBuilder(t *testing.T) {

	// prepare
	t.Helper()
	ldsEnv := getDefaultLdsEnv()
	service := buildService("test.com", wildcardIP, model.ProtocolHTTP, tnow)
	services := []*model.Service{service}

	env := buildListenerEnv(services)
	if err := env.PushContext.InitContext(&env); err != nil {
		t.Fatalf("init push context error: %s", err.Error())
	}
	instances := make([]*model.ServiceInstance, len(services))
	for i, s := range services {
		instances[i] = &model.ServiceInstance{
			Service:  s,
			Endpoint: buildEndpoint(s),
		}
	}

	proxy := getDefaultProxy()
	setInboundCaptureAllOnThisNode(&proxy)
	setNilSidecarOnProxy(&proxy, env.PushContext)

	builder := NewListenerBuilder(&proxy)
	listeners := builder.buildSidecarInboundListeners(ldsEnv.configgen, &env, &proxy, env.PushContext, instances).
		buildVirtualListener(&env, &proxy).
		buildVirtualInboundListener(&env, &proxy).
		getListeners()

	// app port listener and virtual inbound listener
	if len(listeners) != 3 {
		t.Fatalf("expected %d listeners, found %d", 3, len(listeners))
	}

	// The rest attributes are verified in other tests
	verifyInboundHTTPListenerServerName(t, listeners[0])

	if !strings.HasPrefix(listeners[1].Name, VirtualListenerName) {
		t.Fatalf("expect virtual listener, found %s", listeners[1].Name)
	} else {
		t.Logf("found virtual listener: %s", listeners[1].Name)
	}

	if !strings.HasPrefix(listeners[2].Name, VirtualInboundListenerName) {
		t.Fatalf("expect virtual listener, found %s", listeners[2].Name)
	} else {
		t.Logf("found virtual inbound listener: %s", listeners[2].Name)
	}
}
