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
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/pilot/pkg/model"
)

type fakeGen struct {
	pcs        *model.ProxyConfigs
	meshconfig *meshconfig.MeshConfig
}

func NewFakeGenerator(mc *meshconfig.MeshConfig) Generator {
	return &fakeGen{
		pcs:        &model.ProxyConfigs{},
		meshconfig: mc,
	}
}

func (f *fakeGen) Generate(target *model.ProxyConfigTarget) *meshconfig.ProxyConfig {
	mc := f.meshconfig
	if mc == nil {
		mc = mesh.DefaultMeshConfig()
	}
	return model.EffectiveProxyConfig(target, model.MakeIstioStore(model.NewFakeStore()), mc)
}
