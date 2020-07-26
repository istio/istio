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

package security

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
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

type rootNS struct{}

func (i rootNS) Name() string {
	return rootNamespace
}

// TestAuthorization_mTLS tests v1beta1 authorization with mTLS.
func TestAuthorization_mTLS(t *testing.T) {
	framework.NewTest(t).
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
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns2, false, nil)).
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
				file.AsStringOrFail(t, "testdata/authz/v1beta1-mtls.yaml.tmpl"))

			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_JWT tests v1beta1 authorization with JWT token claims.
func TestAuthorization_JWT(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-jwt",
				Inject: true,
			})

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/authz/v1beta1-jwt.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policies...)

			var a, b, c, d echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns, false, nil)).
				With(&d, util.EchoConfig("d", ns, false, nil)).
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

				// Test condition "request.auth.principal" on path "/valid-jwt".
				newTestCase(d, "[NoJWT]", "", "/valid-jwt", false),
				newTestCase(d, "[Token1]", jwt.TokenIssuer1, "/valid-jwt", true),
				newTestCase(d, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/valid-jwt", true),
				newTestCase(d, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/valid-jwt", true),

				// Test condition "request.auth.presenter" on suffix "/presenter".
				newTestCase(d, "[Token1]", jwt.TokenIssuer1, "/request/presenter", false),
				newTestCase(d, "[Token1WithAud]", jwt.TokenIssuer1, "/request/presenter", false),
				newTestCase(d, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/presenter-x", false),
				newTestCase(d, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/presenter", true),

				// Test condition "request.auth.audiences" on suffix "/audiences".
				newTestCase(d, "[Token1]", jwt.TokenIssuer1, "/request/audiences", false),
				newTestCase(d, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/audiences", false),
				newTestCase(d, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/request/audiences-x", false),
				newTestCase(d, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/request/audiences", true),
			}

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_WorkloadSelector tests the workload selector for the v1beta1 policy in two namespaces.
func TestAuthorization_WorkloadSelector(t *testing.T) {
	framework.NewTest(t).
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
				With(&a, util.EchoConfig("a", ns1, false, nil)).
				With(&bInNS1, util.EchoConfig("b", ns1, false, nil)).
				With(&cInNS1, util.EchoConfig("c", ns1, false, nil)).
				With(&cInNS2, util.EchoConfig("c", ns2, false, nil)).
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
				ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
				return policy
			}

			policyNS1 := applyPolicy("testdata/authz/v1beta1-workload-ns1.yaml.tmpl", ns1)
			defer ctx.Config().DeleteYAMLOrFail(t, ns1.Name(), policyNS1...)
			policyNS2 := applyPolicy("testdata/authz/v1beta1-workload-ns2.yaml.tmpl", ns2)
			defer ctx.Config().DeleteYAMLOrFail(t, ns2.Name(), policyNS2...)
			policyNSRoot := applyPolicy("testdata/authz/v1beta1-workload-ns-root.yaml.tmpl", rootNS{})
			defer ctx.Config().DeleteYAMLOrFail(t, rootNS{}.Name(), policyNSRoot...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_Deny tests the authorization policy with action "DENY".
func TestAuthorization_Deny(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-deny",
				Inject: true,
			})

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns, false, nil)).
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
				newTestCase(b, "/deny?param=value", false),
				newTestCase(b, "/global-deny", false),
				newTestCase(b, "/global-deny?param=value", false),
				newTestCase(b, "/other", true),
				newTestCase(b, "/other?param=value", true),
				newTestCase(b, "/allow", true),
				newTestCase(b, "/allow?param=value", true),
				newTestCase(c, "/allow/admin", false),
				newTestCase(c, "/allow/admin?param=value", false),
				newTestCase(c, "/global-deny", false),
				newTestCase(c, "/global-deny?param=value", false),
				newTestCase(c, "/other", false),
				newTestCase(c, "/other?param=value", false),
				newTestCase(c, "/allow", true),
				newTestCase(c, "/allow?param=value", true),
			}

			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootNamespace,
			}

			applyPolicy := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
				return policy
			}

			policy := applyPolicy("testdata/authz/v1beta1-deny.yaml.tmpl", ns)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policy...)
			policyNSRoot := applyPolicy("testdata/authz/v1beta1-deny-ns-root.yaml.tmpl", rootNS{})
			defer ctx.Config().DeleteYAMLOrFail(t, rootNS{}.Name(), policyNSRoot...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_Deny tests the authorization policy with negative match.
func TestAuthorization_NegativeMatch(t *testing.T) {
	framework.NewTest(t).
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
				name := ""
				if ns != nil {
					name = ns.Name()
				}
				ctx.Config().ApplyYAMLOrFail(t, name, policy...)
				return policy
			}

			policies := applyPolicy("testdata/authz/v1beta1-negative-match.yaml.tmpl", nil)
			defer ctx.Config().DeleteYAMLOrFail(t, "", policies...)

			var a, b, c, d, x echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns, false, nil)).
				With(&d, util.EchoConfig("d", ns, false, nil)).
				With(&x, util.EchoConfig("x", ns2, false, nil)).
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
				// - path `/prefix/allowlist` should be excluded from the deny.
				// - path `/allow` should be allowed implicitly.
				newTestCase(a, b, "/prefix", false),
				newTestCase(a, b, "/prefix/other", false),
				newTestCase(a, b, "/prefix/allowlist", true),
				newTestCase(a, b, "/allow", true),
				newTestCase(x, b, "/prefix", false),
				newTestCase(x, b, "/prefix/other", false),
				newTestCase(x, b, "/prefix/allowlist", true),
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

// TestAuthorization_IngressGateway tests the authorization policy on ingress gateway.
func TestAuthorization_IngressGateway(t *testing.T) {
	framework.NewTest(t).
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
				ctx.Config().ApplyYAMLOrFail(t, "", policy...)
				return policy
			}
			policies := applyPolicy("testdata/authz/v1beta1-ingress-gateway.yaml.tmpl")
			defer ctx.Config().DeleteYAMLOrFail(t, "", policies...)

			var b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&b, util.EchoConfig("b", ns, false, nil)).
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

// TestAuthorization_EgressGateway tests v1beta1 authorization on egress gateway.
func TestAuthorization_EgressGateway(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-egress-gateway",
				Inject: true,
			})

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, echo.Config{
					Service:   "b",
					Namespace: ns,
					Subsets:   []echo.SubsetConfig{{}},
					Ports: []echo.Port{
						{
							Name:        "http",
							Protocol:    protocol.HTTP,
							ServicePort: 8090,
						},
					},
				}).
				With(&c, util.EchoConfig("c", ns, false, nil)).
				BuildOrFail(t)

			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootNamespace,
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/authz/v1beta1-egress-gateway.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, "", policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, "", policies...)

			cases := []struct {
				name  string
				path  string
				code  string
				body  string
				host  string
				from  echo.Workload
				token string
			}{
				{
					name: "allow path to company.com",
					path: "/allow",
					code: response.StatusCodeOK,
					body: "handled-by-egress-gateway",
					host: "www.company.com",
					from: getWorkload(a, t),
				},
				{
					name: "deny path to company.com",
					path: "/deny",
					code: response.StatusCodeForbidden,
					body: "RBAC: access denied",
					host: "www.company.com",
					from: getWorkload(a, t),
				},
				{
					name: "allow service account a to a-only.com over mTLS",
					path: "/",
					code: response.StatusCodeOK,
					body: "handled-by-egress-gateway",
					host: "a-only.com",
					from: getWorkload(a, t),
				},
				{
					name: "deny service account c to a-only.com over mTLS",
					path: "/",
					code: response.StatusCodeForbidden,
					body: "RBAC: access denied",
					host: "a-only.com",
					from: getWorkload(c, t),
				},
				{
					name:  "allow a with JWT to jwt-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeOK,
					body:  "handled-by-egress-gateway",
					host:  "jwt-only.com",
					from:  getWorkload(a, t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "allow c with JWT to jwt-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeOK,
					body:  "handled-by-egress-gateway",
					host:  "jwt-only.com",
					from:  getWorkload(c, t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "deny c with wrong JWT to jwt-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeForbidden,
					body:  "RBAC: access denied",
					host:  "jwt-only.com",
					from:  getWorkload(c, t),
					token: jwt.TokenIssuer2,
				},
				{
					name:  "allow service account a with JWT to jwt-and-a-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeOK,
					body:  "handled-by-egress-gateway",
					host:  "jwt-and-a-only.com",
					from:  getWorkload(a, t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "deny service account c with JWT to jwt-and-a-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeForbidden,
					body:  "RBAC: access denied",
					host:  "jwt-and-a-only.com",
					from:  getWorkload(c, t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "deny service account a with wrong JWT to jwt-and-a-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeForbidden,
					body:  "RBAC: access denied",
					host:  "jwt-and-a-only.com",
					from:  getWorkload(a, t),
					token: jwt.TokenIssuer2,
				},
			}

			for _, tc := range cases {
				request := &epb.ForwardEchoRequest{
					// Use a fake IP to make sure the request is handled by our test.
					Url:   fmt.Sprintf("http://10.4.4.4%s", tc.path),
					Count: 1,
					Headers: []*epb.Header{
						{
							Key:   "Host",
							Value: tc.host,
						},
					},
				}
				if tc.token != "" {
					request.Headers = append(request.Headers, &epb.Header{
						Key:   "Authorization",
						Value: "Bearer " + tc.token,
					})
				}
				t.Run(tc.name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, func() error {
						responses, err := tc.from.ForwardEcho(context.TODO(), request)
						if err != nil {
							return err
						}
						if len(responses) < 1 {
							return fmt.Errorf("received no responses from request to %s", tc.path)
						}
						if tc.code != responses[0].Code {
							return fmt.Errorf("want status %s but got %s", tc.code, responses[0].Code)
						}
						if !strings.Contains(responses[0].Body, tc.body) {
							return fmt.Errorf("want %q in body but not found: %s", tc.body, responses[0].Body)
						}
						return nil
					}, retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}
		})
}

// TestAuthorization_TCP tests the authorization policy on workloads using the raw TCP protocol.
func TestAuthorization_TCP(t *testing.T) {
	framework.NewTest(t).
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
			}, file.AsStringOrFail(t, "testdata/authz/v1beta1-tcp.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, "", policy...)
			defer ctx.Config().DeleteYAMLOrFail(t, "", policy...)

			var a, b, c, d, e, x echo.Instance
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
					Name:         "tcp-8092",
					Protocol:     protocol.TCP,
					InstancePort: 8092,
				},
				{
					Name:         "tcp-8093",
					Protocol:     protocol.TCP,
					InstancePort: 8093,
				},
			}
			echoboot.NewBuilderOrFail(t, ctx).
				With(&x, util.EchoConfig("x", ns2, false, nil)).
				With(&a, echo.Config{
					Subsets:        []echo.SubsetConfig{{}},
					Namespace:      ns,
					Service:        "a",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&b, echo.Config{
					Namespace:      ns,
					Subsets:        []echo.SubsetConfig{{}},
					Service:        "b",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&c, echo.Config{
					Namespace:      ns,
					Subsets:        []echo.SubsetConfig{{}},
					Service:        "c",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&d, echo.Config{
					Namespace:      ns,
					Subsets:        []echo.SubsetConfig{{}},
					Service:        "d",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&e, echo.Config{
					Namespace:      ns,
					Service:        "e",
					Ports:          ports,
					ServiceAccount: true,
				}).
				BuildOrFail(t)

			newTestCase := func(from, target echo.Instance, port string, expectAllowed bool, scheme scheme.Instance) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from,
						Options: echo.CallOptions{
							Target:   target,
							PortName: port,
							Scheme:   scheme,
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
				// - request to port tcp-8092 should be allowed because the port is not matched.
				newTestCase(a, b, "http-8090", false, scheme.HTTP),
				newTestCase(a, b, "http-8091", true, scheme.HTTP),
				newTestCase(a, b, "tcp-8092", true, scheme.TCP),

				// The policy on workload c denies request to port 8090:
				// - request to port http-8090 should be denied because the port is matched.
				// - request to http port 8091 should be allowed because the port is not matched.
				// - request to tcp port 8092 should be allowed because the port is not matched.
				// - request from b to tcp port 8092 should be allowed by default.
				// - request from b to tcp port 8093 should be denied because the principal is matched.
				// - request from x to tcp port 8092 should be denied because the namespace is matched.
				// - request from x to tcp port 8093 should be allowed by default.
				newTestCase(a, c, "http-8090", false, scheme.HTTP),
				newTestCase(a, c, "http-8091", true, scheme.HTTP),
				newTestCase(a, c, "tcp-8092", true, scheme.TCP),
				newTestCase(b, c, "tcp-8092", true, scheme.TCP),
				newTestCase(b, c, "tcp-8093", false, scheme.TCP),
				newTestCase(x, c, "tcp-8092", false, scheme.TCP),
				newTestCase(x, c, "tcp-8093", true, scheme.TCP),

				// The policy on workload d denies request from service account a and workloads in namespace 2:
				// - request from a to d should be denied because it has service account a.
				// - request from b to d should be allowed.
				// - request from c to d should be allowed.
				// - request from x to a should be allowed because there is no policy on a.
				// - request from x to d should be denied because it's in namespace 2.
				newTestCase(a, d, "tcp-8092", false, scheme.TCP),
				newTestCase(b, d, "tcp-8092", true, scheme.TCP),
				newTestCase(c, d, "tcp-8092", true, scheme.TCP),
				newTestCase(x, a, "tcp-8092", true, scheme.TCP),
				newTestCase(x, d, "tcp-8092", false, scheme.TCP),

				// The policy on workload e denies request with path "/other":
				// - request to port http-8090 should be allowed because the path is not matched.
				// - request to port http-8091 should be allowed because the path is not matched.
				// - request to port tcp-8092 should be denied because policy uses HTTP fields.
				newTestCase(a, e, "http-8090", true, scheme.HTTP),
				newTestCase(a, e, "http-8091", true, scheme.HTTP),
				newTestCase(a, e, "tcp-8092", false, scheme.TCP),
			}

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_Conditions tests v1beta1 authorization with conditions.
func TestAuthorization_Conditions(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			nsA := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-conditions-a",
				Inject: true,
			})
			nsB := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-conditions-b",
				Inject: true,
			})
			nsC := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-conditions-c",
				Inject: true,
			})

			portC := 8090
			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", nsA, false, nil)).
				With(&b, util.EchoConfig("b", nsB, false, nil)).
				With(&c, echo.Config{
					Service:   "c",
					Namespace: nsC,
					Subsets:   []echo.SubsetConfig{{}},
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							InstancePort: portC,
						},
					},
				}).
				BuildOrFail(t)

			args := map[string]string{
				"NamespaceA": nsA.Name(),
				"NamespaceB": nsB.Name(),
				"NamespaceC": nsC.Name(),
				"IpA":        getWorkload(a, t).Address(),
				"IpB":        getWorkload(b, t).Address(),
				"IpC":        getWorkload(c, t).Address(),
				"PortC":      fmt.Sprintf("%d", portC),
			}
			policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, "testdata/authz/v1beta1-conditions.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, "", policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, "", policies...)

			newTestCase := func(from echo.Instance, path string, headers map[string]string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					Headers:       headers,
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []rbacUtil.TestCase{
				newTestCase(a, "/request-headers", map[string]string{"x-foo": "foo"}, true),
				newTestCase(b, "/request-headers", map[string]string{"x-foo": "foo"}, true),
				newTestCase(a, "/request-headers", map[string]string{"x-foo": "bar"}, false),
				newTestCase(b, "/request-headers", map[string]string{"x-foo": "bar"}, false),
				newTestCase(a, "/request-headers", nil, false),
				newTestCase(b, "/request-headers", nil, false),

				newTestCase(a, "/source-ip-a", nil, true),
				newTestCase(b, "/source-ip-a", nil, false),
				newTestCase(a, "/source-ip-b", nil, false),
				newTestCase(b, "/source-ip-b", nil, true),

				newTestCase(a, "/source-namespace-a", nil, true),
				newTestCase(b, "/source-namespace-a", nil, false),
				newTestCase(a, "/source-namespace-b", nil, false),
				newTestCase(b, "/source-namespace-b", nil, true),

				newTestCase(a, "/source-principal-a", nil, true),
				newTestCase(b, "/source-principal-a", nil, false),
				newTestCase(a, "/source-principal-b", nil, false),
				newTestCase(b, "/source-principal-b", nil, true),

				newTestCase(a, "/destination-ip-good", nil, true),
				newTestCase(b, "/destination-ip-good", nil, true),
				newTestCase(a, "/destination-ip-bad", nil, false),
				newTestCase(b, "/destination-ip-bad", nil, false),

				newTestCase(a, "/destination-port-good", nil, true),
				newTestCase(b, "/destination-port-good", nil, true),
				newTestCase(a, "/destination-port-bad", nil, false),
				newTestCase(b, "/destination-port-bad", nil, false),

				newTestCase(a, "/connection-sni-good", nil, true),
				newTestCase(b, "/connection-sni-good", nil, true),
				newTestCase(a, "/connection-sni-bad", nil, false),
				newTestCase(b, "/connection-sni-bad", nil, false),

				newTestCase(a, "/other", nil, false),
				newTestCase(b, "/other", nil, false),
			}

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_GRPC tests v1beta1 authorization with gRPC protocol.
func TestAuthorization_GRPC(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-grpc",
				Inject: true,
			})
			var a, b, c, d echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns, false, nil)).
				With(&d, util.EchoConfig("d", ns, false, nil)).
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
					ExpectAllowed: true,
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
					ExpectAllowed: true,
				},
			}
			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, "testdata/authz/v1beta1-grpc.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_Path tests the path is normalized before using in authorization. For example, a request
// with path "/a/../b" should be normalized to "/b" before using in authorization.
func TestAuthorization_Path(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "v1beta1-path",
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
					Subsets:   []echo.SubsetConfig{{}},
					Ports:     ports,
				}).
				With(&b, echo.Config{
					Service:   "b",
					Namespace: ns,
					Subsets:   []echo.SubsetConfig{{}},
					Ports:     ports,
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
				file.AsStringOrFail(t, "testdata/authz/v1beta1-path.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}
