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

package v1alpha3

import (
	"testing"

	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/registry"
)

const (
	mixer = "mixer"
)

func TestBuildSharedPushState(t *testing.T) {
	plugins := registry.NewPlugins([]string{mixer})
	configgen := NewConfigGenerator(plugins)

	serviceDiscovery := &fakes.ServiceDiscovery{}

	serviceDiscovery.ServicesReturns([]*model.Service{
		{
			Hostname:    "*.example.org",
			Address:     "1.1.1.1",
			ClusterVIPs: make(map[string]string),
			Ports: model.PortList{
				&model.Port{
					Name:     "default",
					Port:     8080,
					Protocol: model.ProtocolHTTP,
				},
			},
		},
	}, nil)

	meshConfig := &meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
	}

	configStore := &fakes.IstioConfigStore{}

	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ServiceAccounts:  &fakes.ServiceAccounts{},
		IstioConfigStore: configStore,
		Mesh:             meshConfig,
		MixerSAN:         []string{},
	}

	env.PushContext = model.NewPushContext()
	env.PushContext.InitContext(env)

	configgen.buildSharedPushStateForSidecars(env, env.PushContext)
	sidecarsByNamespace := env.PushContext.GetAllSidecarScopes()
	outbounddisablepolicy := plugin.CacheKey{
		Hostname:  "*.example.org",
		Direction: "outbound",
		Policy:    "disable",
	}
	outboundenablepolicy := plugin.CacheKey{
		Hostname:  "*.example.org",
		Direction: "outbound",
		Policy:    "enable",
	}
	for _, sidecarScopes := range sidecarsByNamespace {
		for _, sc := range sidecarScopes {
			filterConfig := sc.RDSPerRouteFilterConfig[mixer]
			c, ok := filterConfig.(map[plugin.CacheKey]*types.Struct)
			if !ok {
				t.Fatal("can not get expected Filter config")
			}

			_, ok = c[outbounddisablepolicy]
			if !ok {
				t.Fatalf("can not get expected Filter config: " +
					outbounddisablepolicy.Hostname + "|" + outbounddisablepolicy.Direction + "|" + outbounddisablepolicy.Policy)
			}

			_, ok = c[outboundenablepolicy]
			if !ok {
				t.Fatalf("can not get expected Filter config: " +
					outboundenablepolicy.Hostname + "|" + outboundenablepolicy.Direction + "|" + outboundenablepolicy.Policy)
			}
		}
	}

}
