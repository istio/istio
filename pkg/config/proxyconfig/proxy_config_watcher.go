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

package proxyconfig

import (
	"sync"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/pkg/log"
)

var _ Generator = &proxyConfigGenerator{}

// Generator generates ProxyConfig resources given a ProxyTargetConfig.
// Wrapping it here rather than calling directly out to EffectiveProxyConfig allows us
// to cache ProxyConfig generation results.
type Generator interface {
	Generate(*model.ProxyConfigTarget) *meshconfig.ProxyConfig
}

// NewGenerator creates a new generator capable of generating ProxyConfig on the fly.
func NewGenerator(configController model.ConfigStoreCache, store model.IstioConfigStore, mesh mesh.Holder) Generator {
	log.Info("SAM: creating generator")
	g := &proxyConfigGenerator{
		store: store,
		mesh:  mesh,
		mu:    sync.RWMutex{},
	}
	if configController != nil {
		log.Info("SAM: registering event handler for ProxyConfig updates")
		configController.RegisterEventHandler(gvk.ProxyConfig, g.proxyConfigHandler)
	}

	return g
}

func (g *proxyConfigGenerator) Generate(target *model.ProxyConfigTarget) *meshconfig.ProxyConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	log.Info("SAM: generating...")
	pc := model.EffectiveProxyConfig(target, g.store, g.mesh.Mesh())
	log.Infof("SAM: generated for target: %v, result: %v", target, pc)
	return model.EffectiveProxyConfig(target, g.store, g.mesh.Mesh())
}

func (g *proxyConfigGenerator) proxyConfigHandler(_, curr config.Config, event model.Event) {
	log.Info("SAM: in proxyConfigHandler")
	log.Infof("SAM: PC name: %s", curr.Name)
	g.mu.Lock()
	pcs, err := model.GetProxyConfigs(g.store, g.mesh.Mesh())
	if err != nil {
		log.Info("SAM: GetProxyConfigs failed: %v", err)
	}
	log.Infof("SAM: retrieved new ProxyConfigs: %v", pcs)
	g.proxyConfigs = pcs
	g.mu.Unlock()
}

type proxyConfigGenerator struct {
	// store is used to list out ProxyConfig resources.
	store model.IstioConfigStore

	// proxyConfigs stores a map from namespace to ProxyConfig resources.
	proxyConfigs *model.ProxyConfigs

	// mesh is required to merge meshConfig.defaultConfig.
	mesh mesh.Holder
	mu   sync.RWMutex
}
