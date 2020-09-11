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
	"context"
	"net"
	"strings"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/plugin"
	kubesecrets "istio.io/istio/pilot/pkg/secrets/kube"
	"istio.io/istio/pilot/pkg/serviceregistry"
	kube "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

type FakeOptions struct {
	// If provided, these objects will be used directly
	KubernetesObjects []runtime.Object
	// If provided, the yaml string will be parsed and used as objects
	KubernetesObjectString string
	// If provided, these configs will be used directly
	Configs []config.Config
	// If provided, the yaml string will be parsed and used as configs
	ConfigString string
	// If provided, the ConfigString will be treated as a go template, with this as input params
	ConfigTemplateInput interface{}
	// If provided, this mesh config will be used
	MeshConfig      *meshconfig.MeshConfig
	NetworksWatcher mesh.NetworksWatcher
}

type FakeDiscoveryServer struct {
	*v1alpha3.ConfigGenTest
	t         test.Failer
	Discovery *DiscoveryServer
	listener  *bufconn.Listener
}

func NewFakeDiscoveryServer(t test.Failer, opts FakeOptions) *FakeDiscoveryServer {
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})

	// Init with a dummy environment, since we have a circular dependency with the env creation.
	s := NewDiscoveryServer(&model.Environment{PushContext: model.NewPushContext()}, []string{plugin.Authn, plugin.Authz})

	k8sObjects := getKubernetesObjects(t, opts)

	k8s, _ := kube.NewFakeControllerWithOptions(kube.FakeControllerOptions{
		Objects:         k8sObjects,
		ClusterID:       "Kubernetes",
		DomainSuffix:    "cluster.local",
		XDSUpdater:      s,
		NetworksWatcher: opts.NetworksWatcher,
	})

	secretFake := kubelib.NewFakeClient(k8sObjects...)
	sc := kubesecrets.NewSecretsController(secretFake.KubeInformer().Core().V1().Secrets())
	secretFake.RunAndWait(stop)
	s.Generators[v3.SecretType] = NewSecretGen(sc, &model.DisabledCache{})

	cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
		Configs:             opts.Configs,
		ConfigString:        opts.ConfigString,
		ConfigTemplateInput: opts.ConfigTemplateInput,
		MeshConfig:          opts.MeshConfig,
		NetworksWatcher:     opts.NetworksWatcher,
		ServiceRegistries:   []serviceregistry.Instance{k8s},
		PushContextLock:     &s.updateMutex,
	})
	s.updateMutex.Lock()
	s.Env = cg.Env()
	// Disable debounce to reduce test times
	s.debounceOptions.debounceAfter = 0
	s.MemRegistry = cg.MemRegistry
	s.MemRegistry.EDSUpdater = s
	s.updateMutex.Unlock()

	// Setup config handlers
	// TODO code re-use from server.go
	configHandler := func(_, curr config.Config, event model.Event) {
		pushReq := &model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      curr.GroupVersionKind,
				Name:      curr.Name,
				Namespace: curr.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.ConfigUpdate},
		}
		s.ConfigUpdate(pushReq)
	}
	schemas := collections.Pilot.All()
	if features.EnableServiceApis {
		schemas = collections.PilotServiceApi.All()
	}
	for _, schema := range schemas {
		// This resource type was handled in external/servicediscovery.go, no need to rehandle here.
		if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Serviceentries.
			Resource().GroupVersionKind() {
			continue
		}
		if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Workloadentries.
			Resource().GroupVersionKind() {
			continue
		}

		cg.Store().RegisterEventHandler(schema.Resource().GroupVersionKind(), configHandler)
	}

	// Start in memory gRPC listener
	buffer := 1024 * 1024
	listener := bufconn.Listen(buffer)
	grpcServer := grpc.NewServer()
	s.Register(grpcServer)
	go func() {
		if err := grpcServer.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			t.Fatal(err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
	})

	cg.ServiceEntryRegistry.XdsUpdater = s
	// Start the discovery server
	s.CachesSynced()
	s.Start(stop)
	cg.ServiceEntryRegistry.ResyncEDS()

	fake := &FakeDiscoveryServer{
		t:             t,
		Discovery:     s,
		listener:      listener,
		ConfigGenTest: cg,
	}

	// currently meshNetworks gateways are stored on the push context
	fake.refreshPushContext()
	cg.Env().AddNetworksHandler(fake.refreshPushContext)

	return fake
}

func (f *FakeDiscoveryServer) PushContext() *model.PushContext {
	f.Discovery.updateMutex.RLock()
	defer f.Discovery.updateMutex.RUnlock()
	return f.Env().PushContext
}

// ConnectADS starts an ADS connection to the server. It will automatically be cleaned up when the test ends
func (f *FakeDiscoveryServer) ConnectADS() discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient {
	conn, err := grpc.Dial("buffcon", grpc.WithInsecure(), grpc.WithBlock(), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return f.listener.Dial()
	}))
	if err != nil {
		f.t.Fatalf("failed to connect: %v", err)
	}
	xds := discovery.NewAggregatedDiscoveryServiceClient(conn)
	client, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		f.t.Fatalf("stream resources failed: %s", err)
	}
	f.t.Cleanup(func() {
		_ = client.CloseSend()
		_ = conn.Close()
	})
	return client
}

// ConnectADS starts an ADS connection to the server using adsc. It will automatically be cleaned up when the test ends
// watch can be configured to determine the resources to watch initially, and wait can be configured to determine what
// resources we should initially wait for.
func (f *FakeDiscoveryServer) Connect(p *model.Proxy, watch []string, wait []string) *adsc.ADSC {
	f.t.Helper()
	p = f.SetupProxy(p)
	if watch == nil {
		watch = []string{v3.ClusterType}
	}
	if wait == nil {
		watch = []string{v3.ClusterType}
	}
	adscConn, err := adsc.Dial("buffcon", "", &adsc.Config{
		IP:        p.IPAddresses[0],
		Meta:      p.Metadata.ToStruct(),
		Locality:  p.Locality,
		Namespace: p.ConfigNamespace,
		Watch:     watch,
		GrpcOpts: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return f.listener.Dial()
		}),
			grpc.WithInsecure()},
	})
	if err != nil {
		f.t.Fatalf("Error connecting: %v", err)
	}
	if len(wait) > 0 {
		_, err = adscConn.Wait(10*time.Second, wait...)
		if err != nil {
			f.t.Fatalf("Error getting initial for %v config: %v", wait, err)
		}
	}
	f.t.Cleanup(func() {
		adscConn.Close()
	})
	return adscConn
}

func (f *FakeDiscoveryServer) Endpoints(p *model.Proxy) []*endpoint.ClusterLoadAssignment {
	loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
	c := f.Clusters(p)
	_ = c
	for _, c := range xdstest.ExtractEdsClusterNames(f.Clusters(p)) {
		loadAssignments = append(loadAssignments, f.Discovery.generateEndpoints(NewEndpointBuilder(c, p, f.PushContext())))
	}
	return loadAssignments
}

func (f *FakeDiscoveryServer) refreshPushContext() {
	_, err := f.Discovery.initPushContext(&model.PushRequest{
		Full:   true,
		Reason: []model.TriggerReason{model.GlobalUpdate},
	}, nil)
	if err != nil {
		f.t.Fatal(err)
	}
}

func getKubernetesObjects(t test.Failer, opts FakeOptions) []runtime.Object {
	if len(opts.KubernetesObjects) > 0 {
		return opts.KubernetesObjects
	}

	objects := make([]runtime.Object, 0)
	if len(opts.KubernetesObjectString) > 0 {
		decode := scheme.Codecs.UniversalDeserializer().Decode
		objectStrs := strings.Split(opts.KubernetesObjectString, "---")
		for _, s := range objectStrs {
			if len(strings.TrimSpace(s)) == 0 {
				continue
			}
			o, _, err := decode([]byte(s), nil, nil)
			if err != nil {
				t.Fatalf("failed deserializing kubernetes object: %v", err)
			}
			objects = append(objects, o)
		}
	}

	return objects
}
