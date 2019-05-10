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

	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/golang/protobuf/ptypes"

	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	mccpb "istio.io/istio/pilot/pkg/networking/plugin/mixer/client"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schemas"
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

type fakeStore struct {
	model.ConfigStore
	cfg map[string][]model.Config
	err error
}

func (l *fakeStore) List(typ, namespace string) ([]model.Config, error) {
	ret := l.cfg[typ]
	return ret, l.err
}

func TestModifyOutboundRouteConfig(t *testing.T) {
	ns := "ns3"
	l := &fakeStore{
		cfg: map[string][]model.Config{
			schemas.QuotaSpecBinding.Type: {
				{
					ConfigMeta: model.ConfigMeta{
						Namespace: ns,
						Domain:    "cluster.local",
					},
					Spec: &mccpb.QuotaSpecBinding{
						Services: []*mccpb.IstioService{
							{
								Name:      "svc",
								Namespace: ns,
							},
						},
						QuotaSpecs: []*mccpb.QuotaSpecBinding_QuotaSpecReference{
							{
								Name: "request-count",
							},
						},
					},
				},
			},
			schemas.QuotaSpec.Type: {
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "request-count",
						Namespace: ns,
					},
					Spec: &mccpb.QuotaSpec{
						Rules: []*mccpb.QuotaRule{
							{
								Quotas: []*mccpb.Quota{
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
	svc := model.Service{
		Hostname: "svc.ns3",
		Attributes: model.ServiceAttributes{
			Name:      "svc",
			Namespace: ns,
			UID:       "istio://ns3/services/svc",
		},
	}
	cases := []struct {
		serviceByHostnameAndNamespace map[host.Name]map[string]*model.Service
		env                           *model.Environment
		node                          *model.Proxy
		httpRoute                     route.Route
		quotaSpec                     []*mccpb.QuotaSpec
	}{
		{
			env: &model.Environment{
				IstioConfigStore: ii,
				Mesh:             &mesh,
			},
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					PolicyCheck: "enable",
				},
			},
			httpRoute: route.Route{
				Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
				Action: &route.Route_Route{Route: &route.RouteAction{
					ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "outbound|||svc.ns3.svc.cluster.local"},
				}}},
			serviceByHostnameAndNamespace: map[host.Name]map[string]*model.Service{
				host.Name("svc.ns3"): {
					"ns3": &svc,
				},
			},
			quotaSpec: []*mccpb.QuotaSpec{{
				Rules: []*mccpb.QuotaRule{{Quotas: []*mccpb.Quota{{Quota: "requestcount", Charge: 100}}}},
			}},
		},
		{
			env: &model.Environment{
				IstioConfigStore: ii,
				Mesh:             &mesh,
			},
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					PolicyCheck: "enable",
				},
			},
			httpRoute: route.Route{
				Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
				Action: &route.Route_Route{Route: &route.RouteAction{
					ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "outbound|||a.ns3.svc.cluster.local"},
				}}},
			serviceByHostnameAndNamespace: map[host.Name]map[string]*model.Service{
				host.Name("a.ns3"): {
					"ns3": &svc,
				},
			},
		},
	}
	for _, c := range cases {
		push := &model.PushContext{
			ServiceByHostnameAndNamespace: c.serviceByHostnameAndNamespace,
		}
		in := plugin.InputParams{
			Env:  c.env,
			Node: c.node,
		}
		tc := modifyOutboundRouteConfig(push, &in, "", &c.httpRoute)

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
