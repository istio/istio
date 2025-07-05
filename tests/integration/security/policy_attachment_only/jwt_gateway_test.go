//go:build integ
// +build integ

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package policyattachmentonly

import (
	"fmt"
	"net/http"
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/config"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/tests/common/jwt"
)

func TestGatewayAPIRequestAuthentication(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Run(func(t framework.TestContext) {
			istio.DeployGatewayAPIOrSkip(t)
			config.New(t).
				Source(config.File("testdata/requestauthn/gateway-api.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): apps.Namespace,
				})).
				Source(config.File("testdata/requestauthn/gateway-jwt.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): apps.Namespace,
					"Services":               apps.A.Append(apps.B).Services(),
				})).
				BuildAll(nil, apps.A.Append(apps.B).Services()).
				Apply()

			t.NewSubTest("gateway-authn-policy-attachment-only").Run(func(t framework.TestContext) {
				test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, false)
				cases := []struct {
					name          string
					customizeCall func(opts *echo.CallOptions, to echo.Target)
				}{
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
						name: "deny with sub-1 token due to ignored RequestAuthentication",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer3).
								Build()
							opts.Check = check.Status(http.StatusUnauthorized)
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
								WithAuthz(jwt.TokenExpired).
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
				}

				newTrafficTest(t, apps.A.Append(apps.B)).
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

func TestGatewayAPIAuthorizationPolicy(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Run(func(t framework.TestContext) {
			istio.DeployGatewayAPIOrSkip(t)
			config.New(t).
				Source(config.File("testdata/authz/gateway-api.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): apps.Namespace,
				})).
				Source(config.File("testdata/authz/gateway-authz.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): apps.Namespace,
					"Services":               apps.A.Append(apps.B).Services(),
				})).
				BuildAll(nil, apps.A.Append(apps.B).Services()).
				Apply()

			t.NewSubTest("gateway-authz-policy-attachment-only").Run(func(t framework.TestContext) {
				test.SetForTest(t, &features.EnableSelectorBasedK8sGatewayPolicy, false)
				cases := []struct {
					name          string
					customizeCall func(opts *echo.CallOptions, to echo.Target)
				}{
					{
						name: "allow with sub-1 token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/allow"
							opts.HTTP.Method = "GET"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.OK()
						},
					},
					{
						name: "deny without token",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/allow"
							opts.HTTP.Method = "GET"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								Build()
							opts.Check = check.Status(http.StatusForbidden)
						},
					},
					{
						name: "deny based on unacceptable HTTP method",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/allow"
							opts.HTTP.Method = "POST"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.Status(http.StatusNotFound)
						},
					},
					{
						name: "deny based on unacceptable HTTP path",
						customizeCall: func(opts *echo.CallOptions, to echo.Target) {
							opts.HTTP.Path = "/deny"
							opts.HTTP.Method = "GET"
							opts.HTTP.Headers = headers.New().
								WithHost(fmt.Sprintf("example.%s.com", to.ServiceName())).
								WithAuthz(jwt.TokenIssuer1).
								Build()
							opts.Check = check.Status(http.StatusNotFound)
						},
					},
				}

				newTrafficTest(t, apps.A.Append(apps.B)).
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

func newTrafficTest(t framework.TestContext, echos ...echo.Instances) *echotest.T {
	var all []echo.Instance
	for _, e := range echos {
		all = append(all, e...)
	}

	return echotest.New(t, all).
		WithDefaultFilters(1, 1).
		FromMatch(match.And(
			match.NotNaked,
			match.NotProxylessGRPC)).
		ToMatch(match.And(
			match.NotNaked,
			match.NotProxylessGRPC)).
		ConditionallyTo(echotest.NoSelfCalls)
}
