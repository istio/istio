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

package util

import (
	"testing"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestHBONEConnectWaitOptIn(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	for _, tt := range []struct {
		name  string
		proxy *model.Proxy
		want  bool
	}{
		{name: "nil"},
		{name: "no metadata", proxy: &model.Proxy{Type: model.Waypoint}},
		{name: "stock", proxy: &model.Proxy{Type: model.Waypoint, Metadata: &model.NodeMetadata{}}},
		{name: "sidecar", want: true, proxy: &model.Proxy{Type: model.SidecarProxy, Metadata: &model.NodeMetadata{EnableHBONEConnectWait: true}}},
		{name: "proxyless", proxy: &model.Proxy{Type: model.SidecarProxy, Metadata: &model.NodeMetadata{EnableHBONEConnectWait: true, Generator: "grpc"}}},
		{name: "waypoint", want: true, proxy: &model.Proxy{Type: model.Waypoint, Metadata: &model.NodeMetadata{EnableHBONEConnectWait: true}}},
		{name: "gateway", want: true, proxy: &model.Proxy{Type: model.Router, Metadata: &model.NodeMetadata{EnableHBONEConnectWait: true}}},
		{name: "sending disabled", proxy: &model.Proxy{
			Type: model.Waypoint, Metadata: &model.NodeMetadata{EnableHBONEConnectWait: true, DisableHBONESend: true},
		}},
		{name: "east west", proxy: &model.Proxy{
			Type: model.Waypoint, Metadata: &model.NodeMetadata{EnableHBONEConnectWait: true},
			Labels: map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) { assert.Equal(t, HBONEConnectWaitEnabled(tt.proxy), tt.want) })
	}
	test.SetForTest(t, &features.EnableHBONESend, false)
	for _, kind := range []model.NodeType{model.SidecarProxy, model.Router} {
		proxy := &model.Proxy{Type: kind, Metadata: &model.NodeMetadata{EnableHBONEConnectWait: true}}
		assert.Equal(t, HBONEConnectWaitEnabled(proxy), false)
	}
	test.SetForTest(t, &features.EnableHBONESend, true)
	for _, kind := range []model.NodeType{model.SidecarProxy, model.Router, model.Waypoint} {
		t.Run(string(kind)+" disabled", func(t *testing.T) {
			proxy := &model.Proxy{Type: kind, Metadata: &model.NodeMetadata{
				EnableHBONEConnectWait: true, DisableHBONESend: true,
			}}
			assert.Equal(t, HBONEConnectWaitEnabled(proxy), false)
			proxy.Metadata.DisableHBONESend = false
			test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			assert.Equal(t, HBONEConnectWaitEnabled(proxy), false)
		})
	}
}

func TestHBONEConnectWaitUnsupportedServices(t *testing.T) {
	assert.Equal(t, UseHBONEConnectWait(true, nil, nil), false)
	assert.Equal(t, UseHBONEConnectWait(false, &model.Service{}, nil), false)
	for _, resolution := range []model.Resolution{model.DNSLB, model.DNSRoundRobinLB, model.Passthrough, model.DynamicDNS} {
		assert.Equal(t, UseHBONEConnectWait(true, &model.Service{Resolution: resolution}, nil), false)
	}
	// Unknown future TLS modes must not silently lose application encryption.
	assert.Equal(t, UseHBONEConnectWait(true, &model.Service{}, &networking.TrafficPolicy{
		Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_TLSmode(99)},
	}), false)
}
