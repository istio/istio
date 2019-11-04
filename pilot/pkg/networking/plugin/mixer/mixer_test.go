// Copyright 2019 Istio Authors
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

package mixer

import (
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	meshconfig "istio.io/api/mesh/v1alpha1"
	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pkg/config/mesh"
)

func TestTransportConfig(t *testing.T) {
	cases := []struct {
		mesh   meshconfig.MeshConfig
		node   model.Proxy
		expect *mccpb.NetworkFailPolicy
	}{
		{
			// defaults set
			mesh: mesh.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_CLOSE,
				MaxRetry:      defaultRetries,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
		{
			// retry and retry times set
			mesh: mesh.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{
					annotation.PolicyCheckRetries.Name:           "5",
					annotation.PolicyCheckBaseRetryWaitTime.Name: "1m",
					annotation.PolicyCheckMaxRetryWaitTime.Name:  "1.5s",
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_CLOSE,
				MaxRetry:      5,
				BaseRetryWait: types.DurationProto(1 * time.Minute),
				MaxRetryWait:  types.DurationProto(1500 * time.Millisecond),
			},
		},
		{
			// just retry amount set
			mesh: mesh.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{
					annotation.PolicyCheckRetries.Name: "1",
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_CLOSE,
				MaxRetry:      1,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
		{
			// fail open from node metadata
			mesh: mesh.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{
					annotation.PolicyCheck.Name: policyCheckDisable,
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_OPEN,
				MaxRetry:      defaultRetries,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
	}
	for _, c := range cases {
		tc := buildTransport(&c.mesh, &c.node)
		if !reflect.DeepEqual(tc.NetworkFailPolicy, c.expect) {
			t.Errorf("got %v, expected %v", tc.NetworkFailPolicy, c.expect)
		}
	}
}

func Test_proxyVersionToString(t *testing.T) {
	type args struct {
		ver *model.IstioVersion
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "major.minor.patch",
			args: args{ver: &model.IstioVersion{Major: 1, Minor: 2, Patch: 0}},
			want: "1.2.0",
		},
		{
			name: "max",
			args: args{ver: model.MaxIstioVersion},
			want: "65535.65535.65535",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proxyVersionToString(tt.args.ver); got != tt.want {
				t.Errorf("proxyVersionToString(ver) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnOutboundListener(t *testing.T) {
	mp := mixerplugin{}
	inputParams := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolTCP,
		Env: &model.Environment{
			Mesh: &meshconfig.MeshConfig{
				MixerReportServer: "mixer.istio-system",
			},
		},
		Node: &model.Proxy{
			ID:       "foo.bar",
			Metadata: &model.NodeMetadata{},
		},
	}
	tests := []struct {
		name           string
		sidecarScope   *model.SidecarScope
		mutableObjects *plugin.MutableObjects
		hostname       string
	}{
		{
			name: "Registry_Only",
			sidecarScope: &model.SidecarScope{
				OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
					Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
				},
			},
			mutableObjects: &plugin.MutableObjects{
				Listener: &xdsapi.Listener{},
				FilterChains: []plugin.FilterChain{
					{
						IsFallThrough: false,
					},
					{
						IsFallThrough: true,
					},
				},
			},
			hostname: "BlackHoleCluster",
		},
		{
			name: "Allow_Any",
			sidecarScope: &model.SidecarScope{
				OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
					Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
				},
			},
			mutableObjects: &plugin.MutableObjects{
				Listener: &xdsapi.Listener{},
				FilterChains: []plugin.FilterChain{
					{
						IsFallThrough: false,
					},
					{
						IsFallThrough: true,
					},
				},
			},
			hostname: "PassthroughCluster",
		},
	}
	for idx := range tests {
		t.Run(tests[idx].name, func(t *testing.T) {
			inputParams.Node.SidecarScope = tests[idx].sidecarScope
			mp.OnOutboundListener(inputParams, tests[idx].mutableObjects)
			for i := 0; i < len(tests[idx].mutableObjects.FilterChains); i++ {
				if len(tests[idx].mutableObjects.FilterChains[i].TCP) != 1 {
					t.Errorf("Expected 1 TCP filter")
				}
				var tcpClientConfig mccpb.TcpClientConfig
				cfg := tests[idx].mutableObjects.FilterChains[i].TCP[0].GetTypedConfig()
				ptypes.UnmarshalAny(cfg, &tcpClientConfig)
				if tests[idx].mutableObjects.FilterChains[i].IsFallThrough {
					hostAttr := tcpClientConfig.MixerAttributes.Attributes["destination.service.host"]
					if !reflect.DeepEqual(hostAttr, attrStringValue(tests[idx].hostname)) {
						t.Errorf("Expected host %s but got %+v\n",
							tests[idx].hostname, hostAttr)
					}

					nameAttr := tcpClientConfig.MixerAttributes.Attributes["destination.service.name"]
					if !reflect.DeepEqual(nameAttr, attrStringValue(tests[idx].hostname)) {
						t.Errorf("Expected name %s but got %+v\n",
							tests[idx].hostname, nameAttr)
					}
				}
			}
		})
	}
}
