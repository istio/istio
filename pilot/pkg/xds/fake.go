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
	"context"
	"net"
	"reflect"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoy_extensions_filters_network_tcp_proxy_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	meshconfig "istio.io/api/mesh/v1alpha1"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kube "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test"
)

type FakeOptions struct {
	// If provided, these objects will be used directly
	KubernetesObjects []runtime.Object
	// If provided, the yaml string will be parsed and used as objects
	KubernetesObjectString string
	// If provided, these configs will be used directly
	Configs []model.Config
	// If provided, the yaml string will be parsed and used as configs
	ConfigString string
	// If provided, the ConfigString will be treated as a go template, with this as input params
	ConfigTemplateInput interface{}
	// If provided, this mesh config will be used
	MeshConfig   *meshconfig.MeshConfig
	MeshNetworks *meshconfig.MeshNetworks
}

type FakeDiscoveryServer struct {
	t           test.Failer
	Store       model.ConfigStore
	Discovery   *DiscoveryServer
	PushContext *model.PushContext
	Env         *model.Environment
	listener    *bufconn.Listener
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
			o, _, err := decode([]byte(s), nil, nil)
			if err != nil {
				t.Fatalf("failed deserializing kubernetes object: %v", err)
			}
			objects = append(objects, o)
		}
	}

	return objects
}

func getConfigs(t test.Failer, opts FakeOptions) []model.Config {
	if len(opts.Configs) > 0 {
		return opts.Configs
	}
	configStr := opts.ConfigString
	if opts.ConfigTemplateInput != nil {
		tmpl := template.Must(template.New("").Funcs(sprig.TxtFuncMap()).Parse(opts.ConfigString))
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, opts.ConfigTemplateInput); err != nil {
			t.Fatalf("failed to execute template: %v", err)
		}
		configStr = buf.String()
	}
	configs, _, err := crd.ParseInputs(configStr)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	// setup default namespace if not defined
	for i, c := range configs {
		if c.Namespace == "" {
			c.Namespace = "default"
		}
		configs[i] = c
	}
	return configs
}

func NewFakeDiscoveryServer(t test.Failer, opts FakeOptions) *FakeDiscoveryServer {
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
	})

	configs := getConfigs(t, opts)
	k8sObjects := getKubernetesObjects(t, opts)
	configStore := memory.MakeWithLedger(collections.Pilot, &model.DisabledLedger{}, true)
	env := &model.Environment{}
	plugins := []string{plugin.Authn, plugin.Authz, plugin.Health}

	s := NewDiscoveryServer(env, plugins)
	// Disable debounce to reduce test times
	s.debounceOptions.debounceAfter = 0
	s.MemRegistry = memregistry.NewServiceDiscovery(nil)
	s.MemRegistry.EDSUpdater = s

	configController := memory.NewSyncController(configStore)
	go configController.Run(stop)

	m := opts.MeshConfig
	if m == nil {
		def := mesh.DefaultMeshConfig()
		m = &def
	}

	serviceDiscovery := aggregate.NewController()
	env.PushContext = model.NewPushContext()
	env.ServiceDiscovery = serviceDiscovery
	env.IstioConfigStore = model.MakeIstioStore(configStore)
	env.Watcher = mesh.NewFixedWatcher(m)
	env.NetworksWatcher = mesh.NewFixedNetworksWatcher(opts.MeshNetworks)

	se := serviceentry.NewServiceDiscovery(configController, model.MakeIstioStore(configStore), s)
	serviceDiscovery.AddRegistry(se)
	k8s, _ := kube.NewFakeControllerWithOptions(kube.FakeControllerOptions{
		Objects:         k8sObjects,
		ClusterID:       "Kubernetes",
		DomainSuffix:    "cluster.local",
		XDSUpdater:      s,
		NetworksWatcher: env,
	})
	serviceDiscovery.AddRegistry(k8s)
	for _, cfg := range configs {
		if _, err := configStore.Create(cfg); err != nil {
			t.Fatalf("failed to create config %v: %v", cfg.Name, err)
		}
	}

	s.MemRegistry.ClusterID = string(serviceregistry.Mock)
	serviceDiscovery.AddRegistry(serviceregistry.Simple{
		ClusterID:        string(serviceregistry.Mock),
		ProviderID:       serviceregistry.Mock,
		ServiceDiscovery: s.MemRegistry,
		Controller:       s.MemRegistry.Controller,
	})

	// Setup config handlers
	// TODO code re-use from server.go
	configHandler := func(_, curr model.Config, event model.Event) {
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

		configController.RegisterEventHandler(schema.Resource().GroupVersionKind(), configHandler)
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

	// Start the discovery server
	s.CachesSynced()
	s.Start(stop)

	se.ResyncEDS()
	if err := k8s.ForceResync(); err != nil {
		t.Fatal(err)
	}

	s.updateMutex.Lock()
	defer s.updateMutex.Unlock()
	ctx := model.NewPushContext()
	if err := ctx.InitContext(env, env.PushContext, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateServiceShards(ctx); err != nil {
		t.Fatal(err)
	}
	env.PushContext = ctx

	fake := &FakeDiscoveryServer{
		t:           t,
		Store:       configController,
		Discovery:   s,
		PushContext: env.PushContext,
		Env:         env,
		listener:    listener,
	}
	return fake
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
		Namespace: p.ConfigNamespace,
		Watch:     watch,
		GrpcOpts: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return f.listener.Dial()
		})},
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

// SetupProxy initializes a proxy for the current environment. This should generally be used when creating
// any proxy. For example, `p := SetupProxy(&model.Proxy{...})`.
func (f *FakeDiscoveryServer) SetupProxy(p *model.Proxy) *model.Proxy {
	// Setup defaults
	if p == nil {
		p = &model.Proxy{}
	}
	if p.Metadata == nil {
		p.Metadata = &model.NodeMetadata{}
	}
	if p.Metadata.IstioVersion == "" {
		p.Metadata.IstioVersion = "1.8.0"
		p.IstioVersion = model.ParseIstioVersion(p.Metadata.IstioVersion)
	}
	if p.Type == "" {
		p.Type = model.SidecarProxy
	}
	if p.ID == "" {
		p.ID = "app.test"
	}
	if len(p.IPAddresses) == 0 {
		p.IPAddresses = []string{"1.1.1.1"}
	}
	// Initialize data structures

	f.Discovery.updateMutex.RLock()
	defer f.Discovery.updateMutex.RUnlock()
	p.SetSidecarScope(f.Discovery.Env.PushContext)
	p.SetGatewaysForProxy(f.Discovery.Env.PushContext)
	if err := p.SetServiceInstances(f.Discovery.Env.ServiceDiscovery); err != nil {
		f.t.Fatal(err)
	}
	p.DiscoverIPVersions()
	return p
}

func (f *FakeDiscoveryServer) Listeners(p *model.Proxy) []*listener.Listener {
	return f.Discovery.ConfigGenerator.BuildListeners(p, f.PushContext)
}

func (f *FakeDiscoveryServer) Clusters(p *model.Proxy) []*cluster.Cluster {
	return f.Discovery.ConfigGenerator.BuildClusters(p, f.PushContext)
}

func (f *FakeDiscoveryServer) Endpoints(p *model.Proxy) []*endpoint.ClusterLoadAssignment {
	loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
	for _, c := range ExtractEdsClusterNames(f.Clusters(p)) {
		loadAssignments = append(loadAssignments, f.Discovery.generateEndpoints(createEndpointBuilder(c, p, f.PushContext)))
	}
	return loadAssignments
}

func (f *FakeDiscoveryServer) Routes(p *model.Proxy) []*route.RouteConfiguration {
	return f.Discovery.ConfigGenerator.BuildHTTPRoutes(p, f.PushContext, ExtractRoutesFromListeners(f.Listeners(p)))
}

func ToDiscoveryResponse(p interface{}) *discovery.DiscoveryResponse {
	slice := InterfaceSlice(p)
	if len(slice) == 0 {
		return &discovery.DiscoveryResponse{}
	}
	resources := make([]*any.Any, 0, len(slice))
	for _, v := range slice {
		resources = append(resources, util.MessageToAny(v.(proto.Message)))
	}
	return &discovery.DiscoveryResponse{
		Resources: resources,
		TypeUrl:   resources[0].TypeUrl,
	}
}

func InterfaceSlice(slice interface{}) []interface{} {
	s := reflect.ValueOf(slice)
	if s.Kind() != reflect.Slice {
		panic("InterfaceSlice() given a non-slice type")
	}

	ret := make([]interface{}, s.Len())

	for i := 0; i < s.Len(); i++ {
		ret[i] = s.Index(i).Interface()
	}

	return ret
}

func ExtractRoutesFromListeners(ll []*listener.Listener) []string {
	routes := []string{}
	for _, l := range ll {
		for _, fc := range l.FilterChains {
			for _, filter := range fc.Filters {
				if filter.Name == wellknown.HTTPConnectionManager {
					filter.GetTypedConfig()
					hcon := &hcm.HttpConnectionManager{}
					if err := ptypes.UnmarshalAny(filter.GetTypedConfig(), hcon); err != nil {
						panic(err)
					}
					switch r := hcon.GetRouteSpecifier().(type) {
					case *hcm.HttpConnectionManager_Rds:
						routes = append(routes, r.Rds.RouteConfigName)
					}
				}
			}
		}
	}
	return routes
}

func ExtractListenerNames(ll []*listener.Listener) []string {
	res := []string{}
	for _, l := range ll {
		res = append(res, l.Name)
	}
	return res
}

func ExtractListener(name string, ll []*listener.Listener) *listener.Listener {
	for _, l := range ll {
		if l.Name == name {
			return l
		}
	}
	return nil
}

func ExtractTCPProxy(t test.Failer, fcs *listener.FilterChain) *envoy_extensions_filters_network_tcp_proxy_v3.TcpProxy {
	for _, fc := range fcs.Filters {
		if fc.Name == wellknown.TCPProxy {
			tcpProxy := &envoy_extensions_filters_network_tcp_proxy_v3.TcpProxy{}
			if fc.GetTypedConfig() != nil {
				if err := ptypes.UnmarshalAny(fc.GetTypedConfig(), tcpProxy); err != nil {
					t.Fatalf("failed to unmarshal tcp proxy")
				}
			}
			return tcpProxy
		}
	}
	return nil
}

func ExtractEndpoints(endpoints []*endpoint.ClusterLoadAssignment) map[string][]string {
	got := map[string][]string{}
	for _, cla := range endpoints {
		if cla == nil {
			continue
		}
		for _, ep := range cla.Endpoints {
			for _, lb := range ep.LbEndpoints {
				if lb.GetEndpoint().Address.GetSocketAddress() != nil {
					got[cla.ClusterName] = append(got[cla.ClusterName], lb.GetEndpoint().Address.GetSocketAddress().Address)
				} else {
					got[cla.ClusterName] = append(got[cla.ClusterName], lb.GetEndpoint().Address.GetPipe().Path)
				}
			}
		}
	}
	return got
}

func ExtractClusterEndpoints(clusters []*cluster.Cluster) map[string][]string {
	cla := []*endpoint.ClusterLoadAssignment{}
	for _, c := range clusters {
		cla = append(cla, c.LoadAssignment)
	}
	return ExtractEndpoints(cla)
}

func ExtractEdsClusterNames(cl []*cluster.Cluster) []string {
	res := []string{}
	for _, c := range cl {
		switch v := c.ClusterDiscoveryType.(type) {
		case *cluster.Cluster_Type:
			if v.Type != cluster.Cluster_EDS {
				continue
			}
		}
		res = append(res, c.Name)
	}
	return res
}
