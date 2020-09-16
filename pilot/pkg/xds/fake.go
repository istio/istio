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
	"reflect"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoy_extensions_filters_network_tcp_proxy_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kube "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
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
	Configs []model.Config
	// If provided, the yaml string will be parsed and used as configs
	ConfigString string
	// If provided, the ConfigString will be treated as a go template, with this as input params
	ConfigTemplateInput interface{}
	// If provided, this mesh config will be used
	MeshConfig      *meshconfig.MeshConfig
	NetworksWatcher mesh.NetworksWatcher
}

type FakeDiscoveryServer struct {
	t          test.Failer
	Store      model.ConfigStore
	Discovery  *DiscoveryServer
	Env        *model.Environment
	KubeClient kubelib.Client
}

func (f *FakeDiscoveryServer) PushContext() *model.PushContext {
	f.Discovery.updateMutex.RLock()
	defer f.Discovery.updateMutex.RUnlock()
	return f.Env.PushContext
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
	configs, badKinds, err := crd.ParseInputs(configStr)
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
	plugins := []string{plugin.Authn, plugin.Authz, plugin.Health, plugin.Mixer}
	s := NewDiscoveryServer(env, plugins)
	go func() {
		for {
			select {
			// Read and drop events. This prevents the channel from getting backed up
			// In the future, we can likely track these for use in tests
			case <-s.pushChannel:
				s.updateMutex.RLock()
				pc := s.Env.PushContext
				s.updateMutex.RUnlock()
				_, _ = s.initPushContext(nil, pc)
			case <-stop:
				return
			}
		}
	}()
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
	if opts.NetworksWatcher == nil {
		opts.NetworksWatcher = mesh.NewFixedNetworksWatcher(nil)
	}
	env.NetworksWatcher = opts.NetworksWatcher

	serviceHandler := func(svc *model.Service, _ model.Event) {
		s.updateMutex.RLock()
		pc := s.Env.PushContext
		s.updateMutex.RUnlock()
		_, _ = s.initPushContext(nil, pc)
	}

	se := serviceentry.NewServiceDiscovery(configController, model.MakeIstioStore(configStore), s)
	serviceDiscovery.AddRegistry(se)
	kubeClient := kubelib.NewFakeClient(k8sObjects...)
	k8s, _ := kube.NewFakeControllerWithOptions(kube.FakeControllerOptions{
		ServiceHandler:  serviceHandler,
		Client:          kubeClient,
		ClusterID:       "Kubernetes",
		DomainSuffix:    "cluster.local",
		XDSUpdater:      s,
		NetworksWatcher: env,
	})
	kubeClient.RunAndWait(stop)
	serviceDiscovery.AddRegistry(k8s)
	for _, cfg := range configs {
		if _, err := configStore.Create(cfg); err != nil {
			t.Fatalf("failed to create config %v: %v", cfg.Name, err)
		}
	}
	cache.WaitForCacheSync(stop, serviceDiscovery.HasSynced)
	s.CachesSynced()
	if err := se.AppendWorkloadHandler(k8s.WorkloadInstanceHandler); err != nil {
		t.Fatal(err)
	}
	if err := k8s.AppendWorkloadHandler(se.WorkloadInstanceHandler); err != nil {
		t.Fatal(err)
	}

	se.ResyncEDS()

	fake := &FakeDiscoveryServer{
		t:          t,
		Store:      configController,
		KubeClient: kubeClient,
		Discovery:  s,
		Env:        env,
	}

	// currently meshNetworks gateways are stored on the push context
	fake.refreshPushContext()
	env.AddNetworksHandler(fake.refreshPushContext)

	return fake
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
	pc := f.PushContext()
	p.SetSidecarScope(pc)
	p.SetGatewaysForProxy(pc)
	if err := p.SetServiceInstances(f.Discovery.Env.ServiceDiscovery); err != nil {
		f.t.Fatal(err)
	}
	p.DiscoverIPVersions()
	return p
}

func (f *FakeDiscoveryServer) Listeners(p *model.Proxy) []*listener.Listener {
	return f.Discovery.ConfigGenerator.BuildListeners(p, f.PushContext())
}

func (f *FakeDiscoveryServer) Clusters(p *model.Proxy) []*cluster.Cluster {
	return f.Discovery.ConfigGenerator.BuildClusters(p, f.PushContext())
}

func (f *FakeDiscoveryServer) Endpoints(p *model.Proxy) []*endpoint.ClusterLoadAssignment {
	loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
	for _, c := range ExtractEdsClusterNames(f.Clusters(p)) {
		loadAssignments = append(loadAssignments, f.Discovery.generateEndpoints(createEndpointBuilder(c, p, f.PushContext())))
	}
	return loadAssignments
}

func (f *FakeDiscoveryServer) Routes(p *model.Proxy) []*route.RouteConfiguration {
	return f.Discovery.ConfigGenerator.BuildHTTPRoutes(p, f.PushContext(), ExtractRoutesFromListeners(f.Listeners(p)))
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
				addr := lb.GetEndpoint().Address.GetSocketAddress()
				if addr != nil {
					got[cla.ClusterName] = append(got[cla.ClusterName], fmt.Sprintf("%s:%d", addr.Address, addr.GetPortValue()))
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
