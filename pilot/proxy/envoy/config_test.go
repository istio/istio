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
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/util"
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
	envoyConfig = "testdata/envoy.json"

	cbPolicy          = "testdata/cb-policy.yaml.golden"
	timeoutRouteRule  = "testdata/timeout-route-rule.yaml.golden"
	weightedRouteRule = "testdata/weighted-route.yaml.golden"
	faultRouteRule    = "testdata/fault-route.yaml.golden"
	redirectRouteRule = "testdata/redirect-route.yaml.golden"
	rewriteRouteRule  = "testdata/rewrite-route.yaml.golden"
)

func configObjectFromYAML(kind, file string) (proto.Message, error) {
	schema, ok := model.IstioConfigTypes.GetByType(kind)
	if !ok {
		return nil, fmt.Errorf("Missing kind %q", kind)
	}
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return schema.FromYAML(string(content))
}

func addCircuitBreaker(r model.ConfigStore, t *testing.T) {
	msg, err := configObjectFromYAML(model.DestinationPolicy.Type, cbPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Post(msg); err != nil {
		t.Fatal(err)
	}
}

func addRewrite(r model.ConfigStore, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule.Type, rewriteRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Post(msg); err != nil {
		t.Fatal(err)
	}
}

func addRedirect(r model.ConfigStore, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule.Type, redirectRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Post(msg); err != nil {
		t.Fatal(err)
	}
}

func addTimeout(r model.ConfigStore, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule.Type, timeoutRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Post(msg); err != nil {
		t.Fatal(err)
	}
}

func addWeightedRoute(r model.ConfigStore, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule.Type, weightedRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Post(msg); err != nil {
		t.Fatal(err)
	}
}

func addFaultRoute(r model.ConfigStore, t *testing.T) {
	msg, err := configObjectFromYAML(model.RouteRule.Type, faultRouteRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Post(msg); err != nil {
		t.Fatal(err)
	}
}

func makeMeshConfig() proxyconfig.ProxyMeshConfig {
	mesh := proxy.DefaultMeshConfig()
	mesh.MixerAddress = "localhost:9091"
	mesh.DiscoveryAddress = "localhost:8080"
	mesh.DiscoveryRefreshDelay = ptypes.DurationProto(10 * time.Millisecond)
	mesh.EgressProxyAddress = "localhost:8888"
	mesh.StatsdUdpAddress = "10.1.1.10:9125"
	mesh.ZipkinAddress = "localhost:6000"
	return mesh
}

func TestSidecarConfig(t *testing.T) {
	mesh := makeMeshConfig()
	config := buildConfig(Listeners{}, Clusters{}, true, &mesh)
	if config == nil {
		t.Fatal("Failed to generate config")
	}

	err := config.WriteFile(envoyConfig)
	if err != nil {
		t.Fatalf(err.Error())
	}

	util.CompareYAML(envoyConfig, t)
}

/*
var (
	ingressCertFile = "testdata/tls.crt"
	ingressKeyFile  = "testdata/tls.key"
)

func compareFile(filename string, golden []byte, t *testing.T) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Error loading %s: %s", filename, err.Error())
	}
	if string(content) != string(golden) {
		t.Errorf("Failed validating file %s, got %s", filename, string(content))
	}
	err = os.Remove(filename)
	if err != nil {
		t.Errorf("Failed cleaning up temporary file %s", filename)
	}
}
*/
