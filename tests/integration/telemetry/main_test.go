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

package telemetry

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i    istio.Instance
	g    galley.Instance
	p    pilot.Instance
	ingr ingress.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("telemetry_test", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&i, func(cfg *istio.Config) {
			cfg.Values["grafana.enabled"] = "true"
			// TODO remove once https://github.com/istio/istio/issues/20137 is fixed
			cfg.ControlPlaneValues = `
addonComponents:
  grafana:
    enabled: true`
		})).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			if ingr, err = ingress.New(ctx, ingress.Config{
				Istio: i,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}
