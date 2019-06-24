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

package basic

import (
	"testing"
	"time"

	securityUtil "istio.io/istio/tests/integration/security/util"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/rbac/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	rbacV2RulesTmpl = "testdata/istio-rbac-v2-rules.yaml.tmpl"
)

// TestRBACV2Basic tests basic features of RBAC V2 such as AuthorizationPolicy policy and exclusion.
func TestRBACV2Basic(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "rbacv2-basic-test", true)

			var a, b, c, d echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, securityUtil.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, securityUtil.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, securityUtil.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, securityUtil.EchoConfig("d", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []util.TestCase{
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
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
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/secret",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},

				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/secrets/admin",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/credentials/admin",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/any_path/admin",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/any_path",
						},
					},
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},
			}

			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, rbacV2RulesTmpl))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			// Sleep 60 seconds for the policy to take effect.
			// TODO(pitlv2109: Check to make sure policies have been created instead.
			time.Sleep(60 * time.Second)

			util.RunRBACTest(t, cases)
		})
}
