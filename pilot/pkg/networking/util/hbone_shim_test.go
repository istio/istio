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

func TestHBONEOriginationShimOptIn(t *testing.T) {
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
		{name: "sidecar", want: true, proxy: &model.Proxy{Type: model.SidecarProxy, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true}}},
		{name: "proxyless", proxy: &model.Proxy{Type: model.SidecarProxy, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true, Generator: "grpc"}}},
		{name: "waypoint", want: true, proxy: &model.Proxy{Type: model.Waypoint, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true}}},
		{name: "gateway", want: true, proxy: &model.Proxy{Type: model.Router, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true}}},
		{name: "sending disabled", proxy: &model.Proxy{
			Type: model.Waypoint, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true, DisableHBONESend: true},
		}},
		{name: "east west", proxy: &model.Proxy{
			Type: model.Waypoint, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true},
			Labels: map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) { assert.Equal(t, HBONEOriginationShimEnabled(tt.proxy), tt.want) })
	}
	test.SetForTest(t, &features.EnableHBONESend, false)
	for _, kind := range []model.NodeType{model.SidecarProxy, model.Router} {
		proxy := &model.Proxy{Type: kind, Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: true}}
		assert.Equal(t, HBONEOriginationShimEnabled(proxy), false)
	}
	test.SetForTest(t, &features.EnableHBONESend, true)
	for _, kind := range []model.NodeType{model.SidecarProxy, model.Router, model.Waypoint} {
		t.Run(string(kind)+" disabled", func(t *testing.T) {
			proxy := &model.Proxy{Type: kind, Metadata: &model.NodeMetadata{
				EnableHBONEOriginationShim: true, DisableHBONESend: true,
			}}
			assert.Equal(t, HBONEOriginationShimEnabled(proxy), false)
			proxy.Metadata.DisableHBONESend = false
			test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
			assert.Equal(t, HBONEOriginationShimEnabled(proxy), false)
		})
	}
}

func TestHBONEOriginationShimUnsupportedServices(t *testing.T) {
	assert.Equal(t, UseHBONEOriginationShim(true, nil, nil), false)
	assert.Equal(t, UseHBONEOriginationShim(false, &model.Service{}, nil), false)
	for _, resolution := range []model.Resolution{model.DNSLB, model.DNSRoundRobinLB, model.Passthrough, model.DynamicDNS} {
		assert.Equal(t, UseHBONEOriginationShim(true, &model.Service{Resolution: resolution}, nil), false)
	}
	// Unknown future TLS modes must not silently lose application encryption.
	assert.Equal(t, UseHBONEOriginationShim(true, &model.Service{}, &networking.TrafficPolicy{
		Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_TLSmode(99)},
	}), false)
}
