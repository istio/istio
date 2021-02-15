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
	"io/ioutil"
	"os"
	"path"
	"testing"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"k8s.io/client-go/kubernetes/fake"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	kubesecrets "istio.io/istio/pilot/pkg/secrets/kube"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
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
		Name:      "empty",
		Services:  100,
		ProxyType: model.SidecarProxy,
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
	{
		Name:     "authorizationpolicy",
		Services: 100,
	},
	{
		Name:     "peerauthentication",
		Services: 100,
	},
}

func disableLogging() {
	for _, s := range log.Scopes() {
		if s.Name() == benchmarkScope.Name() {
			continue
		}
		s.SetOutputLevel(log.NoneLevel)
	}
}

func BenchmarkInitPushContext(b *testing.B) {
	disableLogging()
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupTest(b, tt)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				initPushContext(s.Env(), proxy)
			}
		})
	}
}

func BenchmarkRouteGeneration(b *testing.B) {
	disableLogging()
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupAndInitializeTest(b, tt)
			// To determine which routes to generate, first gen listeners once (not part of benchmark) and extract routes
			l := s.Discovery.ConfigGenerator.BuildListeners(proxy, s.PushContext())
			routeNames := xdstest.ExtractRoutesFromListeners(l)
			if len(routeNames) == 0 {
				b.Fatal("Got no route names!")
			}
			b.ResetTimer()
			var c model.Resources
			for n := 0; n < b.N; n++ {
				c, _ = s.Discovery.Generators[v3.RouteType].Generate(proxy, s.PushContext(), &model.WatchedResource{ResourceNames: routeNames}, nil)
				if len(c) == 0 {
					b.Fatal("Got no routes!")
				}
			}
			logDebug(b, c)
		})
	}
}

// Do a quick sanity tests to make sure telemetry v2 filters are applying. This ensures as they
// update our benchmark doesn't become useless.
func TestValidateTelemetry(t *testing.T) {
	s, proxy := setupAndInitializeTest(t, ConfigInput{Name: "telemetry", Services: 1})
	c, _ := s.Discovery.Generators[v3.ClusterType].Generate(proxy, s.PushContext(), nil, nil)
	if len(c) == 0 {
		t.Fatal("Got no clusters!")
	}
	for _, r := range c {
		cls := &cluster.Cluster{}
		if err := ptypes.UnmarshalAny(r, cls); err != nil {
			t.Fatal(err)
		}
		for _, ff := range cls.Filters {
			if ff.Name == "istio.metadata_exchange" {
				return
			}
		}
	}
	t.Fatalf("telemetry v2 filters not found")
}

func BenchmarkClusterGeneration(b *testing.B) {
	disableLogging()
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupAndInitializeTest(b, tt)
			b.ResetTimer()
			var c model.Resources
			for n := 0; n < b.N; n++ {
				c, _ = s.Discovery.Generators[v3.ClusterType].Generate(proxy, s.PushContext(), nil, nil)
				if len(c) == 0 {
					b.Fatal("Got no clusters!")
				}
			}
			logDebug(b, c)
		})
	}
}

func BenchmarkListenerGeneration(b *testing.B) {
	disableLogging()
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupAndInitializeTest(b, tt)
			b.ResetTimer()
			var c model.Resources
			for n := 0; n < b.N; n++ {
				c, _ = s.Discovery.Generators[v3.ListenerType].Generate(proxy, s.PushContext(), nil, nil)
				if len(c) == 0 {
					b.Fatal("Got no listeners!")
				}
			}
			logDebug(b, c)
		})
	}
}

func BenchmarkNameTableGeneration(b *testing.B) {
	disableLogging()
	for _, tt := range testCases {
		b.Run(tt.Name, func(b *testing.B) {
			s, proxy := setupAndInitializeTest(b, tt)
			b.ResetTimer()
			var c model.Resources
			for n := 0; n < b.N; n++ {
				c, _ = s.Discovery.Generators[v3.NameTableType].Generate(proxy, s.PushContext(), nil, nil)
				if len(c) == 0 && tt.ProxyType != model.Router {
					b.Fatal("Got no name tables!")
				}
			}
			logDebug(b, c)
		})
	}
}

func BenchmarkSecretGeneration(b *testing.B) {
	disableLogging()
	cases := []ConfigInput{
		{
			Name:     "secrets",
			Services: 10,
		},
		{
			Name:     "secrets",
			Services: 1000,
		},
	}
	for _, tt := range cases {
		b.Run(fmt.Sprintf("%s-%d", tt.Name, tt.Services), func(b *testing.B) {
			tmpl := template.Must(template.New("").Funcs(sprig.TxtFuncMap()).ParseFiles(path.Join("testdata", "benchmarks", tt.Name+".yaml")))
			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, tt.Name+".yaml", tt); err != nil {
				b.Fatalf("failed to execute template: %v", err)
			}
			s := NewFakeDiscoveryServer(b, FakeOptions{
				KubernetesObjectString: buf.String(),
			})
			kubesecrets.DisableAuthorizationForTest(s.KubeClient().Kube().(*fake.Clientset))
			watchedResources := []string{}
			for i := 0; i < tt.Services; i++ {
				watchedResources = append(watchedResources, fmt.Sprintf("kubernetes://istio-system/sds-credential-%d", i))
			}
			proxy := s.SetupProxy(&model.Proxy{Type: model.Router, ConfigNamespace: "istio-system", VerifiedIdentity: &spiffe.Identity{}})
			gen := s.Discovery.Generators[v3.SecretType]
			res := &model.WatchedResource{ResourceNames: watchedResources}
			b.ResetTimer()
			var c model.Resources
			for n := 0; n < b.N; n++ {
				c, _ = gen.Generate(proxy, s.PushContext(), res, &model.PushRequest{Full: true})
				if len(c) == 0 {
					b.Fatal("Got no secrets!")
				}
			}
			logDebug(b, c)
		})
	}
}

// BenchmarkEDS measures performance of EDS config generation
// TODO Add more variables, such as different services
func BenchmarkEndpointGeneration(b *testing.B) {
	disableLogging()
	tests := []struct {
		endpoints int
		services  int
	}{
		{1, 100},
		{10, 10},
		{100, 10},
		{1000, 1},
	}

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
				loadAssignments := make([]*any.Any, 0)
				for svc := 0; svc < tt.services; svc++ {
					l := s.Discovery.generateEndpoints(NewEndpointBuilder(fmt.Sprintf("outbound|80||foo-%d.com", svc), proxy, push))
					loadAssignments = append(loadAssignments, util.MessageToAny(l))
				}
				response = endpointDiscoveryResponse(loadAssignments, version, push.LedgerVersion)
			}
			logDebug(b, response.GetResources())
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
			IstioVersion: "1.10.0",
		},
		ConfigNamespace: "default",
	}
	proxy.IstioVersion = model.ParseIstioVersion(proxy.Metadata.IstioVersion)

	configs := getConfigsWithCache(t, config)
	s := NewFakeDiscoveryServer(t, FakeOptions{
		Configs: configs,
		// Allow debounce to avoid overwhelming with writes
		DebounceTime: time.Millisecond * 10,
	})

	return s, proxy
}

var configCache = map[ConfigInput][]config.Config{}

func getConfigsWithCache(t testing.TB, input ConfigInput) []config.Config {
	// Config setup is slow for large tests. Cache this and return from Cache.
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
	extra := path.Join("testdata", "benchmarks", configName+".extra.yaml")
	inputYAML := buf.String()
	if _, err := os.Stat(extra); err == nil {
		bdata, err := ioutil.ReadFile(extra)
		if err != nil {
			t.Fatal(err)
		}

		inputYAML += "\n---\n" + yml.SplitYamlByKind(string(bdata))[gvk.EnvoyFilter.Kind]
	}

	configs, badKinds, err := crd.ParseInputs(inputYAML)
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
	initPushContext(s.Env(), proxy)
	return s, proxy
}

func initPushContext(env *model.Environment, proxy *model.Proxy) {
	env.PushContext.InitContext(env, nil, nil)
	proxy.SetSidecarScope(env.PushContext)
	proxy.SetGatewaysForProxy(env.PushContext)
	proxy.SetServiceInstances(env.ServiceDiscovery)
}

var debugGeneration = env.RegisterBoolVar("DEBUG_CONFIG_DUMP", false, "if enabled, print a full config dump of the generated config")

var benchmarkScope = log.RegisterScope("benchmark", "", 0)

// Add additional debug info for a test
func logDebug(b *testing.B, m model.Resources) {
	b.Helper()
	b.StopTimer()

	if debugGeneration.Get() {
		for i, r := range m {
			s, err := (&jsonpb.Marshaler{Indent: "  "}).MarshalToString(r)
			if err != nil {
				b.Fatal(err)
			}
			// Cannot use b.Logf, it truncates
			benchmarkScope.Infof("Generated: %d %s", i, s)
		}
	}
	bytes := 0
	for _, r := range m {
		bytes += len(r.Value)
	}
	b.ReportMetric(float64(bytes)/1000, "kb/msg")
	b.ReportMetric(float64(len(m)), "resources/msg")
	b.StartTimer()
}

func createEndpoints(numEndpoints int, numServices int) []config.Config {
	result := make([]config.Config, 0, numServices)
	for s := 0; s < numServices; s++ {
		endpoints := make([]*networking.WorkloadEntry, 0, numEndpoints)
		for e := 0; e < numEndpoints; e++ {
			endpoints = append(endpoints, &networking.WorkloadEntry{Address: fmt.Sprintf("111.%d.%d.%d", e/(256*256), (e/256)%256, e%256)})
		}
		result = append(result, config.Config{
			Meta: config.Meta{
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
