// Copyright 2019 Istio Authors
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

package mcp

import (
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i istio.Instance
	g galley.Instance
	p pilot.Instance
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	meshCfg := mesh.DefaultMeshConfig()
	framework.
		NewSuite("mcp_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&i, setupConfig)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{CreateClient: true}); err != nil {
				return err
			}
			galleyHostPort := g.Address()[6:]
			meshCfg.ConfigSources = []*meshconfig.ConfigSource{
				{
					Address:             galleyHostPort,
					SubscribedResources: []meshconfig.Resource{meshconfig.Resource_SERVICE_REGISTRY},
				},
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley:     g,
				MeshConfig: &meshCfg,
				ServiceArgs: bootstrap.ServiceArgs{
					Registries: []string{},
				},
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["galley.enableServiceDiscovery"] = "true"
	cfg.Values["pilot.configSource.subscribedResources[0]"] = "SERVICE_REGISTRY"
	// ICP doesn't support set with list, so need to override here
	cfg.ControlPlaneValues = `
components:
  galley:
    enabled: true
values:
  galley:
    enableServiceDiscovery: true
  pilot:
    configSource:
      subscribedResources: ["SERVICE_REGISTRY"]
`
}
