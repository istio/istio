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

package security

import (
	"strconv"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
	rbacUtil "istio.io/istio/tests/integration/security/util/rbac_util"
)

const (
	rbacClusterConfigTmpl = "testdata/rbac/clusterrbacconfig.yaml"
)

func TestV1_OptionalJWT(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1-optional-jwt",
				Inject: true,
			})

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []rbacUtil.TestCase{
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
					Jwt:           jwt.TokenIssuer1,
					ExpectAllowed: true,
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
					ExpectAllowed: false,
				},
			}

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, "testdata/rbac/v1-policy-optional-jwt.yaml.tmpl"))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

func TestV1_Group(t *testing.T) {
	testIssuer1Token := jwt.TokenIssuer1
	testIssuer2Token := jwt.TokenIssuer2

	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1-group",
				Inject: true,
			})

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []rbacUtil.TestCase{
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
				file.AsStringOrFail(t, "testdata/rbac/v1-policy-group.yaml.tmpl"))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

func TestV1_GRPC(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1-grpc",
				Inject: true,
			})
			var a, b, c, d echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, util.EchoConfig("d", ns, false, nil, g, p)).
				BuildOrFail(t)

			for _, mtls := range []bool{true, false} {
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
						ExpectAllowed: mtls,
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
						ExpectAllowed: mtls,
					},
				}
				namespaceTmpl := map[string]string{
					"Namespace": ns.Name(),
					"mtls":      strconv.FormatBool(mtls),
				}
				policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
					file.AsStringOrFail(t, rbacClusterConfigTmpl),
					file.AsStringOrFail(t, "testdata/rbac/v1-policy-grpc.yaml.tmpl"),
					file.AsStringOrFail(t, "testdata/rbac/mtls-for-a.yaml.tmpl"))
				g.ApplyConfigOrFail(t, ns, policies...)
				defer g.DeleteConfigOrFail(t, ns, policies...)

				rbacUtil.RunRBACTest(t, cases)
			}
		})
}

// TestV1_Path tests the path is normalized before using in authorization. For example, a request
// with path "/a/../b" should be normalized to "/b" before using in authorization.
func TestV1_Path(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1-path",
				Inject: true,
			})
			ports := []echo.Port{
				{
					Name:        "http",
					Protocol:    protocol.HTTP,
					ServicePort: 80,
					// We use a port > 1024 to not require root
					InstancePort: 8090,
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

			newTestCase := func(path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
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
			cases := []rbacUtil.TestCase{
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
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, "testdata/rbac/v1-policy-path.yaml.tmpl"))
			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}
