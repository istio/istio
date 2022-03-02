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
	"istio.io/istio/pkg/test/echo/common/scheme"
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

			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}

			type testCase struct {
				name          string
				customizeCall func(to echo.Instances, opts *echo.CallOptions)
			}

			newTest := func(policy string, cases []testCase) func(framework.TestContext) {
				return func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, dst echo.Instances) error {
							if policy != "" {
								args := map[string]string{
									"Namespace": ns.Name(),
									"dst":       dst[0].Config().Service,
								}
								return t.ConfigIstio().EvalFile(args, policy).Apply(ns.Name(), resource.Wait)
							}
							return nil
						}).
						From(
							// TODO(JimmyCYJ): enable VM for all test cases.
							util.SourceFilter(t, apps, ns.Name(), true)...).
						ConditionallyTo(echotest.ReachableDestinations).
						To(util.DestFilter(t, apps, ns.Name(), true)...).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Instances) {
							for _, c := range cases {
								t.NewSubTest(c.name).Run(func(t framework.TestContext) {
									opts := echo.CallOptions{
										Target:   to[0],
										PortName: "http",
										Scheme:   scheme.HTTP,
										Count:    callCount,
									}

									// Apply any custom options for the test.
									c.customizeCall(to, &opts)

									from.CallWithRetryOrFail(t, opts)
								})
							}
						})
				}
			}

			t.NewSubTest("authn-only").Run(newTest("testdata/requestauthn/authn-only.yaml.tmpl", []testCase{
				{
					name: "valid-token-noauthz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token-noauthz"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "",
								"X-Test-Payload":      payload1,
							}))
					},
				},
				{
					name: "valid-token-2-noauthz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token-2-noauthz"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer2).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "",
								"X-Test-Payload":      payload2,
							}))
					},
				},
				{
					name: "expired-token-noauthz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/expired-token-noauthz"
						opts.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "expired-token-cors-preflight-request-allowed",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/expired-token-cors-preflight-request-allowed"
						opts.Method = "OPTIONS"
						opts.Headers = headers.New().
							WithAuthz(jwt.TokenExpired).
							With(headers.AccessControlRequestMethod, "POST").
							With(headers.Origin, "https://istio.io").
							Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
				{
					name: "expired-token-bad-cors-preflight-request-rejected",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/expired-token-cors-preflight-request-allowed"
						opts.Method = "OPTIONS"
						opts.Headers = headers.New().
							WithAuthz(jwt.TokenExpired).
							With(headers.AccessControlRequestMethod, "POST").
							// the required Origin header is missing.
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "no-token-noauthz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/no-token-noauthz"
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
			}))

			t.NewSubTest("authn-authz").Run(newTest("testdata/requestauthn/authn-authz.yaml.tmpl", []testCase{
				{
					name: "valid-token",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts),
							check.RequestHeader(headers.Authorization, ""))
					},
				},
				{
					name: "expired-token",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/expired-token"
						opts.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "no-token",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/no-token"
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
			}))

			t.NewSubTest("no-authn-authz").Run(newTest("", []testCase{
				{
					name: "no-authn-authz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/no-authn-authz"
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
			}))

			t.NewSubTest("forward").Run(newTest("testdata/requestauthn/forward.yaml.tmpl", []testCase{
				{
					name: "valid-token-forward",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token-forward"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts),
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
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token-forward-remote-jwks"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts),
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
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-aud"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
				{
					name: "valid-aud",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-aud"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer1WithAud).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
				{
					name: "verify-policies-are-combined",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/verify-policies-are-combined"
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer2).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
			}))

			t.NewSubTest("invalid-jwks").Run(newTest("testdata/requestauthn/invalid-jwks.yaml.tmpl", []testCase{
				{
					name: "invalid-jwks-valid-token-noauthz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = ""
						opts.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "invalid-jwks-expired-token-noauthz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/invalid-jwks-valid-token-noauthz"
						opts.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "invalid-jwks-no-token-noauthz",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/invalid-jwks-no-token-noauthz"
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
			}))

			t.NewSubTest("headers-params").Run(newTest("testdata/requestauthn/headers-params.yaml.tmpl", []testCase{
				{
					name: "valid-params",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token?token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
				{
					name: "valid-params-secondary",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token?secondary_token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
				{
					name: "invalid-params",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token?token_value=" + jwt.TokenIssuer1
						opts.Check = check.Status(http.StatusForbidden)
					},
				},
				{
					name: "valid-token-set",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token?token=" + jwt.TokenIssuer1 + "&secondary_token=" + jwt.TokenIssuer1
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
				{
					name: "invalid-token-set",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = "/valid-token?token=" + jwt.TokenIssuer1 + "&secondary_token=" + jwt.TokenExpired
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "valid-header",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = ""
						opts.Headers = headers.New().
							With("X-Jwt-Token", "Value "+jwt.TokenIssuer1).
							Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
				{
					name: "valid-header-secondary",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = ""
						opts.Headers = headers.New().
							With("Auth-Token", "Token "+jwt.TokenIssuer1).
							Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
				{
					name: "invalid-header",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Path = ""
						opts.Headers = headers.New().
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

			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}

			type testCase struct {
				name          string
				customizeCall func(to echo.Instances, opts *echo.CallOptions)
			}

			newTest := func(policy string, cases []testCase) func(framework.TestContext) {
				return func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, dst echo.Instances) error {
							if policy != "" {
								args := map[string]string{
									"Namespace": ns.Name(),
									"dst":       dst[0].Config().Service,
								}
								return t.ConfigIstio().EvalFile(args, policy).Apply(ns.Name(), resource.Wait)
							}
							return nil
						}).
						From(util.SourceFilter(t, apps, ns.Name(), false)...).
						ConditionallyTo(echotest.ReachableDestinations).
						ConditionallyTo(func(from echo.Instance, to echo.Instances) echo.Instances {
							return to.Match(echo.InCluster(from.Config().Cluster))
						}).
						To(util.DestFilter(t, apps, ns.Name(), false)...).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Instances) {
							for _, c := range cases {
								t.NewSubTest(c.name).Run(func(t framework.TestContext) {
									opts := echo.CallOptions{
										Target:   to[0],
										PortName: "http",
										Scheme:   scheme.HTTP,
										Count:    callCount,
									}

									// Apply any custom options for the test.
									c.customizeCall(to, &opts)

									from.CallWithRetryOrFail(t, opts)
								})
							}
						})
				}
			}

			t.NewSubTest("in-mesh-authn").Run(newTest("testdata/requestauthn/ingress.yaml.tmpl", []testCase{
				{
					name: "in-mesh-with-expired-token",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Headers = headers.New().WithAuthz(jwt.TokenExpired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "in-mesh-without-token",
					customizeCall: func(to echo.Instances, opts *echo.CallOptions) {
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(to, opts))
					},
				},
			}))

			t.NewSubTest("ingress-authn").Run(func(t framework.TestContext) {
				// TODO(JimmyCYJ): add workload-agnostic test pattern to support ingress gateway tests.
				t.ConfigIstio().EvalFile(map[string]string{
					"Namespace": ns.Name(),
					"dst":       util.BSvc,
				}, "testdata/requestauthn/ingress.yaml.tmpl").ApplyOrFail(t, ns.Name())

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
								opts.Path = "/"
								opts.Headers = headers.New().WithHost("example.com").Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "allow with sub-1 token",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/"
								opts.Headers = headers.New().
									WithHost("example.com").
									WithAuthz(jwt.TokenIssuer1).
									Build()
								opts.Check = check.OK()
							},
						},
						{
							name: "deny with sub-2 token",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/"
								opts.Headers = headers.New().
									WithHost("example.com").
									WithAuthz(jwt.TokenIssuer2).
									Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "deny with expired token",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/"
								opts.Headers = headers.New().
									WithHost("example.com").
									WithAuthz(jwt.TokenExpired).
									Build()
								opts.Check = check.Status(http.StatusUnauthorized)
							},
						},
						{
							name: "allow with sub-1 token on any.com",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/"
								opts.Headers = headers.New().
									WithHost("any-request-principlal-ok.com").
									WithAuthz(jwt.TokenIssuer1).
									Build()
								opts.Check = check.OK()
							},
						},
						{
							name: "allow with sub-2 token on any.com",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/"
								opts.Headers = headers.New().
									WithHost("any-request-principlal-ok.com").
									WithAuthz(jwt.TokenIssuer2).
									Build()
								opts.Check = check.OK()
							},
						},
						{
							name: "deny without token on any.com",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/"
								opts.Headers = headers.New().
									WithHost("any-request-principlal-ok.com").
									Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "deny with token on other host",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/"
								opts.Headers = headers.New().
									WithHost("other-host.com").
									WithAuthz(jwt.TokenIssuer1).
									Build()
								opts.Check = check.Status(http.StatusForbidden)
							},
						},
						{
							name: "allow healthz",
							customizeCall: func(opts *echo.CallOptions) {
								opts.Path = "/healthz"
								opts.Headers = headers.New().
									WithHost("example.com").
									Build()
								opts.Check = check.OK()
							},
						},
					}

					for _, c := range ingTestCases {
						t.NewSubTest(c.name).Run(func(t framework.TestContext) {
							opts := echo.CallOptions{
								Port: &echo.Port{
									Protocol: protocol.HTTP,
								},
							}

							c.customizeCall(&opts)

							ingr.CallWithRetryOrFail(t, opts)
						})
					}
				}
			})
		})
}
