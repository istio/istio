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
	"testing"

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

// TestV1beta1_mTLS tests v1beta1 authorization with mTLS.
func TestV1beta1_mTLS(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-mtls-ns1",
				Inject: true,
			})
			ns2 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-mtls-ns2",
				Inject: true,
			})

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns2, false, nil, g, p)).
				BuildOrFail(t)

			newTestCase := func(from echo.Instance, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []rbacUtil.TestCase{
				newTestCase(a, "/principal-a", true),
				newTestCase(a, "/namespace-2", false),
				newTestCase(c, "/principal-a", false),
				newTestCase(c, "/namespace-2", true),
			}

			args := map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/rbac/v1beta1-mtls.yaml.tmpl"))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestV1beta1_JWT tests v1beta1 authorization with JWT token claims.
func TestV1beta1_JWT(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-jwt",
				Inject: true,
			})

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/rbac/v1beta1-jwt.yaml.tmpl"))
			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				BuildOrFail(t)

			newTestCase := func(namePrefix string, jwt string, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					NamePrefix: namePrefix,
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					Jwt:           jwt,
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []rbacUtil.TestCase{
				newTestCase("[NoJWT]", "", "/token1", false),
				newTestCase("[NoJWT]", "", "/token2", false),
				newTestCase("[Token1]", jwt.TokenIssuer1, "/token1", true),
				newTestCase("[Token1]", jwt.TokenIssuer1, "/token2", false),
				newTestCase("[Token2]", jwt.TokenIssuer2, "/token1", false),
				newTestCase("[Token2]", jwt.TokenIssuer2, "/token2", true),
				newTestCase("[Token1]", jwt.TokenIssuer1, "/tokenAny", true),
				newTestCase("[Token2]", jwt.TokenIssuer2, "/tokenAny", true),
				newTestCase("[NoJWT]", "", "/tokenAny", false),
			}

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestV1beta1_OverrideV1alpha1 tests v1beta1 authorization overrides the v1alpha1 RBAC policy for
// a given workload.
func TestV1beta1_OverrideV1alpha1(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-override-v1alpha1",
				Inject: true,
			})

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			newTestCase := func(target echo.Instance, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   target,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []rbacUtil.TestCase{
				newTestCase(b, "/path-v1alpha1", false),
				newTestCase(b, "/path-v1beta1", true),
				newTestCase(c, "/path-v1alpha1", true),
				newTestCase(c, "/path-v1beta1", false),
			}

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/rbac/v1beta1-override-v1alpha1.yaml.tmpl"))
			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

type rootNS struct{}

func (i rootNS) Name() string {
	return rootNamespace
}

// TestV1beta1_WorkloadSelector tests the workload selector for the v1beta1 policy in two namespaces.
func TestV1beta1_WorkloadSelector(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns1 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-workload-1",
				Inject: true,
			})
			ns2 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-workload-2",
				Inject: true,
			})

			var a, bInNS1, cInNS1, cInNS2 echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns1, false, nil, g, p)).
				With(&bInNS1, util.EchoConfig("b", ns1, false, nil, g, p)).
				With(&cInNS1, util.EchoConfig("c", ns1, false, nil, g, p)).
				With(&cInNS2, util.EchoConfig("c", ns2, false, nil, g, p)).
				BuildOrFail(t)

			newTestCase := func(namePrefix string, target echo.Instance, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					NamePrefix: namePrefix,
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   target,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []rbacUtil.TestCase{
				newTestCase("[bInNS1]", bInNS1, "/policy-ns1-b", true),
				newTestCase("[bInNS1]", bInNS1, "/policy-ns1-c", false),
				newTestCase("[bInNS1]", bInNS1, "/policy-ns1-x", false),
				newTestCase("[bInNS1]", bInNS1, "/policy-ns1-all", true),
				newTestCase("[bInNS1]", bInNS1, "/policy-ns2-c", false),
				newTestCase("[bInNS1]", bInNS1, "/policy-ns2-all", false),
				newTestCase("[bInNS1]", bInNS1, "/policy-ns-root-c", false),

				newTestCase("[cInNS1]", cInNS1, "/policy-ns1-b", false),
				newTestCase("[cInNS1]", cInNS1, "/policy-ns1-c", true),
				newTestCase("[cInNS1]", cInNS1, "/policy-ns1-x", false),
				newTestCase("[cInNS1]", cInNS1, "/policy-ns1-all", true),
				newTestCase("[cInNS1]", cInNS1, "/policy-ns2-c", false),
				newTestCase("[cInNS1]", cInNS1, "/policy-ns2-all", false),
				newTestCase("[cInNS1]", cInNS1, "/policy-ns-root-c", true),

				newTestCase("[cInNS2]", cInNS2, "/policy-ns1-b", false),
				newTestCase("[cInNS2]", cInNS2, "/policy-ns1-c", false),
				newTestCase("[cInNS2]", cInNS2, "/policy-ns1-x", false),
				newTestCase("[cInNS2]", cInNS2, "/policy-ns1-all", false),
				newTestCase("[cInNS2]", cInNS2, "/policy-ns2-c", true),
				newTestCase("[cInNS2]", cInNS2, "/policy-ns2-all", true),
				newTestCase("[cInNS2]", cInNS2, "/policy-ns-root-c", true),
			}

			args := map[string]string{
				"Namespace1":    ns1.Name(),
				"Namespace2":    ns2.Name(),
				"RootNamespace": rootNamespace,
			}

			applyPolicy := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				g.ApplyConfigOrFail(t, ns, policy...)
				return policy
			}

			policyNS1 := applyPolicy("testdata/rbac/v1beta1-workload-ns1.yaml.tmpl", ns1)
			defer g.DeleteConfigOrFail(t, ns1, policyNS1...)
			policyNS2 := applyPolicy("testdata/rbac/v1beta1-workload-ns2.yaml.tmpl", ns2)
			defer g.DeleteConfigOrFail(t, ns2, policyNS2...)
			policyNSRoot := applyPolicy("testdata/rbac/v1beta1-workload-ns-root.yaml.tmpl", rootNS{})
			defer g.DeleteConfigOrFail(t, rootNS{}, policyNSRoot...)

			rbacUtil.RunRBACTest(t, cases)
		})
}
