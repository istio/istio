// Copyright 2017 Istio Authors
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

package envoy

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"sort"
	"testing"

	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
	"istio.io/manager/proxy"
	"istio.io/manager/test/mock"
	"istio.io/manager/test/util"
)

func TestRoutesByPath(t *testing.T) {
	cases := []struct {
		in       []*HTTPRoute
		expected []*HTTPRoute
	}{

		// Case 2: Prefix before path
		{
			in: []*HTTPRoute{
				{Prefix: "/api"},
				{Path: "/api/v1"},
			},
			expected: []*HTTPRoute{
				{Path: "/api/v1"},
				{Prefix: "/api"},
			},
		},

		// Case 3: Longer prefix before shorter prefix
		{
			in: []*HTTPRoute{
				{Prefix: "/api"},
				{Prefix: "/api/v1"},
			},
			expected: []*HTTPRoute{
				{Prefix: "/api/v1"},
				{Prefix: "/api"},
			},
		},
	}

	// Function to determine if two *Route slices
	// are the same (same Routes, same order)
	sameOrder := func(r1, r2 []*HTTPRoute) bool {
		for i, r := range r1 {
			if r.Path != r2[i].Path || r.Prefix != r2[i].Prefix {
				return false
			}
		}
		return true
	}

	for i, c := range cases {
		sort.Sort(RoutesByPath(c.in))
		if !sameOrder(c.in, c.expected) {
			t.Errorf("Invalid sort order for case %d", i)
		}
	}
}

func TestTCPRouteConfigByRoute(t *testing.T) {
	cases := []struct {
		name string
		in   []*TCPRoute
		want []*TCPRoute
	}{
		{
			name: "sorted by cluster",
			in: []*TCPRoute{{
				Cluster:           "cluster-b",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.2/32", "192.168.1.1/32"},
				DestinationPorts:  "5000",
			}},
			want: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.2/32", "192.168.1.1/32"},
				DestinationPorts:  "5000",
			}, {
				Cluster:           "cluster-b",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
			}},
		},
		{
			name: "sorted by DestinationIPList",
			in: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.2.1/32", "192.168.2.2/32"},
				DestinationPorts:  "5000",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
			}},
			want: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.2.1/32", "192.168.2.2/32"},
				DestinationPorts:  "5000",
			}},
		},
		{
			name: "sorted by DestinationPorts",
			in: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5001",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
			}},
			want: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5001",
			}},
		},
		{
			name: "sorted by SourceIPList",
			in: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.3.1/32", "192.168.3.2/32"},
				SourcePorts:       "5002",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.2.1/32", "192.168.2.2/32"},
				SourcePorts:       "5002",
			}},
			want: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.2.1/32", "192.168.2.2/32"},
				SourcePorts:       "5002",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.3.1/32", "192.168.3.2/32"},
				SourcePorts:       "5002",
			}},
		},
		{
			name: "sorted by SourcePorts",
			in: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.2.1/32", "192.168.2.2/32"},
				SourcePorts:       "5003",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.2.1/32", "192.168.2.2/32"},
				SourcePorts:       "5002",
			}},
			want: []*TCPRoute{{
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.2.1/32", "192.168.2.2/32"},
				SourcePorts:       "5002",
			}, {
				Cluster:           "cluster-a",
				DestinationIPList: []string{"192.168.1.1/32", "192.168.1.2/32"},
				DestinationPorts:  "5000",
				SourceIPList:      []string{"192.168.2.1/32", "192.168.2.2/32"},
				SourcePorts:       "5003",
			}},
		},
	}

	for _, c := range cases {
		sort.Sort(TCPRouteByRoute(c.in))
		if !reflect.DeepEqual(c.in, c.want) {
			t.Errorf("Invalid sort order for case %q:\n got  %#v\n want %#v", c.name, c.in, c.want)
		}
	}
}

const (
	envoyV0Config     = "testdata/envoy-v0.json"
	envoyV0ConfigAuth = "testdata/envoy-v0-auth.json"
	envoyV1Config     = "testdata/envoy-v1.json"
	envoyV1ConfigAuth = "testdata/envoy-v1-auth.json"
	envoyFaultConfig  = "testdata/envoy-fault.json"
	cbPolicy          = "testdata/cb-policy.yaml.golden"
	timeoutRouteRule  = "testdata/timeout-route-rule.yaml.golden"
	weightedRouteRule = "testdata/weighted-route.yaml.golden"
	faultRouteRule    = "testdata/fault-route.yaml.golden"
)

func testConfig(r *model.IstioRegistry, mesh *proxyconfig.ProxyMeshConfig, instance, envoyConfig string, t *testing.T) {
	config := Generate(&proxy.Context{
		Discovery:  mock.Discovery,
		Accounts:   mock.Discovery,
		Config:     r,
		MeshConfig: mesh,
		IPAddress:  instance,
		UID:        fmt.Sprintf("uid://%s.my-namespace", instance),
		// 1090 is deliberately already used by the instances, 3333 requires a new listener
		PassthroughPorts: []int{1090, 3333},
	})

	if config == nil {
		t.Fatal("Failed to generate config")
	}

	err := config.WriteFile(envoyConfig)
	if err != nil {
		t.Fatalf(err.Error())
	}

	util.CompareYAML(envoyConfig, t)
}

func configObjectFromYAML(kind, file string) (proto.Message, error) {
	schema, ok := model.IstioConfig[kind]
	if !ok {
		return nil, fmt.Errorf("Missing kind %q", kind)
	}
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return schema.FromYAML(string(content))
}

func addCircuitBreaker(r *model.IstioRegistry, t *testing.T) {
	msg, err := configObjectFromYAML(model.DestinationPolicy, cbPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Post(model.Key{
		Kind: model.DestinationPolicy,
		Name: "circuit-breaker"},
		msg); err != nil {
		t.Fatal(err)
	}
}

func addTimeout(r *model.IstioRegistry, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule, timeoutRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Post(model.Key{Kind: model.RouteRule, Name: "timeouts"}, msg); err != nil {
		t.Fatal(err)
	}
}

func addWeightedRoute(r *model.IstioRegistry, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule, weightedRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Post(model.Key{Kind: model.RouteRule, Name: "weighted-route"}, msg); err != nil {
		t.Fatal(err)
	}
}

func addFaultRoute(r *model.IstioRegistry, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule, faultRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Post(model.Key{Kind: model.RouteRule, Name: "fault-route"}, msg); err != nil {
		t.Fatal(err)
	}
}

func makeMeshConfig() proxyconfig.ProxyMeshConfig {
	mesh := DefaultMeshConfig
	mesh.MixerAddress = "localhost:9091"
	mesh.DiscoveryAddress = "localhost:8080"
	return mesh
}

func TestMockConfig(t *testing.T) {
	r := mock.MakeRegistry()
	mesh := makeMeshConfig()
	testConfig(r, &mesh, mock.HostInstanceV0, envoyV0Config, t)
	testConfig(r, &mesh, mock.HostInstanceV1, envoyV1Config, t)
}

func TestMockConfigWithAuth(t *testing.T) {
	r := mock.MakeRegistry()
	mesh := makeMeshConfig()
	mesh.AuthPolicy = proxyconfig.ProxyMeshConfig_MUTUAL_TLS
	testConfig(r, &mesh, mock.HostInstanceV0, envoyV0ConfigAuth, t)
	testConfig(r, &mesh, mock.HostInstanceV1, envoyV1ConfigAuth, t)
}

func TestMockConfigTimeout(t *testing.T) {
	r := mock.MakeRegistry()
	mesh := makeMeshConfig()
	addTimeout(r, t)
	testConfig(r, &mesh, mock.HostInstanceV0, envoyV0Config, t)
	testConfig(r, &mesh, mock.HostInstanceV1, envoyV1Config, t)
}

func TestMockConfigCircuitBreaker(t *testing.T) {
	r := mock.MakeRegistry()
	mesh := makeMeshConfig()
	addCircuitBreaker(r, t)
	testConfig(r, &mesh, mock.HostInstanceV0, envoyV0Config, t)
	testConfig(r, &mesh, mock.HostInstanceV1, envoyV1Config, t)
}

func TestMockConfigWeighted(t *testing.T) {
	r := mock.MakeRegistry()
	mesh := makeMeshConfig()
	addWeightedRoute(r, t)
	testConfig(r, &mesh, mock.HostInstanceV0, envoyV0Config, t)
	testConfig(r, &mesh, mock.HostInstanceV1, envoyV1Config, t)
}

func TestMockConfigFault(t *testing.T) {
	r := mock.MakeRegistry()
	mesh := makeMeshConfig()
	addFaultRoute(r, t)
	// Fault rule uses source condition, hence the different golden artifacts
	testConfig(r, &mesh, mock.HostInstanceV0, envoyFaultConfig, t)
	testConfig(r, &mesh, mock.HostInstanceV1, envoyV1Config, t)
}
