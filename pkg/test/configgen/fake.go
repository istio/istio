package configgen

import (
	"sync"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	cluster2 "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test"
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

	// Additional ConfigStoreController to use
	ConfigStoreCaches []model.ConfigStoreController

	// CreateConfigStore defines a function that, given a ConfigStoreController, returns another ConfigStoreController to use
	CreateConfigStore func(c model.ConfigStoreController) model.ConfigStoreController

	// Mutex used for push context access. Should generally only be used by NewFakeDiscoveryServer
	PushContextLock *sync.RWMutex

	// If set, we will not run immediately, allowing adding event handlers, etc prior to start.
	SkipRun bool

	// Used to set the serviceentry registry's cluster id
	ClusterID cluster2.ID
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

type Factory = func(t test.Failer, to TestOptions) ConfigGenTest

type ConfigGenTest interface {
	Run()
	SetupProxy(p *model.Proxy) *model.Proxy
	Listeners(p *model.Proxy) []*listener.Listener
	Clusters(p *model.Proxy) []*cluster.Cluster
	DeltaClusters(
		p *model.Proxy,
		configUpdated map[model.ConfigKey]struct{},
		watched *model.WatchedResource,
	) ([]*cluster.Cluster, []string, bool)
	RoutesFromListeners(p *model.Proxy, l []*listener.Listener) []*route.RouteConfiguration
	Routes(p *model.Proxy) []*route.RouteConfiguration
	PushContext() *model.PushContext
	Env() *model.Environment
	Store() model.ConfigStoreController
	MemRegistry() *memregistry.ServiceDiscovery
}

var impl Factory

func SetFactory(f Factory) {
	if impl != nil {
		panic("multiple factories set")
	}
	impl = f
}

func New(t test.Failer, options TestOptions) ConfigGenTest {
	return impl(t, options)
}
