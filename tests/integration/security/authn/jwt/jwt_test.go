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

package jwt

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/tests/integration/security/authn"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	authHeaderKey = "Authorization"
	// Simple jwt from the testing@secure.istio.io issuer. This jwt is expired in the year of 3160.
	testingIstioIssuerJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkRIRmJwb0lVcXJZOHQyenBBMnFYZkNtcjVWTzVaRX" +
		"I0UnpIVV8tZW52dlEiLCJ0eXAiOiJKV1QifQ.eyJleHAiOjM3NTU5ODU5MzQ2LCJpYXQiOjE1NTk4NTkzNDYsImlzcyI" +
		"6InRlc3RpbmdAc2VjdXJlLmlzdGlvLmlvIiwic3ViIjoidGVzdGluZ0BzZWN1cmUuaXN0aW8uaW8ifQ.A4j9ft49KrKw" +
		"zvgpzoKXdMxVyYTUjuU3LMgqjvfZXDSUIYtL6gx31CSMb2UZiESmJ7Xu8XkqhcZvmfcuU_WApl01emBOwjwtg50M-Yuc" +
		"oy6A4DbnsVcKnSY7VtAWrJ9ACdr1CUPseTfkbaIW-PYDfLPTMJljO2NdzF0yC9l97N8lau9afNu9Ilc9DIlQ5CgW6h1J" +
		"VasV21wSLlrglnWhut6yV6yQOqUjMcwgntgVkv_3UbdtzJKt46u29Juh_8m4OEdAXeyy00midPSIJWIm9J6lgLF35XUB" +
		"e_nvonKwdsxKpV-LrWlKMZB8QneHbb3194J7Wr2H7WVnNGcbVUCORw"
	// JWT token used is borrowed from https://github.com/istio/proxy/blob/master/src/envoy/http/jwt_auth/sample/correct_jwt.
	// The Token expires in year 2132, issuer is 628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com.
	// Test will fail if this service account is deleted.
	gsaIsserJwt = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6ImIzMzE5YTE0NzUxNGRmN2VlNWU0YmNkZWU1MTM1" +
		"MGNjODkwY2M4OWUifQ==.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZ" +
		"GV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN" +
		"2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJib29rc3RvcmUtZXNwLWVjaG8uY" +
		"2xvdWRlbmRwb2ludHNhcGlzLmNvbSIsImlhdCI6MTUxMjc1NDIwNSwiZXhwIjo1MTEyNzU0MjA1fQ==.HKWpc8zLw7NAzl" +
		"gPphHpQ6fWh7k1cJ0XM7B_9YqcOQYLe8UA9KvOC_4D6cNw7HCaEv8UQufA4d8ErDn5PI3mPxn6m8pciJbcqblXmNN8jCJU" +
		"SH2OHZsWDdzipHPrt5kxz9onx39m9Zdb_xXAffHREVDXO6eMzNte8ZihZwmZauIT9fbL8BbD74_D5tQvswdjUNAQuTdK6-" +
		"pBXOH1Qf7fE3V92ESVqUmqM05FkTBfDZw6CGKj47W8ecs0QiLyERth8opCTLsRi5QN1xEPggTpfH_YBZTtsuIybVjiw9UA" +
		"izWE-ziFWx2qlt9JPEArjvroMfNmJz4gTenbKNuXBMJOQg=="
	// 	Jwt from the testing@secure.istio.io issuer. This jwt has already expired as of June 10, 2019.
	expiredJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IkRIRmJwb0lVcXJZOHQyenBBMnFYZkNtcjVWTzVaRXI0UnpIVV8tZ" +
		"W52dlEiLCJ0eXAiOiJKV1QifQ.eyJleHAiOjE1NTk5MzEyMDksImlhdCI6MTU1OTkzMTIwOSwiaXNzIjoidGVzdGluZ0" +
		"BzZWN1cmUuaXN0aW8uaW8iLCJzdWIiOiJ0ZXN0aW5nQHNlY3VyZS5pc3Rpby5pbyJ9.a7zuyoF7eaVcnHOkJKK25WqTd" +
		"K4OT0sgQzrAqYhANv-6MfbIHgRtbOYvQW0pSL5saeSkpd8YV35NdmipsbPcMTVYgSlSPkmNdinZwJyGpqdGEu6fVYq3P" +
		"FET0bBSm5yVTkO7yyX8AgVVH31ouGJ8OQ11gGZ66Jmle5PNwyGCh1ccZsT8LefYTbcDHMbXnoYwU4e3WwcphLBqFoafF" +
		"JUpRXV5dtn2YLnMwA0ALTteRMVMYIkXQkR6QhBwUufC3aUQmZydzrGMaKqbwcYbcp1GG05v4A99rikNQ-Ia6xswgAEJu" +
		"JaYhppL-0B7E-i4jhpGbKOrFyc6vFZlk9oejUpi7Q"
)

func TestAuthnJwt(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "authn-jwt", true)

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				With(&c, util.EchoConfig("c", ns, false, nil, g, p)).
				BuildOrFail(t)

			testCases := []struct {
				configFile string
				subTests   []authn.TestCase
			}{
				{
					configFile: "simple-jwt-policy.yaml.tmpl",
					subTests: []authn.TestCase{
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testingIstioIssuerJwt},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + expiredJwt},
									},
								},
							},
							ExpectAuthenticated: false,
						},
						{
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
					},
				},
				{
					configFile: "wrong-issuer.yaml.tmpl",
					subTests: []authn.TestCase{
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testingIstioIssuerJwt},
									},
								},
							},
							ExpectAuthenticated: false,
						},
					},
				},
				{
					// Test jwt with paths with trigger rules with one issuer.
					configFile: "jwt-with-paths.yaml.tmpl",
					subTests: []authn.TestCase{
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/health_check",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/guest-us",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/index.html",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									Path:     "/index.html",
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testingIstioIssuerJwt},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   c,
									Path:     "/index.html",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   c,
									Path:     "/something-confidential",
									PortName: "http",
									Scheme:   scheme.HTTP,
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   c,
									Path:     "/something-confidential",
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testingIstioIssuerJwt},
									},
								},
							},
							ExpectAuthenticated: true,
						},
					},
				},
				{
					// Test jwt with paths with trigger rules with two issuers.
					configFile: "two-issuers.yaml.tmpl",
					subTests: []authn.TestCase{
						{
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
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + gsaIsserJwt},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testingIstioIssuerJwt},
									},
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Path:     "/testing-istio-jwt",
									Scheme:   scheme.HTTP,
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + "invalid token"},
									},
								},
							},
							ExpectAuthenticated: false,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Path:     "/testing-istio-jwt",
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + testingIstioIssuerJwt},
									},
								},
							},
							ExpectAuthenticated: true,
						},
						{
							Request: connection.Checker{
								From: a,
								Options: echo.CallOptions{
									Target:   b,
									PortName: "http",
									Scheme:   scheme.HTTP,
									Path:     "/testing-istio-jwt",
									Headers: map[string][]string{
										authHeaderKey: {"Bearer " + gsaIsserJwt},
									},
								},
							},
							ExpectAuthenticated: false,
						},
					},
				},
			}

			for _, c := range testCases {
				testName := strings.TrimSuffix(c.configFile, filepath.Ext(c.configFile))
				t.Run(testName, func(t *testing.T) {

					// Apply the policy.
					namespaceTmpl := map[string]string{
						"Namespace": ns.Name(),
					}
					deploymentYAML := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
						file.AsStringOrFail(t, filepath.Join("testdata", c.configFile)))
					g.ApplyConfigOrFail(t, ns, deploymentYAML...)
					defer g.DeleteConfigOrFail(t, ns, deploymentYAML...)

					// Give some time for the policy propagate.
					time.Sleep(60 * time.Second)
					for _, subTest := range c.subTests {
						subTestName := fmt.Sprintf("%s->%s:%s",
							subTest.Request.From.Config().Service,
							subTest.Request.Options.Target.Config().Service,
							subTest.Request.Options.PortName)
						t.Run(subTestName, func(t *testing.T) {
							retry.UntilSuccessOrFail(t, subTest.CheckAuthn, retry.Delay(time.Second), retry.Timeout(10*time.Second))
						})
					}
				})
			}
		})
}
