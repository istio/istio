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

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
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

// Test authentication policy with optional JWT, and enforce requirement per host, path etc via authZ.
func TestOptionalAuthnJwt(t *testing.T) {
	testIssuer1Token := jwt.TokenIssuer1
	testIssuer2Token := jwt.TokenIssuer2

	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "authn-jwt",
				Inject: true,
			})

			// Apply the policy.
			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}
			jwtPolicies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, "testdata/jwt/optional-jwt-policy.yaml.tmpl"),
				file.AsStringOrFail(t, "testdata/jwt/authz.yaml.tmpl"),
			g.ApplyConfigOrFail(t, ns, jwtPolicies...)
			defer g.DeleteConfigOrFail(t, ns, jwtPolicies...)

			var a, b, c, d, e echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				With(&d, util.EchoConfig("d", ns, false, nil, g, p)).
				BuildOrFail(t)

			testCases := []authn.TestCase{
				{
					Name: "accessing-b-with-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + testIssuer1Token},
							},
						},
					},
					ExpectAuthenticated: true,
				},
				{
					Name: "accessing-b-with-bad-token",
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
					ExpectAuthenticated: true,
				},
				{
					Name: "accessing-b-without-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: false,
				},
				{
					Name: "accessing-c-include-path-without-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							Path:     "/get",
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: false,
				},
				{
					Name: "accessing-c-exclude-path-without-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							Path:     "/healthz",
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: true,
				},
				{
					Name: "accessing-d-exclude-path-without-token",
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAuthenticated: true,
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
