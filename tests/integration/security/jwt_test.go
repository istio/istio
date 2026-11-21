//go:build integ

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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/config"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/common/jwt"
)

// TestRequestAuthentication tests beta authn policy for jwt.
func TestRequestAuthentication(t *testing.T) {
	payload1 := strings.Split(jwt.TokenIssuer1, ".")[1]
	payload2 := strings.Split(jwt.TokenIssuer2, ".")[1]
	payload3 := strings.Split(jwt.TokenIssuer1WithNestedClaims2, ".")[1]
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Run(func(t framework.TestContext) {
			type testCase struct {
				name          string
				customizeCall func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions)
			}

			newTest := func(policy string, cases []testCase) func(framework.TestContext) {
				return func(t framework.TestContext) {
					if len(policy) > 0 {
						// Apply the policy for all targets.
						config.New(t).
							Source(config.File(policy).WithParams(param.Params{
								"JWTServer": jwtServer,
							})).
							BuildAll(nil, apps.Ns1.All).
							Apply()
					}

					newTrafficTest(t, apps.Ns1.All.Instances()).Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
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
								"X-Jwt-Iss":           "test-issuer-1@istio.io",
								"X-Jwt-Iat":           "1562182722",
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
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1Expired).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "expired-token-cors-preflight-request-allowed",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token-cors-preflight-request-allowed"
						opts.HTTP.Method = "OPTIONS"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1Expired).
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
							WithAuthz(jwt.TokenIssuer1Expired).
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
				{
					name: "valid-nested-claim-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-noauthz"
						opts.HTTP.Headers = headers.New().
							With("Authorization", "Bearer "+jwt.TokenIssuer1WithNestedClaims2).
							With("X-Jwt-Nested-Claim", "value_to_be_replaced").
							Build()
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1WithNestedClaims2).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "",
								"X-Test-Payload":      payload3,
								"X-Jwt-Iss":           "test-issuer-1@istio.io",
								"X-Jwt-Iat":           "1604008018",
								"X-Jwt-Nested-Claim":  "valueC",
							}))
					},
				},
			}))

			t.NewSubTest("required-multi-jwt").Run(newTest("testdata/requestauthn/required-multi-jwt.yaml.tmpl", []testCase{
				{
					name: "both-tokens-valid",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/both-tokens-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "only-primary-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-primary-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "only-scope-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-scope-token"
						opts.HTTP.Headers = headers.New().
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "no-tokens",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-tokens"
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "primary-valid-scope-expired",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-valid-scope-expired"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer1Expired).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "primary-expired-scope-valid",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-expired-scope-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1Expired).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
			}))

			t.NewSubTest("required-mixed-jwt").Run(newTest("testdata/requestauthn/required-mixed-jwt.yaml.tmpl", []testCase{
				{
					name: "both-tokens-valid",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/both-tokens-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "only-primary-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-primary-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "only-scope-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-scope-token"
						opts.HTTP.Headers = headers.New().
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "no-tokens",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-tokens"
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					// Required token is expired: Envoy rejects an expired token even when presence=REQUIRED.
					name: "primary-expired-scope-optional-valid",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-expired-scope-optional-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1Expired).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					// Optional token is expired and present: allow_missing only covers absent tokens,
					// not expired ones, so this must be rejected.
					name: "primary-valid-scope-optional-expired",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-valid-scope-optional-expired"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer1Expired).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
			}))

			// presence: PERMISSIVE rejects neither missing nor invalid tokens, so all
			// cases below pass.
			t.NewSubTest("permissive-multi-jwt").Run(newTest("testdata/requestauthn/permissive-multi-jwt.yaml.tmpl", []testCase{
				{
					name: "both-tokens-valid",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/both-tokens-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "only-primary-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-primary-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "only-scope-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-scope-token"
						opts.HTTP.Headers = headers.New().
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "no-tokens",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-tokens"
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					// An expired token is invalid but not rejected under PERMISSIVE.
					name: "primary-valid-scope-expired",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-valid-scope-expired"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer1Expired).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "primary-expired-scope-valid",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-expired-scope-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1Expired).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
			}))

			t.NewSubTest("permissive-mixed-jwt").Run(newTest("testdata/requestauthn/permissive-mixed-jwt.yaml.tmpl", []testCase{
				{
					name: "both-tokens-valid",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/both-tokens-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					// The PERMISSIVE rule tolerates the missing scope token.
					name: "only-primary-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-primary-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					name: "only-scope-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/only-scope-token"
						opts.HTTP.Headers = headers.New().
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					name: "no-tokens",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/no-tokens"
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					// The REQUIRED rule still rejects an invalid primary token.
					name: "primary-expired-scope-valid",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-expired-scope-valid"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1Expired).
							With("x-scope-token", jwt.TokenIssuer2).
							Build()
						opts.Check = check.Status(http.StatusUnauthorized)
					},
				},
				{
					// Unlike presence: OPTIONAL (see required-mixed-jwt), the PERMISSIVE rule
					// tolerates the invalid scope token.
					name: "primary-valid-scope-expired",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/primary-valid-scope-expired"
						opts.HTTP.Headers = headers.New().
							WithAuthz(jwt.TokenIssuer1).
							With("x-scope-token", jwt.TokenIssuer1Expired).
							Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
			}))

			// presence: PERMISSIVE, with an AuthorizationPolicy that requires a populated
			// principal. An invalid token passes the JWT filter, and the AuthorizationPolicy
			// denies the request with 403 instead of the filter's 401. A valid token still
			// populates the principal.
			t.NewSubTest("permissive-authz").Run(newTest("testdata/requestauthn/permissive-authz.yaml.tmpl", []testCase{
				{
					name: "valid-token",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							check.ReachedTargetClusters(t))
					},
				},
				{
					// 403, not 401: the JWT filter no longer rejects the invalid token.
					// The AuthorizationPolicy denies it instead, as no principal was populated.
					name: "expired-token",
					customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/expired-token"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1Expired).Build()
						opts.Check = check.Status(http.StatusForbidden)
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

			// presence: PERMISSIVE with failedStatusMetadataKey, one key per rule. The
			// JWT filter writes each rule's verification failure to dynamic metadata
			// under envoy.filters.http.jwt_authn. The jwt-failure-log access log
			// provider (see main_test.go) gives each rule its own field, so failures
			// stay attributable to their rule. The request itself is never rejected.
			// With multiple rules, no failure is recorded for a missing token. Envoy
			// attributes a failure to a rule only when a token is present.
			t.NewSubTest("permissive-failure-metadata").Run(func(t framework.TestContext) {
				config.New(t).
					Source(config.File("testdata/requestauthn/permissive-failure-metadata.yaml.tmpl").WithParams(param.Params{
						"JWTServer": jwtServer,
					})).
					BuildAll(nil, apps.Ns1.All).
					Apply()

				runJwtFailureMetadataTest(t, []jwtFailureCase{
					{name: "both-tokens-valid", authzToken: jwt.TokenIssuer1, scopeToken: jwt.TokenIssuer2},
					{name: "primary-valid-scope-wrong-issuer", authzToken: jwt.TokenIssuer1, scopeToken: jwt.TokenIssuer1,
						issuer2Msg: "Jwt issuer is not configured"},
					{name: "primary-expired-scope-valid", authzToken: jwt.TokenIssuer1Expired, scopeToken: jwt.TokenIssuer2,
						issuer1Msg: "Jwt is expired"},
					{name: "both-tokens-invalid", authzToken: jwt.TokenIssuer1Expired, scopeToken: jwt.TokenIssuer2Expired,
						issuer1Msg: "Jwt is expired", issuer2Msg: "Jwt is expired"},
					// No token at all: neither rule records a failure, see the comment above.
					{name: "no-tokens"},
				})
			})

			// A single rule with failedStatusMetadataKey. With one rule the key is
			// unambiguous, so even a missing token is recorded. With multiple rules
			// it is not, see permissive-failure-metadata above.
			t.NewSubTest("permissive-failure-metadata-single-rule").Run(func(t framework.TestContext) {
				config.New(t).
					Source(config.File("testdata/requestauthn/permissive-failure-metadata-single-rule.yaml.tmpl").WithParams(param.Params{
						"JWTServer": jwtServer,
					})).
					BuildAll(nil, apps.Ns1.All).
					Apply()

				runJwtFailureMetadataTest(t, []jwtFailureCase{
					{name: "no-token", issuer1Msg: "Jwt is missing"},
				})
			})

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
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1Expired).Build()
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

			t.NewSubTest("timeout").Run(newTest("testdata/requestauthn/timeout.yaml.tmpl", []testCase{
				{
					name: "valid-token-forward-remote-jwks",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward-remote-jwks"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.NotOK(),
							check.Status(http.StatusUnauthorized))
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
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1Expired).Build()
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
						opts.HTTP.Path = "/valid-token?token=" + jwt.TokenIssuer1 + "&secondary_token=" + jwt.TokenIssuer1Expired
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

// jwtFailureEntry is one line of the jwt-failure-log provider (see main_test.go).
// Each rule's verification failure appears under its label, or is null when that
// rule recorded no failure.
type jwtFailureEntry struct {
	Path    string            `json:"path"`
	Issuer1 *jwtFailureStatus `json:"issuer1"`
	Issuer2 *jwtFailureStatus `json:"issuer2"`
}

// jwtFailureStatus is the failure the JWT filter writes to dynamic metadata for
// a rule whose token did not verify.
type jwtFailureStatus struct {
	Message string `json:"message"`
}

// findFailureEntries reads the target's sidecar access logs and returns the
// jwt-failure-log lines (see main_test.go) for the given request path. It returns
// an error while no line is found yet; the caller is expected to retry until the
// access log is flushed.
func findFailureEntries(t framework.TestContext, to echo.Target, path string) ([]jwtFailureEntry, error) {
	var entries []jwtFailureEntry
	vm := to.Config().IsVM()
	for _, w := range to.WorkloadsOrFail(t) {
		var logs string
		if vm {
			// VM sidecars run via istio-sidecar.deb, whose start script redirects
			// Envoy output (including /dev/stdout access logs) to
			// /var/log/istio/istio.log instead of the container log.
			stdout, stderr, err := w.Cluster().PodExec(w.PodName(), to.NamespaceName(), "istio-proxy", "cat /var/log/istio/istio.log")
			if err != nil {
				return nil, fmt.Errorf("failed reading /var/log/istio/istio.log: %v, %s", err, stderr)
			}
			logs = stdout
		} else {
			var err error
			logs, err = w.Sidecar().Logs()
			if err != nil {
				return nil, fmt.Errorf("failed getting logs: %v", err)
			}
		}
		for _, line := range strings.Split(logs, "\n") {
			// Only lines from the jwt-failure-log provider are JSON objects.
			// Other access log lines and proxy output start with a timestamp.
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var e jwtFailureEntry
			if err := json.Unmarshal([]byte(line), &e); err != nil || e.Path != path {
				continue
			}
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no jwt-failure-log line found for %s yet", path)
	}
	return entries, nil
}

// verifyFailureMetadata checks the failure entries of the given lines. wantIssuer1
// and wantIssuer2 are the expected failure messages for the two rules. An empty
// want means the rule recorded no failure.
func verifyFailureMetadata(entries []jwtFailureEntry, path, wantIssuer1, wantIssuer2 string) error {
	for _, e := range entries {
		if err := checkFailureEntry("issuer1", e.Issuer1, wantIssuer1); err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
		if err := checkFailureEntry("issuer2", e.Issuer2, wantIssuer2); err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
	}
	return nil
}

// checkFailureEntry checks one rule's failure entry. want is the expected failure
// message. An empty want means the rule recorded no failure, so the entry must
// be null.
func checkFailureEntry(rule string, got *jwtFailureStatus, want string) error {
	if want == "" {
		if got != nil {
			return fmt.Errorf("expected no failure for %s, got: %s", rule, got.Message)
		}
		return nil
	}
	if got == nil {
		return fmt.Errorf("expected failure %q for %s, got none", want, rule)
	}
	if !strings.Contains(got.Message, want) {
		return fmt.Errorf("expected failure %q for %s, got: %s", want, rule, got.Message)
	}
	return nil
}

// jwtFailureCase is a test case for the failure-metadata subtests. It holds the
// tokens to present and the failure message each rule should record. An empty
// message means the rule records no failure.
type jwtFailureCase struct {
	name       string
	authzToken string // token presented on the Authorization header; "" for none
	scopeToken string // token presented on the x-scope-token header; "" for none
	issuer1Msg string // expected issuer-1 failure message; "" for no failure
	issuer2Msg string // expected issuer-2 failure message; "" for no failure
}

// runJwtFailureMetadataTest sends the given cases from a single source to every
// target. The failure metadata is written by the target's sidecar and does not
// depend on the source, so one source is enough. Every target is kept, to cover
// each sidecar variant.
func runJwtFailureMetadataTest(t framework.TestContext, cases []jwtFailureCase) {
	newTrafficTest(t, apps.Ns1.All.Instances()).
		FromMatch(match.ServiceName(apps.Ns1.A.NamespacedName())).
		Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
			if !to.Config().HasSidecar() {
				// Without a sidecar there is no jwt_authn filter at all.
				return
			}
			for _, c := range cases {
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: "http",
						},
					}
					if c.authzToken != "" || c.scopeToken != "" {
						hdrs := headers.New()
						if c.authzToken != "" {
							hdrs = hdrs.WithAuthz(c.authzToken)
						}
						if c.scopeToken != "" {
							hdrs = hdrs.With("x-scope-token", c.scopeToken)
						}
						opts.HTTP.Headers = hdrs.Build()
					}
					opts.Check = check.And(
						check.OK(),
						check.ReachedTargetClusters(t))
					retry.UntilSuccessOrFail(t, func() error {
						// Each attempt uses a unique path, so only this attempt's log line
						// can match.
						path := "/" + rand.String(16)
						opts.HTTP.Path = path
						if _, err := from.Call(opts); err != nil {
							return err
						}
						// Wait for the line to appear, to tolerate the access log flush delay.
						var entries []jwtFailureEntry
						if err := retry.Until(func() bool {
							var err error
							entries, err = findFailureEntries(t, to, path)
							return err == nil
						}, retry.Timeout(10*time.Second), retry.Delay(150*time.Millisecond)); err != nil {
							return fmt.Errorf("no jwt-failure-log line found for %s", path)
						}
						// The line is written once, so a mismatch never corrects itself. And a
						// request right after a config update can hit the previous rules:
						// Envoy serves established connections with the old listener config.
						// The retry above sends a fresh request.
						return verifyFailureMetadata(entries, path, c.issuer1Msg, c.issuer2Msg)
					}, retry.Timeout(60*time.Second), retry.Delay(500*time.Millisecond))
				})
			}
		})
}

// TestIngressRequestAuthentication tests beta authn policy for jwt on ingress.
// The policy is also set at global namespace, with authorization on ingressgateway.
func TestIngressRequestAuthentication(t *testing.T) {
	framework.NewFullTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Run(func(t framework.TestContext) {
			config.New(t).
				Source(config.File("testdata/requestauthn/global-jwt.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): istio.ClaimSystemNamespaceOrFail(t, t),
					"Services":               apps.Ns1.All,
					"GatewayIstioLabel":      i.Settings().IngressGatewayIstioLabel,
					"JWTServer":              jwtServer,
				})).
				Source(config.File("testdata/requestauthn/ingress.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): apps.Ns1.Namespace,
					"GatewayIstioLabel":      i.Settings().IngressGatewayIstioLabel,
				})).
				BuildAll(nil, apps.Ns1.All).
				Apply()

			t.NewSubTest("in-mesh-authn").Run(func(t framework.TestContext) {
				cases := []struct {
					name          string
					customizeCall func(framework.TestContext, echo.Instance, *echo.CallOptions)
				}{
					{
						name: "in-mesh-with-expired-token",
						customizeCall: func(_ framework.TestContext, _ echo.Instance, opts *echo.CallOptions) {
							opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1Expired).Build()
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
				}
				newTrafficTest(t, apps.Ns1.All.Instances()).
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
			})

			t.NewSubTest("ingress-authn").Run(func(t framework.TestContext) {
				cases := []struct {
					name          string
					customizeCall func(opts *echo.CallOptions, to echo.Target)
				}{
					{
						name: "allow healthz",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/healthz"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "deny without token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						name: "allow with sub-1 token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "deny with sub-2 token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer2).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						name: "deny with expired token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1Expired).
								Build()
							opts.Check = check.Status(http.StatusUnauthorized)
						},
					},
					{
						name: "allow with sub-1 token on any.com",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("any-request-principal-ok.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "allow with sub-2 token on any.com",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("any-request-principal-ok.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer2).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "deny without token on any.com",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("any-request-principal-ok.%s.com", to.ServiceName())).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						name: "deny with token on other host",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("other-host.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
				}

				newTrafficTest(t, apps.Ns1.All.Instances()).
					RunViaIngress(func(t framework.TestContext, from ingress.Instance, to echo.Target) {
						for _, c := range cases {
							t.NewSubTest(c.name).Run(func(t framework.TestContext) {
								opts := echo.CallOptions{
									Port: echo.Port{
										Protocol: protocol.HTTP,
									},
								}

								c.customizeCall(&opts, to)

								from.CallOrFail(t, opts)
							})
						}
					})
			})
		})
}

// TestGatewayAPIRequestAuthentication tests beta authn policy for jwt on gateway API.
func TestGatewayAPIRequestAuthentication(t *testing.T) {
	framework.NewFullTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Run(func(t framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(t)
			config.New(t).
				Source(config.File("testdata/requestauthn/gateway-api.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): apps.Ns1.Namespace,
				})).
				Source(config.File("testdata/requestauthn/gateway-jwt.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): apps.Ns1.Namespace,
					"Services":               apps.Ns1.A.Append(apps.Ns1.B).Services(),
					"JWTServer":              jwtServer,
				})).
				BuildAll(nil, apps.Ns1.A.Append(apps.Ns1.B).Services()).
				Apply()

			t.NewSubTest("gateway-authn").Run(func(t framework.TestContext) {
				cases := []struct {
					name          string
					customizeCall func(opts *echo.CallOptions, to echo.Target)
				}{
					{
						name: "allow healthz",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/healthz"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "deny without token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						name: "allow with sub-1 token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "allow with sub-1 token despite \"ignored\" RequestAuthentication (enableGatewayAPISelectorPolicies flag = true)",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer3).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "deny with sub-2 token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer2).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						name: "deny with expired token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1Expired).
								Build()
							opts.Check = check.Status(http.StatusUnauthorized)
						},
					},
					{
						name: "allow with sub-1 token on any.com",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("any-request-principal-ok.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "allow with sub-2 token on any.com",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("any-request-principal-ok.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer2).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "deny without token on any.com",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("any-request-principal-ok.%s.com", to.ServiceName())).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						name: "deny with token on other host",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("other-host.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
				}

				newTrafficTest(t, apps.Ns1.A.Append(apps.Ns1.B)).
					RunViaGatewayIngress("istio", func(t framework.TestContext, from ingress.Instance, to echo.Target) {
						for _, c := range cases {
							t.NewSubTest(c.name).Run(func(t framework.TestContext) {
								opts := echo.CallOptions{
									Port: echo.Port{
										Protocol: protocol.HTTP,
									},
								}

								c.customizeCall(&opts, to)

								from.CallOrFail(t, opts)
							})
						}
					})
			})
		})
}
