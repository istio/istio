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

package remotejwks

import (
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
)

// TestRemoteJwks tests always delegate Envoy to fetch http jwks server.
func TestRemoteJwks(t *testing.T) {
	payload1 := strings.Split(jwt.TokenIssuer1, ".")[1]
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			ns := apps.EchoNamespace.Namespace

			cases := []struct {
				name          string
				policyFile    string
				delay         string
				timeout       string
				customizeCall func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions)
			}{
				{
					name:       "remote-jwks-without-service-entry",
					policyFile: "./testdata/requestauthn-no-se.yaml.tmpl",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward-remote-jwks"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.NotOK(),
							check.Status(http.StatusUnauthorized))
					},
				},
				{
					name:       "remote-jwks-with-service-entry",
					policyFile: "./testdata/requestauthn-with-se.yaml.tmpl",
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
				{
					name:       "remote-jwks-with-service-entry",
					policyFile: "./testdata/requestauthn-with-se-timeout.yaml.tmpl",
					timeout:    "10ms",
					delay:      "30ms",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward-remote-jwks"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.NotOK(),
							check.Status(http.StatusUnauthorized),
						)
					},
				},
				{
					name:       "remote-jwks-without-issuer",
					policyFile: "./testdata/requestauthn-no-se-no-issuer.yaml.tmpl",
					timeout:    "10ms",
					delay:      "30ms",
					customizeCall: func(t framework.TestContext, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward-remote-jwks"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.NotOK(),
							check.Status(http.StatusUnauthorized),
						)
					},
				},
				{
					name:       "remote-jwks-without-issuer-with-service-entry",
					policyFile: "./testdata/requestauthn-with-se-no-issuer.yaml.tmpl",
					timeout:    "10ms",
					delay:      "30ms",
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
			}

			for _, c := range cases {
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					echotest.New(t, apps.All.Instances()).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							args := map[string]string{
								"Namespace": ns.Name(),
								"dst":       to.Config().Service,
								"delay":     c.delay,
								"timeout":   c.timeout,
							}
							return t.ConfigIstio().EvalFile(ns.Name(), args, c.policyFile).Apply(apply.Wait)
						}).
						FromMatch(
							// TODO(JimmyCYJ): enable VM for all test cases.
							util.SourceMatcher(ns, true)).
						ConditionallyTo(echotest.ReachableDestinations).
						ToMatch(util.DestMatcher(ns, true)).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							opts := echo.CallOptions{
								To: to,
								Port: echo.Port{
									Name: "http",
								},
							}

							c.customizeCall(t, from, &opts)

							from.CallOrFail(t, opts)
						})
				})
			}
		})
}
