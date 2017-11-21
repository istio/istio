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

package discovery

import (
	"time"

	"github.com/golang/protobuf/ptypes"
	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/adapter/config/memory"
	"istio.io/istio/pilot/adapter/serviceregistry/aggregate"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform"
	"istio.io/istio/pilot/proxy"
	"istio.io/istio/pilot/proxy/envoy"
	"istio.io/istio/pilot/test/mock"
)

var (
	defaultDiscoveryOptions = envoy.DiscoveryServiceOptions{
		Port:            8080,
		EnableProfiling: true,
		EnableCaching:   true}
)

func makeMeshConfig() *proxyconfig.MeshConfig {
	mesh := proxy.DefaultMeshConfig()
	mesh.MixerAddress = "istio-mixer.istio-system:9091"
	mesh.RdsRefreshDelay = ptypes.DurationProto(10 * time.Millisecond)
	return &mesh
}

// mockController specifies a mock Controller for testing
type mockController struct{}

func (c *mockController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	return nil
}

func (c *mockController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	return nil
}

func (c *mockController) Run(<-chan struct{}) {}

func buildMockController() *aggregate.Controller {
	discovery1 := mock.NewDiscovery(
		map[string]*model.Service{
			mock.HelloService.Hostname:   mock.HelloService,
			mock.ExtHTTPService.Hostname: mock.ExtHTTPService,
		}, 2)

	discovery2 := mock.NewDiscovery(
		map[string]*model.Service{
			mock.WorldService.Hostname:    mock.WorldService,
			mock.ExtHTTPSService.Hostname: mock.ExtHTTPSService,
		}, 2)

	registry1 := aggregate.Registry{
		Name:             platform.ServiceRegistry("mockAdapter1"),
		ServiceDiscovery: discovery1,
		ServiceAccounts:  discovery1,
		Controller:       &mockController{},
	}

	registry2 := aggregate.Registry{
		Name:             platform.ServiceRegistry("mockAdapter2"),
		ServiceDiscovery: discovery2,
		ServiceAccounts:  discovery2,
		Controller:       &mockController{},
	}

	ctls := aggregate.NewController()
	ctls.AddRegistry(registry1)
	ctls.AddRegistry(registry2)

	return ctls
}

// PilotConfig gathers the dependencies of the Pilot discovery service into a single configuration structure.
type PilotConfig struct {
	ConfigCache      model.ConfigStoreCache
	ServiceDiscovery model.ServiceDiscovery
	ServiceAccounts  model.ServiceAccounts
	Controller       model.Controller
	Mesh             *proxyconfig.MeshConfig
}

// WithInMemoryConfigCache updates the config to use an in-memory cache, backed by the given config store.
func (cfg *PilotConfig) WithInMemoryConfigCache(configStore model.ConfigStore) *PilotConfig {
	cfg.ConfigCache = memory.NewController(configStore)
	return cfg
}

// WithMockDiscovery updates the config to use the mock Discovery
func (cfg *PilotConfig) WithMockDiscovery() *PilotConfig {
	serviceDiscovery := mock.Discovery
	serviceDiscovery.ClearErrors()

	cfg.ServiceDiscovery = serviceDiscovery
	cfg.ServiceAccounts = serviceDiscovery
	return cfg
}

// WithMockMesh updates the config with a mock Mesh for testing.
func (cfg *PilotConfig) WithMockMesh() *PilotConfig {
	cfg.Mesh = makeMeshConfig()
	return cfg
}

// WithMockServiceController updates the config with a mock Mesh for testing.
func (cfg *PilotConfig) WithMockServiceController() *PilotConfig {
	cfg.Controller = buildMockController()
	return cfg
}

// NewPilot creates a new test instance of the Pilot discovery service based on the given configuration.
func NewPilot(cfg *PilotConfig) (*envoy.DiscoveryService, error) {
	environment := proxy.Environment{
		ServiceDiscovery: cfg.ServiceDiscovery,
		ServiceAccounts:  cfg.ServiceAccounts,
		IstioConfigStore: model.MakeIstioStore(cfg.ConfigCache),
		Mesh:             cfg.Mesh}

	return envoy.NewDiscoveryService(
		cfg.Controller,
		cfg.ConfigCache,
		environment,
		defaultDiscoveryOptions)
}
