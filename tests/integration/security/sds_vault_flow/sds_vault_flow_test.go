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

package sds_vault_test

import (
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util/connection"
)

func TestSdsVaultCaFlow(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			ns := namespace.NewOrFail(t, ctx, "reachability", true)

			ports := []echo.Port{
				{
					Name:     "http",
					Protocol: model.ProtocolHTTP,
				},
				{
					Name:     "tcp",
					Protocol: model.ProtocolTCP,
				},
			}

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, echo.Config{
					Service:        "a",
					Namespace:      ns,
					ServiceAccount: true,
					Ports:          ports,
					Galley:         g,
					Pilot:          p,
				}).
				With(&b, echo.Config{
					Service:        "b",
					Namespace:      ns,
					ServiceAccount: true,
					Ports:          ports,
					Galley:         g,
					Pilot:          p,
				}).
				BuildOrFail(t)

			checkers := []connection.Checker{
				{
					From: a,
					Options: echo.CallOptions{
						Target:   b,
						PortName: "http",
						Scheme:   scheme.HTTP,
					},
					ExpectSuccess: true,
				},
			}

			// Apply the policy to the system namespace.
			deployment := tmpl.EvaluateOrFail(t, file.AsStringOrFail(t, "testdata/global-mtls.yaml"),
				map[string]string{
					"Namespace": ns.Name(),
				})
			g.ApplyConfigOrFail(t, systemNS, deployment)
			defer g.DeleteConfigOrFail(t, systemNS, deployment)

			// Sleep 3 seconds for the policy to take effect.
			time.Sleep(3 * time.Second)

			for _, checker := range checkers {
				retry.UntilSuccessOrFail(t, checker.Check, retry.Delay(time.Second), retry.Timeout(10*time.Second))
			}
		})
}
