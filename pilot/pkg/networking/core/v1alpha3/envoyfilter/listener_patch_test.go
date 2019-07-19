// Copyright 2018 Istio Authors
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

package envoyfilter

import (
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
)

var (
	testMesh = meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
	}
)

func buildEnvoyFilterConfigStore(configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch) *fakes.IstioConfigStore {
	return &fakes.IstioConfigStore{
		ListStub: func(typ, namespace string) (configs []model.Config, e error) {
			if typ == "envoy-filter" {
				configs = []model.Config{
					{
						ConfigMeta: model.ConfigMeta{
							Name:      "test-envoyfilter",
							Namespace: "not-default",
						},
						Spec: &networking.EnvoyFilter{
							ConfigPatches: configPatches,
						},
					},
				}
			}
			return
		},
		EnvoyFilterStub: func(workloadLabels model.LabelsCollection) *model.Config {
			return &model.Config{
				ConfigMeta: model.ConfigMeta{
					Name:      "test-envoyfilter",
					Namespace: "not-default",
				},
				Spec: &networking.EnvoyFilter{
					ConfigPatches: configPatches,
				},
			}
		},
	}
}

func buildPatchStruct(config string) *types.Struct {
	val := &types.Struct{}
	jsonpb.Unmarshal(strings.NewReader(config), val)
	return val
}

func newTestEnvironment(serviceDiscovery model.ServiceDiscovery, mesh meshconfig.MeshConfig, configStore model.IstioConfigStore) *model.Environment {
	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             &mesh,
	}

	env.PushContext = model.NewPushContext()
	_ = env.PushContext.InitContext(env)

	return env
}
