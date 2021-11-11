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
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/authn"
)

const (
	authHeaderKey = "Authorization"
)

// TestRequestAuthentication tests beta authn policy for jwt.
func TestRequestAuthentication(t *testing.T) {
	payload1 := strings.Split(jwt.TokenIssuer1, ".")[1]
	payload2 := strings.Split(jwt.TokenIssuer2, ".")[1]

	framework.NewTest(t).
		Features("security.authentication.jwt").
		Run(func(t framework.TestContext) {
			ns := istio.ClaimSystemNamespaceOrFail(t, t)
			fmt.Printf("String=%s", ns.Name())
			args := map[string]string{"Namespace": ns.Name()}
			applyYAML := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				t.ConfigKube().ApplyYAMLOrFail(t, ns.Name(), policy...)
				return policy
			}
			jwtServer := applyYAML("../../../samples/jwt-server/jwt-server.yaml", ns)
			defer t.ConfigKube().DeleteYAMLOrFail(t, ns.Name(), jwtServer...)
			for _, cluster := range t.Clusters() {
				if _, _, err := kube.WaitUntilServiceEndpointsAreReady(cluster, ns.Name(), "jwt-server"); err != nil {
					t.Fatalf("Wait for jwt-server server failed: %v", err)
				}
			}

			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}

			t.NewSubTest("jwt-authn").Run(func(t framework.TestContext) {
				testCases := []authn.TestCase{
					{
						Name:   "valid-token-noauthz",
						Config: "authn-only",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
							Path:  "/valid-token-noauthz",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
						ExpectHeaders: map[string]string{
							authHeaderKey:    "",
							"X-Test-Payload": payload1,
						},
					},
					{
						Name:   "valid-token-2-noauthz",
						Config: "authn-only",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer2},
							},
							Path:  "/valid-token-2-noauthz",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
						ExpectHeaders: map[string]string{
							authHeaderKey:    "",
							"X-Test-Payload": payload2,
						},
					},
					{
						Name:   "expired-token-noauthz",
						Config: "authn-only",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenExpired},
							},
							Path:  "/expired-token-noauthz",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusUnauthorized,
					},
					{
						Name:   "no-token-noauthz",
						Config: "authn-only",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/no-token-noauthz",
							Count:    callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
					},
					// Destination app is configured with authorization, only request with valid JWT succeed.
					{
						Name:   "valid-token",
						Config: "authn-authz",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
							Path:  "/valid-token",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
						ExpectHeaders: map[string]string{
							authHeaderKey: "",
						},
					},
					{
						Name:   "expired-token",
						Config: "authn-authz",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenExpired},
							},
							Path:  "/expired-token",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusUnauthorized,
					},
					{
						Name:   "no-token",
						Config: "authn-authz",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/no-token",
							Count:    callCount,
						},
						ExpectResponseCode: response.StatusCodeForbidden,
					},
					{
						Name: "no-authn-authz",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/no-authn-authz",
							Count:    callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
					},
					{
						Name:   "valid-token-forward",
						Config: "forward",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
							Path:  "/valid-token-forward",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
						ExpectHeaders: map[string]string{
							authHeaderKey:    "Bearer " + jwt.TokenIssuer1,
							"X-Test-Payload": payload1,
						},
					},
					{
						Name:   "valid-token-forward-remote-jwks",
						Config: "remote",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
							Path:  "/valid-token-forward-remote-jwks",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
						ExpectHeaders: map[string]string{
							authHeaderKey:    "Bearer " + jwt.TokenIssuer1,
							"X-Test-Payload": payload1,
						},
						// This test does not generate cross-cluster traffic, but is flaky
						// in multicluster test. Skip in multicluster mesh.
						// TODO(JimmyCYJ): enable the test in multicluster mesh.
						SkipMultiCluster: true,
					},
					{
						Name:   "valid-token-forward-remote-httpsjwks",
						Config: "remotehttps",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
							Path:  "/valid-token-forward-remote-jwks",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
						ExpectHeaders: map[string]string{
							authHeaderKey:          "Bearer " + jwt.TokenIssuer1,
							"X-Test-https-Payload": payload1,
						},
						// This test does not generate cross-cluster traffic, but is flaky
						// in multicluster test. Skip in multicluster mesh.
						// TODO(JimmyCYJ): enable the test in multicluster mesh.
						SkipMultiCluster: true,
					},
					{
						Name:   "invalid-aud",
						Config: "aud",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
							Path:  "/valid-aud",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeForbidden,
					},
					{
						Name:   "valid-aud",
						Config: "aud",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1WithAud},
							},
							Path:  "/valid-aud",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
					},
					{
						Name:   "verify-policies-are-combined",
						Config: "aud",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer2},
							},
							Path:  "/verify-policies-are-combined",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
					},
					{
						Name:   "invalid-jwks-valid-token-noauthz",
						Config: "invalid-jwks",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenIssuer1},
							},
							Count: callCount,
						},
						ExpectResponseCode: response.StatusUnauthorized,
					},
					{
						Name:   "invalid-jwks-expired-token-noauthz",
						Config: "invalid-jwks",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenExpired},
							},
							Path:  "/invalid-jwks-valid-token-noauthz",
							Count: callCount,
						},
						ExpectResponseCode: response.StatusUnauthorized,
					},
					{
						Name:   "invalid-jwks-no-token-noauthz",
						Config: "invalid-jwks",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/invalid-jwks-no-token-noauthz",
							Count:    callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
					},
				}
				for _, c := range testCases {
					if c.SkipMultiCluster && t.Clusters().IsMulticluster() {
						t.Skip()
					}
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, dst echo.Instances) error {
							if c.Config != "" {
								policy := yml.MustApplyNamespace(t, tmpl.MustEvaluate(
									file.AsStringOrFail(t, fmt.Sprintf("testdata/requestauthn/%s.yaml.tmpl", c.Config)),
									map[string]string{
										"Namespace": ns.Name(),
										"dst":       dst[0].Config().Service,
									},
								), ns.Name())
								if err := t.ConfigIstio().ApplyYAML(ns.Name(), policy); err != nil {
									t.Logf("failed to apply security config %s: %v", c.Config, err)
									return err
								}
								util.WaitForConfig(t, ns, policy)
							}
							return nil
						}).
						From(
							// TODO(JimmyCYJ): enable VM for all test cases.
							util.SourceFilter(t, apps, ns.Name(), true)...).
						ConditionallyTo(echotest.ReachableDestinations).
						To(util.DestFilter(t, apps, ns.Name(), true)...).
						Run(func(t framework.TestContext, src echo.Instance, dest echo.Instances) {
							t.NewSubTest(c.Name).Run(func(t framework.TestContext) {
								c.CallOpts.Target = dest[0]
								c.DestClusters = dest.Match(echo.InCluster(src.Config().Cluster)).Clusters()
								c.CallOpts.Validator = echo.And(echo.ValidatorFunc(c.CheckAuthn))
								src.CallWithRetryOrFail(t, c.CallOpts, echo.DefaultCallRetryOptions()...)
							})
						})
				}
			})
		})
}

// TestIngressRequestAuthentication tests beta authn policy for jwt on ingress.
// The policy is also set at global namespace, with authorization on ingressgateway.
func TestIngressRequestAuthentication(t *testing.T) {
	framework.NewTest(t).
		Features("security.authentication.ingressjwt").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1

			// Apply the policy.
			namespaceTmpl := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
			}

			applyPolicy := func(filename string, ns namespace.Instance) {
				policy := tmpl.EvaluateAllOrFail(t, namespaceTmpl, file.AsStringOrFail(t, filename))
				t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policy...)
				util.WaitForConfig(t, ns, policy...)
			}
			applyPolicy("testdata/requestauthn/global-jwt.yaml.tmpl", newRootNS(t))

			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}

			t.NewSubTest("in-mesh-authn").Run(func(t framework.TestContext) {
				// These test cases verify in-mesh traffic doesn't need tokens.
				testCases := []authn.TestCase{
					{
						Name: "in-mesh-with-expired-token",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								authHeaderKey: {"Bearer " + jwt.TokenExpired},
							},
							Count: callCount,
						},
						ExpectResponseCode: response.StatusUnauthorized,
					},
					{
						Name: "in-mesh-without-token",
						CallOpts: echo.CallOptions{
							PortName: "http",
							Scheme:   scheme.HTTP,
							Count:    callCount,
						},
						ExpectResponseCode: response.StatusCodeOK,
					},
				}
				for _, c := range testCases {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, dst echo.Instances) error {
							policy := yml.MustApplyNamespace(t, tmpl.MustEvaluate(
								file.AsStringOrFail(t, "testdata/requestauthn/ingress.yaml.tmpl"),
								map[string]string{
									"Namespace": ns.Name(),
									"dst":       dst[0].Config().Service,
								},
							), ns.Name())
							if err := t.ConfigIstio().ApplyYAML(ns.Name(), policy); err != nil {
								t.Logf("failed to deploy ingress: %v", err)
								return err
							}
							util.WaitForConfig(t, ns, policy)
							return nil
						}).
						From(util.SourceFilter(t, apps, ns.Name(), false)...).
						ConditionallyTo(echotest.ReachableDestinations).
						ConditionallyTo(func(from echo.Instance, to echo.Instances) echo.Instances {
							return to.Match(echo.InCluster(from.Config().Cluster))
						}).
						To(util.DestFilter(t, apps, ns.Name(), false)...).
						Run(func(t framework.TestContext, src echo.Instance, dest echo.Instances) {
							t.NewSubTest(c.Name).Run(func(t framework.TestContext) {
								c.CallOpts.Target = dest[0]
								c.DestClusters = dest.Clusters()
								c.CallOpts.Validator = echo.And(echo.ValidatorFunc(c.CheckAuthn))
								src.CallWithRetryOrFail(t, c.CallOpts, echo.DefaultCallRetryOptions()...)
							})
						})
				}
			})

			// TODO(JimmyCYJ): add workload-agnostic test pattern to support ingress gateway tests.
			policy := yml.MustApplyNamespace(t, tmpl.MustEvaluate(
				file.AsStringOrFail(t, "testdata/requestauthn/ingress.yaml.tmpl"),
				map[string]string{
					"Namespace": ns.Name(),
					"dst":       util.BSvc,
				},
			), ns.Name())
			t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policy)
			t.NewSubTest("ingress-authn").Run(func(t framework.TestContext) {
				for _, cluster := range t.Clusters() {
					ingr := ist.IngressFor(cluster)
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
						t.NewSubTest(c.Name).Run(func(t framework.TestContext) {
							authn.CheckIngressOrFail(t, ingr, c.Host, c.Path, nil, c.Token, c.ExpectResponseCode)
						})
					}
				}
			})
		})
}
