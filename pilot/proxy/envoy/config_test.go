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
	"io/ioutil"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/proxy"
	"istio.io/istio/pilot/test/util"
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

type fileConfig struct {
	meta model.ConfigMeta
	file string
}

const (
	envoySidecarConfig     = "testdata/envoy-sidecar.json"
	envoySidecarAuthConfig = "testdata/envoy-sidecar-auth.json"
)

var (
	cbPolicy = fileConfig{
		meta: model.ConfigMeta{Type: model.DestinationPolicy.Type, Name: "circuit-breaker"},
		file: "testdata/cb-policy.yaml.golden",
	}

	timeoutRouteRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "timeout"},
		file: "testdata/timeout-route-rule.yaml.golden",
	}

	weightedRouteRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "weighted"},
		file: "testdata/weighted-route.yaml.golden",
	}

	faultRouteRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "fault"},
		file: "testdata/fault-route.yaml.golden",
	}

	redirectRouteRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "redirect"},
		file: "testdata/redirect-route.yaml.golden",
	}

	rewriteRouteRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "rewrite"},
		file: "testdata/rewrite-route.yaml.golden",
	}

	websocketRouteRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "websocket"},
		file: "testdata/websocket-route.yaml.golden",
	}

	egressRule = fileConfig{
		meta: model.ConfigMeta{Type: model.EgressRule.Type, Name: "google"},
		file: "testdata/egress-rule.yaml.golden",
	}

	egressRuleCBPolicy = fileConfig{
		meta: model.ConfigMeta{Type: model.DestinationPolicy.Type, Name: "egress-circuit-breaker"},
		file: "testdata/egress-rule-cb-policy.yaml.golden",
	}

	egressRuleTimeoutRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "egress-timeout"},
		file: "testdata/egress-rule-timeout-route-rule.yaml.golden",
	}

	ingressRouteRule1 = fileConfig{
		meta: model.ConfigMeta{Type: model.IngressRule.Type, Name: "world"},
		file: "testdata/ingress-route-world.yaml.golden",
	}

	ingressRouteRule2 = fileConfig{
		meta: model.ConfigMeta{Type: model.IngressRule.Type, Name: "foo"},
		file: "testdata/ingress-route-foo.yaml.golden",
	}

	addHeaderRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "append-headers"},
		file: "testdata/addheaders-route.yaml.golden",
	}

	corsPolicyRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "cors-policy"},
		file: "testdata/corspolicy-route.yaml.golden",
	}

	mirrorRule = fileConfig{
		meta: model.ConfigMeta{Type: model.RouteRule.Type, Name: "mirror-requests"},
		file: "testdata/mirror-route.yaml.golden",
	}
)

func addConfig(r model.ConfigStore, config fileConfig, t *testing.T) {
	schema, ok := model.IstioConfigTypes.GetByType(config.meta.Type)
	if !ok {
		t.Fatalf("missing schema for %q", config.meta.Type)
	}
	content, err := ioutil.ReadFile(config.file)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := schema.FromYAML(string(content))
	if err != nil {
		t.Fatal(err)
	}
	out := model.Config{
		ConfigMeta: config.meta,
		Spec:       spec,
	}

	// set default values for overriding
	out.ConfigMeta.Namespace = "default"
	out.ConfigMeta.Domain = "cluster.local"

	_, err = r.Create(out)
	if err != nil {
		t.Fatal(err)
	}
}

func makeProxyConfig() proxyconfig.ProxyConfig {
	proxyConfig := proxy.DefaultProxyConfig()
	proxyConfig.ZipkinAddress = "localhost:6000"
	proxyConfig.StatsdUdpAddress = "10.1.1.10:9125"
	proxyConfig.DiscoveryAddress = "istio-pilot.istio-system:15003"
	proxyConfig.DiscoveryRefreshDelay = ptypes.DurationProto(10 * time.Millisecond)
	return proxyConfig
}

var (
	pilotSAN = []string{"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"}
)

func makeProxyConfigControlPlaneAuth() proxyconfig.ProxyConfig {
	proxyConfig := makeProxyConfig()
	proxyConfig.ControlPlaneAuthPolicy = proxyconfig.AuthenticationPolicy_MUTUAL_TLS
	return proxyConfig
}

func makeMeshConfig() proxyconfig.MeshConfig {
	mesh := proxy.DefaultMeshConfig()
	mesh.MixerAddress = "istio-mixer.istio-system:9091"
	mesh.RdsRefreshDelay = ptypes.DurationProto(10 * time.Millisecond)
	return mesh
}

func TestProxyConfig(t *testing.T) {
	cases := []struct {
		envoyConfigFilename string
	}{
		{
			envoySidecarConfig,
		},
	}

	proxyConfig := makeProxyConfig()
	for _, c := range cases {
		config := buildConfig(proxyConfig, nil)
		if config == nil {
			t.Fatal("Failed to generate config")
		}

		err := config.WriteFile(c.envoyConfigFilename)
		if err != nil {
			t.Fatalf(err.Error())
		}

		util.CompareYAML(c.envoyConfigFilename, t)
	}
}

func TestProxyConfigControlPlaneAuth(t *testing.T) {
	cases := []struct {
		envoyConfigFilename string
	}{
		{
			envoySidecarAuthConfig,
		},
	}

	proxyConfig := makeProxyConfigControlPlaneAuth()
	for _, c := range cases {
		config := buildConfig(proxyConfig, pilotSAN)
		if config == nil {
			t.Fatal("Failed to generate config")
		}

		err := config.WriteFile(c.envoyConfigFilename)
		if err != nil {
			t.Fatalf(err.Error())
		}

		util.CompareYAML(c.envoyConfigFilename, t)
	}
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
