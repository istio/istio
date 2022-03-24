//go:build gofuzz
// +build gofuzz

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

// nolint: golint
package v1alpha3

import (
	"errors"
	"fmt"
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/tests/fuzz/utils"
)

func init() {
	testing.Init()
}

func ValidateTestOptions(to TestOptions) error {
	for _, plugin := range to.Plugins {
		if plugin == nil {
			return errors.New("a Plugin was nil")
		}
	}
	for _, csc := range to.ConfigStoreCaches {
		if csc == nil {
			return errors.New("a ConfigStoreCache was nil")
		}
	}
	for _, sr := range to.ServiceRegistries {
		if sr == nil {
			return errors.New("a ServiceRegistry was nil")
		}
	}
	return nil
}

func InternalFuzzbuildGatewayListeners(data []byte) int {
	proxy := &model.Proxy{}
	f := fuzz.NewConsumer(data)
	to := TestOptions{}
	err := f.GenerateStruct(&to)
	if err != nil {
		return 0
	}
	err = ValidateTestOptions(to)
	if err != nil {
		return 0
	}
	err = f.GenerateStruct(proxy)
	if err != nil {
		return 0
	}
	if !proxyValid(proxy) {
		return 0
	}
	lb := &ListenerBuilder{}
	err = f.GenerateStruct(lb)
	if err != nil {
		return 0
	}
	cg := NewConfigGenTest(utils.NopTester{}, to)
	lb.node = cg.SetupProxy(proxy)
	lb.push = cg.PushContext()
	_ = cg.ConfigGen.buildGatewayListeners(lb)
	return 1
}

func InternalFuzzbuildSidecarOutboundHTTPRouteConfig(data []byte) int {
	proxy := &model.Proxy{}
	f := fuzz.NewConsumer(data)
	to := TestOptions{}
	err := f.GenerateStruct(&to)
	if err != nil {
		return 0
	}
	err = ValidateTestOptions(to)
	if err != nil {
		return 0
	}
	err = f.GenerateStruct(proxy)
	if err != nil {
		return 0
	}
	if !proxyValid(proxy) {
		return 0
	}
	req := &model.PushRequest{}
	err = f.GenerateStruct(req)
	if err != nil {
		return 0
	}
	cg := NewConfigGenTest(utils.NopTester{}, to)
	req.Push = cg.PushContext()

	vHostCache := make(map[int][]*route.VirtualHost)

	_, _ = cg.ConfigGen.buildSidecarOutboundHTTPRouteConfig(
		cg.SetupProxy(proxy), req, "80", vHostCache, nil, nil)
	return 1
}

func InternalFuzzbuildSidecarInboundListeners(data []byte) int {
	f := fuzz.NewConsumer(data)
	proxy := model.Proxy{}
	err := f.GenerateStruct(&proxy)
	if err != nil {
		return 0
	}
	if !proxyValid(proxy) {
		return 0
	}

	// create fuzzed plugins
	number, err := f.GetInt()
	if err != nil {
		return 0
	}
	maxPlugins := number % 20
	if maxPlugins == 0 {
		return 0
	}
	allPlugins := make([]plugin.Plugin, maxPlugins)
	for i := 0; i < maxPlugins; i++ {
		p := &fakePlugin{}
		err = f.GenerateStruct(p)
		if err != nil {
			return 0
		}
		allPlugins = append(allPlugins, p)
	}
	cg := NewConfigGenerator(allPlugins, &model.DisabledCache{})

	// create services
	number, err = f.GetInt()
	if err != nil {
		return 0
	}
	maxServices := number % 20
	if maxServices == 0 {
		return 0
	}
	allServices := make([]*model.Service, 0, maxServices)
	for i := 0; i < maxServices; i++ {
		s := &model.Service{}
		err = f.GenerateStruct(s)
		if err != nil {
			return 0
		}
		if len(s.Ports) == 0 {
			continue
		}
		allServices = append(allServices, s)
	}
	if len(allServices) == 0 {
		return 0
	}
	env := buildListenerEnv(allServices)
	if err := env.PushContext.InitContext(env, nil, nil); err != nil {
		return 0
	}
	proxy.SetServiceInstances(env)
	proxy.IstioVersion = model.ParseIstioVersion(proxy.Metadata.IstioVersion)
	proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")

	fmt.Println("Calling our target:")
	listeners := cg.buildSidecarInboundListeners(&proxy, env.PushContext)
	_ = listeners
	return 1
}

func InternalFuzzbuildSidecarOutboundListeners(data []byte) int {
	t := utils.NopTester{}
	proxy := &model.Proxy{}
	f := fuzz.NewConsumer(data)
	to := TestOptions{}
	err := f.GenerateStruct(&to)
	if err != nil {
		return 0
	}
	err = ValidateTestOptions(to)
	if err != nil {
		return 0
	}
	err = f.GenerateStruct(proxy)
	if err != nil {
		return 0
	}
	if !proxyValid(proxy) {
		return 0
	}
	cg := NewConfigGenTest(t, to)
	p := cg.SetupProxy(proxy)
	listeners := cg.ConfigGen.buildSidecarOutboundListeners(p, cg.env.PushContext)
	_ = listeners
	return 1
}

func proxyValid(p *model.Proxy) bool {
	if len(p.IPAddresses) == 0 {
		return false
	}
	return true
}
