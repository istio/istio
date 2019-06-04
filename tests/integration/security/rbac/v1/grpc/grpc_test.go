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

package grpc

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/tests/integration/security/util"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	rbacUtil "istio.io/istio/tests/integration/security/rbac/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

func TestRBACV1GRPC(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "rbacv2-grpc-test", true)
			var a, b, c, d echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, util.EchoConfig("d", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []rbacUtil.TestCase{
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "grpc",
							Scheme:   scheme.GRPC,
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "grpc",
							Scheme:   scheme.GRPC,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "grpc",
							Scheme:   scheme.GRPC,
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
			}

			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}

			policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, "testdata/istio-clusterrbacconfig.yaml.tmpl"),
				file.AsStringOrFail(t, "testdata/istio-rbac-v1-grpc-rules.yaml.tmpl"))
			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			// Sleep 60 seconds for the policy to take effect.
			time.Sleep(60 * time.Second)
			for _, tc := range cases {
				testName := fmt.Sprintf("%s->%s:%s%s[%v]",
					tc.Request.From.Config().Service,
					tc.Request.Options.Target.Config().Service,
					tc.Request.Options.PortName,
					tc.Request.Options.Path,
					tc.ExpectAllowed)
				t.Run(testName, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, tc.CheckRBACRequest, retry.Delay(time.Second), retry.Timeout(10*time.Second))
				})
			}
		})
}
