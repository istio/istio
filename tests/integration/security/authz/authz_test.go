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
	"net/http"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/authz"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/config"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/tests/common/jwt"
)

func TestAuthz_Principal(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local",
			"security.authorization.grpc-protocol",
			"security.authorization.tcp").
		Run(func(t framework.TestContext) {
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			from := allowed.Append(denied)
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/mtls.yaml.tmpl")).
				Source(config.File("testdata/v1beta1/allow-principal.yaml.tmpl").WithParams(
					param.Params{
						"Allowed": allowed,
					})).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.NamespacedName() == allowed.NamespacedName())

					cases := []struct {
						ports []string
						path  string
						allow allowValue
					}{
						{
							ports: []string{ports.GRPC, ports.TCP},
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow?param=value",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny",
							allow: false,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny?param=value",
							allow: false,
						},
					}

					for _, c := range cases {
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							Path(c.path).
							BuildAndRunForPorts(t, c.ports...)
					}
				})
		})
}

func TestAuthz_DenyPrincipal(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local",
			"security.authorization.grpc-protocol",
			"security.authorization.tcp",
			"security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			from := allowed.Append(denied)
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/mtls.yaml.tmpl")).
				Source(config.File("testdata/v1beta1/deny-global.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): istio.ClaimSystemNamespaceOrFail(t, t),
				})).
				Source(config.File("testdata/v1beta1/deny-principal.yaml.tmpl").WithParams(
					param.Params{
						"Denied": denied,
					})).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.NamespacedName() != denied.NamespacedName())

					cases := []struct {
						ports []string
						path  string
						allow allowValue
					}{
						{
							ports: []string{ports.GRPC, ports.TCP},
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny?param=value",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny/allow",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny/allow?param=value",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow?param=value",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny",
							allow: false,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny?param=value",
							allow: false,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny/allow",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny/allow?param=value",
							allow: true,
						},
					}

					for _, c := range cases {
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							Path(c.path).
							BuildAndRunForPorts(t, c.ports...)
					}
				})
		})
}

func TestAuthz_Namespace(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local",
			"security.authorization.grpc-protocol",
			"security.authorization.tcp").
		Run(func(t framework.TestContext) {
			// Allow anything from ns1. Any service in ns1 will work as the `from` (just using ns1.A)
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			from := allowed.Append(denied)
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1AndNs2)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/mtls.yaml.tmpl")).
				Source(config.File("testdata/v1beta1/allow-namespace.yaml.tmpl").WithParams(
					param.Params{
						"Allowed": allowed,
					})).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.Config().Namespace.Name() == allowed.Config().Namespace.Name())

					cases := []struct {
						ports []string
						path  string
						allow allowValue
					}{
						{
							ports: []string{ports.GRPC, ports.TCP},
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow?param=value",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny",
							allow: false,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny?param=value",
							allow: false,
						},
					}

					for _, c := range cases {
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							Path(c.path).
							BuildAndRunForPorts(t, c.ports...)
					}
				})
		})
}

func TestAuthz_DenyNamespace(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local",
			"security.authorization.grpc-protocol",
			"security.authorization.tcp",
			"security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			from := allowed.Append(denied)
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1AndNs2)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/mtls.yaml.tmpl")).
				Source(config.File("testdata/v1beta1/deny-global.yaml.tmpl").WithParams(param.Params{
					param.Namespace.String(): istio.ClaimSystemNamespaceOrFail(t, t),
				})).
				Source(config.File("testdata/v1beta1/deny-namespace.yaml.tmpl").WithParams(
					param.Params{
						"Denied": denied,
					})).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.Config().Namespace.Name() == allowed.Config().Namespace.Name())

					cases := []struct {
						ports []string
						path  string
						allow allowValue
					}{
						{
							ports: []string{ports.GRPC, ports.TCP},
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny?param=value",
							allow: allow,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny/allow",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/deny/allow?param=value",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/allow?param=value",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny",
							allow: false,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny?param=value",
							allow: false,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny/allow",
							allow: true,
						},
						{
							ports: []string{ports.HTTP, ports.HTTP2},
							path:  "/global-deny/allow?param=value",
							allow: true,
						},
					}

					for _, c := range cases {
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							Path(c.path).
							BuildAndRunForPorts(t, c.ports...)
					}
				})
		})
}

func TestAuthz_NotNamespace(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local",
			"security.authorization.grpc-protocol",
			"security.authorization.tcp",
			"security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			from := allowed.Append(denied)
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/mtls.yaml.tmpl")).
				Source(config.File("testdata/v1beta1/not-namespace.yaml.tmpl").WithParams(
					param.Params{
						"Allowed": allowed,
					})).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.Config().Namespace.Name() == allowed.Config().Namespace.Name())

					newAuthzTest().
						From(from).
						To(to).
						Allow(allow).
						BuildAndRunForPorts(t, ports.GRPC, ports.TCP, ports.HTTP, ports.HTTP2)
				})
		})
}

func TestAuthz_NotHost(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			from := apps.Ns1.A
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/not-host.yaml.tmpl")).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				RunViaIngress(func(t framework.TestContext, from ingress.Instance, to echo.Target) {
					cases := []struct {
						host  string
						allow allowValue
					}{
						{
							host:  fmt.Sprintf("allow.%s.com", to.Config().Service),
							allow: true,
						},
						{
							host:  fmt.Sprintf("deny.%s.com", to.Config().Service),
							allow: false,
						},
					}

					for _, c := range cases {
						c := c
						testName := fmt.Sprintf("%s(%s)/http", c.host, c.allow)
						t.NewSubTest(testName).RunParallel(func(t framework.TestContext) {
							wantCode := http.StatusOK
							if !c.allow {
								wantCode = http.StatusForbidden
							}

							opts := echo.CallOptions{
								Port: echo.Port{
									Protocol: protocol.HTTP,
								},
								HTTP: echo.HTTP{
									Headers: headers.New().WithHost(c.host).Build(),
								},
								Check: check.And(check.NoError(), check.Status(wantCode)),
							}
							from.CallOrFail(t, opts)
						})
					}
				})
		})
}

func TestAuthz_NotMethod(t *testing.T) {
	// NOTE: negative match for mtls is tested by TestAuthz_DenyPlaintext.
	// Negative match for paths is tested by TestAuthz_DenyPrincipal, TestAuthz_DenyNamespace.
	framework.NewTest(t).
		Features("security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			from := apps.Ns1.A
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1AndNs2)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/not-method.yaml.tmpl")).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					cases := []struct {
						ports  []string
						method string
						allow  allowValue
					}{
						{
							ports:  []string{ports.HTTP, ports.HTTP2},
							method: "GET",
							allow:  true,
						},
						{
							ports:  []string{ports.HTTP, ports.HTTP2},
							method: "PUT",
							allow:  false,
						},
					}

					for _, c := range cases {
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							Method(c.method).
							BuildAndRunForPorts(t, c.ports...)
					}
				})
		})
}

func TestAuthz_NotPort(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			from := apps.Ns1.A
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1AndNs2)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/not-port.yaml.tmpl")).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					cases := []struct {
						ports []string
						allow allowValue
					}{
						{
							ports: []string{ports.HTTP},
							allow: true,
						},
						{
							ports: []string{ports.HTTP2},
							allow: false,
						},
					}

					for _, c := range cases {
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							BuildAndRunForPorts(t, c.ports...)
					}
				})
		})
}

func TestAuthz_DenyPlaintext(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local",
			"security.authorization.grpc-protocol",
			"security.authorization.tcp",
			"security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			newTrafficTest(t, apps.Ns1.All.Instances().Append(denied)).
				Config(config.File("testdata/v1beta1/plaintext.yaml.tmpl").WithParams(param.Params{
					"Denied": denied,
					// The namespaces for each resource are specified in the file. Use "" as the ns to apply to.
					param.Namespace.String(): "",
				})).
				// Just test from A in each namespace to show the policy works.
				FromMatch(match.AnyServiceName(allowed.Append(denied).NamespacedNames())).
				ToMatch(match.Namespace(apps.Ns1.Namespace)).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.Config().Namespace.Name() == allowed.Config().Namespace.Name())
					newAuthzTest().
						From(from).
						To(to).
						Allow(allow).
						BuildAndRunForPorts(t, ports.GRPC, ports.TCP, ports.HTTP, ports.HTTP2)
				})
		})
}

func TestAuthz_JWT(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("security.authorization.jwt-token").
		Run(func(t framework.TestContext) {
			from := apps.Ns1.A
			fromMatch := match.ServiceName(from.NamespacedName())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/jwt.yaml.tmpl").WithNamespace(apps.Ns1.Namespace)).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					cases := []struct {
						prefix string
						jwt    string
						path   string
						allow  allowValue
					}{
						{
							prefix: "[No JWT]",
							jwt:    "",
							path:   "/token1",
							allow:  false,
						},
						{
							prefix: "[No JWT]",
							jwt:    "",
							path:   "/token2",
							allow:  false,
						},
						{
							prefix: "[Token1]",
							jwt:    jwt.TokenIssuer1, path: "/token1", allow: true,
						},
						{
							prefix: "[Token1]",
							jwt:    jwt.TokenIssuer1,
							path:   "/token2",
							allow:  false,
						},
						{
							prefix: "[Token2]",
							jwt:    jwt.TokenIssuer2,
							path:   "/token1",
							allow:  false,
						},
						{
							prefix: "[Token2]",
							jwt:    jwt.TokenIssuer2,
							path:   "/token2",
							allow:  true,
						},
						{
							prefix: "[Token3]",
							jwt:    jwt.TokenIssuer1,
							path:   "/token3",
							allow:  false,
						},
						{
							prefix: "[Token3]",
							jwt:    jwt.TokenIssuer2,
							path:   "/token3",
							allow:  true,
						},
						{
							prefix: "[Token1]",
							jwt:    jwt.TokenIssuer1,
							path:   "/tokenAny",
							allow:  true,
						},
						{
							prefix: "[Token2]",
							jwt:    jwt.TokenIssuer2,
							path:   "/tokenAny",
							allow:  true,
						},
						{
							prefix: "[PermissionToken1]",
							jwt:    jwt.TokenIssuer1,
							path:   "/permission",
							allow:  false,
						},
						{
							prefix: "[PermissionToken2]",
							jwt:    jwt.TokenIssuer2,
							path:   "/permission",
							allow:  false,
						},
						{
							prefix: "[PermissionTokenWithSpaceDelimitedScope]",
							jwt:    jwt.TokenIssuer2WithSpaceDelimitedScope,
							path:   "/permission",
							allow:  true,
						},
						{
							prefix: "[NestedToken1]",
							jwt:    jwt.TokenIssuer1WithNestedClaims1,
							path:   "/nested-key1",
							allow:  true,
						},
						{
							prefix: "[NestedToken2]",
							jwt:    jwt.TokenIssuer1WithNestedClaims2,
							path:   "/nested-key1",
							allow:  false,
						},
						{
							prefix: "[NestedToken1]",
							jwt:    jwt.TokenIssuer1WithNestedClaims1,
							path:   "/nested-key2",
							allow:  false,
						},
						{
							prefix: "[NestedToken2]",
							jwt:    jwt.TokenIssuer1WithNestedClaims2,
							path:   "/nested-key2",
							allow:  true,
						},
						{
							prefix: "[NestedToken1]",
							jwt:    jwt.TokenIssuer1WithNestedClaims1,
							path:   "/nested-2-key1",
							allow:  true,
						},
						{
							prefix: "[NestedToken2]",
							jwt:    jwt.TokenIssuer1WithNestedClaims2,
							path:   "/nested-2-key1",
							allow:  false,
						},
						{
							prefix: "[NestedToken1]",
							jwt:    jwt.TokenIssuer1WithNestedClaims1,
							path:   "/nested-non-exist",
							allow:  false,
						},
						{
							prefix: "[NestedToken2]",
							jwt:    jwt.TokenIssuer1WithNestedClaims2,
							path:   "/nested-non-exist",
							allow:  false,
						},
						{
							prefix: "[NoJWT]",
							jwt:    "",
							path:   "/tokenAny",
							allow:  false,
						},
					}
					for _, c := range cases {
						h := headers.New().WithAuthz(c.jwt).Build()
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							Prefix(c.prefix).
							Path(c.path).
							Headers(h).
							BuildAndRunForPorts(t, ports.HTTP, ports.HTTP2)
					}
				})
		})
}

func TestAuthz_WorkloadSelector(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.workload-selector").
		Run(func(t framework.TestContext) {
			// Verify that the workload-specific path (/policy-<ns>-<svc>) works only on the selected workload.
			t.NewSubTestf("single workload").
				Run(func(t framework.TestContext) {
					from := apps.Ns1.A
					fromMatch := match.ServiceName(from.NamespacedName())
					toMatch := match.Not(fromMatch)
					to := toMatch.GetServiceMatches(apps.Ns1.All)
					fromAndTo := to.Instances().Append(from)

					config.New(t).
						Source(config.File("testdata/v1beta1/workload.yaml.tmpl")).
						// Also define a bad workload selector for path /policy-<ns>-<svc>-bad.
						Source(config.File("testdata/v1beta1/workload-bad.yaml.tmpl")).
						// Allow /policy-<ns>-all for all workloads.
						Source(config.File("testdata/v1beta1/workload-ns.yaml.tmpl").WithParams(param.Params{
							param.Namespace.String(): apps.Ns1.Namespace,
						})).
						Source(config.File("testdata/v1beta1/workload-ns.yaml.tmpl").WithParams(param.Params{
							param.Namespace.String(): apps.Ns2.Namespace,
						})).
						// Allow /policy-istio-system-<svc> for all services in all namespaces. Just using ns1 to avoid
						// creating duplicate resources.
						Source(config.File("testdata/v1beta1/workload-system-ns.yaml.tmpl").WithParams(param.Params{
							param.Namespace.String(): istio.ClaimSystemNamespaceOrFail(t, t),
						})).
						BuildAll(nil, to).
						Apply()

					newTrafficTest(t, fromAndTo).
						FromMatch(fromMatch).
						ToMatch(toMatch).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							type testCase struct {
								path  string
								allow allowValue
							}

							cases := []testCase{
								{
									// Make sure the bad policy did not select this workload.
									path:  fmt.Sprintf("/policy-%s-%s-bad", to.Config().Namespace.Prefix(), to.Config().Service),
									allow: false,
								},
							}

							// Make sure the namespace-wide policy was applied to this workload.
							for _, ns := range []namespace.Instance{apps.Ns1.Namespace, apps.Ns2.Namespace} {
								cases = append(cases,
									testCase{
										path:  fmt.Sprintf("/policy-%s-all", ns.Prefix()),
										allow: ns.Name() == to.Config().Namespace.Name(),
									})
							}

							// Make sure the workload-specific paths succeeds.
							cases = append(cases,
								testCase{
									path:  fmt.Sprintf("/policy-%s-%s", to.Config().Namespace.Prefix(), to.Config().Service),
									allow: true,
								},
								testCase{
									path:  fmt.Sprintf("/policy-system-%s", to.Config().Service),
									allow: true,
								})

							// The workload-specific paths should fail for another service (just add a single test case).
							for _, svc := range apps.Ns1.All {
								if svc.Config().Service != to.Config().Service {
									cases = append(cases,
										testCase{
											path:  fmt.Sprintf("/policy-%s-%s", svc.Config().Namespace.Prefix(), svc.Config().Service),
											allow: false,
										},
										testCase{
											path:  fmt.Sprintf("/policy-system-%s", svc.Config().Service),
											allow: false,
										})
									break
								}
							}

							for _, c := range cases {
								newAuthzTest().
									From(from).
									To(to).
									Allow(c.allow).
									Path(c.path).
									BuildAndRunForPorts(t, ports.HTTP, ports.HTTP2)
							}
						})
				})
		})
}

func TestAuthz_PathPrecedence(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.deny-action").
		Run(func(t framework.TestContext) {
			from := apps.Ns1.A
			fromMatch := match.ServiceName(from.NamespacedName())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/path-precedence.yaml.tmpl")).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					cases := []struct {
						path  string
						allow allowValue
					}{
						{
							path:  "/allow/admin",
							allow: false,
						},
						{
							path:  "/allow/admin?param=value",
							allow: false,
						},
						{
							path:  "/allow",
							allow: true,
						},
						{
							path:  "/allow?param=value",
							allow: true,
						},
					}

					for _, c := range cases {
						newAuthzTest().
							From(from).
							To(to).
							Allow(c.allow).
							Path(c.path).
							BuildAndRunForPorts(t, ports.HTTP, ports.HTTP2)
					}
				})
		})
}

func TestAuthz_IngressGateway(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.ingress-gateway").
		Run(func(t framework.TestContext) {
			to := apps.Ns1.All
			config.New(t).
				Source(config.File("testdata/v1beta1/ingress-gateway.yaml.tmpl").WithParams(param.Params{
					// The namespaces for each resource are specified in the file. Use "" as the ns to apply to.
					param.Namespace.String(): "",
				})).
				BuildAll(nil, to).
				Apply()
			newTrafficTest(t, to.Instances()).
				RunViaIngress(func(t framework.TestContext, from ingress.Instance, to echo.Target) {
					host := func(fmtStr string) string {
						return fmt.Sprintf(fmtStr, to.Config().Service)
					}
					cases := []struct {
						host  string
						path  string
						ip    string
						allow allowValue
					}{
						{
							host:  host("deny.%s.com"),
							allow: false,
						},
						{
							host:  host("DENY.%s.COM"),
							allow: false,
						},
						{
							host:  host("Deny.%s.Com"),
							allow: false,
						},
						{
							host:  host("deny.suffix.%s.com"),
							allow: false,
						},
						{
							host:  host("DENY.SUFFIX.%s.COM"),
							allow: false,
						},
						{
							host:  host("Deny.Suffix.%s.Com"),
							allow: false,
						},
						{
							host:  host("prefix.%s.com"),
							allow: false,
						},
						{
							host:  host("PREFIX.%s.COM"),
							allow: false,
						},
						{
							host:  host("Prefix.%s.Com"),
							allow: false,
						},
						{
							host:  host("www.%s.com"),
							path:  "/",
							ip:    "172.16.0.1",
							allow: true,
						},
						{
							host:  host("www.%s.com"),
							path:  "/private",
							ip:    "172.16.0.1",
							allow: false,
						},
						{
							host:  host("www.%s.com"),
							path:  "/public",
							ip:    "172.16.0.1",
							allow: true,
						},
						{
							host:  host("internal.%s.com"),
							path:  "/",
							ip:    "172.16.0.1",
							allow: false,
						},
						{
							host:  host("internal.%s.com"),
							path:  "/private",
							ip:    "172.16.0.1",
							allow: false,
						},
						{
							host:  host("remoteipblocks.%s.com"),
							path:  "/",
							ip:    "172.17.72.46",
							allow: false,
						},
						{
							host:  host("remoteipblocks.%s.com"),
							path:  "/",
							ip:    "192.168.5.233",
							allow: false,
						},
						{
							host:  host("remoteipblocks.%s.com"),
							path:  "/",
							ip:    "10.4.5.6",
							allow: true,
						},
						{
							host:  host("notremoteipblocks.%s.com"),
							path:  "/",
							ip:    "10.2.3.4",
							allow: false,
						},
						{
							host:  host("notremoteipblocks.%s.com"),
							path:  "/",
							ip:    "172.23.242.188",
							allow: true,
						},
						{
							host:  host("remoteipattr.%s.com"),
							path:  "/",
							ip:    "10.242.5.7",
							allow: false,
						},
						{
							host:  host("remoteipattr.%s.com"),
							path:  "/",
							ip:    "10.124.99.10",
							allow: false,
						},
						{
							host:  host("remoteipattr.%s.com"),
							path:  "/",
							ip:    "10.4.5.6",
							allow: true,
						},
					}

					for _, c := range cases {
						c := c
						testName := fmt.Sprintf("%s%s(%s)/http", c.host, c.path, c.allow)
						if len(c.ip) > 0 {
							testName = c.ip + "->" + testName
						}
						t.NewSubTest(testName).RunParallel(func(t framework.TestContext) {
							wantCode := http.StatusOK
							if !c.allow {
								wantCode = http.StatusForbidden
							}

							opts := echo.CallOptions{
								Port: echo.Port{
									Protocol: protocol.HTTP,
								},
								HTTP: echo.HTTP{
									Path:    c.path,
									Headers: headers.New().WithHost(c.host).WithXForwardedFor(c.ip).Build(),
								},
								Check: check.And(check.NoError(), check.Status(wantCode)),
							}
							from.CallOrFail(t, opts)
						})
					}
				})
		})
}

func TestAuthz_EgressGateway(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("security.authorization.egress-gateway").
		Run(func(t framework.TestContext) {
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			from := allowed.Append(denied)
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Config(config.File("testdata/v1beta1/egress-gateway.yaml.tmpl").WithParams(param.Params{
					// The namespaces for each resource are specified in the file. Use "" as the ns to apply to.
					param.Namespace.String(): "",
					"Allowed":                allowed,
				})).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.NamespacedName() == allowed.Config().NamespacedName())

					cases := []struct {
						host  string
						path  string
						token string
						allow allowValue
					}{
						{
							host:  "www.company.com",
							path:  "/allow",
							allow: true,
						},
						{
							host:  "www.company.com",
							path:  "/deny",
							allow: false,
						},
						{
							host:  fmt.Sprintf("%s-%s-only.com", allowed.Config().Service, allowed.Config().Namespace.Name()),
							path:  "/",
							allow: allow,
						},
						{
							host:  "jwt-only.com",
							path:  "/",
							token: jwt.TokenIssuer1,
							allow: true,
						},
						{
							host:  "jwt-only.com",
							path:  "/",
							token: jwt.TokenIssuer2,
							allow: false,
						},
						{
							host:  fmt.Sprintf("jwt-and-%s-%s-only.com", allowed.Config().Service, allowed.Config().Namespace.Name()),
							path:  "/",
							token: jwt.TokenIssuer1,
							allow: allow,
						},
						{
							path:  "/",
							host:  fmt.Sprintf("jwt-and-%s-%s-only.com", allowed.Config().Service, allowed.Config().Namespace.Name()),
							token: jwt.TokenIssuer2,
							allow: false,
						},
					}

					for _, c := range cases {
						c := c
						testName := fmt.Sprintf("%s%s(%s)/http", c.host, c.path, c.allow)
						t.NewSubTest(testName).Run(func(t framework.TestContext) {
							wantCode := http.StatusOK
							body := "handled-by-egress-gateway"
							if !c.allow {
								wantCode = http.StatusForbidden
								body = "RBAC: access denied"
							}

							opts := echo.CallOptions{
								// Use a fake IP address to bypass DNS lookup (which will fail). The host
								// header will be used for routing decisions.
								Address: "10.4.4.4",
								Port: echo.Port{
									Name:        ports.HTTP,
									Protocol:    protocol.HTTP,
									ServicePort: 80,
								},
								HTTP: echo.HTTP{
									Path:    c.path,
									Headers: headers.New().WithHost(c.host).WithAuthz(c.token).Build(),
								},
								Check: check.And(check.NoErrorAndStatus(wantCode), check.BodyContains(body)),
							}

							from.CallOrFail(t, opts)
						})
					}
				})
		})
}

func TestAuthz_Conditions(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.conditions").
		Run(func(t framework.TestContext) {
			allowed := apps.Ns1.A
			denied := apps.Ns2.A

			from := allowed.Append(denied)
			fromMatch := match.AnyServiceName(from.NamespacedNames())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/mtls.yaml.tmpl")).
				Source(config.File("testdata/v1beta1/conditions.yaml.tmpl").WithParams(param.Params{
					"Allowed": allowed,
					"Denied":  denied,
				})).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					allow := allowValue(from.NamespacedName() == allowed.Config().NamespacedName())

					skipSourceIPTestsForMulticluster := func(t framework.TestContext) {
						t.Helper()
						if t.Clusters().IsMulticluster() {
							// TODO(nmittler): Needs to be documented as a limitation for multi-network.
							t.Skip("https://github.com/istio/istio/issues/37307: " +
								"Source IP-based authz tests are not supported in multi-network configurations " +
								"due to the fact that the origin source IP will be lost when traversing the " +
								"east-west gateway.")
						}
					}

					cases := []struct {
						path    string
						headers http.Header
						allow   allowValue
						skipFn  func(t framework.TestContext)
					}{
						// Test headers.
						{
							path:    "/request-headers",
							headers: headers.New().With("x-foo", "foo").Build(),
							allow:   true,
						},
						{
							path:    "/request-headers",
							headers: headers.New().With("x-foo", "bar").Build(),
							allow:   false,
						},
						{
							path:  "/request-headers",
							allow: false,
						},
						{
							path:    "/request-headers-notValues",
							headers: headers.New().With("x-foo", "foo").Build(),
							allow:   true,
						},
						{
							path:    "/request-headers-notValues",
							headers: headers.New().With("x-foo", "bar").Build(),
							allow:   false,
						},

						// Test source IP
						{
							path:   "/source-ip",
							allow:  allow,
							skipFn: skipSourceIPTestsForMulticluster,
						},
						{
							path:   "/source-ip-notValues",
							allow:  allow,
							skipFn: skipSourceIPTestsForMulticluster,
						},

						// Test source namespace
						{
							path:  "/source-namespace",
							allow: allow,
						},
						{
							path:  "/source-namespace-notValues",
							allow: allow,
						},

						// Test source principal
						{
							path:  "/source-principal",
							allow: allow,
						},
						{
							path:  "/source-principal-notValues",
							allow: allow,
						},

						// Test destination IP
						{
							path:  "/destination-ip-good",
							allow: true,
						},
						{
							path:  "/destination-ip-bad",
							allow: false,
						},
						{
							path:  "/destination-ip-notValues",
							allow: false,
						},

						// Test destination port
						{
							path:  "/destination-port-good",
							allow: true,
						},
						{
							path:  "/destination-port-bad",
							allow: false,
						},
						{
							path:  "/destination-port-notValues",
							allow: false,
						},

						// Test SNI
						{
							path:  "/connection-sni-good",
							allow: true,
						},
						{
							path:  "/connection-sni-bad",
							allow: false,
						},
						{
							path:  "/connection-sni-notValues",
							allow: false,
						},

						{
							path:  "/other",
							allow: false,
						},
					}

					for _, c := range cases {
						c := c
						xfooHeader := ""
						if c.headers != nil {
							xfooHeader = "?x-foo=" + c.headers.Get("x-foo")
						}
						testName := fmt.Sprintf("%s%s(%s)/http", c.path, xfooHeader, c.allow)
						t.NewSubTest(testName).RunParallel(func(t framework.TestContext) {
							if c.skipFn != nil {
								c.skipFn(t)
							}

							newAuthzTest().
								From(from).
								To(to).
								PortName(ports.HTTP).
								Path(c.path).
								Allow(c.allow).
								Headers(c.headers).
								BuildAndRun(t)
						})
					}
				})
		})
}

func TestAuthz_PathNormalization(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.path-normalization").
		Run(func(t framework.TestContext) {
			from := apps.Ns1.A
			fromMatch := match.ServiceName(from.NamespacedName())
			toMatch := match.Not(fromMatch)
			to := toMatch.GetServiceMatches(apps.Ns1.All)
			fromAndTo := to.Instances().Append(from)

			config.New(t).
				Source(config.File("testdata/v1beta1/path-normalization.yaml.tmpl")).
				BuildAll(nil, to).
				Apply()

			newTrafficTest(t, fromAndTo).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					cases := []struct {
						path  string
						allow allowValue
					}{
						{
							path:  "/public",
							allow: true,
						},
						{
							path:  "/public/../public",
							allow: true,
						},
						{
							path:  "/private",
							allow: false,
						},
						{
							path:  "/public/../private",
							allow: false,
						},
						{
							path:  "/public/./../private",
							allow: false,
						},
						{
							path:  "/public/.././private",
							allow: false,
						},
						{
							path:  "/public/%2E%2E/private",
							allow: false,
						},
						{
							path:  "/public/%2e%2e/private",
							allow: false,
						},
						{
							path:  "/public/%2E/%2E%2E/private",
							allow: false,
						},
						{
							path:  "/public/%2e/%2e%2e/private",
							allow: false,
						},
						{
							path:  "/public/%2E%2E/%2E/private",
							allow: false,
						},
						{
							path:  "/public/%2e%2e/%2e/private",
							allow: false,
						},
					}

					for _, c := range cases {
						c := c
						testName := fmt.Sprintf("%s(%s)/http", c.path, c.allow)
						t.NewSubTest(testName).RunParallel(func(t framework.TestContext) {
							newAuthzTest().
								From(from).
								To(to).
								PortName(ports.HTTP).
								Path(c.path).
								Allow(c.allow).
								BuildAndRun(t)
						})
					}
				})
		})
}

func TestAuthz_CustomServer(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.custom").
		Run(func(t framework.TestContext) {
			extAuthzHeaders := func(value string) http.Header {
				return headers.New().
					With(authz.XExtAuthz, value).
					With(authz.XExtAuthzAdditionalHeaderOverride, "should-be-override").
					Build()
			}
			allowHeaders := func() http.Header {
				return extAuthzHeaders(authz.XExtAuthzAllow)
			}
			denyHeaders := func() http.Header {
				return extAuthzHeaders("deny")
			}

			allProviders := append(authzServer.Providers(), localAuthzServer.Providers()...)
			for _, provider := range allProviders {
				t.NewSubTest(provider.Name()).Run(func(t framework.TestContext) {
					// The ext-authz server is hard-coded to allow requests from any service account ending in
					// "/sa/a". Since the namespace is ignored, we use ns2.B for our denied app (rather than ns2.A).
					// We'll only need the service account for TCP, since we send headers in all other protocols
					// to control the server's behavior.
					allowed := apps.Ns1.A
					var denied echo.Instances
					if provider.IsProtocolSupported(protocol.TCP) {
						denied = apps.Ns2.B
					}

					from := allowed.Append(denied)
					fromMatch := match.AnyServiceName(from.NamespacedNames())
					toMatch := match.And(match.Not(fromMatch), provider.MatchSupportedTargets())
					to := toMatch.GetServiceMatches(apps.Ns1.All)
					fromAndTo := to.Instances().Append(from)

					config.New(t).
						Source(config.File("testdata/v1beta1/custom-provider.yaml.tmpl").WithParams(param.Params{
							"Provider": provider,
						})).
						BuildAll(nil, to).
						Apply()

					newTrafficTest(t, fromAndTo).
						FromMatch(fromMatch).
						ToMatch(toMatch).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							fromAllowed := from.NamespacedName() == allowed.NamespacedName()

							authzPath := "/custom"
							cases := []struct {
								ports   []string
								path    string
								headers http.Header
								allow   allowValue
								skip    bool
							}{
								{
									ports: []string{ports.TCP},
									// For TCP, we rely on the hard-coded allowed service account.
									allow: allowValue(fromAllowed),
								},
								{
									ports:   []string{ports.HTTP, ports.HTTP2, ports.GRPC},
									path:    authzPath,
									headers: allowHeaders(),
									allow:   true,
									skip:    !fromAllowed,
								},
								{
									ports:   []string{ports.HTTP, ports.HTTP2, ports.GRPC},
									path:    authzPath,
									headers: denyHeaders(),
									allow:   false,
									skip:    !fromAllowed,
								},
								{
									ports:   []string{ports.HTTP, ports.HTTP2},
									path:    "/health",
									headers: extAuthzHeaders(authz.XExtAuthzAllow),
									allow:   true,
									skip:    !fromAllowed,
								},
								{
									ports:   []string{ports.HTTP, ports.HTTP2},
									path:    "/health",
									headers: extAuthzHeaders("deny"),
									allow:   true,
									skip:    !fromAllowed,
								},
							}

							for _, c := range cases {
								c := c
								if c.skip {
									continue
								}
								tsts := newAuthzTest().
									From(from).
									To(to).
									Path(c.path).
									Headers(c.headers).
									Allow(c.allow).
									BuildForPorts(t, c.ports...).
									Filter(func(tst authzTest) bool {
										return provider.IsProtocolSupported(tst.opts.Port.Protocol)
									})
								for _, tst := range tsts {
									params := ""
									if c.headers != nil {
										params = fmt.Sprintf("?%s=%s", authz.XExtAuthz, c.headers.Get(authz.XExtAuthz))
									}
									testName := fmt.Sprintf("%s%s(%s)/%s", c.path, params, c.allow, tst.opts.Port.Name)
									t.NewSubTest(testName).RunParallel(func(t framework.TestContext) {
										if c.path == authzPath {
											tst.opts.Check = check.And(tst.opts.Check, provider.Check(tst.opts, c.allow.Bool()))
										}

										tst.Run(t)
									})
								}
							}
						})
				})
			}
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
		ConditionallyTo(func(from echo.Instance, to echo.Instances) echo.Instances {
			// Disallow self-calls since it will bypass the sidecar.
			return match.Not(match.ServiceName(from.NamespacedName())).GetMatches(to)
		})
}

type allowValue bool

func (v allowValue) Bool() bool {
	return bool(v)
}

func (v allowValue) String() string {
	if v {
		return "allow"
	}
	return "deny"
}

type authzTest struct {
	from   echo.Instance
	opts   echo.CallOptions
	allow  allowValue
	prefix string
}

func newAuthzTest() *authzTest {
	return &authzTest{}
}

func (b *authzTest) Prefix(prefix string) *authzTest {
	b.prefix = prefix
	return b
}

func (b *authzTest) From(from echo.Instance) *authzTest {
	b.from = from
	return b
}

func (b *authzTest) To(to echo.Target) *authzTest {
	b.opts.To = to
	return b
}

func (b *authzTest) PortName(portName string) *authzTest {
	b.opts.Port.Name = portName
	return b
}

func (b *authzTest) Method(method string) *authzTest {
	b.opts.HTTP.Method = method
	return b
}

func (b *authzTest) Path(path string) *authzTest {
	b.opts.HTTP.Path = path
	return b
}

func (b *authzTest) Headers(headers http.Header) *authzTest {
	b.opts.HTTP.Headers = headers
	return b
}

func (b *authzTest) Allow(allow allowValue) *authzTest {
	b.allow = allow
	return b
}

func (b *authzTest) Build(t framework.TestContext) *authzTest {
	t.Helper()

	// Fill in the defaults.
	b.opts.FillDefaultsOrFail(t)

	if b.allow {
		b.opts.Check = check.And(check.OK(), check.ReachedTargetClusters(t))
	} else {
		b.opts.Check = check.Forbidden(b.opts.Port.Protocol)
	}
	return b
}

func (b *authzTest) BuildForPorts(t framework.TestContext, ports ...string) authzTests {
	out := make(authzTests, 0, len(ports))
	for _, p := range ports {
		opts := b.opts.DeepCopy()
		opts.Port.Name = p

		tst := (&authzTest{
			prefix: b.prefix,
			from:   b.from,
			opts:   opts,
			allow:  b.allow,
		}).Build(t)
		out = append(out, *tst)
	}
	return out
}

func (b *authzTest) BuildAndRunForPorts(t framework.TestContext, ports ...string) {
	tsts := b.BuildForPorts(t, ports...)
	tsts.RunAll(t)
}

func (b *authzTest) Run(t framework.TestContext) {
	t.Helper()
	b.from.CallOrFail(t, b.opts)
}

func (b *authzTest) BuildAndRun(t framework.TestContext) {
	t.Helper()
	b.Build(t).Run(t)
}

type authzTests []authzTest

func (tsts authzTests) checkValid() {
	path := tsts[0].opts.HTTP.Path
	allow := tsts[0].allow
	prefix := tsts[0].prefix
	for _, tst := range tsts {
		if tst.opts.HTTP.Path != path {
			panic("authz tests have different paths")
		}
		if tst.allow != allow {
			panic("authz tests have different allow")
		}
		if tst.prefix != prefix {
			panic("authz tests have different prefixes")
		}
	}
}

func (tsts authzTests) Filter(keep func(authzTest) bool) authzTests {
	out := make(authzTests, 0, len(tsts))
	for _, tst := range tsts {
		if keep(tst) {
			out = append(out, tst)
		}
	}
	return out
}

func (tsts authzTests) RunAll(t framework.TestContext) {
	t.Helper()

	firstTest := tsts[0]
	if len(tsts) == 1 {
		// Testing a single port. Just run a single test.
		testName := fmt.Sprintf("%s%s(%s)/%s", firstTest.prefix, firstTest.opts.HTTP.Path, firstTest.allow, firstTest.opts.Port.Name)
		t.NewSubTest(testName).RunParallel(func(t framework.TestContext) {
			firstTest.BuildAndRun(t)
		})
		return
	}

	tsts.checkValid()

	// Testing multiple ports...
	// Name outer test with constant info. Name inner test with port.
	outerTestName := fmt.Sprintf("%s%s(%s)", firstTest.prefix, firstTest.opts.HTTP.Path, firstTest.allow)
	t.NewSubTest(outerTestName).RunParallel(func(t framework.TestContext) {
		for _, tst := range tsts {
			tst := tst
			t.NewSubTest(tst.opts.Port.Name).RunParallel(func(t framework.TestContext) {
				tst.BuildAndRun(t)
			})
		}
	})
}
