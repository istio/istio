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

package sds_vault_test

import (
	"testing"
	"time"

	"istio.io/istio/tests/integration/security/util"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util/connection"
)

func TestSdsVaultCaFlow(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Istio 1.3 uses Trustworthy JWT, which, unlike normal k8s JWT, is not
			// recognized on Vault.
			// https://github.com/istio/istio/issues/17561,
			// https://github.com/istio/istio/issues/17572.
			t.Skip("skipped for Istio versions using Trustworthy JWT, https://github.com/istio/istio/issues/17572")

			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "sds-vault-flow",
				Inject: true,
			})

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
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

			// Apply the policy
			deployment := tmpl.EvaluateOrFail(t, file.AsStringOrFail(t, "testdata/config.yaml"),
				map[string]string{
					"Namespace": ns.Name(),
				})

			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), deployment)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), deployment)

			// Sleep 10 seconds for the policy to take effect.
			time.Sleep(10 * time.Second)

			for _, checker := range checkers {
				retry.UntilSuccessOrFail(t, checker.Check, retry.Delay(time.Second), retry.Timeout(10*time.Second))
			}
		})
}
