//go:build !agent
// +build !agent

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

package core

import (
	"bytes"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	meshconfig "istio.io/api/mesh/v1alpha1"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	cluster2 "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

type TestOptions struct {
	// If provided, these configs will be used directly
	Configs        []config.Config
	ConfigPointers []*config.Config

	// If provided, the yaml string will be parsed and used as configs
	ConfigString string
	// If provided, the ConfigString will be treated as a go template, with this as input params
	ConfigTemplateInput any

	// Services to pre-populate as part of the service discovery
	Services  []*model.Service
	Instances []*model.ServiceInstance
	Gateways  []model.NetworkGateway

	// If provided, this mesh config will be used
	MeshConfig      *meshconfig.MeshConfig
	NetworksWatcher mesh.NetworksWatcher

	// Additional service registries to use. A ServiceEntry and memory registry will always be created.
	ServiceRegistries []serviceregistry.Instance

	// Base ConfigController to use. If not set, a in-memory store will be used
	ConfigController model.ConfigStoreController

	// Additional ConfigStoreController to use
	ConfigStoreCaches []model.ConfigStoreController

	// CreateConfigStore defines a function that, given a ConfigStoreController, returns another ConfigStoreController to use
	CreateConfigStore func(c model.ConfigStoreController) model.ConfigStoreController

	// If set, we will not run immediately, allowing adding event handlers, etc prior to start.
	SkipRun bool

	// Used to set the serviceentry registry's cluster id
	ClusterID cluster2.ID

	// XDSUpdater to use. Otherwise, our own will be used
	XDSUpdater model.XDSUpdater
}

func (to TestOptions) FuzzValidate() bool {
	for _, csc := range to.ConfigStoreCaches {
		if csc == nil {
			return false
		}
	}
	for _, sr := range to.ServiceRegistries {
		if sr == nil {
			return false
		}
	}
	return true
}

type ConfigGenTest struct {
	t                    test.Failer
	store                model.ConfigStoreController
	env                  *model.Environment
	ConfigGen            *ConfigGeneratorImpl
	MemRegistry          *memregistry.ServiceDiscovery
	ServiceEntryRegistry *serviceentry.Controller
	Registry             model.Controller
	initialConfigs       []config.Config
	stop                 chan struct{}
	MemServiceRegistry   serviceregistry.Simple
}

func NewConfigGenTest(t test.Failer, opts TestOptions) *ConfigGenTest {
	t.Helper()
	configs := getConfigs(t, opts)
	cc := opts.ConfigController
	if cc == nil {
		cc = memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))
	}
	controllers := []model.ConfigStoreController{cc}
	if opts.CreateConfigStore != nil {
		controllers = append(controllers, opts.CreateConfigStore(cc))
	}
	controllers = append(controllers, opts.ConfigStoreCaches...)
	configController, _ := configaggregate.MakeWriteableCache(controllers, cc)

	m := opts.MeshConfig
	if m == nil {
		m = mesh.DefaultMeshConfig()
	}

	env := model.NewEnvironment()
	env.Watcher = meshwatcher.NewTestWatcher(m)

	xdsUpdater := opts.XDSUpdater
	if xdsUpdater == nil {
		xdsUpdater = model.NewEndpointIndexUpdater(env.EndpointIndex)
	}

	serviceDiscovery := aggregate.NewController(aggregate.Options{
		ConfigClusterID: opts.ClusterID,
	})
	se := serviceentry.NewController(
		configController,
		xdsUpdater,
		env.Watcher,
		serviceentry.WithClusterID(opts.ClusterID))
	// TODO allow passing in registry, for k8s, mem registry
	serviceDiscovery.AddRegistry(se)
	msd := memregistry.NewServiceDiscovery(opts.Services...)
	msd.XdsUpdater = xdsUpdater
	for _, instance := range opts.Instances {
		msd.AddInstance(instance)
	}
	msd.AddGateways(opts.Gateways...)
	msd.ClusterID = cluster2.ID(provider.Mock)
	memserviceRegistry := serviceregistry.Simple{
		ClusterID:           cluster2.ID(provider.Mock),
		ProviderID:          provider.Mock,
		DiscoveryController: msd,
	}
	serviceDiscovery.AddRegistry(memserviceRegistry)
	for _, reg := range opts.ServiceRegistries {
		serviceDiscovery.AddRegistry(reg)
	}
	if opts.NetworksWatcher == nil {
		opts.NetworksWatcher = meshwatcher.NewFixedNetworksWatcher(nil)
	}
	env.ServiceDiscovery = serviceDiscovery
	env.ConfigStore = configController
	env.NetworksWatcher = opts.NetworksWatcher
	env.Init()

	fake := &ConfigGenTest{
		t:                    t,
		store:                configController,
		env:                  env,
		initialConfigs:       configs,
		stop:                 test.NewStop(t),
		ConfigGen:            NewConfigGenerator(&model.DisabledCache{}),
		MemRegistry:          msd,
		MemServiceRegistry:   memserviceRegistry,
		Registry:             serviceDiscovery,
		ServiceEntryRegistry: se,
	}
	if !opts.SkipRun {
		fake.Run()
		if err := env.InitNetworksManager(xdsUpdater); err != nil {
			t.Fatal(err)
		}
		env.PushContext().InitContext(env, nil, nil)
	}
	return fake
}

func (f *ConfigGenTest) Run() {
	go f.Registry.Run(f.stop)
	go f.store.Run(f.stop)
	// Setup configuration. This should be done after registries are added so they can process events.
	for _, cfg := range f.initialConfigs {
		if _, err := f.store.Create(cfg); err != nil {
			f.t.Fatalf("failed to create config %v: %v", cfg.Name, err)
		}
	}

	// TODO allow passing event handlers for controller

	retry.UntilOrFail(f.t, f.store.HasSynced, retry.Delay(time.Millisecond))
	retry.UntilOrFail(f.t, f.Registry.HasSynced, retry.Delay(time.Millisecond))

	f.ServiceEntryRegistry.ResyncEDS()
}

// SetupProxy initializes a proxy for the current environment. This should generally be used when creating
// any proxy. For example, `p := SetupProxy(&model.Proxy{...})`.
func (f *ConfigGenTest) SetupProxy(p *model.Proxy) *model.Proxy {
	// Setup defaults
	if p == nil {
		p = &model.Proxy{}
	}
	if p.Metadata == nil {
		p.Metadata = &model.NodeMetadata{}
	}
	if p.Metadata.IstioVersion == "" {
		p.Metadata.IstioVersion = "1.23.0"
	}
	if p.IstioVersion == nil {
		p.IstioVersion = model.ParseIstioVersion(p.Metadata.IstioVersion)
	}
	if p.Type == "" {
		p.Type = model.SidecarProxy
	}
	if p.ConfigNamespace == "" {
		p.ConfigNamespace = "default"
	}
	if p.Metadata.Namespace == "" {
		p.Metadata.Namespace = p.ConfigNamespace
	}
	if p.ID == "" {
		p.ID = "app.test"
	}
	if p.DNSDomain == "" {
		p.DNSDomain = p.ConfigNamespace + ".svc.cluster.local"
	}
	if len(p.IPAddresses) == 0 {
		p.IPAddresses = []string{"1.1.1.1"}
	}

	// Initialize data structures
	pc := f.PushContext()
	p.SetSidecarScope(pc)
	p.SetServiceTargets(f.env.ServiceDiscovery)
	p.SetGatewaysForProxy(pc)
	p.DiscoverIPMode()
	return p
}

func (f *ConfigGenTest) Listeners(p *model.Proxy) []*listener.Listener {
	return f.ConfigGen.BuildListeners(p, f.PushContext())
}

func (f *ConfigGenTest) Clusters(p *model.Proxy) []*cluster.Cluster {
	raw, _ := f.ConfigGen.BuildClusters(p, &model.PushRequest{Push: f.PushContext()})
	res := make([]*cluster.Cluster, 0, len(raw))
	for _, r := range raw {
		c := &cluster.Cluster{}
		if err := r.Resource.UnmarshalTo(c); err != nil {
			f.t.Fatal(err)
		}
		res = append(res, c)
	}
	return res
}

func (f *ConfigGenTest) DeltaClusters(
	p *model.Proxy,
	configUpdated sets.Set[model.ConfigKey],
	watched *model.WatchedResource,
) ([]*cluster.Cluster, []string, bool) {
	raw, removed, _, delta := f.ConfigGen.BuildDeltaClusters(p,
		&model.PushRequest{
			Push: f.PushContext(), ConfigsUpdated: configUpdated,
		}, watched)
	res := make([]*cluster.Cluster, 0, len(raw))
	for _, r := range raw {
		c := &cluster.Cluster{}
		if err := r.Resource.UnmarshalTo(c); err != nil {
			f.t.Fatal(err)
		}
		res = append(res, c)
	}
	return res, removed, delta
}

func (f *ConfigGenTest) RoutesFromListeners(p *model.Proxy, l []*listener.Listener) []*route.RouteConfiguration {
	resources, _ := f.ConfigGen.BuildHTTPRoutes(p, &model.PushRequest{Push: f.PushContext()}, ExtractRoutesFromListeners(l))
	out := make([]*route.RouteConfiguration, 0, len(resources))
	for _, resource := range resources {
		routeConfig := &route.RouteConfiguration{}
		_ = resource.Resource.UnmarshalTo(routeConfig)
		out = append(out, routeConfig)
	}
	return out
}

func (f *ConfigGenTest) Routes(p *model.Proxy) []*route.RouteConfiguration {
	return f.RoutesFromListeners(p, f.Listeners(p))
}

func (f *ConfigGenTest) PushContext() *model.PushContext {
	return f.env.PushContext()
}

func (f *ConfigGenTest) Env() *model.Environment {
	return f.env
}

func (f *ConfigGenTest) Store() model.ConfigStoreController {
	return f.store
}

func getConfigs(t test.Failer, opts TestOptions) []config.Config {
	for _, p := range opts.ConfigPointers {
		if p != nil {
			opts.Configs = append(opts.Configs, *p)
		}
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
	cfgs := opts.Configs
	if configStr != "" {
		t0 := time.Now()
		configs, _, err := crd.ParseInputs(configStr)
		if err != nil {
			t.Fatalf("failed to read config: %v: %v", err, configStr)
		}
		// setup default namespace if not defined
		for _, c := range configs {
			if c.Namespace == "" {
				c.Namespace = "default"
			}
			// Set creation timestamp to same time for all of them for consistency.
			// If explicit setting is needed it can be set in the yaml
			if c.CreationTimestamp.IsZero() {
				c.CreationTimestamp = t0
			}
			cfgs = append(cfgs, c)
		}
	}
	return cfgs
}

// copied from xdstest to avoid import issues
func ExtractRoutesFromListeners(ll []*listener.Listener) []string {
	routes := []string{}
	for _, l := range ll {
		for _, fc := range l.FilterChains {
			for _, filter := range fc.Filters {
				if filter.Name == wellknown.HTTPConnectionManager {
					h := &hcm.HttpConnectionManager{}
					_ = filter.GetTypedConfig().UnmarshalTo(h)
					switch r := h.GetRouteSpecifier().(type) {
					case *hcm.HttpConnectionManager_Rds:
						routes = append(routes, r.Rds.RouteConfigName)
					}
				}
			}
		}
	}
	return routes
}
