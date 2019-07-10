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

package group

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/rbac/util"
	securityUtil "istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	rbacClusterConfigTmpl  = "testdata/istio-clusterrbacconfig.yaml.tmpl"
	rbacGroupListRulesTmpl = "testdata/istio-group-list-rbac-rules.yaml.tmpl"
)

func TestRBACV1Group(t *testing.T) {
	testIssuer1Token := jwt.TokenIssuer1
	testIssuer2Token := jwt.TokenIssuer2

	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := namespace.NewOrFail(t, ctx, "rbacv1-group-test", true)

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, securityUtil.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, securityUtil.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, securityUtil.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []util.TestCase{
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					Jwt:           testIssuer2Token,
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					Jwt:           testIssuer1Token,
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					Jwt:           testIssuer2Token,
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					Jwt:           testIssuer1Token,
					ExpectAllowed: true,
				},
			}

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, rbacGroupListRulesTmpl))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			// Sleep 60 seconds for the policy to take effect.
			// TODO: query pilot or app to know instead of sleep.
			time.Sleep(60 * time.Second)

			util.RunRBACTest(ctx, cases)
		})
}
