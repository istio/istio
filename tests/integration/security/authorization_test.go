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
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/check"
	echoCommon "istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/scheck"
)

func newRootNS(ctx framework.TestContext) namespace.Instance {
	return istio.ClaimSystemNamespaceOrFail(ctx, ctx)
}

// TestAuthorization_mTLS tests v1beta1 authorization with mTLS.
func TestAuthorization_mTLS(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local").
		Run(func(t framework.TestContext) {
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			for _, to := range []echo.Instances{b, vm} {
				to := to
				t.ConfigIstio().EvalFile(apps.Namespace1.Name(), map[string]string{
					"Namespace":  apps.Namespace1.Name(),
					"Namespace2": apps.Namespace2.Name(),
					"dst":        to.Config().Service,
				}, "testdata/authz/v1beta1-mtls.yaml.tmpl").ApplyOrFail(t, resource.Wait)
				callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
				for _, cluster := range t.Clusters() {
					a := match.And(match.Cluster(cluster), match.Namespace(apps.Namespace1.Name())).GetMatches(apps.A)
					c := match.And(match.Cluster(cluster), match.Namespace(apps.Namespace2.Name())).GetMatches(apps.C)
					if len(a) == 0 || len(c) == 0 {
						continue
					}

					t.NewSubTestf("From %s", cluster.StableName()).Run(func(t framework.TestContext) {
						newTestCase := func(from echo.Instance, path string, expectAllowed bool) func(t framework.TestContext) {
							return func(t framework.TestContext) {
								opts := echo.CallOptions{
									To: to,
									Port: echo.Port{
										Name: "http",
									},
									HTTP: echo.HTTP{
										Path: path,
									},
									Count: callCount,
								}
								if expectAllowed {
									opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
								} else {
									opts.Check = scheck.RBACFailure(&opts)
								}

								name := newRbacTestName("", expectAllowed, from, &opts)
								t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
									name.SkipIfNecessary(t)
									from.CallOrFail(t, opts)
								})
							}
						}
						// a and c send requests to dst
						cases := []func(testContext framework.TestContext){
							newTestCase(a[0], "/principal-a", true),
							newTestCase(a[0], "/namespace-2", false),
							newTestCase(c[0], "/principal-a", false),
							newTestCase(c[0], "/namespace-2", true),
						}
						for _, c := range cases {
							c(t)
						}
					})
				}
			}
		})
}

// TestAuthorization_JWT tests v1beta1 authorization with JWT token claims.
func TestAuthorization_JWT(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("security.authorization.jwt-token").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			b := match.Namespace(ns.Name()).GetMatches(apps.B)
			c := match.Namespace(ns.Name()).GetMatches(apps.C)
			vm := match.Namespace(ns.Name()).GetMatches(apps.VM)
			for _, dst := range []echo.Instances{b, vm} {
				args := map[string]string{
					"Namespace":  apps.Namespace1.Name(),
					"Namespace2": apps.Namespace2.Name(),
					"dst":        dst[0].Config().Service,
				}
				t.ConfigIstio().EvalFile(ns.Name(), args, "testdata/authz/v1beta1-jwt.yaml.tmpl").ApplyOrFail(t, resource.Wait)
				for _, srcCluster := range t.Clusters() {
					a := match.And(match.Cluster(srcCluster), match.Namespace(ns.Name())).GetMatches(apps.A)
					if len(a) == 0 {
						continue
					}

					t.NewSubTestf("From %s", srcCluster.StableName()).Run(func(t framework.TestContext) {
						newTestCase := func(from echo.Instance, to echo.Target, namePrefix, jwt, path string, expectAllowed bool) func(t framework.TestContext) {
							callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
							return func(t framework.TestContext) {
								opts := echo.CallOptions{
									To: to,
									Port: echo.Port{
										Name: "http",
									},
									HTTP: echo.HTTP{
										Path:    path,
										Headers: headers.New().WithAuthz(jwt).Build(),
									},
									Count: callCount,
								}
								if expectAllowed {
									opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
								} else {
									opts.Check = scheck.RBACFailure(&opts)
								}

								name := newRbacTestName(namePrefix, expectAllowed, from, &opts)
								t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
									name.SkipIfNecessary(t)
									from.CallOrFail(t, opts)
								})
							}
						}
						cases := []func(testContext framework.TestContext){
							newTestCase(a[0], dst, "[NoJWT]", "", "/token1", false),
							newTestCase(a[0], dst, "[NoJWT]", "", "/token2", false),
							newTestCase(a[0], dst, "[Token3]", jwt.TokenIssuer1, "/token3", false),
							newTestCase(a[0], dst, "[Token3]", jwt.TokenIssuer2, "/token3", true),
							newTestCase(a[0], dst, "[Token1]", jwt.TokenIssuer1, "/token1", true),
							newTestCase(a[0], dst, "[Token1]", jwt.TokenIssuer1, "/token2", false),
							newTestCase(a[0], dst, "[Token2]", jwt.TokenIssuer2, "/token1", false),
							newTestCase(a[0], dst, "[Token2]", jwt.TokenIssuer2, "/token2", true),
							newTestCase(a[0], dst, "[Token1]", jwt.TokenIssuer1, "/tokenAny", true),
							newTestCase(a[0], dst, "[Token2]", jwt.TokenIssuer2, "/tokenAny", true),
							newTestCase(a[0], dst, "[PermissionToken1]", jwt.TokenIssuer1, "/permission", false),
							newTestCase(a[0], dst, "[PermissionToken2]", jwt.TokenIssuer2, "/permission", false),
							newTestCase(a[0], dst, "[PermissionTokenWithSpaceDelimitedScope]", jwt.TokenIssuer2WithSpaceDelimitedScope, "/permission", true),
							newTestCase(a[0], dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-key1", true),
							newTestCase(a[0], dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-key1", false),
							newTestCase(a[0], dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-key2", false),
							newTestCase(a[0], dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-key2", true),
							newTestCase(a[0], dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-2-key1", true),
							newTestCase(a[0], dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-2-key1", false),
							newTestCase(a[0], dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-non-exist", false),
							newTestCase(a[0], dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-non-exist", false),
							newTestCase(a[0], dst, "[NoJWT]", "", "/tokenAny", false),
							newTestCase(a[0], c, "[NoJWT]", "", "/somePath", true),

							// Test condition "request.auth.principal" on path "/valid-jwt".
							newTestCase(a[0], dst, "[NoJWT]", "", "/valid-jwt", false),
							newTestCase(a[0], dst, "[Token1]", jwt.TokenIssuer1, "/valid-jwt", true),
							newTestCase(a[0], dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/valid-jwt", true),
							newTestCase(a[0], dst, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/valid-jwt", true),

							// Test condition "request.auth.presenter" on suffix "/presenter".
							newTestCase(a[0], dst, "[Token1]", jwt.TokenIssuer1, "/request/presenter", false),
							newTestCase(a[0], dst, "[Token1WithAud]", jwt.TokenIssuer1, "/request/presenter", false),
							newTestCase(a[0], dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/presenter-x", false),
							newTestCase(a[0], dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/presenter", true),

							// Test condition "request.auth.audiences" on suffix "/audiences".
							newTestCase(a[0], dst, "[Token1]", jwt.TokenIssuer1, "/request/audiences", false),
							newTestCase(a[0], dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/audiences", false),
							newTestCase(a[0], dst, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/request/audiences-x", false),
							newTestCase(a[0], dst, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/request/audiences", true),
						}
						for _, c := range cases {
							c(t)
						}
					})
				}
			}
		})
}

// TestAuthorization_WorkloadSelector tests the workload selector for the v1beta1 policy in two namespaces.
func TestAuthorization_WorkloadSelector(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.workload-selector").
		Run(func(t framework.TestContext) {
			bInNS1 := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			vmInNS1 := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			cInNS1 := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			cInNS2 := match.Namespace(apps.Namespace2.Name()).GetMatches(apps.C)
			ns1 := apps.Namespace1
			ns2 := apps.Namespace2
			rootns := newRootNS(t)

			newTestCase := func(from echo.Instance, to echo.Target, namePrefix, path string,
				expectAllowed bool) func(t framework.TestContext) {
				callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
				return func(t framework.TestContext) {
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: "http",
						},
						HTTP: echo.HTTP{
							Path: path,
						},
						Count: callCount,
					}
					if expectAllowed {
						opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
					} else {
						opts.Check = scheck.RBACFailure(&opts)
					}

					name := newRbacTestName(namePrefix, expectAllowed, from, &opts)
					t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
						name.SkipIfNecessary(t)
						from.CallOrFail(t, opts)
					})
				}
			}

			for _, srcCluster := range t.Clusters() {
				a := match.And(match.Cluster(srcCluster), match.Namespace(apps.Namespace1.Name())).GetMatches(apps.A)
				if len(a) == 0 {
					continue
				}

				t.NewSubTestf("From %s", srcCluster.StableName()).Run(func(t framework.TestContext) {
					applyPolicy := func(filename string, ns namespace.Instance) {
						t.ConfigIstio().EvalFile(ns.Name(), map[string]string{
							"Namespace1":    ns1.Name(),
							"Namespace2":    ns2.Name(),
							"RootNamespace": rootns.Name(),
							"b":             util.BSvc,
							"c":             util.CSvc,
						}, filename).ApplyOrFail(t, resource.Wait)
					}
					applyPolicy("testdata/authz/v1beta1-workload-ns1.yaml.tmpl", ns1)
					applyPolicy("testdata/authz/v1beta1-workload-ns2.yaml.tmpl", ns2)
					applyPolicy("testdata/authz/v1beta1-workload-ns-root.yaml.tmpl", rootns)

					cases := []func(test framework.TestContext){
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns1-b", true),
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns1-vm", false),
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns1-c", false),
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns1-x", false),
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns1-all", true),
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns2-c", false),
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns2-all", false),
						newTestCase(a[0], bInNS1, "[bInNS1]", "/policy-ns-root-c", false),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns1-b", false),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns1-vm", false),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns1-c", true),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns1-x", false),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns1-all", true),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns2-c", false),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns2-all", false),
						newTestCase(a[0], cInNS1, "[cInNS1]", "/policy-ns-root-c", true),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns1-b", false),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns1-vm", false),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns1-c", false),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns1-x", false),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns1-all", false),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns2-c", true),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns2-all", true),
						newTestCase(a[0], cInNS2, "[cInNS2]", "/policy-ns-root-c", true),
					}
					for _, c := range cases {
						c(t)
					}
				})

				// TODO(JimmyCYJ): Support multiple VMs in different namespaces for workload selector test and set c to service on VM.
				t.NewSubTestf("VM From %s", srcCluster.StableName()).Run(func(t framework.TestContext) {
					applyPolicy := func(filename string, ns namespace.Instance) {
						t.ConfigIstio().EvalFile(ns.Name(), map[string]string{
							"Namespace1":    ns1.Name(),
							"Namespace2":    ns2.Name(),
							"RootNamespace": rootns.Name(),
							"b":             util.VMSvc, // This is the only difference from standard args.
							"c":             util.CSvc,
						}, filename).ApplyOrFail(t, resource.Wait)
					}
					applyPolicy("testdata/authz/v1beta1-workload-ns1.yaml.tmpl", ns1)
					applyPolicy("testdata/authz/v1beta1-workload-ns2.yaml.tmpl", ns2)
					applyPolicy("testdata/authz/v1beta1-workload-ns-root.yaml.tmpl", rootns)

					cases := []func(test framework.TestContext){
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns1-b", false),
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns1-vm", true),
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns1-c", false),
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns1-x", false),
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns1-all", true),
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns2-b", false),
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns2-all", false),
						newTestCase(a[0], vmInNS1, "[vmInNS1]", "/policy-ns-root-c", false),
					}
					for _, c := range cases {
						c(t)
					}
				})
			}
		})
}

// TestAuthorization_Deny tests the authorization policy with action "DENY".
func TestAuthorization_Deny(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.deny-action").
		Run(func(t framework.TestContext) {
			if t.Clusters().IsMulticluster() {
				t.Skip("https://github.com/istio/istio/issues/37307")
			}
			ns := apps.Namespace1
			rootns := newRootNS(t)
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)

			applyPolicy := func(filename string, ns namespace.Instance) {
				t.ConfigIstio().EvalFile(ns.Name(), map[string]string{
					"Namespace":     ns.Name(),
					"RootNamespace": rootns.Name(),
					"b":             b[0].Config().Service,
					"c":             c[0].Config().Service,
					"vm":            vm[0].Config().Service,
				}, filename).ApplyOrFail(t, resource.Wait)
			}
			applyPolicy("testdata/authz/v1beta1-deny.yaml.tmpl", ns)
			applyPolicy("testdata/authz/v1beta1-deny-ns-root.yaml.tmpl", rootns)
			for _, srcCluster := range t.Clusters() {
				a := match.And(match.Cluster(srcCluster), match.Namespace(apps.Namespace1.Name())).GetMatches(apps.A)
				if len(a) == 0 {
					continue
				}

				t.NewSubTestf("From %s", srcCluster.StableName()).Run(func(t framework.TestContext) {
					newTestCase := func(from echo.Instance, to echo.Target, path string, expectAllowed bool) func(t framework.TestContext) {
						callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
						return func(t framework.TestContext) {
							opts := echo.CallOptions{
								To: to,
								Port: echo.Port{
									Name: "http",
								},
								HTTP: echo.HTTP{
									Path: path,
								},
								Count: callCount,
							}
							if expectAllowed {
								opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
							} else {
								opts.Check = scheck.RBACFailure(&opts)
							}

							name := newRbacTestName("", expectAllowed, from, &opts)
							t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
								name.SkipIfNecessary(t)
								from.CallOrFail(t, opts)
							})
						}
					}
					cases := []func(t framework.TestContext){
						newTestCase(a[0], b, "/deny", false),
						newTestCase(a[0], b, "/deny?param=value", false),
						newTestCase(a[0], b, "/global-deny", false),
						newTestCase(a[0], b, "/global-deny?param=value", false),
						newTestCase(a[0], b, "/other", true),
						newTestCase(a[0], b, "/other?param=value", true),
						newTestCase(a[0], b, "/allow", true),
						newTestCase(a[0], b, "/allow?param=value", true),
						newTestCase(a[0], c, "/allow/admin", false),
						newTestCase(a[0], c, "/allow/admin?param=value", false),
						newTestCase(a[0], c, "/global-deny", false),
						newTestCase(a[0], c, "/global-deny?param=value", false),
						newTestCase(a[0], c, "/other", false),
						newTestCase(a[0], c, "/other?param=value", false),
						newTestCase(a[0], c, "/allow", true),
						newTestCase(a[0], c, "/allow?param=value", true),

						// TODO(JimmyCYJ): support multiple VMs and test deny policies on multiple VMs.
						newTestCase(a[0], vm, "/allow/admin", false),
						newTestCase(a[0], vm, "/allow/admin?param=value", false),
						newTestCase(a[0], vm, "/global-deny", false),
						newTestCase(a[0], vm, "/global-deny?param=value", false),
						newTestCase(a[0], vm, "/other", false),
						newTestCase(a[0], vm, "/other?param=value", false),
						newTestCase(a[0], vm, "/allow", true),
						newTestCase(a[0], vm, "/allow?param=value", true),
					}

					for _, c := range cases {
						c(t)
					}
				})
			}
		})
}

// TestAuthorization_NegativeOperation tests the authorization policy with negative match.
func TestAuthorization_NegativeOperation(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			d := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.D)
			t.ConfigIstio().EvalFile("", map[string]string{
				"Namespace": ns.Name(),
				"b":         b[0].Config().Service,
				"c":         c[0].Config().Service,
				"d":         d[0].Config().Service,
			}, "testdata/authz/v1beta1-operation.yaml.tmpl").ApplyOrFail(t)

			for _, srcCluster := range t.Clusters() {
				a := match.And(match.Cluster(srcCluster), match.Namespace(apps.Namespace1.Name())).GetMatches(apps.A)
				if len(a) == 0 {
					continue
				}
				t.NewSubTestf("From %s", srcCluster.StableName()).Run(func(t framework.TestContext) {
					newTestCase := func(from echo.Instance, to echo.Target, expectAllowed bool, method string,
						address string, port string, s scheme.Instance) func(t framework.TestContext) {
						callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
						return func(t framework.TestContext) {
							opts := echo.CallOptions{
								To: to,
								Port: echo.Port{
									Name: port,
								},
								HTTP: echo.HTTP{
									Method:  method,
									Headers: headers.New().WithHost(address).Build(),
								},
								Count:  callCount,
								Scheme: s,
							}
							if expectAllowed {
								opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
							} else {
								opts.Check = scheck.RBACFailure(&opts)
							}

							name := newRbacTestName("", expectAllowed, from, &opts)
							t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
								name.SkipIfNecessary(t)
								from.CallOrFail(t, opts)
							})
						}
					}

					// a, b, c and d are in the same namespace and another b(bInNs2) is in a different namespace.
					// a connects to b, c and d in ns1 with mTLS.
					// bInNs2 connects to b and c with mTLS, to d with plain-text.
					cases := []func(testContext framework.TestContext){
						// request to workload b will be deined only for method "GET"
						newTestCase(a[0], b, false, "GET", "", "http", scheme.HTTP),
						newTestCase(a[0], b, true, "PUT", "", "http", scheme.HTTP),
						// request to c workload will be deined only for host "deny.com"
						newTestCase(a[0], c, true, "GET", "", "http", scheme.HTTP),
						newTestCase(a[0], c, false, "GET", "deny.com", "http", scheme.HTTP),
						// request to d workload will be deined only for port 8091
						newTestCase(a[0], d, false, "GET", "", "http-8091", scheme.HTTP),
						newTestCase(a[0], d, true, "GET", "", "http-8092", scheme.HTTP),
					}

					for _, c := range cases {
						c(t)
					}
				})
			}
		})
}

// TestAuthorization_NegativeMatch tests the authorization policy with negative match.
func TestAuthorization_NegativeMatch(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			ns2 := apps.Namespace2
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			d := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.D)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			t.ConfigIstio().EvalFile("", map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
				"b":          b[0].Config().Service,
				"c":          c[0].Config().Service,
				"d":          d[0].Config().Service,
				"vm":         vm[0].Config().Service,
			}, "testdata/authz/v1beta1-negative-match.yaml.tmpl").ApplyOrFail(t)

			for _, srcCluster := range t.Clusters() {
				a := match.And(match.Cluster(srcCluster), match.Namespace(apps.Namespace1.Name())).GetMatches(apps.A)
				bInNS2 := match.And(match.Cluster(srcCluster), match.Namespace(apps.Namespace2.Name())).GetMatches(apps.B)
				if len(a) == 0 || len(bInNS2) == 0 {
					continue
				}

				t.NewSubTestf("From %s", srcCluster.StableName()).Run(func(t framework.TestContext) {
					newTestCase := func(from echo.Instance, to echo.Target, path string, expectAllowed bool) func(t framework.TestContext) {
						callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
						return func(t framework.TestContext) {
							opts := echo.CallOptions{
								To: to,
								Port: echo.Port{
									Name: "http",
								},
								HTTP: echo.HTTP{
									Path: path,
								},
								Count: callCount,
							}
							if expectAllowed {
								opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
							} else {
								opts.Check = scheck.RBACFailure(&opts)
							}

							name := newRbacTestName("", expectAllowed, from, &opts)
							t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
								name.SkipIfNecessary(t)
								from.CallOrFail(t, opts)
							})
						}
					}

					// a, b, c and d are in the same namespace and another b(bInNs2) is in a different namespace.
					// a connects to b, c and d in ns1 with mTLS.
					// bInNs2 connects to b and c with mTLS, to d with plain-text.
					cases := []func(testContext framework.TestContext){
						// Test the policy with overlapped `paths` and `not_paths` on b.
						// a and bInNs2 should have the same results:
						// - path with prefix `/prefix` should be denied explicitly.
						// - path `/prefix/allowlist` should be excluded from the deny.
						// - path `/allow` should be allowed implicitly.
						newTestCase(a[0], b, "/prefix", false),
						newTestCase(a[0], b, "/prefix/other", false),
						newTestCase(a[0], b, "/prefix/allowlist", true),
						newTestCase(a[0], b, "/allow", true),
						newTestCase(bInNS2[0], b, "/prefix", false),
						newTestCase(bInNS2[0], b, "/prefix/other", false),
						newTestCase(bInNS2[0], b, "/prefix/allowlist", true),
						newTestCase(bInNS2[0], b, "/allow", true),

						// Test the policy that denies other namespace on c.
						// a should be allowed because it's from the same namespace.
						// bInNs2 should be denied because it's from a different namespace.
						newTestCase(a[0], c, "/", true),
						newTestCase(bInNS2[0], c, "/", false),

						// Test the policy that denies plain-text traffic on d.
						// a should be allowed because it's using mTLS.
						// bInNs2 should be denied because it's using plain-text.
						newTestCase(a[0], d, "/", true),
						newTestCase(bInNS2[0], d, "/", false),

						// Test the policy with overlapped `paths` and `not_paths` on vm.
						// a and bInNs2 should have the same results:
						// - path with prefix `/prefix` should be denied explicitly.
						// - path `/prefix/allowlist` should be excluded from the deny.
						// - path `/allow` should be allowed implicitly.
						// TODO(JimmyCYJ): support multiple VMs and test negative match on multiple VMs.
						newTestCase(a[0], vm, "/prefix", false),
						newTestCase(a[0], vm, "/prefix/other", false),
						newTestCase(a[0], vm, "/prefix/allowlist", true),
						newTestCase(a[0], vm, "/allow", true),
						newTestCase(bInNS2[0], vm, "/prefix", false),
						newTestCase(bInNS2[0], vm, "/prefix/other", false),
						newTestCase(bInNS2[0], vm, "/prefix/allowlist", true),
						newTestCase(bInNS2[0], vm, "/allow", true),
					}

					for _, c := range cases {
						c(t)
					}
				})
			}
		})
}

// TestAuthorization_IngressGateway tests the authorization policy on ingress gateway.
func TestAuthorization_IngressGateway(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.ingress-gateway").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			rootns := newRootNS(t)
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			// Gateways on VMs are not supported yet. This test verifies that security
			// policies at gateways are useful for managing accessibility to services
			// running on a VM.
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			for _, dst := range []echo.Instances{b, vm} {
				t.NewSubTestf("to %s/", dst[0].Config().Service).Run(func(t framework.TestContext) {
					t.ConfigIstio().EvalFile("", map[string]string{
						"Namespace":     ns.Name(),
						"RootNamespace": rootns.Name(),
						"dst":           dst[0].Config().Service,
					}, "testdata/authz/v1beta1-ingress-gateway.yaml.tmpl").ApplyOrFail(t)

					ingr := ist.IngressFor(t.Clusters().Default())

					cases := []struct {
						Name     string
						Host     string
						Path     string
						IP       string
						WantCode int
					}{
						{
							Name:     "case-insensitive-deny deny.company.com",
							Host:     "deny.company.com",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny DENY.COMPANY.COM",
							Host:     "DENY.COMPANY.COM",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny Deny.Company.Com",
							Host:     "Deny.Company.Com",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny deny.suffix.company.com",
							Host:     "deny.suffix.company.com",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny DENY.SUFFIX.COMPANY.COM",
							Host:     "DENY.SUFFIX.COMPANY.COM",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny Deny.Suffix.Company.Com",
							Host:     "Deny.Suffix.Company.Com",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny prefix.company.com",
							Host:     "prefix.company.com",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny PREFIX.COMPANY.COM",
							Host:     "PREFIX.COMPANY.COM",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "case-insensitive-deny Prefix.Company.Com",
							Host:     "Prefix.Company.Com",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "allow www.company.com",
							Host:     "www.company.com",
							Path:     "/",
							IP:       "172.16.0.1",
							WantCode: http.StatusOK,
						},
						{
							Name:     "deny www.company.com/private",
							Host:     "www.company.com",
							Path:     "/private",
							IP:       "172.16.0.1",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "allow www.company.com/public",
							Host:     "www.company.com",
							Path:     "/public",
							IP:       "172.16.0.1",
							WantCode: http.StatusOK,
						},
						{
							Name:     "deny internal.company.com",
							Host:     "internal.company.com",
							Path:     "/",
							IP:       "172.16.0.1",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "deny internal.company.com/private",
							Host:     "internal.company.com",
							Path:     "/private",
							IP:       "172.16.0.1",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "deny 172.17.72.46",
							Host:     "remoteipblocks.company.com",
							Path:     "/",
							IP:       "172.17.72.46",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "deny 192.168.5.233",
							Host:     "remoteipblocks.company.com",
							Path:     "/",
							IP:       "192.168.5.233",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "allow 10.4.5.6",
							Host:     "remoteipblocks.company.com",
							Path:     "/",
							IP:       "10.4.5.6",
							WantCode: http.StatusOK,
						},
						{
							Name:     "deny 10.2.3.4",
							Host:     "notremoteipblocks.company.com",
							Path:     "/",
							IP:       "10.2.3.4",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "allow 172.23.242.188",
							Host:     "notremoteipblocks.company.com",
							Path:     "/",
							IP:       "172.23.242.188",
							WantCode: http.StatusOK,
						},
						{
							Name:     "deny 10.242.5.7",
							Host:     "remoteipattr.company.com",
							Path:     "/",
							IP:       "10.242.5.7",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "deny 10.124.99.10",
							Host:     "remoteipattr.company.com",
							Path:     "/",
							IP:       "10.124.99.10",
							WantCode: http.StatusForbidden,
						},
						{
							Name:     "allow 10.4.5.6",
							Host:     "remoteipattr.company.com",
							Path:     "/",
							IP:       "10.4.5.6",
							WantCode: http.StatusOK,
						},
						{
							Name:     "allow 172.19.19.19",
							Host:     "ipblocks.company.com",
							Path:     "/",
							IP:       "172.19.19.19",
							WantCode: 200,
						},
						{
							Name:     "deny 172.19.19.20",
							Host:     "notipblocks.company.com",
							Path:     "/",
							IP:       "172.19.19.20",
							WantCode: 403,
						},
					}

					for _, tc := range cases {
						t.NewSubTest(tc.Name).Run(func(t framework.TestContext) {
							opts := echo.CallOptions{
								Port: echo.Port{
									Protocol: protocol.HTTP,
								},
								HTTP: echo.HTTP{
									Path:    tc.Path,
									Headers: headers.New().WithHost(tc.Host).WithXForwardedFor(tc.IP).Build(),
								},
								Check: check.Status(tc.WantCode),
							}
							ingr.CallOrFail(t, opts)
						})
					}
				})
			}
		})
}

// TestAuthorization_EgressGateway tests v1beta1 authorization on egress gateway.
func TestAuthorization_EgressGateway(t *testing.T) {
	framework.NewTest(t).
		Label(label.IPv4). // https://github.com/istio/istio/issues/35835
		Features("security.authorization.egress-gateway").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			rootns := newRootNS(t)
			a := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.A)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			// Gateways on VMs are not supported yet. This test verifies that security
			// policies at gateways are useful for managing accessibility to external
			// services running on a VM.
			for _, a := range []echo.Instances{a, vm} {
				t.NewSubTestf("to %s/", a[0].Config().Service).Run(func(t framework.TestContext) {
					t.ConfigIstio().EvalFile("", map[string]string{
						"Namespace":     ns.Name(),
						"RootNamespace": rootns.Name(),
						"a":             a[0].Config().Service,
					}, "testdata/authz/v1beta1-egress-gateway.yaml.tmpl").ApplyOrFail(t)

					cases := []struct {
						name  string
						path  string
						code  int
						body  string
						host  string
						from  echo.Workload
						token string
					}{
						{
							name: "allow path to company.com",
							path: "/allow",
							code: http.StatusOK,
							body: "handled-by-egress-gateway",
							host: "www.company.com",
							from: a.WorkloadsOrFail(t)[0],
						},
						{
							name: "deny path to company.com",
							path: "/deny",
							code: http.StatusForbidden,
							body: "RBAC: access denied",
							host: "www.company.com",
							from: a.WorkloadsOrFail(t)[0],
						},
						{
							name: "allow service account a to a-only.com over mTLS",
							path: "/",
							code: http.StatusOK,
							body: "handled-by-egress-gateway",
							host: fmt.Sprintf("%s-only.com", a[0].Config().Service),
							from: a.WorkloadsOrFail(t)[0],
						},
						{
							name: "deny service account b to a-only.com over mTLS",
							path: "/",
							code: http.StatusForbidden,
							body: "RBAC: access denied",
							host: fmt.Sprintf("%s-only.com", a[0].Config().Service),
							from: c.WorkloadsOrFail(t)[0],
						},
						{
							name:  "allow a with JWT to jwt-only.com over mTLS",
							path:  "/",
							code:  http.StatusOK,
							body:  "handled-by-egress-gateway",
							host:  "jwt-only.com",
							from:  a.WorkloadsOrFail(t)[0],
							token: jwt.TokenIssuer1,
						},
						{
							name:  "allow b with JWT to jwt-only.com over mTLS",
							path:  "/",
							code:  http.StatusOK,
							body:  "handled-by-egress-gateway",
							host:  "jwt-only.com",
							from:  c.WorkloadsOrFail(t)[0],
							token: jwt.TokenIssuer1,
						},
						{
							name:  "deny b with wrong JWT to jwt-only.com over mTLS",
							path:  "/",
							code:  http.StatusForbidden,
							body:  "RBAC: access denied",
							host:  "jwt-only.com",
							from:  c.WorkloadsOrFail(t)[0],
							token: jwt.TokenIssuer2,
						},
						{
							name:  "allow service account a with JWT to jwt-and-a-only.com over mTLS",
							path:  "/",
							code:  http.StatusOK,
							body:  "handled-by-egress-gateway",
							host:  fmt.Sprintf("jwt-and-%s-only.com", a[0].Config().Service),
							from:  a.WorkloadsOrFail(t)[0],
							token: jwt.TokenIssuer1,
						},
						{
							name:  "deny service account c with JWT to jwt-and-a-only.com over mTLS",
							path:  "/",
							code:  http.StatusForbidden,
							body:  "RBAC: access denied",
							host:  fmt.Sprintf("jwt-and-%s-only.com", a[0].Config().Service),
							from:  c.WorkloadsOrFail(t)[0],
							token: jwt.TokenIssuer1,
						},
						{
							name:  "deny service account a with wrong JWT to jwt-and-a-only.com over mTLS",
							path:  "/",
							code:  http.StatusForbidden,
							body:  "RBAC: access denied",
							host:  fmt.Sprintf("jwt-and-%s-only.com", a[0].Config().Service),
							from:  a.WorkloadsOrFail(t)[0],
							token: jwt.TokenIssuer2,
						},
					}

					for _, tc := range cases {
						request := &epb.ForwardEchoRequest{
							// Use a fake IP to make sure the request is handled by our test.
							Url:     fmt.Sprintf("http://10.4.4.4%s", tc.path),
							Count:   1,
							Headers: echoCommon.HTTPToProtoHeaders(headers.New().WithHost(tc.host).WithAuthz(tc.token).Build()),
						}
						t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
							retry.UntilSuccessOrFail(t, func() error {
								rs, err := tc.from.ForwardEcho(context.TODO(), request)
								if err != nil {
									return err
								}
								return check.And(
									check.NoError(),
									check.Status(tc.code),
									check.Each(func(r echoClient.Response) error {
										if !strings.Contains(r.RawContent, tc.body) {
											return fmt.Errorf("want %q in body but not found: %s", tc.body, r.RawContent)
										}
										return nil
									})).Check(rs, err)
							}, echo.DefaultCallRetryOptions()...)
						})
					}
				})
			}
		})
}

// TestAuthorization_TCP tests the authorization policy on workloads using the raw TCP protocol.
func TestAuthorization_TCP(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.tcp").
		Run(func(t framework.TestContext) {
			newTestCase := func(from echo.Instance, to echo.Target, s scheme.Instance, portName string, expectAllowed bool) func(t framework.TestContext) {
				return func(t framework.TestContext) {
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: portName,
						},
						Scheme: s,
						HTTP: echo.HTTP{
							Path: "/data",
						},
					}
					if expectAllowed {
						opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
					} else {
						opts.Check = scheck.RBACFailure(&opts)
					}

					name := newRbacTestName("", expectAllowed, from, &opts)
					t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
						name.SkipIfNecessary(t)
						from.CallOrFail(t, opts)
					})
				}
			}

			ns := apps.Namespace1
			ns2 := apps.Namespace2
			a := match.Namespace(ns.Name()).GetMatches(apps.A)
			b := match.Namespace(ns.Name()).GetMatches(apps.B)
			c := match.Namespace(ns.Name()).GetMatches(apps.C)
			eInNS2 := match.Namespace(ns2.Name()).GetMatches(apps.E)
			d := match.Namespace(ns.Name()).GetMatches(apps.D)
			e := match.Namespace(ns.Name()).GetMatches(apps.E)
			t.NewSubTest("non-vms").
				Run(func(t framework.TestContext) {
					t.ConfigIstio().EvalFile("", map[string]string{
						"Namespace":  ns.Name(),
						"Namespace2": ns2.Name(),
						"b":          b[0].Config().Service,
						"c":          c[0].Config().Service,
						"d":          d[0].Config().Service,
						"e":          e[0].Config().Service,
						"a":          a[0].Config().Service,
					}, "testdata/authz/v1beta1-tcp.yaml.tmpl").ApplyOrFail(t)

					cases := []func(testContext framework.TestContext){
						// The policy on workload b denies request with path "/data" to port 8091:
						// - request to port http-8091 should be denied because both path and port are matched.
						// - request to port http-8092 should be allowed because the port is not matched.
						// - request to port tcp-8093 should be allowed because the port is not matched.
						newTestCase(a[0], b, scheme.HTTP, "http-8091", false),
						newTestCase(a[0], b, scheme.HTTP, "http-8092", true),
						newTestCase(a[0], b, scheme.TCP, "tcp-8093", true),

						// The policy on workload c denies request to port 8091:
						// - request to port http-8091 should be denied because the port is matched.
						// - request to http port 8092 should be allowed because the port is not matched.
						// - request to tcp port 8093 should be allowed because the port is not matched.
						// - request from b to tcp port 8093 should be allowed by default.
						// - request from b to tcp port 8094 should be denied because the principal is matched.
						// - request from eInNS2 to tcp port 8093 should be denied because the namespace is matched.
						// - request from eInNS2 to tcp port 8094 should be allowed by default.
						newTestCase(a[0], c, scheme.HTTP, "http-8091", false),
						newTestCase(a[0], c, scheme.HTTP, "http-8092", true),
						newTestCase(a[0], c, scheme.TCP, "tcp-8093", true),
						newTestCase(b[0], c, scheme.TCP, "tcp-8093", true),
						newTestCase(b[0], c, scheme.TCP, "tcp-8094", false),
						newTestCase(eInNS2[0], c, scheme.TCP, "tcp-8093", false),
						newTestCase(eInNS2[0], c, scheme.TCP, "tcp-8094", true),

						// The policy on workload d denies request from service account a and workloads in namespace 2:
						// - request from a to d should be denied because it has service account a.
						// - request from b to d should be allowed.
						// - request from c to d should be allowed.
						// - request from eInNS2 to a should be allowed because there is no policy on a.
						// - request from eInNS2 to d should be denied because it's in namespace 2.
						newTestCase(a[0], d, scheme.TCP, "tcp-8093", false),
						newTestCase(b[0], d, scheme.TCP, "tcp-8093", true),
						newTestCase(c[0], d, scheme.TCP, "tcp-8093", true),
						newTestCase(eInNS2[0], a, scheme.TCP, "tcp-8093", true),
						newTestCase(eInNS2[0], d, scheme.TCP, "tcp-8093", false),

						// The policy on workload e denies request with path "/other":
						// - request to port http-8091 should be allowed because the path is not matched.
						// - request to port http-8092 should be allowed because the path is not matched.
						// - request to port tcp-8093 should be denied because policy uses HTTP fields.
						newTestCase(a[0], e, scheme.HTTP, "http-8091", true),
						newTestCase(a[0], e, scheme.HTTP, "http-8092", true),
						newTestCase(a[0], e, scheme.TCP, "tcp-8093", false),
					}

					for _, c := range cases {
						c(t)
					}
				})
			// TODO(JimmyCYJ): support multiple VMs and apply different security policies to each VM.
			vm := match.Namespace(ns.Name()).GetMatches(apps.VM)
			t.NewSubTest("vms").
				Run(func(t framework.TestContext) {
					t.ConfigIstio().EvalFile("", map[string]string{
						"Namespace":  ns.Name(),
						"Namespace2": ns2.Name(),
						"b":          b[0].Config().Service,
						"c":          vm[0].Config().Service,
						"d":          d[0].Config().Service,
						"e":          e[0].Config().Service,
						"a":          a[0].Config().Service,
					}, "testdata/authz/v1beta1-tcp.yaml.tmpl").ApplyOrFail(t)
					cases := []func(testContext framework.TestContext){
						// The policy on workload vm denies request to port 8091:
						// - request to port http-8091 should be denied because the port is matched.
						// - request to http port 8092 should be allowed because the port is not matched.
						// - request to tcp port 8093 should be allowed because the port is not matched.
						// - request from b to tcp port 8093 should be allowed by default.
						// - request from b to tcp port 8094 should be denied because the principal is matched.
						// - request from eInNS2 to tcp port 8093 should be denied because the namespace is matched.
						// - request from eInNS2 to tcp port 8094 should be allowed by default.
						newTestCase(a[0], vm, scheme.HTTP, "http-8091", false),
						newTestCase(a[0], vm, scheme.HTTP, "http-8092", true),
						newTestCase(a[0], vm, scheme.TCP, "tcp-8093", true),
						newTestCase(b[0], vm, scheme.TCP, "tcp-8093", true),
						newTestCase(b[0], vm, scheme.TCP, "tcp-8094", false),
						newTestCase(eInNS2[0], vm, scheme.TCP, "tcp-8093", false),
						newTestCase(eInNS2[0], vm, scheme.TCP, "tcp-8094", true),
					}
					for _, c := range cases {
						c(t)
					}
				})
		})
}

// TestAuthorization_Conditions tests v1beta1 authorization with conditions.
func TestAuthorization_Conditions(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.conditions").
		Run(func(t framework.TestContext) {
			nsA := apps.Namespace1
			nsB := apps.Namespace2
			nsC := apps.Namespace3

			c := match.Namespace(nsC.Name()).GetMatches(apps.C)
			vm := match.Namespace(nsA.Name()).GetMatches(apps.VM)
			for _, to := range []echo.Instances{c, vm} {
				to := to
				for _, a := range match.Namespace(nsA.Name()).GetMatches(apps.A) {
					a := a
					bs := match.And(match.Cluster(a.Config().Cluster), match.Namespace(nsB.Name())).GetMatches(apps.B)
					if len(bs) < 1 {
						t.Skip()
					}
					b := bs[0]
					t.NewSubTestf("from %s to %s in %s",
						a.Config().Cluster.StableName(), to.Config().Service, to.Config().Cluster.StableName()).
						Run(func(t framework.TestContext) {
							addresses := func(to echo.Target) string {
								var out []string
								for _, w := range to.WorkloadsOrFail(t) {
									out = append(out, "\""+w.Address()+"\"")
								}
								return strings.Join(out, ",")
							}

							args := map[string]string{
								"NamespaceA": nsA.Name(),
								"NamespaceB": nsB.Name(),
								"NamespaceC": to.Config().Namespace.Name(),
								"cSet":       to.Config().Service,
								"ipA":        addresses(a),
								"ipB":        addresses(b),
								"ipC":        addresses(to),
								"portC":      "8090",
								"a":          util.ASvc,
								"b":          util.BSvc,
							}

							t.ConfigIstio().EvalFile("", args, "testdata/authz/v1beta1-conditions.yaml.tmpl").ApplyOrFail(t)
							callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
							newTestCase := func(from echo.Instance, path string, headers http.Header, expectAllowed bool) func(t framework.TestContext) {
								return func(t framework.TestContext) {
									opts := echo.CallOptions{
										To: to,
										Port: echo.Port{
											Name: "http",
										},
										HTTP: echo.HTTP{
											Path:    path,
											Headers: headers,
										},
										Count: callCount,
									}
									if expectAllowed {
										opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
									} else {
										opts.Check = scheck.RBACFailure(&opts)
									}

									name := newRbacTestName("", expectAllowed, from, &opts)
									t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
										name.SkipIfNecessary(t)
										from.CallOrFail(t, opts)
									})
								}
							}

							cases := []func(framework.TestContext){
								newTestCase(a, "/request-headers", headers.New().With("x-foo", "foo").Build(), true),
								newTestCase(b, "/request-headers", headers.New().With("x-foo", "foo").Build(), true),
								newTestCase(a, "/request-headers", headers.New().With("x-foo", "bar").Build(), false),
								newTestCase(b, "/request-headers", headers.New().With("x-foo", "bar").Build(), false),
								newTestCase(a, "/request-headers", nil, false),
								newTestCase(b, "/request-headers", nil, false),
								newTestCase(a, "/request-headers-notValues-bar", headers.New().With("x-foo", "foo").Build(), true),
								newTestCase(a, "/request-headers-notValues-bar", headers.New().With("x-foo", "bar").Build(), false),

								newTestCase(a, fmt.Sprintf("/source-ip-%s", args["a"]), nil, true),
								newTestCase(b, fmt.Sprintf("/source-ip-%s", args["a"]), nil, false),
								newTestCase(a, fmt.Sprintf("/source-ip-%s", args["b"]), nil, false),
								newTestCase(b, fmt.Sprintf("/source-ip-%s", args["b"]), nil, true),
								newTestCase(a, fmt.Sprintf("/source-ip-notValues-%s", args["b"]), nil, true),
								newTestCase(b, fmt.Sprintf("/source-ip-notValues-%s", args["b"]), nil, false),

								newTestCase(a, fmt.Sprintf("/source-namespace-%s", args["a"]), nil, true),
								newTestCase(b, fmt.Sprintf("/source-namespace-%s", args["a"]), nil, false),
								newTestCase(a, fmt.Sprintf("/source-namespace-%s", args["b"]), nil, false),
								newTestCase(b, fmt.Sprintf("/source-namespace-%s", args["b"]), nil, true),
								newTestCase(a, fmt.Sprintf("/source-namespace-notValues-%s", args["b"]), nil, true),
								newTestCase(b, fmt.Sprintf("/source-namespace-notValues-%s", args["b"]), nil, false),

								newTestCase(a, fmt.Sprintf("/source-principal-%s", args["a"]), nil, true),
								newTestCase(b, fmt.Sprintf("/source-principal-%s", args["a"]), nil, false),
								newTestCase(a, fmt.Sprintf("/source-principal-%s", args["b"]), nil, false),
								newTestCase(b, fmt.Sprintf("/source-principal-%s", args["b"]), nil, true),
								newTestCase(a, fmt.Sprintf("/source-principal-notValues-%s", args["b"]), nil, true),
								newTestCase(b, fmt.Sprintf("/source-principal-notValues-%s", args["b"]), nil, false),

								newTestCase(a, "/destination-ip-good", nil, true),
								newTestCase(b, "/destination-ip-good", nil, true),
								newTestCase(a, "/destination-ip-bad", nil, false),
								newTestCase(b, "/destination-ip-bad", nil, false),
								newTestCase(a, fmt.Sprintf("/destination-ip-notValues-%s-or-%s", args["a"], args["b"]), nil, true),
								newTestCase(a, fmt.Sprintf("/destination-ip-notValues-%s-or-%s-or-%s", args["a"], args["b"], args["cSet"]), nil, false),

								newTestCase(a, "/destination-port-good", nil, true),
								newTestCase(b, "/destination-port-good", nil, true),
								newTestCase(a, "/destination-port-bad", nil, false),
								newTestCase(b, "/destination-port-bad", nil, false),
								newTestCase(a, fmt.Sprintf("/destination-port-notValues-%s", args["cSet"]), nil, false),
								newTestCase(b, fmt.Sprintf("/destination-port-notValues-%s", args["cSet"]), nil, false),

								newTestCase(a, "/connection-sni-good", nil, true),
								newTestCase(b, "/connection-sni-good", nil, true),
								newTestCase(a, "/connection-sni-bad", nil, false),
								newTestCase(b, "/connection-sni-bad", nil, false),
								newTestCase(a, fmt.Sprintf("/connection-sni-notValues-%s-or-%s", args["a"], args["b"]), nil, true),
								newTestCase(a, fmt.Sprintf("/connection-sni-notValues-%s-or-%s-or-%s", args["a"], args["b"], args["cSet"]), nil, false),

								newTestCase(a, "/other", nil, false),
								newTestCase(b, "/other", nil, false),
							}
							for _, c := range cases {
								c(t)
							}
						})
				}
			}
		})
}

// TestAuthorization_GRPC tests v1beta1 authorization with gRPC protocol.
func TestAuthorization_GRPC(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.grpc-protocol").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			a := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.A)
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			d := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.D)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			for _, a := range []echo.Instances{a, vm} {
				for _, b := range []echo.Instances{b, vm} {
					if a[0].Config().Service == b[0].Config().Service {
						t.Skip()
					}
					t.NewSubTestf("to %s in %s", a[0].Config().Service, a[0].Config().Cluster.StableName()).
						Run(func(t framework.TestContext) {
							args := map[string]string{
								"Namespace": ns.Name(),
								"a":         a[0].Config().Service,
								"b":         b[0].Config().Service,
								"c":         c[0].Config().Service,
								"d":         d[0].Config().Service,
							}
							t.ConfigIstio().EvalFile(ns.Name(), args, "testdata/authz/v1beta1-grpc.yaml.tmpl").ApplyOrFail(t, resource.Wait)
							newTestCase := func(from echo.Instance, to echo.Target, expectAllowed bool) func(t framework.TestContext) {
								return func(t framework.TestContext) {
									opts := echo.CallOptions{
										To: to,
										Port: echo.Port{
											Name: "grpc",
										},
									}
									if expectAllowed {
										opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
									} else {
										opts.Check = scheck.RBACFailure(&opts)
									}

									name := newRbacTestName("", expectAllowed, from, &opts)
									t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
										name.SkipIfNecessary(t)
										from.CallOrFail(t, opts)
									})
								}
							}
							cases := []func(testContext framework.TestContext){
								newTestCase(b[0], a, true),
								newTestCase(c[0], a, false),
								newTestCase(d[0], a, true),
							}

							for _, c := range cases {
								c(t)
							}
						})
				}
			}
		})
}

// TestAuthorization_Path tests the path is normalized before using in authorization. For example, a request
// with path "/a/../b" should be normalized to "/b" before using in authorization.
func TestAuthorization_Path(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.path-normalization").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			a := match.Namespace(ns.Name()).GetMatches(apps.A)
			vm := match.Namespace(ns.Name()).GetMatches(apps.VM)
			for _, a := range []echo.Instances{a, vm} {
				for _, srcCluster := range t.Clusters() {
					b := match.And(match.Cluster(srcCluster), match.Namespace(ns.Name())).GetMatches(apps.B)
					if len(b) == 0 {
						continue
					}

					t.NewSubTestf("In %s", srcCluster.StableName()).Run(func(t framework.TestContext) {
						args := map[string]string{
							"Namespace": ns.Name(),
							"a":         a[0].Config().Service,
						}
						t.ConfigIstio().EvalFile(ns.Name(), args, "testdata/authz/v1beta1-path.yaml.tmpl").ApplyOrFail(t, resource.Wait)

						newTestCase := func(from echo.Instance, to echo.Target, path string, expectAllowed bool) func(t framework.TestContext) {
							callCount := util.CallsPerCluster * to.WorkloadsOrFail(t).Len()
							return func(t framework.TestContext) {
								opts := echo.CallOptions{
									To: to,
									Port: echo.Port{
										Name: "http",
									},
									HTTP: echo.HTTP{
										Path: path,
									},
									Count: callCount,
								}
								if expectAllowed {
									opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
								} else {
									opts.Check = scheck.RBACFailure(&opts)
								}

								name := newRbacTestName("", expectAllowed, from, &opts)
								t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
									name.SkipIfNecessary(t)
									from.CallOrFail(t, opts)
								})
							}
						}
						cases := []func(test framework.TestContext){
							newTestCase(b[0], a, "/public", true),
							newTestCase(b[0], a, "/public/../public", true),
							newTestCase(b[0], a, "/private", false),
							newTestCase(b[0], a, "/public/../private", false),
							newTestCase(b[0], a, "/public/./../private", false),
							newTestCase(b[0], a, "/public/.././private", false),
							newTestCase(b[0], a, "/public/%2E%2E/private", false),
							newTestCase(b[0], a, "/public/%2e%2e/private", false),
							newTestCase(b[0], a, "/public/%2E/%2E%2E/private", false),
							newTestCase(b[0], a, "/public/%2e/%2e%2e/private", false),
							newTestCase(b[0], a, "/public/%2E%2E/%2E/private", false),
							newTestCase(b[0], a, "/public/%2e%2e/%2e/private", false),
						}
						for _, c := range cases {
							c(t)
						}
					})
				}
			}
		})
}

// TestAuthorization_Audit tests that the AUDIT action does not impact allowing or denying a request
func TestAuthorization_Audit(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			a := match.Namespace(ns.Name()).GetMatches(apps.A)
			b := match.Namespace(ns.Name()).GetMatches(apps.B)
			c := match.Namespace(ns.Name()).GetMatches(apps.C)
			d := match.Namespace(ns.Name()).GetMatches(apps.D)
			vm := match.Namespace(ns.Name()).GetMatches(apps.VM)

			policy := func(filename string) func(t framework.TestContext) {
				return func(t framework.TestContext) {
					t.ConfigIstio().EvalFile(ns.Name(), map[string]string{
						"b":             b[0].Config().Service,
						"c":             c[0].Config().Service,
						"d":             d[0].Config().Service,
						"Namespace":     ns.Name(),
						"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
					}, filename).ApplyOrFail(t, resource.Wait)
				}
			}

			vmPolicy := func(filename string) func(t framework.TestContext) {
				return func(t framework.TestContext) {
					t.ConfigIstio().EvalFile(ns.Name(), map[string]string{
						"Namespace": ns.Name(),
						"dst":       vm[0].Config().Service,
					}, filename).ApplyOrFail(t, resource.Wait)
				}
			}

			newTestCase := func(applyPolicy func(t framework.TestContext), from echo.Instance, to echo.Target,
				path string, expectAllowed bool) func(t framework.TestContext) {
				return func(t framework.TestContext) {
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: "http",
						},
						HTTP: echo.HTTP{
							Path: path,
						},
					}
					if expectAllowed {
						opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
					} else {
						opts.Check = scheck.RBACFailure(&opts)
					}

					name := newRbacTestName("", expectAllowed, from, &opts)
					t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
						name.SkipIfNecessary(t)

						applyPolicy(t)

						from.CallOrFail(t, opts)
					})
				}
			}

			cases := []func(t framework.TestContext){
				newTestCase(policy("testdata/authz/v1beta1-audit.yaml.tmpl"), a[0], b, "/allow", true),
				newTestCase(policy("testdata/authz/v1beta1-audit.yaml.tmpl"), a[0], b, "/audit", false),
				newTestCase(policy("testdata/authz/v1beta1-audit.yaml.tmpl"), a[0], c, "/audit", true),
				newTestCase(policy("testdata/authz/v1beta1-audit.yaml.tmpl"), a[0], c, "/deny", false),
				newTestCase(policy("testdata/authz/v1beta1-audit.yaml.tmpl"), a[0], d, "/audit", true),
				newTestCase(policy("testdata/authz/v1beta1-audit.yaml.tmpl"), a[0], d, "/other", true),

				// (TODO)JimmyCYJ: Support multiple VMs and apply audit policies to multiple VMs for testing.
				// The tests below are duplicated from above for VM workloads. With support for multiple VMs,
				// These tests will be merged to the tests above.
				newTestCase(vmPolicy("testdata/authz/v1beta1-audit-allow.yaml.tmpl"), b[0], vm, "/allow", true),
				newTestCase(vmPolicy("testdata/authz/v1beta1-audit-allow.yaml.tmpl"), b[0], vm, "/audit", false),

				newTestCase(vmPolicy("testdata/authz/v1beta1-audit-deny.yaml.tmpl"), b[0], vm, "/audit", true),
				newTestCase(vmPolicy("testdata/authz/v1beta1-audit-deny.yaml.tmpl"), b[0], vm, "/deny", false),

				newTestCase(vmPolicy("testdata/authz/v1beta1-audit-default.yaml.tmpl"), b[0], vm, "/audit", true),
				newTestCase(vmPolicy("testdata/authz/v1beta1-audit-default.yaml.tmpl"), b[0], vm, "/other", true),
			}
			for _, c := range cases {
				c(t)
			}
		})
}

// TestAuthorization_Custom tests that the CUSTOM action with the sample ext_authz server.
func TestAuthorization_Custom(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.custom").
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "v1beta1-custom",
				Inject: true,
			})
			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
			}

			// Deploy and wait for the ext-authz server to be ready.
			t.ConfigIstio().EvalFile(ns.Name(), args, "../../../samples/extauthz/ext-authz.yaml").ApplyOrFail(t)
			if _, _, err := kube.WaitUntilServiceEndpointsAreReady(t.Clusters().Default(), ns.Name(), "ext-authz"); err != nil {
				t.Fatalf("Wait for ext-authz server failed: %v", err)
			}
			// Update the mesh config extension provider for the ext-authz service.
			extService := fmt.Sprintf("ext-authz.%s.svc.cluster.local", ns.Name())
			extServiceWithNs := fmt.Sprintf("%s/%s", ns.Name(), extService)
			istio.PatchMeshConfig(t, ist.Settings().SystemNamespace, t.Clusters(), fmt.Sprintf(`
extensionProviders:
- name: "ext-authz-http"
  envoyExtAuthzHttp:
    service: %q
    port: 8000
    pathPrefix: "/check"
    headersToUpstreamOnAllow: ["x-ext-authz-*"]
    headersToDownstreamOnDeny: ["x-ext-authz-*"]
    includeRequestHeadersInCheck: ["x-ext-authz"]
    includeAdditionalHeadersInCheck:
      x-ext-authz-additional-header-new: additional-header-new-value
      x-ext-authz-additional-header-override: additional-header-override-value
- name: "ext-authz-grpc"
  envoyExtAuthzGrpc:
    service: %q
    port: 9000
- name: "ext-authz-http-local"
  envoyExtAuthzHttp:
    service: ext-authz-http.local
    port: 8000
    pathPrefix: "/check"
    headersToUpstreamOnAllow: ["x-ext-authz-*"]
    headersToDownstreamOnDeny: ["x-ext-authz-*"]
    includeRequestHeadersInCheck: ["x-ext-authz"]
    includeAdditionalHeadersInCheck:
      x-ext-authz-additional-header-new: additional-header-new-value
      x-ext-authz-additional-header-override: additional-header-override-value
- name: "ext-authz-grpc-local"
  envoyExtAuthzGrpc:
    service: ext-authz-grpc.local
    port: 9000`, extService, extServiceWithNs))

			t.ConfigIstio().EvalFile("", args, "testdata/authz/v1beta1-custom.yaml.tmpl").ApplyOrFail(t)
			ports := []echo.Port{
				{
					Name:         "tcp-8092",
					Protocol:     protocol.TCP,
					WorkloadPort: 8092,
				},
				{
					Name:         "tcp-8093",
					Protocol:     protocol.TCP,
					WorkloadPort: 8093,
				},
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8090,
				},
			}

			var a, b, c, d, e, f, g, x echo.Instance
			echoConfig := func(name string, includeExtAuthz bool) echo.Config {
				cfg := util.EchoConfig(name, ns, false, nil)
				cfg.IncludeExtAuthz = includeExtAuthz
				cfg.Ports = ports
				return cfg
			}
			deployment.New(t).
				With(&a, echoConfig("a", false)).
				With(&b, echoConfig("b", false)).
				With(&c, echoConfig("c", false)).
				With(&d, echoConfig("d", true)).
				With(&e, echoConfig("e", true)).
				With(&f, echoConfig("f", false)).
				With(&g, echoConfig("g", false)).
				With(&x, echoConfig("x", false)).
				BuildOrFail(t)

			newTestCase := func(from echo.Instance, to echo.Target, s scheme.Instance, port, path string, headers http.Header,
				checker check.Checker, expectAllowed bool) func(t framework.TestContext) {
				return func(t framework.TestContext) {
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: port,
						},
						Scheme: s,
						HTTP: echo.HTTP{
							Path:    path,
							Headers: headers,
						},
					}
					if expectAllowed {
						opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
					} else {
						opts.Check = scheck.RBACFailure(&opts)
					}
					opts.Check = check.And(opts.Check, checker)

					name := newRbacTestName("", expectAllowed, from, &opts)
					t.NewSubTest(name.String()).Run(func(t framework.TestContext) {
						name.SkipIfNecessary(t)
						from.CallOrFail(t, opts)
					})
				}
			}
			checkHTTPHeaders := func(hType echoClient.HeaderType) check.Checker {
				return check.And(
					scheck.HeaderContains(hType, map[string][]string{
						"X-Ext-Authz-Check-Received":             {"additional-header-new-value", "additional-header-override-value"},
						"X-Ext-Authz-Additional-Header-Override": {"additional-header-override-value"},
					}),
					scheck.HeaderNotContains(hType, map[string][]string{
						"X-Ext-Authz-Check-Received":             {"should-be-override"},
						"X-Ext-Authz-Additional-Header-Override": {"should-be-override"},
					}))
			}
			checkGRPCHeaders := func(hType echoClient.HeaderType) check.Checker {
				return check.And(
					scheck.HeaderContains(hType, map[string][]string{
						"X-Ext-Authz-Check-Received":             {"should-be-override"},
						"X-Ext-Authz-Additional-Header-Override": {"grpc-additional-header-override-value"},
					}),
					scheck.HeaderNotContains(hType, map[string][]string{
						"X-Ext-Authz-Additional-Header-Override": {"should-be-override"},
					}))
			}

			authzHeaders := func(h string) http.Header {
				return headers.New().With("x-ext-authz", h).With("x-ext-authz-additional-header-override", "should-be-override").Build()
			}

			// Path "/custom" is protected by ext-authz service and is accessible with the header `x-ext-authz: allow`.
			// Path "/health" is not protected and is accessible to public.
			cases := []func(test framework.TestContext){
				// workload b is using an ext-authz service in its own pod of HTTP API.
				newTestCase(x, b, scheme.HTTP, "http", "/custom", authzHeaders("allow"), checkHTTPHeaders(echoClient.RequestHeader), true),
				newTestCase(x, b, scheme.HTTP, "http", "/custom", authzHeaders("deny"), checkHTTPHeaders(echoClient.ResponseHeader), false),
				newTestCase(x, b, scheme.HTTP, "http", "/health", authzHeaders("allow"), nil, true),
				newTestCase(x, b, scheme.HTTP, "http", "/health", authzHeaders("deny"), nil, true),

				// workload c is using an ext-authz service in its own pod of gRPC API.
				newTestCase(x, c, scheme.HTTP, "http", "/custom", authzHeaders("allow"), checkGRPCHeaders(echoClient.RequestHeader), true),
				newTestCase(x, c, scheme.HTTP, "http", "/custom", authzHeaders("deny"), checkGRPCHeaders(echoClient.ResponseHeader), false),
				newTestCase(x, c, scheme.HTTP, "http", "/health", authzHeaders("allow"), nil, true),
				newTestCase(x, c, scheme.HTTP, "http", "/health", authzHeaders("deny"), nil, true),

				// workload d is using an local ext-authz service in the same pod as the application of HTTP API.
				newTestCase(x, d, scheme.HTTP, "http", "/custom", authzHeaders("allow"), checkHTTPHeaders(echoClient.RequestHeader), true),
				newTestCase(x, d, scheme.HTTP, "http", "/custom", authzHeaders("deny"), checkHTTPHeaders(echoClient.ResponseHeader), false),
				newTestCase(x, d, scheme.HTTP, "http", "/health", authzHeaders("allow"), nil, true),
				newTestCase(x, d, scheme.HTTP, "http", "/health", authzHeaders("deny"), nil, true),

				// workload e is using an local ext-authz service in the same pod as the application of gRPC API.
				newTestCase(x, e, scheme.HTTP, "http", "/custom", authzHeaders("allow"), checkGRPCHeaders(echoClient.RequestHeader), true),
				newTestCase(x, e, scheme.HTTP, "http", "/custom", authzHeaders("deny"), checkGRPCHeaders(echoClient.ResponseHeader), false),
				newTestCase(x, e, scheme.HTTP, "http", "/health", authzHeaders("allow"), nil, true),
				newTestCase(x, e, scheme.HTTP, "http", "/health", authzHeaders("deny"), nil, true),

				// workload f is using an ext-authz service in its own pod of TCP API.
				newTestCase(a, f, scheme.TCP, "tcp-8092", "", authzHeaders(""), nil, true),
				newTestCase(x, f, scheme.TCP, "tcp-8092", "", authzHeaders(""), nil, false),
				newTestCase(a, f, scheme.TCP, "tcp-8093", "", authzHeaders(""), nil, true),
				newTestCase(x, f, scheme.TCP, "tcp-8093", "", authzHeaders(""), nil, true),
			}

			for _, c := range cases {
				c(t)
			}

			t.NewSubTest("ingress").Run(func(t framework.TestContext) {
				ingr := ist.IngressFor(t.Clusters().Default())
				newIngressTestCase := func(from, to echo.Instance, path string, h http.Header,
					checker check.Checker, expectAllowed bool) func(t framework.TestContext) {
					return func(t framework.TestContext) {
						opts := echo.CallOptions{
							To: to,
							Port: echo.Port{
								Protocol: protocol.HTTP,
							},
							Scheme: scheme.HTTP,
							HTTP: echo.HTTP{
								Path: path,
								Headers: headers.New().
									WithHost("www.company.com").
									With("X-Ext-Authz", h.Get("x-ext-authz")).
									Build(),
							},
						}
						if expectAllowed {
							opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
						} else {
							opts.Check = scheck.RBACFailure(&opts)
						}
						opts.Check = check.And(opts.Check, checker)

						name := fmt.Sprintf("%s->%s%s[%t]",
							from.Config().Service,
							to.Config().Service,
							path,
							expectAllowed)

						t.NewSubTest(name).Run(func(t framework.TestContext) {
							ingr.CallOrFail(t, opts)
						})
					}
				}

				ingressCases := []func(framework.TestContext){
					// workload g is using an ext-authz service in its own pod of HTTP API.
					newIngressTestCase(x, g, "/custom", authzHeaders("allow"), checkHTTPHeaders(echoClient.RequestHeader), true),
					newIngressTestCase(x, g, "/custom", authzHeaders("deny"), checkHTTPHeaders(echoClient.ResponseHeader), false),
					newIngressTestCase(x, g, "/health", authzHeaders("allow"), nil, true),
					newIngressTestCase(x, g, "/health", authzHeaders("deny"), nil, true),
				}
				for _, c := range ingressCases {
					c(t)
				}
			})
		})
}

type rbacTestName string

func (n rbacTestName) String() string {
	return string(n)
}

func (n rbacTestName) SkipIfNecessary(t framework.TestContext) {
	t.Helper()

	if strings.Contains(n.String(), "source-ip") && t.Clusters().IsMulticluster() {
		t.Skip("https://github.com/istio/istio/issues/37307: " +
			"Current source ip based authz test cases are not required in multicluster setup because " +
			"cross-network traffic will lose the origin source ip info")
	}
}

func newRbacTestName(prefix string, expectAllowed bool, from echo.Instance, opts *echo.CallOptions) rbacTestName {
	want := "deny"
	if expectAllowed {
		want = "allow"
	}

	return rbacTestName(fmt.Sprintf("%s%s->%s:%s%s[%s]",
		prefix,
		from.Config().Service,
		opts.To.Config().Service,
		opts.Port.Name,
		opts.HTTP.Path,
		want))
}
