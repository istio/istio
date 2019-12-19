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

package minimal

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i    istio.Instance
	p    pilot.Instance
	ingr ingress.Instance
	env  *kube.Environment
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("minimal_test", m).
		Label(label.CustomSetup).
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(&i, func(cfg *istio.Config) {
			cfg.Values["gateways.enabled"] = "true"
			cfg.ValuesFile = "values-istio-minimal.yaml"
			cfg.SkipWaitForValidationWebhook = true
		})).
		Setup(func(ctx resource.Context) (err error) {
			if p, err = pilot.New(ctx, pilot.Config{}); err != nil {
				return err
			}
			if ingr, err = ingress.New(ctx, ingress.Config{
				Istio: i,
			}); err != nil {
				return err
			}
			env = ctx.Environment().(*kube.Environment)
			return nil
		}).
		Run()
}
