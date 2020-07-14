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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
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
)

const (
	authHeaderKey = "Authorization"
)

// TestRequestAuthentication tests beta authn policy for jwt.
func TestRequestAuthentication(t *testing.T) {
	payload1 := strings.Split(jwt.TokenIssuer1, ".")[1]
	payload2 := strings.Split(jwt.TokenIssuer2, ".")[1]
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "req-authn",
				Inject: true,
			})

			// Apply the policy.
			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}
			jwtPolicies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, "testdata/requestauthn/a-authn.yaml.tmpl"),
				file.AsStringOrFail(t, "testdata/requestauthn/b-authn-authz.yaml.tmpl"),
				file.AsStringOrFail(t, "testdata/requestauthn/c-authn.yaml.tmpl"),
				file.AsStringOrFail(t, "testdata/requestauthn/e-authn.yaml.tmpl"),
			)
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), jwtPolicies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), jwtPolicies...)

			var a, b, c, d, e echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns, false, nil)).
				With(&d, util.EchoConfig("d", ns, false, nil)).
				With(&e, util.EchoConfig("e", ns, false, nil)).
				BuildOrFail(t)

			testCases := []authn.TestCase{
				{
					Name: "valid-token-noauthz",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
					ExpectHeaders: map[string]string{
						authHeaderKey:    "",
						"X-Test-Payload": payload1,
					},
				},
				{
					Name: "valid-token-2-noauthz",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer2},
							},
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
					ExpectHeaders: map[string]string{
						authHeaderKey:    "",
						"X-Test-Payload": payload2,
					},
				},
				{
					Name: "expired-token-noauthz",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenExpired},
							},
						},
					},
					ExpectResponseCode: response.StatusUnauthorized,
				},
				{
					Name: "no-token-noauthz",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
				},
				// Following app b is configured with authorization, only request with valid JWT succeed.
				{
					Name: "valid-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
					ExpectHeaders: map[string]string{
						authHeaderKey: "",
					},
				},
				{
					Name: "expired-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenExpired},
							},
						},
					},
					ExpectResponseCode: response.StatusUnauthorized,
				},
				{
					Name: "no-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectResponseCode: response.StatusCodeForbidden,
				},
				{
					Name: "no-authn-authz",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
				},
				{
					Name: "valid-token-forward",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   e,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
					ExpectHeaders: map[string]string{
						authHeaderKey:    "Bearer " + jwt.TokenIssuer1,
						"X-Test-Payload": payload1,
					},
				},
				{
					Name: "invalid aud",
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
						},
					},
					ExpectResponseCode: response.StatusCodeForbidden,
				},
				{
					Name: "valid aud",
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1WithAud},
							},
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
				},
				{
					Name: "verify policies are combined",
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer2},
							},
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
				},
			}
			for _, c := range testCases {
				t.Run(c.Name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, c.CheckAuthn,
						retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}
		})
}

// TestIngressRequestAuthentication tests beta authn policy for jwt on ingress.
// The policy is also set at global namespace, with authorization on ingressgateway.
func TestIngressRequestAuthentication(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			var ingr ingress.Instance
			var err error
			if ingr, err = ingress.New(ctx, ingress.Config{
				Istio: ist,
			}); err != nil {
				t.Fatal(err)
			}

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "req-authn-ingress",
				Inject: true,
			})

			// Apply the policy.
			namespaceTmpl := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootNamespace,
			}

			applyPolicy := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, namespaceTmpl, file.AsStringOrFail(t, filename))
				ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
				return policy
			}

			securityPolicies := applyPolicy("testdata/requestauthn/global-jwt.yaml.tmpl", rootNS{})
			ingressCfgs := applyPolicy("testdata/requestauthn/ingress.yaml.tmpl", ns)

			defer ctx.Config().DeleteYAMLOrFail(t, rootNS{}.Name(), securityPolicies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), ingressCfgs...)

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				BuildOrFail(t)

			// These test cases verify in-mesh traffic doesn't need tokens.
			testCases := []authn.TestCase{
				{
					Name: "in-mesh-with-expired-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenExpired},
							},
						},
					},
					ExpectResponseCode: response.StatusUnauthorized,
				},
				{
					Name: "in-mesh-without-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectResponseCode: response.StatusCodeOK,
				},
			}
			for _, c := range testCases {
				t.Run(c.Name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, c.CheckAuthn,
						retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}

			// These test cases verify requests go through ingress will be checked for validate token.
			ingTestCases := []struct {
				Name               string
				Host               string
				Path               string
				Token              string
				ExpectResponseCode int
			}{
				{
					Name:               "deny without token",
					Host:               "example.com",
					Path:               "/",
					ExpectResponseCode: 403,
				},
				{
					Name:               "allow with sub-1 token",
					Host:               "example.com",
					Path:               "/",
					Token:              jwt.TokenIssuer1,
					ExpectResponseCode: 200,
				},
				{
					Name:               "deny with sub-2 token",
					Host:               "example.com",
					Path:               "/",
					Token:              jwt.TokenIssuer2,
					ExpectResponseCode: 403,
				},
				{
					Name:               "deny with expired token",
					Host:               "example.com",
					Path:               "/",
					Token:              jwt.TokenExpired,
					ExpectResponseCode: 401,
				},
				{
					Name:               "allow with sub-1 token on any.com",
					Host:               "any-request-principlal-ok.com",
					Path:               "/",
					Token:              jwt.TokenIssuer1,
					ExpectResponseCode: 200,
				},
				{
					Name:               "allow with sub-2 token on any.com",
					Host:               "any-request-principlal-ok.com",
					Path:               "/",
					Token:              jwt.TokenIssuer2,
					ExpectResponseCode: 200,
				},
				{
					Name:               "deny without token on any.com",
					Host:               "any-request-principlal-ok.com",
					Path:               "/",
					ExpectResponseCode: 403,
				},
				{
					Name:               "deny with token on other host",
					Host:               "other-host.com",
					Path:               "/",
					Token:              jwt.TokenIssuer1,
					ExpectResponseCode: 403,
				},
				{
					Name:               "allow healthz",
					Host:               "example.com",
					Path:               "/healthz",
					ExpectResponseCode: 200,
				},
			}

			for _, c := range ingTestCases {
				t.Run(c.Name, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, func() error {
						return authn.CheckIngress(ingr, c.Host, c.Path, c.Token, c.ExpectResponseCode)
					},
						retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
				})
			}
		})
}
