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

package rbac

import (
	"testing"

	"istio.io/istio/pkg/config"
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
)

// TestV2_Basic tests basic features of RBAC V2 such as AuthorizationPolicy policy and exclusion.
func TestV2_Basic(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "v2-basic", true)

			var a, b, c, d echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, util.EchoConfig("d", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []TestCase{
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
				file.AsStringOrFail(t, "testdata/v2-policy-basic.yaml.tmpl"))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			RunRBACTest(t, cases)
		})
}

// TestV2_Extended tests extended features of RBAC v2 such as global namespace and inline role def.
func TestV2_Extended(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "v2-extended", true)

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []TestCase{
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/some-path",
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/bad-path/black-hole",
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
					ExpectAllowed: isMtlsEnabled,
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
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/good-path",
						},
					},
					ExpectAllowed: isMtlsEnabled,
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
						From: a,
						Options: echo.CallOptions{
							Target:   b,
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
					ExpectAllowed: true,
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
					ExpectAllowed: true,
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
					ExpectAllowed: true,
				},
			}

			rootNamespace := config.DefaultMeshConfig().RootNamespace
			namespaceTmpl := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootNamespace,
			}
			policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, "testdata/v2-policy-extended.yaml.tmpl"))

			// Pass in nil for namespace to apply the policies for all namespaces.
			g.ApplyConfigOrFail(t, nil, policies...)
			rootNs := namespace.ClaimOrFail(t, ctx, rootNamespace)
			defer func() { _ = g.DeleteConfig(ns, policies...) }()
			defer func() { _ = g.DeleteConfig(rootNs, policies...) }()

			RunRBACTest(t, cases)
		})
}

func TestV2_GRPC(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "v2-grpc", true)
			var a, b, c, d echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, util.EchoConfig("d", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []TestCase{
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
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, "testdata/v2-policy-grpc.yaml.tmpl"))
			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			RunRBACTest(t, cases)
		})
}

func TestV2_Group(t *testing.T) {
	JwtWithClaim1 := jwt.TokenIssuer1
	JwtWithClaim2 := jwt.TokenIssuer2

	framework.NewTest(t).
		// TODO(lei-tang): add the test to the native environment
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := namespace.NewOrFail(t, ctx, "v2-group", true)

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			cases := []TestCase{
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
					Jwt:           JwtWithClaim2,
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
					Jwt:           JwtWithClaim1,
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
					Jwt:           JwtWithClaim2,
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
					Jwt:           JwtWithClaim1,
					ExpectAllowed: true,
				},
			}

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, "testdata/v2-policy-group.yaml.tmpl"))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			RunRBACTest(t, cases)
		})
}
