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

package v1alpha3

import (
	"errors"
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/tests/fuzz/utils"
)

func init() {
	testing.Init()
}

func ValidateTestOptions(to TestOptions) error {
	for _, csc := range to.ConfigStoreCaches {
		if csc == nil {
			return errors.New("a ConfigStoreController was nil")
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
	listeners := NewListenerBuilder(proxy, cg.env.PushContext).buildSidecarOutboundListeners(cg.SetupProxy(proxy), cg.env.PushContext)
	//listeners := cg.ConfigGen.buildSidecarOutboundListeners(p, cg.env.PushContext)
	_ = listeners
	return 1
}

func proxyValid(p *model.Proxy) bool {
	if len(p.IPAddresses) == 0 {
		return false
	}
	return true
}
