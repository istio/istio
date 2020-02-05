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
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/authn"
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

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			newTestCase := func(target echo.Instance, namePrefix string, jwt string, path string, expectAllowed bool) rbacUtil.TestCase {
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
					Jwt:           jwt,
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []rbacUtil.TestCase{
				newTestCase(b, "[NoJWT]", "", "/token1", false),
				newTestCase(b, "[NoJWT]", "", "/token2", false),
				newTestCase(b, "[Token1]", jwt.TokenIssuer1, "/token1", true),
				newTestCase(b, "[Token1]", jwt.TokenIssuer1, "/token2", false),
				newTestCase(b, "[Token2]", jwt.TokenIssuer2, "/token1", false),
				newTestCase(b, "[Token2]", jwt.TokenIssuer2, "/token2", true),
				newTestCase(b, "[Token1]", jwt.TokenIssuer1, "/tokenAny", true),
				newTestCase(b, "[Token2]", jwt.TokenIssuer2, "/tokenAny", true),
				newTestCase(b, "[PermissionToken1]", jwt.TokenIssuer1, "/permission", false),
				newTestCase(b, "[PermissionToken2]", jwt.TokenIssuer2, "/permission", false),
				newTestCase(b, "[PermissionTokenWithSpaceDelimitedScope]", jwt.TokenIssuer2WithSpaceDelimitedScope, "/permission", true),
				newTestCase(b, "[NoJWT]", "", "/tokenAny", false),
				newTestCase(c, "[NoJWT]", "", "/somePath", true),
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

// TestV1beta1_Deny tests the authorization policy with action "DENY".
func TestV1beta1_Deny(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-deny",
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
				newTestCase(b, "/deny", false),
				newTestCase(b, "/global-deny", false),
				newTestCase(b, "/other", true),
				newTestCase(b, "/allow", true),
				newTestCase(c, "/allow/admin", false),
				newTestCase(c, "/global-deny", false),
				newTestCase(c, "/other", false),
				newTestCase(c, "/allow", true),
			}

			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootNamespace,
			}

			applyPolicy := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				g.ApplyConfigOrFail(t, ns, policy...)
				return policy
			}

			policy := applyPolicy("testdata/rbac/v1beta1-deny.yaml.tmpl", ns)
			defer g.DeleteConfigOrFail(t, ns, policy...)
			policyNSRoot := applyPolicy("testdata/rbac/v1beta1-deny-ns-root.yaml.tmpl", rootNS{})
			defer g.DeleteConfigOrFail(t, rootNS{}, policyNSRoot...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestV1beta1_Deny tests the authorization policy with negative match.
func TestV1beta1_NegativeMatch(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-negative-match-1",
				Inject: true,
			})
			ns2 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-negative-match-2",
				Inject: true,
			})

			args := map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
			}

			applyPolicy := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				g.ApplyConfigOrFail(t, ns, policy...)
				return policy
			}

			policies := applyPolicy("testdata/rbac/v1beta1-negative-match.yaml.tmpl", nil)
			defer g.DeleteConfigOrFail(t, nil, policies...)

			var a, b, c, d, x echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, util.EchoConfig("d", ns, false, nil, g, p)).
				With(&x, util.EchoConfig("x", ns2, false, nil, g, p)).
				BuildOrFail(t)

			newTestCase := func(from, target echo.Instance, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from,
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

			// a, b, c and d are in the same namespace and x is in a different namespace.
			// a connects to b, c and d with mTLS.
			// x connects to b and c with mTLS, to d with plain-text.
			cases := []rbacUtil.TestCase{
				// Test the policy with overlapped `paths` and `not_paths` on b.
				// a and x should have the same results:
				// - path with prefix `/prefix` should be denied explicitly.
				// - path `/prefix/whitelist` should be excluded from the deny.
				// - path `/allow` should be allowed implicitly.
				newTestCase(a, b, "/prefix", false),
				newTestCase(a, b, "/prefix/other", false),
				newTestCase(a, b, "/prefix/whitelist", true),
				newTestCase(a, b, "/allow", true),
				newTestCase(x, b, "/prefix", false),
				newTestCase(x, b, "/prefix/other", false),
				newTestCase(x, b, "/prefix/whitelist", true),
				newTestCase(x, b, "/allow", true),

				// Test the policy that denies other namespace on c.
				// a should be allowed because it's from the same namespace.
				// x should be denied because it's from a different namespace.
				newTestCase(a, c, "/", true),
				newTestCase(x, c, "/", false),

				// Test the policy that denies plain-text traffic on d.
				// a should be allowed because it's using mTLS.
				// x should be denied because it's using plain-text.
				newTestCase(a, d, "/", true),
				newTestCase(x, d, "/", false),
			}

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestV1beta1_IngressGateway tests the authorization policy on ingress gateway.
func TestV1beta1_IngressGateway(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-ingress-gateway",
				Inject: true,
			})
			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootNamespace,
			}

			applyPolicy := func(filename string) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				g.ApplyConfigOrFail(t, nil, policy...)
				return policy
			}
			policies := applyPolicy("testdata/rbac/v1beta1-ingress-gateway.yaml.tmpl")
			defer g.DeleteConfigOrFail(t, nil, policies...)

			var b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				BuildOrFail(t)

			var ingr ingress.Instance
			var err error
			if ingr, err = ingress.New(ctx, ingress.Config{
				Istio: ist,
			}); err != nil {
				t.Fatal(err)
			}

			cases := []struct {
				Name     string
				Host     string
				Path     string
				WantCode int
			}{
				{
					Name:     "allow www.company.com",
					Host:     "www.company.com",
					Path:     "/",
					WantCode: 200,
				},
				{
					Name:     "deny www.company.com/private",
					Host:     "www.company.com",
					Path:     "/private",
					WantCode: 403,
				},
				{
					Name:     "allow www.company.com/public",
					Host:     "www.company.com",
					Path:     "/public",
					WantCode: 200,
				},
				{
					Name:     "deny internal.company.com",
					Host:     "internal.company.com",
					Path:     "/",
					WantCode: 403,
				},
				{
					Name:     "deny internal.company.com/private",
					Host:     "internal.company.com",
					Path:     "/private",
					WantCode: 403,
				},
			}

			for _, tc := range cases {
				t.Run(tc.Name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, func() error {
						return authn.CheckIngress(ingr, tc.Host, tc.Path, "", tc.WantCode)
					},
						retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				},
				)
			}
		})
}

// TestV1beta1_TCP tests the authorization policy on workloads using the raw TCP protocol.
func TestV1beta1_TCP(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-tcp-1",
				Inject: true,
			})
			ns2 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-tcp-2",
				Inject: true,
			})
			policy := tmpl.EvaluateAllOrFail(t, map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
			}, file.AsStringOrFail(t, "testdata/rbac/v1beta1-tcp.yaml.tmpl"))
			g.ApplyConfigOrFail(t, nil, policy...)
			defer g.DeleteConfigOrFail(t, nil, policy...)

			var a, b, c, d, x echo.Instance
			ports := []echo.Port{
				{
					Name:         "http-8090",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
				{
					Name:         "http-8091",
					Protocol:     protocol.HTTP,
					InstancePort: 8091,
				},
				{
					Name:         "tcp",
					Protocol:     protocol.TCP,
					InstancePort: 8092,
				},
			}
			echoboot.NewBuilderOrFail(t, ctx).
				With(&x, util.EchoConfig("x", ns2, false, nil, g, p)).
				With(&a, echo.Config{
					Namespace:      ns,
					Galley:         g,
					Pilot:          p,
					Service:        "a",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&b, echo.Config{
					Namespace:      ns,
					Galley:         g,
					Pilot:          p,
					Service:        "b",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&c, echo.Config{
					Namespace:      ns,
					Galley:         g,
					Pilot:          p,
					Service:        "c",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&d, echo.Config{
					Namespace:      ns,
					Galley:         g,
					Pilot:          p,
					Service:        "d",
					Ports:          ports,
					ServiceAccount: true,
				}).
				BuildOrFail(t)

			newTestCase := func(from, target echo.Instance, port string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from,
						Options: echo.CallOptions{
							Target:   target,
							PortName: port,
							Scheme:   scheme.HTTP,
							Path:     "/data",
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}

			cases := []rbacUtil.TestCase{
				// The policy on workload b denies request with path "/data" to port 8090:
				// - request to port http-8090 should be denied because both path and port are matched.
				// - request to port http-8091 should be allowed because the port is not matched.
				// - request to port tcp should be denied because a default deny-all policy should
				//   be generated when HTTP field (i.e. path) is used on TCP port.
				newTestCase(a, b, "http-8090", false),
				newTestCase(a, b, "http-8091", true),
				newTestCase(a, b, "tcp", false),

				// The policy on workload c denies request to port 8090:
				// - request to port http-8090 should be denied because the port is matched.
				// - request to http port 8091 should be allowed because the port is not matched.
				// - request to tcp port 8092 should be allowed because the port is not matched.
				newTestCase(a, c, "http-8090", false),
				newTestCase(a, c, "http-8091", true),
				newTestCase(a, c, "tcp", true),

				// The policy on workload d denies request from service account a and workloads in namespace 2:
				// - request from a to d should be denied because it has service account a.
				// - request from b to d should be allowed.
				// - request from c to d should be allowed.
				// - request from x to a should be allowed because there is no policy on a.
				// - request from x to d should be denied because it's in namespace 2.
				newTestCase(a, d, "tcp", false),
				newTestCase(b, d, "tcp", true),
				newTestCase(c, d, "tcp", true),
				newTestCase(x, a, "tcp", true),
				newTestCase(x, d, "tcp", false),
			}

			rbacUtil.RunRBACTest(t, cases)
		})
}
