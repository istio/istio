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

package path

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/rbac/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	rbacPolicyYamlTmpl = "testdata/rbac-policy.yaml.tmpl"
)

// TestRBACV1Path tests the path is normalized before using in authorization. For example, a request
// with path "/a/../b" should be normalized to "/b" before using in authorization.
func TestRBACV1Path(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := namespace.NewOrFail(t, ctx, "rbacv1-path-test", true)
			ports := []echo.Port{
				{
					Name:        "http",
					Protocol:    model.ProtocolHTTP,
					ServicePort: 80,
				},
			}

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, echo.Config{
					Service:   "a",
					Namespace: ns,
					Ports:     ports,
					Galley:    g,
					Pilot:     p,
				}).
				With(&b, echo.Config{
					Service:   "b",
					Namespace: ns,
					Ports:     ports,
					Galley:    g,
					Pilot:     p,
				}).
				BuildOrFail(t)

			newTestCase := func(path string, expectAllowed bool) util.TestCase {
				return util.TestCase{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []util.TestCase{
				newTestCase("/public", true),
				newTestCase("/private", false),
				newTestCase("/public/../private", false),
				newTestCase("/public/./../private", false),
				newTestCase("/public/.././private", false),
				newTestCase("/public/%2E%2E/private", false),
				newTestCase("/public/%2e%2e/private", false),
				newTestCase("/public/%2E/%2E%2E/private", false),
				newTestCase("/public/%2e/%2e%2e/private", false),
				newTestCase("/public/%2E%2E/%2E/private", false),
				newTestCase("/public/%2e%2e/%2e/private", false),
			}

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, rbacPolicyYamlTmpl))
			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			// Sleep 60 seconds for the policy to take effect.
			// TODO: query pilot or app to know instead of sleep.
			time.Sleep(60 * time.Second)

			for _, tc := range cases {
				testName := fmt.Sprintf("%s->%s:%s%s[%v]",
					tc.Request.From.Config().Service,
					tc.Request.Options.Target.Config().Service,
					tc.Request.Options.PortName,
					tc.Request.Options.Path,
					tc.ExpectAllowed)
				t.Run(testName, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, tc.CheckRBACRequest, retry.Delay(5*time.Second), retry.Timeout(30*time.Second))
				})
			}
		})
}
