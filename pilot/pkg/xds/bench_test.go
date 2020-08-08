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

package xds

import (
	"bytes"
	"fmt"
	"path"
	"testing"
	"text/template"
	"time"

	"github.com/Masterminds/sprig"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"

	"istio.io/pkg/env"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/schema/collections"
)

// ConfigInput defines inputs passed to the test config templates
// This allows tests to do things like create a virtual service for each service, for example
type ConfigInput struct {
	// Name of the test
	Name string
	// Name of the test config file to use. If not set, <Name> is used
	ConfigName string
	// Number of services to make
	Services int
	// Type of proxy to generate configs for
	ProxyType model.NodeType
}

var testCases = []ConfigInput{
	{
		// Gateways provides an example config for a large Ingress deployment. This will create N
		// virtual services and gateways, where routing is determined by hostname, meaning we generate N routes for HTTPS.
		Name:      "gateways",
		Services:  1000,
		ProxyType: model.Router,
	},
	{
		// Gateways-shared provides an example config for a large Ingress deployment. This will create N
		// virtual services and gateways, where routing is determined by path. This means there will be a single large route.
		Name:      "gateways-shared",
		Services:  1000,
		ProxyType: model.Router,
	},
	{
		Name:     "empty",
		Services: 100,
	},
	{
		Name:     "tls",
		Services: 100,
	},
	{
		Name:     "telemetry",
		Services: 100,
	},
	{
		Name:     "virtualservice",
		Services: 100,
	},
}

func BenchmarkInitPushContext(b *testing.B) {
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupTest(b, tt)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				initPushContext(s.Env, proxy)
			}
		})
	}
}

func BenchmarkRouteGeneration(b *testing.B) {
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupAndInitializeTest(b, tt)
			// To determine which routes to generate, first gen listeners once (not part of benchmark) and extract routes
			l := s.Discovery.ConfigGenerator.BuildListeners(proxy, s.PushContext())
			routeNames := ExtractRoutesFromListeners(l)
			if len(routeNames) == 0 {
				b.Fatal("Got no route names!")
			}
			b.ResetTimer()
			var response *discovery.DiscoveryResponse
			for n := 0; n < b.N; n++ {
				r := s.Discovery.ConfigGenerator.BuildHTTPRoutes(proxy, s.PushContext(), routeNames)
				if len(r) == 0 {
					b.Fatal("Got no routes!")
				}
				response = routeDiscoveryResponse(r, "", "", v3.RouteType)
			}
			logDebug(b, response)
		})
	}
}

func BenchmarkClusterGeneration(b *testing.B) {
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupAndInitializeTest(b, tt)
			b.ResetTimer()
			var response *discovery.DiscoveryResponse
			for n := 0; n < b.N; n++ {
				c := s.Discovery.ConfigGenerator.BuildClusters(proxy, s.PushContext())
				if len(c) == 0 {
					b.Fatal("Got no clusters!")
				}
				response = cdsDiscoveryResponse(c, "", v3.ClusterType)
			}
			logDebug(b, response)
		})
	}
}

func BenchmarkListenerGeneration(b *testing.B) {
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupAndInitializeTest(b, tt)
			b.ResetTimer()
			var response *discovery.DiscoveryResponse
			for n := 0; n < b.N; n++ {
				l := s.Discovery.ConfigGenerator.BuildListeners(proxy, s.PushContext())
				if len(l) == 0 {
					b.Fatal("Got no listeners!")
				}
				response = ldsDiscoveryResponse(l, "", "", v3.ListenerType)
			}
			logDebug(b, response)
		})
	}
}

// BenchmarkEDS measures performance of EDS config generation
// TODO Add more variables, such as different services
func BenchmarkEndpointGeneration(b *testing.B) {
	tests := []struct {
		endpoints int
		services  int
	}{
		{1, 100},
		{10, 10},
		{100, 10},
		{1000, 1},
	}
	adsLog.SetOutputLevel(log.WarnLevel)
	var response *discovery.DiscoveryResponse
	for _, tt := range tests {
		b.Run(fmt.Sprintf("%d/%d", tt.endpoints, tt.services), func(b *testing.B) {
			s := NewFakeDiscoveryServer(b, FakeOptions{
				Configs: createEndpoints(tt.endpoints, tt.services),
			})
			proxy := &model.Proxy{
				Type:            model.SidecarProxy,
				IPAddresses:     []string{"10.3.3.3"},
				ID:              "random",
				ConfigNamespace: "default",
				Metadata:        &model.NodeMetadata{},
			}
			push := s.Discovery.globalPushContext()
			proxy.SetSidecarScope(push)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
				for svc := 0; svc < tt.services; svc++ {
					l := s.Discovery.generateEndpoints(createEndpointBuilder(fmt.Sprintf("outbound|80||foo-%d.com", svc), proxy, push))
					loadAssignments = append(loadAssignments, l)
				}
				response = endpointDiscoveryResponse(loadAssignments, version, push.Version, v3.EndpointType)
			}
			logDebug(b, response)
		})
	}
}

// Setup test builds a mock test environment. Note: push context is not initialized, to be able to benchmark separately
// most should just call setupAndInitializeTest
func setupTest(t testing.TB, config ConfigInput) (*FakeDiscoveryServer, *model.Proxy) {
	proxyType := config.ProxyType
	if proxyType == "" {
		proxyType = model.SidecarProxy
	}
	proxy := &model.Proxy{
		Type:        proxyType,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			Namespace: "default",
			Labels: map[string]string{
				"istio.io/benchmark": "true",
			},
			IstioVersion: "1.7.0",
			SdsEnabled:   true,
		},
		// TODO: if you update this, make sure telemetry.yaml is also updated
		IstioVersion:    &model.IstioVersion{Major: 1, Minor: 6},
		ConfigNamespace: "default",
	}

	configs := getConfigsWithCache(t, config)
	s := NewFakeDiscoveryServer(t, FakeOptions{
		Configs: configs,
	})

	return s, proxy
}

var configCache = map[ConfigInput][]model.Config{}

func getConfigsWithCache(t testing.TB, input ConfigInput) []model.Config {
	// Config setup is slow for large tests. Cache this and return from cache.
	// This improves even running a single test, as go will run the full test (including setup) at least twice.
	if cached, f := configCache[input]; f {
		return cached
	}
	configName := input.ConfigName
	if configName == "" {
		configName = input.Name
	}
	tmpl := template.Must(template.New("").Funcs(sprig.TxtFuncMap()).ParseFiles(path.Join("testdata", "benchmarks", configName+".yaml")))
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, configName+".yaml", input); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}
	configs, badKinds, err := crd.ParseInputs(buf.String())
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	if len(badKinds) != 0 {
		t.Fatalf("Got unknown resources: %v", badKinds)
	}
	// setup default namespace if not defined
	for i, c := range configs {
		if c.Namespace == "" {
			c.Namespace = "default"
		}
		configs[i] = c
	}
	configCache[input] = configs
	return configs
}

func setupAndInitializeTest(t testing.TB, config ConfigInput) (*FakeDiscoveryServer, *model.Proxy) {
	s, proxy := setupTest(t, config)
	initPushContext(s.Env, proxy)
	return s, proxy
}

func initPushContext(env *model.Environment, proxy *model.Proxy) {
	env.PushContext.InitContext(env, nil, nil)
	proxy.SetSidecarScope(env.PushContext)
	proxy.SetGatewaysForProxy(env.PushContext)
	proxy.SetServiceInstances(env.ServiceDiscovery)
}

var debugGeneration = env.RegisterBoolVar("DEBUG_CONFIG_DUMP", false, "if enabled, print a full config dump of the generated config")

// Add additional debug info for a test
func logDebug(b *testing.B, m *discovery.DiscoveryResponse) {
	b.Helper()
	b.StopTimer()

	if debugGeneration.Get() {
		s, err := (&jsonpb.Marshaler{Indent: "  "}).MarshalToString(m)
		if err != nil {
			b.Fatal(err)
		}
		// Cannot use b.Logf, it truncates
		log.Infof("Generated: %s", s)
	}
	bytes, err := proto.Marshal(m)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(bytes))/1000, "kb/msg")
	b.ReportMetric(float64(len(m.Resources)), "resources/msg")
	b.StartTimer()
}

func createEndpoints(numEndpoints int, numServices int) []model.Config {
	result := make([]model.Config, 0, numServices)
	for s := 0; s < numServices; s++ {
		endpoints := make([]*networking.WorkloadEntry, 0, numEndpoints)
		for e := 0; e < numEndpoints; e++ {
			endpoints = append(endpoints, &networking.WorkloadEntry{Address: fmt.Sprintf("111.%d.%d.%d", e/(256*256), (e/256)%256, e%256)})
		}
		result = append(result, model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind:  collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
				Name:              fmt.Sprintf("foo-%d", s),
				Namespace:         "default",
				CreationTimestamp: time.Now(),
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{fmt.Sprintf("foo-%d.com", s)},
				Ports: []*networking.Port{
					{Number: 80, Name: "http-port", Protocol: "http"},
				},
				Endpoints:  endpoints,
				Resolution: networking.ServiceEntry_STATIC,
			},
		})
	}
	return result
}
