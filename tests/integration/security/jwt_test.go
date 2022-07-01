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
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
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

			for _, cluster := range t.Clusters() {
				t.ConfigKube(cluster).EvalFile(ns.Name(), map[string]string{
					"Namespace": ns.Name(),
				}, filepath.Join(env.IstioSrc, "samples/jwt-server", "jwt-server.yaml")).ApplyOrFail(t)
			}

			for _, cluster := range t.Clusters() {
				fetchFn := kube.NewPodFetch(cluster, ns.Name(), "app=jwt-server")
				_, err := kube.WaitUntilPodsAreReady(fetchFn)
				if err != nil {
					t.Fatalf("pod is not getting ready : %v", err)
				}
			}

			for _, cluster := range t.Clusters() {
				if _, _, err := kube.WaitUntilServiceEndpointsAreReady(cluster.Kube(), ns.Name(), "jwt-server"); err != nil {
					t.Fatalf("jwt-server failed with : %v", err)
				}
			}

			type testCase struct {
				name          string
				customizeCall func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions)
			}

			newTest := func(policy string, cases []testCase) func(framework.TestContext) {
				return func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							if policy != "" {
								args := map[string]string{
									"Namespace": ns.Name(),
									"dst":       to.Config().Service,
								}
								return t.ConfigIstio().EvalFile(ns.Name(), args, policy).Apply(apply.Wait)
							}
							return nil
						}).
						FromMatch(
							// TODO(JimmyCYJ): enable VM for all test cases.
							util.SourceMatcher(ns, true)).
						ConditionallyTo(echotest.ReachableDestinations).
						ToMatch(util.DestMatcher(ns, true)).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							for _, c := range cases {
								t.NewSubTest(c.name).Run(func(t framework.TestContext) {
									opts := echo.CallOptions{
										To: to,
										Port: echo.Port{
											Name: "http",
										},
									}

									// Apply any custom options for the test.
									c.customizeCall(t, from, &opts)

									from.CallOrFail(t, opts)
								})
							}
						})
				}
			}

			t.NewSubTest("authn-only").Run(newTest("testdata/requestauthn/authn-only.yaml.tmpl", []testCase{
				{
					name: "valid-token-noauthz",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "",
								"X-Test-Payload":      payload1,
							}))
					},
				},
				{
					name: "valid-token-2-noauthz",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-2-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer2).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "",
								"X-Test-Payload":      payload2,
							}))
					},
				},
				{
					name: "expired-token-noauthz",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "expired-token-cors-preflight-request-allowed",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token-cors-preflight-request-allowed"
						opts.HTTP.Method = "OPTIONS"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenExpired).
							With(headers.AccessControlRequestMethod, "POST").
							With(headers.Origin, "https://istio.io").
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "expired-token-bad-cors-preflight-request-rejected",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
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
					name: "no-token-noauthz",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-token-noauthz"
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
			}))

			t.NewSubTest("authn-authz").Run(newTest("testdata/requestauthn/authn-authz.yaml.tmpl", []testCase{
				{
					name: "valid-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t),
							check.RequestHeader(headers.Authorization, ""))
					},
				},
				{
					name: "expired-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "no-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-token"
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
			}))

			t.NewSubTest("no-authn-authz").Run(newTest("", []testCase{
				{
					name: "no-authn-authz",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-authn-authz"
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
			}))

			t.NewSubTest("forward").Run(newTest("testdata/requestauthn/forward.yaml.tmpl", []testCase{
				{
					name: "valid-token-forward",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "Bearer " + jwt.TokenIssuer1,
								"X-Test-Payload":      payload1,
							}))
					},
				},
			}))

			t.NewSubTest("remote").Run(newTest("testdata/requestauthn/remote.yaml.tmpl", []testCase{
				{
					name: "valid-token-forward-remote-jwks",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward-remote-jwks"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "Bearer " + jwt.TokenIssuer1,
								"X-Test-Payload":      payload1,
							}))
					},
				},
			}))

			t.NewSubTest("aud").Run(newTest("testdata/requestauthn/aud.yaml.tmpl", []testCase{
				{
					name: "invalid-aud",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-aud"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
				{
					name: "valid-aud",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-aud"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1WithAud).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "verify-policies-are-combined",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/verify-policies-are-combined"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer2).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
			}))

			t.NewSubTest("invalid-jwks").Run(newTest("testdata/requestauthn/invalid-jwks.yaml.tmpl", []testCase{
				{
					name: "invalid-jwks-valid-token-noauthz",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = ""
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "invalid-jwks-expired-token-noauthz",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/invalid-jwks-valid-token-noauthz"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "invalid-jwks-no-token-noauthz",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/invalid-jwks-no-token-noauthz"
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
			}))

			t.NewSubTest("headers-params").Run(newTest("testdata/requestauthn/headers-params.yaml.tmpl", []testCase{
				{
					name: "valid-params",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "valid-params-secondary",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?secondary_token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "invalid-params",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token_value=" + jwt.TokenIssuer1
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
				{
					name: "valid-token-set",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token=" + jwt.TokenIssuer1 + "&secondary_token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "invalid-token-set",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token?token=" + jwt.TokenIssuer1 + "&secondary_token=" + jwt.TokenExpired
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "valid-header",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = ""
						opts.HTTP.Headers = headers.New().
							With("X-Jwt-Token", "Value "+jwt.TokenIssuer1).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "valid-header-secondary",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = ""
						opts.HTTP.Headers = headers.New().
							With("Auth-Token", "Token "+jwt.TokenIssuer1).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "invalid-header",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
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
			systemNS := istio.ClaimSystemNamespaceOrFail(t, t)
			t.ConfigIstio().EvalFile(systemNS.Name(), map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
			}, "testdata/requestauthn/global-jwt.yaml.tmpl").ApplyOrFail(t, apply.Wait)

			type testCase struct {
				name          string
				customizeCall func(framework.TestContext, echo.Instance, *echo.CallOptions)
			}

			newTest := func(policy string, cases []testCase) func(framework.TestContext) {
				return func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							if policy != "" {
								args := map[string]string{
									"Namespace": ns.Name(),
									"dst":       to.Config().Service,
								}
								return t.ConfigIstio().EvalFile(ns.Name(), args, policy).Apply(apply.Wait)
							}
							return nil
						}).
						WithDefaultFilters(1, 1).
						FromMatch(util.SourceMatcher(ns, false)).
						ToMatch(util.DestMatcher(ns, false)).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							for _, c := range cases {
								t.NewSubTest(c.name).Run(func(t framework.TestContext) {
									opts := echo.CallOptions{
										To: to,
										Port: echo.Port{
											Name: "http",
										},
									}

									// Apply any custom options for the test.
									c.customizeCall(t, from, &opts)

									from.CallOrFail(t, opts)
								})
							}
						})
				}
			}

			t.NewSubTest("in-mesh-authn").Run(newTest("testdata/requestauthn/ingress.yaml.tmpl", []testCase{
				{
					name: "in-mesh-with-expired-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "in-mesh-without-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
			}))

			t.NewSubTest("ingress-authn").Run(func(t framework.TestContext) {
				// TODO(JimmyCYJ): add workload-agnostic test pattern to support ingress gateway tests.
				t.ConfigIstio().EvalFile(ns.Name(), map[string]string{
					"Namespace": ns.Name(),
					"dst":       util.BSvc,
				}, "testdata/requestauthn/ingress.yaml.tmpl").ApplyOrFail(t)

				for _, cluster := range t.Clusters() {
					ingr := ist.IngressFor(cluster)

					// These test cases verify requests go through ingress will be checked for validate token.
					ingTestCases := []struct {
						name          string
						customizeCall func(opts *echo.CallOptions)
					}{
						{
							name: "deny without token",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().WithHost("example.com").Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "allow with sub-1 token",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().
									WithHost("example.com").
									WithAuthz(jwt.TokenIssuer1).
									Build()
								opts.Check = check.OK()
							},
						},
						{
							name: "deny with sub-2 token",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().
									WithHost("example.com").
									WithAuthz(jwt.TokenIssuer2).
									Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "deny with expired token",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().
									WithHost("example.com").
									WithAuthz(jwt.TokenExpired).
									Build()
								opts.Check = check.Status(http.StatusUnauthorized)
							},
						},
						{
							name: "allow with sub-1 token on any.com",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().
									WithHost("any-request-principlal-ok.com").
									WithAuthz(jwt.TokenIssuer1).
									Build()
								opts.Check = check.OK()
							},
						},
						{
							name: "allow with sub-2 token on any.com",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().
									WithHost("any-request-principlal-ok.com").
									WithAuthz(jwt.TokenIssuer2).
									Build()
								opts.Check = check.OK()
							},
						},
						{
							name: "deny without token on any.com",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().
									WithHost("any-request-principlal-ok.com").
									Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "deny with token on other host",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/"
								opts.HTTP.Headers = headers.New().
									WithHost("other-host.com").
									WithAuthz(jwt.TokenIssuer1).
									Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "allow healthz",
							customizeCall: func(opts *echo.CallOptions) {
								opts.HTTP.Path = "/healthz"
								opts.HTTP.Headers = headers.New().
									WithHost("example.com").
									Build()
								opts.Check = check.OK()
							},
						},
					}

					for _, c := range ingTestCases {
						t.NewSubTest(c.name).Run(func(t framework.TestContext) {
							opts := echo.CallOptions{
								Port: echo.Port{
									Protocol: protocol.HTTP,
								},
							}

							c.customizeCall(&opts)

							ingr.CallOrFail(t, opts)
						})
					}
				}
			})
		})
}
