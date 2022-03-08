//go:build integ
// +build integ

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
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/scheck"
)

// TestRequestAuthentication tests beta authn policy for jwt.
func TestRequestAuthentication(t *testing.T) {
	payload1 := strings.Split(jwt.TokenIssuer1, ".")[1]
	payload2 := strings.Split(jwt.TokenIssuer2, ".")[1]
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("security.authentication.jwt").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			t.ConfigKube().EvalFile(map[string]string{
				"Namespace": ns.Name(),
			}, "../../../samples/jwt-server/jwt-server.yaml").ApplyOrFail(t, ns.Name())

			newTest := func(policy string, cases []echotest.TestCase) func(framework.TestContext) {
				return func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							if policy != "" {
								args := map[string]string{
									"Namespace": ns.Name(),
									"dst":       to.Config().Service,
								}
								return t.ConfigIstio().EvalFile(args, policy).Apply(ns.Name(), resource.Wait)
							}
							return nil
						}).
						From(
							// TODO(JimmyCYJ): enable VM for all test cases.
							util.SourceFilter(apps, ns.Name(), true)...).
						ConditionallyTo(echotest.ReachableDestinations).
						To(util.DestFilter(apps, ns.Name(), true)...).
						RunCases(cases)
				}
			}

			t.NewSubTest("authn-only").Run(newTest("testdata/requestauthn/authn-only.yaml.tmpl", []echotest.TestCase{
				{
					Name: "valid-token-noauthz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "",
								"X-Test-Payload":      payload1,
							}))
					},
				},
				{
					Name: "valid-token-2-noauthz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-2-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer2).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "",
								"X-Test-Payload":      payload2,
							}))
					},
				},
				{
					Name: "expired-token-noauthz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					Name: "expired-token-cors-preflight-request-allowed",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token-cors-preflight-request-allowed"
						opts.HTTP.Method = "OPTIONS"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenExpired).
							With(headers.AccessControlRequestMethod, "POST").
							With(headers.Origin, "https://istio.io").
							Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
				{
					Name: "expired-token-bad-cors-preflight-request-rejected",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token-cors-preflight-request-allowed"
						opts.HTTP.Method = "OPTIONS"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenExpired).
							With(headers.AccessControlRequestMethod, "POST").
							// the required Origin header is missing.
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					Name: "no-token-noauthz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-token-noauthz"
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
			}))

			t.NewSubTest("authn-authz").Run(newTest("testdata/requestauthn/authn-authz.yaml.tmpl", []echotest.TestCase{
				{
					Name: "valid-token",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts),
							check.RequestHeader(headers.Authorization, ""))
					},
				},
				{
					Name: "expired-token",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					Name: "no-token",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-token"
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
			}))

			t.NewSubTest("no-authn-authz").Run(newTest("", []echotest.TestCase{
				{
					Name: "no-authn-authz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-authn-authz"
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
			}))

			t.NewSubTest("forward").Run(newTest("testdata/requestauthn/forward.yaml.tmpl", []echotest.TestCase{
				{
					Name: "valid-token-forward",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "Bearer " + jwt.TokenIssuer1,
								"X-Test-Payload":      payload1,
							}))
					},
				},
			}))

			t.NewSubTest("remote").Run(newTest("testdata/requestauthn/remote.yaml.tmpl", []echotest.TestCase{
				{
					Name: "valid-token-forward-remote-jwks",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward-remote-jwks"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "Bearer " + jwt.TokenIssuer1,
								"X-Test-Payload":      payload1,
							}))
					},
				},
			}))

			t.NewSubTest("aud").Run(newTest("testdata/requestauthn/aud.yaml.tmpl", []echotest.TestCase{
				{
					Name: "invalid-aud",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-aud"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
				{
					Name: "valid-aud",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-aud"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1WithAud).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
				{
					Name: "verify-policies-are-combined",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/verify-policies-are-combined"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer2).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
			}))

			t.NewSubTest("invalid-jwks").Run(newTest("testdata/requestauthn/invalid-jwks.yaml.tmpl", []echotest.TestCase{
				{
					Name: "invalid-jwks-valid-token-noauthz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = ""
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					Name: "invalid-jwks-expired-token-noauthz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/invalid-jwks-valid-token-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					Name: "invalid-jwks-no-token-noauthz",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/invalid-jwks-no-token-noauthz"
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
			}))

			t.NewSubTest("headers-params").Run(newTest("testdata/requestauthn/headers-params.yaml.tmpl", []echotest.TestCase{
				{
					Name: "valid-params",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
				{
					Name: "valid-params-secondary",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?secondary_token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
				{
					Name: "invalid-params",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token_value=" + jwt.TokenIssuer1
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
				{
					Name: "valid-token-set",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token=" + jwt.TokenIssuer1 + "&secondary_token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
				{
					Name: "invalid-token-set",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token=" + jwt.TokenIssuer1 + "&secondary_token=" + jwt.TokenExpired
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					Name: "valid-header",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = ""
						opts.HTTP.Headers = headers.New().
							With("X-Jwt-Token", "Value "+jwt.TokenIssuer1).
							Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
				{
					Name: "valid-header-secondary",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = ""
						opts.HTTP.Headers = headers.New().
							With("Auth-Token", "Token "+jwt.TokenIssuer1).
							Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
				{
					Name: "invalid-header",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.HTTP.Path = ""
						opts.HTTP.Headers = headers.New().
							With("Auth-Header-Param", "Bearer "+jwt.TokenIssuer1).
							Build()
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
			}))
		})
}

// TestIngressRequestAuthentication tests beta authn policy for jwt on ingress.
// The policy is also set at global namespace, with authorization on ingressgateway.
func TestIngressRequestAuthentication(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("security.authentication.ingressjwt").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1

			// Apply the policy.
			t.ConfigIstio().EvalFile(map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
			}, "testdata/requestauthn/global-jwt.yaml.tmpl").ApplyOrFail(t, newRootNS(t).Name(), resource.Wait)

			newTest := func(policy string, cases []echotest.TestCase) func(framework.TestContext) {
				return func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							if policy != "" {
								args := map[string]string{
									"Namespace": ns.Name(),
									"dst":       to.Config().Service,
								}
								return t.ConfigIstio().EvalFile(args, policy).Apply(ns.Name(), resource.Wait)
							}
							return nil
						}).
						From(util.SourceFilter(apps, ns.Name(), false)...).
						ConditionallyTo(echotest.ReachableDestinations).
						ConditionallyTo(func(from echo.Instance, to echo.Instances) echo.Instances {
							return to.Match(echo.InCluster(from.Config().Cluster))
						}).
						To(util.DestFilter(apps, ns.Name(), false)...).
						RunCases(cases)
				}
			}

			t.NewSubTest("in-mesh-authn").Run(newTest("testdata/requestauthn/ingress.yaml.tmpl", []echotest.TestCase{
				{
					Name: "in-mesh-with-expired-token",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.Port.Name = "http"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					Name: "in-mesh-without-token",
					CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
						opts.Port.Name = "http"
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(opts))
					},
				},
			}))

			t.NewSubTest("ingress-authn").Run(func(t framework.TestContext) {
				// TODO(JimmyCYJ): add workload-agnostic test pattern to support ingress gateway tests.
				t.ConfigIstio().EvalFile(map[string]string{
					"Namespace": ns.Name(),
					"dst":       util.BSvc,
				}, "testdata/requestauthn/ingress.yaml.tmpl").ApplyOrFail(t, ns.Name())

				echotest.New(t, apps.All).
					From(util.SourceFilter(apps, ns.Name(), false)...).
					ConditionallyTo(echotest.ReachableDestinations).
					ConditionallyTo(func(from echo.Instance, to echo.Instances) echo.Instances {
						return to.Match(echo.InCluster(from.Config().Cluster))
					}).
					To(util.DestFilter(apps, ns.Name(), false)...).
					RunCasesViaIngress([]echotest.TestCase{
					{
						Name: "deny without token",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().WithHost("example.com").Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						Name: "allow with sub-1 token",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost("example.com").
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						Name: "deny with sub-2 token",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost("example.com").
								WithAuthz(jwt.TokenIssuer2).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						Name: "deny with expired token",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost("example.com").
								WithAuthz(jwt.TokenExpired).
								Build()
							opts.Check = check.Status(http.StatusUnauthorized)
						},
					},
					{
						Name: "allow with sub-1 token on any.com",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost("any-request-principlal-ok.com").
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						Name: "allow with sub-2 token on any.com",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost("any-request-principlal-ok.com").
								WithAuthz(jwt.TokenIssuer2).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						Name: "deny without token on any.com",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost("any-request-principlal-ok.com").
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						Name: "deny with token on other host",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost("other-host.com").
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						Name: "allow healthz",
						CustomizeCall: func(_ framework.TestContext, opts *echo.CallOptions) {
							opts.Port.Protocol = protocol.HTTP
							opts.HTTP.Path = "/healthz"
							opts.HTTP.Headers = headers.New().
								WithHost("example.com").
								Build()
							opts.Check = check.OK()
						},
					},
				})
			})
		})
}
