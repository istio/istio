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
	"testing"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/fuzz"
)

func FuzzBuildGatewayListeners(f *testing.F) {
	f.Fuzz(func(t *testing.T, patchCount int, hostname string, data []byte) {
		fg := fuzz.New(t, data)
		proxy := fuzz.Struct[*model.Proxy](fg)
		to := fuzz.Struct[TestOptions](fg)
		lb := fuzz.Struct[*ListenerBuilder](fg)
		cg := NewConfigGenTest(t, to)
		lb.node = cg.SetupProxy(proxy)
		lb.push = cg.PushContext()
		cg.ConfigGen.buildGatewayListeners(lb)
	})
}

func FuzzBuildSidecarOutboundHTTPRouteConfig(f *testing.F) {
	f.Fuzz(func(t *testing.T, patchCount int, hostname string, data []byte) {
		fg := fuzz.New(t, data)
		proxy := fuzz.Struct[*model.Proxy](fg)
		to := fuzz.Struct[TestOptions](fg)
		cg := NewConfigGenTest(t, to)
		req := fuzz.Struct[*model.PushRequest](fg)
		req.Push = cg.PushContext()
		vHostCache := make(map[int][]*route.VirtualHost)
		cg.ConfigGen.buildSidecarOutboundHTTPRouteConfig(cg.SetupProxy(proxy), req, "80", vHostCache, nil, nil)
	})
}

func FuzzBuildSidecarOutboundListeners(f *testing.F) {
	f.Fuzz(func(t *testing.T, patchCount int, hostname string, data []byte) {
		fg := fuzz.New(t, data)
		proxy := fuzz.Struct[*model.Proxy](fg)
		to := fuzz.Struct[TestOptions](fg)
		cg := NewConfigGenTest(t, to)
		req := fuzz.Struct[*model.PushRequest](fg)
		req.Push = cg.PushContext()
		NewListenerBuilder(proxy, cg.env.PushContext).buildSidecarOutboundListeners(cg.SetupProxy(proxy), cg.env.PushContext)
	})
}
