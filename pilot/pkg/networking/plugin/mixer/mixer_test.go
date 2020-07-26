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

package mixer

import (
	"reflect"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/ptypes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mixerClient "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/plugin"
	mccpb "istio.io/istio/pilot/pkg/networking/plugin/mixer/client"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
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
			node: model.Proxy{Metadata: &model.NodeMetadata{}},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.NetworkFailPolicy_FAIL_CLOSE,
				MaxRetry:      defaultRetries,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
		{
			// retry and retry times set
			mesh: mesh.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: &model.NodeMetadata{
					PolicyCheckRetries:           "5",
					PolicyCheckBaseRetryWaitTime: "1m",
					PolicyCheckMaxRetryWaitTime:  "1.5s",
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.NetworkFailPolicy_FAIL_CLOSE,
				MaxRetry:      5,
				BaseRetryWait: ptypes.DurationProto(1 * time.Minute),
				MaxRetryWait:  ptypes.DurationProto(1500 * time.Millisecond),
			},
		},
		{
			// just retry amount set
			mesh: mesh.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: &model.NodeMetadata{
					PolicyCheckRetries: "1",
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.NetworkFailPolicy_FAIL_CLOSE,
				MaxRetry:      1,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
		{
			// fail open from node metadata
			mesh: mesh.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: &model.NodeMetadata{
					PolicyCheck: policyCheckDisable,
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.NetworkFailPolicy_FAIL_OPEN,
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
	meshConfig := &meshconfig.MeshConfig{
		MixerReportServer: "mixer.istio-system",
	}
	inputParams := &plugin.InputParams{
		ListenerProtocol: istionetworking.ListenerProtocolTCP,
		Push: &model.PushContext{
			Mesh: meshConfig,
		},
		Node: &model.Proxy{
			ID:       "foo.bar",
			Metadata: &model.NodeMetadata{},
		},
	}
	tests := []struct {
		name           string
		sidecarScope   *model.SidecarScope
		mutableObjects *istionetworking.MutableObjects
		hostname       string
	}{
		{
			name: "Registry_Only",
			sidecarScope: &model.SidecarScope{
				OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
					Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
				},
			},
			mutableObjects: &istionetworking.MutableObjects{
				Listener: &listener.Listener{},
				FilterChains: []istionetworking.FilterChain{
					{},
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
			mutableObjects: &istionetworking.MutableObjects{
				Listener: &listener.Listener{},
				FilterChains: []istionetworking.FilterChain{
					{},
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
			}
		})
	}
}

func TestOnOutboundListenerSkipMixer(t *testing.T) {
	mp := mixerplugin{}

	testcases := []struct {
		name        string
		meshconfig  *meshconfig.MeshConfig
		nodeType    model.NodeType
		wantFilters int
	}{
		{"both disabled", &meshconfig.MeshConfig{DisablePolicyChecks: true, DisableMixerHttpReports: true}, model.Router, 0},
		{"only check disabled", &meshconfig.MeshConfig{DisablePolicyChecks: true, DisableMixerHttpReports: false}, model.Router, 1},
		{"router only report disabled", &meshconfig.MeshConfig{DisablePolicyChecks: false, DisableMixerHttpReports: true}, model.Router, 1},
		// no client side checks, so sidecar only + report disabled should mean no mixer filter
		{"sidecar only report disabled", &meshconfig.MeshConfig{DisablePolicyChecks: false, DisableMixerHttpReports: true}, model.SidecarProxy, 0},
		{"both enabled", &meshconfig.MeshConfig{DisablePolicyChecks: false, DisableMixerHttpReports: false}, model.SidecarProxy, 1},
	}

	for _, v := range testcases {
		t.Run(v.name, func(tt *testing.T) {
			mcfg := v.meshconfig
			mcfg.MixerCheckServer = "mixer"
			mcfg.MixerReportServer = "mixer"
			inputParams := &plugin.InputParams{
				ListenerProtocol: istionetworking.ListenerProtocolHTTP,
				Push: &model.PushContext{
					Mesh: v.meshconfig,
				},
				Node: &model.Proxy{
					ID:       "foo.bar",
					Type:     v.nodeType,
					Metadata: &model.NodeMetadata{},
				},
			}
			mutable := &istionetworking.MutableObjects{Listener: &listener.Listener{}, FilterChains: []istionetworking.FilterChain{{}}}
			_ = mp.OnOutboundListener(inputParams, mutable)
			for _, chain := range mutable.FilterChains {
				if got := len(chain.HTTP); got != v.wantFilters {
					tt.Errorf("Got %d HTTP filters; wanted %d", got, v.wantFilters)
				}
			}
		})
	}
}

func Test_attrNamespace(t *testing.T) {
	testcases := []struct {
		name      string
		nodeID    string
		namespace string
	}{
		{"standard pod name", "foo.bar", "bar"},
		{"pod name with dot", "foo.123.bar", "bar"},
	}
	for _, v := range testcases {
		t.Run(v.name, func(t *testing.T) {
			node := model.Proxy{ID: v.nodeID}
			ns := attrNamespace(&node).GetStringValue()
			if ns != v.namespace {
				t.Errorf("%s: expecting %v but got %v", v.name, ns, v.namespace)
			}
		})
	}
}

func TestOnInboundListenerSkipMixer(t *testing.T) {
	mp := mixerplugin{}

	testcases := []struct {
		name        string
		meshconfig  *meshconfig.MeshConfig
		nodeType    model.NodeType
		wantFilters int
	}{
		{"both disabled", &meshconfig.MeshConfig{DisablePolicyChecks: true, DisableMixerHttpReports: true}, model.Router, 0},
		{"only check disabled", &meshconfig.MeshConfig{DisablePolicyChecks: true, DisableMixerHttpReports: false}, model.Router, 1},
		{"router only report disabled", &meshconfig.MeshConfig{DisablePolicyChecks: false, DisableMixerHttpReports: true}, model.Router, 1},
		{"sidecar only report disabled", &meshconfig.MeshConfig{DisablePolicyChecks: false, DisableMixerHttpReports: true}, model.SidecarProxy, 1},
		{"both enabled", &meshconfig.MeshConfig{DisablePolicyChecks: false, DisableMixerHttpReports: false}, model.SidecarProxy, 1},
	}

	for _, v := range testcases {
		t.Run(v.name, func(tt *testing.T) {
			mcfg := v.meshconfig
			mcfg.MixerCheckServer = "mixer"
			mcfg.MixerReportServer = "mixer"
			inputParams := &plugin.InputParams{
				ListenerProtocol: istionetworking.ListenerProtocolHTTP,
				Push: &model.PushContext{
					Mesh: v.meshconfig,
				},
				Node: &model.Proxy{
					ID:       "foo.bar",
					Type:     v.nodeType,
					Metadata: &model.NodeMetadata{},
				},
			}
			mutable := &istionetworking.MutableObjects{Listener: &listener.Listener{Address: testAddress()}, FilterChains: []istionetworking.FilterChain{{}}}
			_ = mp.OnInboundListener(inputParams, mutable)
			for _, chain := range mutable.FilterChains {
				if got := len(chain.HTTP); got != v.wantFilters {
					tt.Errorf("Got %d HTTP filters; wanted %d", got, v.wantFilters)
				}
			}
		})
	}
}

func testAddress() *core.Address {
	return &core.Address{Address: &core.Address_SocketAddress{SocketAddress: &core.SocketAddress{
		Address:       "127.0.0.1",
		PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(9090)}}}}
}

type fakeStore struct {
	model.ConfigStore
	cfg map[resource.GroupVersionKind][]model.Config
	err error
}

func (l *fakeStore) List(typ resource.GroupVersionKind, namespace string) ([]model.Config, error) {
	ret := l.cfg[typ]
	return ret, l.err
}

func (l *fakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func TestModifyOutboundRouteConfig(t *testing.T) {
	ns := "ns3"
	l := &fakeStore{
		cfg: map[resource.GroupVersionKind][]model.Config{
			gvk.QuotaSpecBinding: {
				{
					ConfigMeta: model.ConfigMeta{
						Namespace: ns,
						Domain:    "cluster.local",
					},
					Spec: &mixerClient.QuotaSpecBinding{
						Services: []*mixerClient.IstioService{
							{
								Name:      "svc",
								Namespace: ns,
							},
						},
						QuotaSpecs: []*mixerClient.QuotaSpecBinding_QuotaSpecReference{
							{
								Name: "request-count",
							},
						},
					},
				},
			},
			gvk.QuotaSpec: {
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "request-count",
						Namespace: ns,
					},
					Spec: &mixerClient.QuotaSpec{
						Rules: []*mixerClient.QuotaRule{
							{
								Quotas: []*mixerClient.Quota{
									{
										Quota:  "requestcount",
										Charge: 100,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	ii := model.MakeIstioStore(l)
	mesh := mesh.DefaultMeshConfig()
	cases := []struct {
		push      *model.PushContext
		node      *model.Proxy
		httpRoute *route.Route
		quotaSpec []*mccpb.QuotaSpec
	}{
		{
			push: &model.PushContext{
				IstioConfigStore: ii,
				Mesh:             &mesh,
			},
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					PolicyCheck: "enable",
				},
			},
			httpRoute: &route.Route{
				Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
				Action: &route.Route_Route{Route: &route.RouteAction{
					ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "outbound|||svc.ns3.svc.cluster.local"},
				}}},
			quotaSpec: []*mccpb.QuotaSpec{{
				Rules: []*mccpb.QuotaRule{{Quotas: []*mccpb.Quota{{Quota: "requestcount", Charge: 100}}}},
			}},
		},
		{
			push: &model.PushContext{
				IstioConfigStore: ii,
				Mesh:             &mesh,
			},
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					PolicyCheck: "enable",
				},
			},
			httpRoute: &route.Route{
				Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
				Action: &route.Route_Route{Route: &route.RouteAction{
					ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "outbound|||a.ns3.svc.cluster.local"},
				}}},
		},
	}
	for _, c := range cases {
		c.node.SetSidecarScope(c.push)
		in := plugin.InputParams{
			Push: c.push,
			Node: c.node,
		}
		tc := modifyOutboundRouteConfig(&in, "", c.httpRoute)

		mixerSvcConfigAny := tc.TypedPerFilterConfig["mixer"]
		mixerSvcConfig := &mccpb.ServiceConfig{}
		err := ptypes.UnmarshalAny(mixerSvcConfigAny, mixerSvcConfig)
		if err != nil {
			t.Errorf("got err %v", err)
		}
		if !reflect.DeepEqual(mixerSvcConfig.QuotaSpec, c.quotaSpec) {
			t.Errorf("got %v, expected %v", mixerSvcConfig.QuotaSpec, c.quotaSpec)
		}
	}
}
