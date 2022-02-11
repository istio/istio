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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/authn"
	"istio.io/istio/tests/integration/security/util/connection"
	rbacUtil "istio.io/istio/tests/integration/security/util/rbac_util"
)

func newRootNS(ctx framework.TestContext) namespace.Instance {
	return istio.ClaimSystemNamespaceOrFail(ctx, ctx)
}

// TestAuthorization_mTLS tests v1beta1 authorization with mTLS.
func TestAuthorization_mTLS(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.mtls-local").
		Run(func(t framework.TestContext) {
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			for _, dst := range []echo.Instances{b, vm} {
				args := map[string]string{
					"Namespace":  apps.Namespace1.Name(),
					"Namespace2": apps.Namespace2.Name(),
					"dst":        dst[0].Config().Service,
				}
				policies := tmpl.EvaluateAllOrFail(t, args,
					file.AsStringOrFail(t, "testdata/authz/v1beta1-mtls.yaml.tmpl"))
				t.ConfigIstio().ApplyYAMLOrFail(t, apps.Namespace1.Name(), policies...)
				util.WaitForConfig(t, apps.Namespace1, policies...)
				for _, cluster := range t.Clusters() {
					t.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(t framework.TestContext) {
						a := apps.A.Match(echo.InCluster(cluster).And(echo.Namespace(apps.Namespace1.Name())))
						c := apps.C.Match(echo.InCluster(cluster).And(echo.Namespace(apps.Namespace2.Name())))
						util.CheckExistence(t, a, c)
						newTestCase := func(from, to echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
							callCount := 1
							if to.Clusters().IsMulticluster() {
								// so we can validate all clusters are hit
								callCount = util.CallsPerCluster * len(to.Clusters())
							}
							return rbacUtil.TestCase{
								Request: connection.Checker{
									From: from[0], // From service
									Options: echo.CallOptions{
										Target:   to[0],
										PortName: "http",
										Scheme:   scheme.HTTP,
										Path:     path, // Requested path
										Count:    callCount,
									},
									DestClusters: to.Clusters(),
								},
								ExpectAllowed: expectAllowed,
							}
						}
						// a and c send requests to dst
						cases := []rbacUtil.TestCase{
							newTestCase(a, dst, "/principal-a", true),
							newTestCase(a, dst, "/namespace-2", false),
							newTestCase(c, dst, "/principal-a", false),
							newTestCase(c, dst, "/namespace-2", true),
						}
						rbacUtil.RunRBACTest(t, cases)
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
			b := apps.B.Match(echo.Namespace(ns.Name()))
			c := apps.C.Match(echo.Namespace(ns.Name()))
			vm := apps.VM.Match(echo.Namespace(ns.Name()))
			for _, dst := range []echo.Instances{b, vm} {
				args := map[string]string{
					"Namespace":  apps.Namespace1.Name(),
					"Namespace2": apps.Namespace2.Name(),
					"dst":        dst[0].Config().Service,
				}
				policies := tmpl.EvaluateAllOrFail(t, args,
					file.AsStringOrFail(t, "testdata/authz/v1beta1-jwt.yaml.tmpl"))
				t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policies...)
				util.WaitForConfig(t, ns, policies...)
				for _, srcCluster := range t.Clusters() {
					t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
						a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(ns.Name())))
						util.CheckExistence(t, a)
						callCount := 1
						if t.Clusters().IsMulticluster() {
							// so we can validate all clusters are hit
							callCount = util.CallsPerCluster * len(t.Clusters())
						}
						newTestCase := func(target echo.Instances, namePrefix string, jwt string, path string, expectAllowed bool) rbacUtil.TestCase {
							return rbacUtil.TestCase{
								NamePrefix: namePrefix,
								Request: connection.Checker{
									From: a[0],
									Options: echo.CallOptions{
										Target:   target[0],
										PortName: "http",
										Scheme:   scheme.HTTP,
										Path:     path,
										Count:    callCount,
									},
									DestClusters: target.Clusters(),
								},
								Jwt:           jwt,
								ExpectAllowed: expectAllowed,
							}
						}
						cases := []rbacUtil.TestCase{
							newTestCase(dst, "[NoJWT]", "", "/token1", false),
							newTestCase(dst, "[NoJWT]", "", "/token2", false),
							newTestCase(dst, "[Token1]", jwt.TokenIssuer1, "/token1", true),
							newTestCase(dst, "[Token1]", jwt.TokenIssuer1, "/token2", false),
							newTestCase(dst, "[Token2]", jwt.TokenIssuer2, "/token1", false),
							newTestCase(dst, "[Token2]", jwt.TokenIssuer2, "/token2", true),
							newTestCase(dst, "[Token1]", jwt.TokenIssuer1, "/tokenAny", true),
							newTestCase(dst, "[Token2]", jwt.TokenIssuer2, "/tokenAny", true),
							newTestCase(dst, "[PermissionToken1]", jwt.TokenIssuer1, "/permission", false),
							newTestCase(dst, "[PermissionToken2]", jwt.TokenIssuer2, "/permission", false),
							newTestCase(dst, "[PermissionTokenWithSpaceDelimitedScope]", jwt.TokenIssuer2WithSpaceDelimitedScope, "/permission", true),
							newTestCase(dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-key1", true),
							newTestCase(dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-key1", false),
							newTestCase(dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-key2", false),
							newTestCase(dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-key2", true),
							newTestCase(dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-2-key1", true),
							newTestCase(dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-2-key1", false),
							newTestCase(dst, "[NestedToken1]", jwt.TokenIssuer1WithNestedClaims1, "/nested-non-exist", false),
							newTestCase(dst, "[NestedToken2]", jwt.TokenIssuer1WithNestedClaims2, "/nested-non-exist", false),
							newTestCase(dst, "[NoJWT]", "", "/tokenAny", false),
							newTestCase(c, "[NoJWT]", "", "/somePath", true),

							// Test condition "request.auth.principal" on path "/valid-jwt".
							newTestCase(dst, "[NoJWT]", "", "/valid-jwt", false),
							newTestCase(dst, "[Token1]", jwt.TokenIssuer1, "/valid-jwt", true),
							newTestCase(dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/valid-jwt", true),
							newTestCase(dst, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/valid-jwt", true),

							// Test condition "request.auth.presenter" on suffix "/presenter".
							newTestCase(dst, "[Token1]", jwt.TokenIssuer1, "/request/presenter", false),
							newTestCase(dst, "[Token1WithAud]", jwt.TokenIssuer1, "/request/presenter", false),
							newTestCase(dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/presenter-x", false),
							newTestCase(dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/presenter", true),

							// Test condition "request.auth.audiences" on suffix "/audiences".
							newTestCase(dst, "[Token1]", jwt.TokenIssuer1, "/request/audiences", false),
							newTestCase(dst, "[Token1WithAzp]", jwt.TokenIssuer1WithAzp, "/request/audiences", false),
							newTestCase(dst, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/request/audiences-x", false),
							newTestCase(dst, "[Token1WithAud]", jwt.TokenIssuer1WithAud, "/request/audiences", true),
						}
						rbacUtil.RunRBACTest(t, cases)
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
			bInNS1 := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			vmInNS1 := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			cInNS1 := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
			cInNS2 := apps.C.Match(echo.Namespace(apps.Namespace2.Name()))
			ns1 := apps.Namespace1
			ns2 := apps.Namespace2
			rootns := newRootNS(t)
			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}
			newTestCase := func(namePrefix string, from, target echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					NamePrefix: namePrefix,
					Request: connection.Checker{
						From: from[0],
						Options: echo.CallOptions{
							Target:   target[0],
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
							Count:    callCount,
						},
						DestClusters: target.Clusters(),
					},
					ExpectAllowed: expectAllowed,
				}
			}

			for _, srcCluster := range t.Clusters() {
				a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace1.Name())))
				util.CheckExistence(t, a)
				cases := []struct {
					b        string
					c        string
					subCases []rbacUtil.TestCase
				}{
					{
						b: util.BSvc,
						c: util.CSvc,
						subCases: []rbacUtil.TestCase{
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-b", true),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-vm", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-c", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-x", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-all", true),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns2-c", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns2-all", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns-root-c", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-b", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-vm", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-c", true),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-x", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-all", true),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns2-c", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns2-all", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns-root-c", true),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns1-b", false),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns1-vm", false),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns1-c", false),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns1-x", false),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns1-all", false),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns2-c", true),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns2-all", true),
							newTestCase("[cInNS2]", a, cInNS2, "/policy-ns-root-c", true),
						},
					},
					{
						// TODO(JimmyCYJ): Support multiple VMs in different namespaces for workload selector test and set c to service on VM.
						b: util.VMSvc,
						c: util.CSvc,
						subCases: []rbacUtil.TestCase{
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns1-b", false),
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns1-vm", true),
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns1-c", false),
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns1-x", false),
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns1-all", true),
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns2-b", false),
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns2-all", false),
							newTestCase("[vmInNS1]", a, vmInNS1, "/policy-ns-root-c", false),
						},
					},
				}

				for _, tc := range cases {
					t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).
						Run(func(t framework.TestContext) {
							args := map[string]string{
								"Namespace1":    ns1.Name(),
								"Namespace2":    ns2.Name(),
								"RootNamespace": rootns.Name(),
								"b":             tc.b,
								"c":             tc.c,
							}
							applyPolicy := func(filename string, ns namespace.Instance) {
								policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
								t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policy...)
								util.WaitForConfig(t, ns, policy...)
							}
							applyPolicy("testdata/authz/v1beta1-workload-ns1.yaml.tmpl", ns1)
							applyPolicy("testdata/authz/v1beta1-workload-ns2.yaml.tmpl", ns2)
							applyPolicy("testdata/authz/v1beta1-workload-ns-root.yaml.tmpl", rootns)
							rbacUtil.RunRBACTest(t, tc.subCases)
						})
				}
			}
		})
}

// TestAuthorization_Deny tests the authorization policy with action "DENY".
func TestAuthorization_Deny(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.deny-action").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			rootns := newRootNS(t)
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			c := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootns.Name(),
				"b":             b[0].Config().Service,
				"c":             c[0].Config().Service,
				"vm":            vm[0].Config().Service,
			}
			applyPolicy := func(filename string, ns namespace.Instance) {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policy...)
				util.WaitForConfig(t, ns, policy...)
			}
			applyPolicy("testdata/authz/v1beta1-deny.yaml.tmpl", ns)
			applyPolicy("testdata/authz/v1beta1-deny-ns-root.yaml.tmpl", rootns)
			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}
			for _, srcCluster := range t.AllClusters() {
				t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
					a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace1.Name())))
					util.CheckExistence(t, a)
					newTestCase := func(target echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
						return rbacUtil.TestCase{
							Request: connection.Checker{
								From: a[0],
								Options: echo.CallOptions{
									Target:   target[0],
									PortName: "http",
									Scheme:   scheme.HTTP,
									Path:     path,
									Count:    callCount,
								},
								DestClusters: target.Clusters(),
							},
							ExpectAllowed: expectAllowed,
						}
					}
					cases := []rbacUtil.TestCase{
						newTestCase(b, "/deny", false),
						newTestCase(b, "/deny?param=value", false),
						newTestCase(b, "/global-deny", false),
						newTestCase(b, "/global-deny?param=value", false),
						newTestCase(b, "/other", true),
						newTestCase(b, "/other?param=value", true),
						newTestCase(b, "/allow", true),
						newTestCase(b, "/allow?param=value", true),
						newTestCase(c, "/allow/admin", false),
						newTestCase(c, "/allow/admin?param=value", false),
						newTestCase(c, "/global-deny", false),
						newTestCase(c, "/global-deny?param=value", false),
						newTestCase(c, "/other", false),
						newTestCase(c, "/other?param=value", false),
						newTestCase(c, "/allow", true),
						newTestCase(c, "/allow?param=value", true),

						// TODO(JimmyCYJ): support multiple VMs and test deny policies on multiple VMs.
						newTestCase(vm, "/allow/admin", false),
						newTestCase(vm, "/allow/admin?param=value", false),
						newTestCase(vm, "/global-deny", false),
						newTestCase(vm, "/global-deny?param=value", false),
						newTestCase(vm, "/other", false),
						newTestCase(vm, "/other?param=value", false),
						newTestCase(vm, "/allow", true),
						newTestCase(vm, "/allow?param=value", true),
					}

					rbacUtil.RunRBACTest(t, cases)
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
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			c := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
			d := apps.D.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			args := map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
				"b":          b[0].Config().Service,
				"c":          c[0].Config().Service,
				"d":          d[0].Config().Service,
				"vm":         vm[0].Config().Service,
			}
			applyPolicy := func(filename string) {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				t.ConfigIstio().ApplyYAMLOrFail(t, "", policy...)
			}
			applyPolicy("testdata/authz/v1beta1-negative-match.yaml.tmpl")
			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}
			for _, srcCluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
					a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace1.Name())))
					bInNS2 := apps.B.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace2.Name())))
					util.CheckExistence(t, a, bInNS2)
					newTestCase := func(from echo.Instance, target echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
						return rbacUtil.TestCase{
							Request: connection.Checker{
								From: from,
								Options: echo.CallOptions{
									Target:   target[0],
									PortName: "http",
									Scheme:   scheme.HTTP,
									Path:     path,
									Count:    callCount,
								},
								DestClusters: target.Clusters(),
							},
							ExpectAllowed: expectAllowed,
						}
					}

					// a, b, c and d are in the same namespace and another b(bInNs2) is in a different namespace.
					// a connects to b, c and d in ns1 with mTLS.
					// bInNs2 connects to b and c with mTLS, to d with plain-text.
					cases := []rbacUtil.TestCase{
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

					rbacUtil.RunRBACTest(t, cases)
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
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			// Gateways on VMs are not supported yet. This test verifies that security
			// policies at gateways are useful for managing accessibility to services
			// running on a VM.
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			for _, dst := range []echo.Instances{b, vm} {
				t.NewSubTest(fmt.Sprintf("to %s/", dst[0].Config().Service)).Run(func(t framework.TestContext) {
					args := map[string]string{
						"Namespace":     ns.Name(),
						"RootNamespace": rootns.Name(),
						"dst":           dst[0].Config().Service,
					}

					applyPolicy := func(filename string) {
						policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
						t.ConfigIstio().ApplyYAMLOrFail(t, "", policy...)
					}
					applyPolicy("testdata/authz/v1beta1-ingress-gateway.yaml.tmpl")

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
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny DENY.COMPANY.COM",
							Host:     "DENY.COMPANY.COM",
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny Deny.Company.Com",
							Host:     "Deny.Company.Com",
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny deny.suffix.company.com",
							Host:     "deny.suffix.company.com",
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny DENY.SUFFIX.COMPANY.COM",
							Host:     "DENY.SUFFIX.COMPANY.COM",
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny Deny.Suffix.Company.Com",
							Host:     "Deny.Suffix.Company.Com",
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny prefix.company.com",
							Host:     "prefix.company.com",
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny PREFIX.COMPANY.COM",
							Host:     "PREFIX.COMPANY.COM",
							WantCode: 403,
						},
						{
							Name:     "case-insensitive-deny Prefix.Company.Com",
							Host:     "Prefix.Company.Com",
							WantCode: 403,
						},
						{
							Name:     "allow www.company.com",
							Host:     "www.company.com",
							Path:     "/",
							IP:       "172.16.0.1",
							WantCode: 200,
						},
						{
							Name:     "deny www.company.com/private",
							Host:     "www.company.com",
							Path:     "/private",
							IP:       "172.16.0.1",
							WantCode: 403,
						},
						{
							Name:     "allow www.company.com/public",
							Host:     "www.company.com",
							Path:     "/public",
							IP:       "172.16.0.1",
							WantCode: 200,
						},
						{
							Name:     "deny internal.company.com",
							Host:     "internal.company.com",
							Path:     "/",
							IP:       "172.16.0.1",
							WantCode: 403,
						},
						{
							Name:     "deny internal.company.com/private",
							Host:     "internal.company.com",
							Path:     "/private",
							IP:       "172.16.0.1",
							WantCode: 403,
						},
						{
							Name:     "deny 172.17.72.46",
							Host:     "remoteipblocks.company.com",
							Path:     "/",
							IP:       "172.17.72.46",
							WantCode: 403,
						},
						{
							Name:     "deny 192.168.5.233",
							Host:     "remoteipblocks.company.com",
							Path:     "/",
							IP:       "192.168.5.233",
							WantCode: 403,
						},
						{
							Name:     "allow 10.4.5.6",
							Host:     "remoteipblocks.company.com",
							Path:     "/",
							IP:       "10.4.5.6",
							WantCode: 200,
						},
						{
							Name:     "deny 10.2.3.4",
							Host:     "notremoteipblocks.company.com",
							Path:     "/",
							IP:       "10.2.3.4",
							WantCode: 403,
						},
						{
							Name:     "allow 172.23.242.188",
							Host:     "notremoteipblocks.company.com",
							Path:     "/",
							IP:       "172.23.242.188",
							WantCode: 200,
						},
						{
							Name:     "deny 10.242.5.7",
							Host:     "remoteipattr.company.com",
							Path:     "/",
							IP:       "10.242.5.7",
							WantCode: 403,
						},
						{
							Name:     "deny 10.124.99.10",
							Host:     "remoteipattr.company.com",
							Path:     "/",
							IP:       "10.124.99.10",
							WantCode: 403,
						},
						{
							Name:     "allow 10.4.5.6",
							Host:     "remoteipattr.company.com",
							Path:     "/",
							IP:       "10.4.5.6",
							WantCode: 200,
						},
					}

					for _, tc := range cases {
						t.NewSubTest(tc.Name).Run(func(t framework.TestContext) {
							headers := map[string][]string{
								"X-Forwarded-For": {tc.IP},
							}
							authn.CheckIngressOrFail(t, ingr, tc.Host, tc.Path, headers, "", tc.WantCode)
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
			a := apps.A.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			c := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
			// Gateways on VMs are not supported yet. This test verifies that security
			// policies at gateways are useful for managing accessibility to external
			// services running on a VM.
			for _, a := range []echo.Instances{a, vm} {
				t.NewSubTest(fmt.Sprintf("to %s/", a[0].Config().Service)).Run(func(t framework.TestContext) {
					args := map[string]string{
						"Namespace":     ns.Name(),
						"RootNamespace": rootns.Name(),
						"a":             a[0].Config().Service,
					}
					policies := tmpl.EvaluateAllOrFail(t, args,
						file.AsStringOrFail(t, "testdata/authz/v1beta1-egress-gateway.yaml.tmpl"))
					t.ConfigIstio().ApplyYAMLOrFail(t, "", policies...)

					cases := []struct {
						name  string
						path  string
						code  string
						body  string
						host  string
						from  echo.Workload
						token string
					}{
						{
							name: "allow path to company.com",
							path: "/allow",
							code: response.StatusCodeOK,
							body: "handled-by-egress-gateway",
							host: "www.company.com",
							from: getWorkload(a[0], t),
						},
						{
							name: "deny path to company.com",
							path: "/deny",
							code: response.StatusCodeForbidden,
							body: "RBAC: access denied",
							host: "www.company.com",
							from: getWorkload(a[0], t),
						},
						{
							name: "allow service account a to a-only.com over mTLS",
							path: "/",
							code: response.StatusCodeOK,
							body: "handled-by-egress-gateway",
							host: fmt.Sprintf("%s-only.com", a[0].Config().Service),
							from: getWorkload(a[0], t),
						},
						{
							name: "deny service account b to a-only.com over mTLS",
							path: "/",
							code: response.StatusCodeForbidden,
							body: "RBAC: access denied",
							host: fmt.Sprintf("%s-only.com", a[0].Config().Service),
							from: getWorkload(c[0], t),
						},
						{
							name:  "allow a with JWT to jwt-only.com over mTLS",
							path:  "/",
							code:  response.StatusCodeOK,
							body:  "handled-by-egress-gateway",
							host:  "jwt-only.com",
							from:  getWorkload(a[0], t),
							token: jwt.TokenIssuer1,
						},
						{
							name:  "allow b with JWT to jwt-only.com over mTLS",
							path:  "/",
							code:  response.StatusCodeOK,
							body:  "handled-by-egress-gateway",
							host:  "jwt-only.com",
							from:  getWorkload(c[0], t),
							token: jwt.TokenIssuer1,
						},
						{
							name:  "deny b with wrong JWT to jwt-only.com over mTLS",
							path:  "/",
							code:  response.StatusCodeForbidden,
							body:  "RBAC: access denied",
							host:  "jwt-only.com",
							from:  getWorkload(c[0], t),
							token: jwt.TokenIssuer2,
						},
						{
							name:  "allow service account a with JWT to jwt-and-a-only.com over mTLS",
							path:  "/",
							code:  response.StatusCodeOK,
							body:  "handled-by-egress-gateway",
							host:  fmt.Sprintf("jwt-and-%s-only.com", a[0].Config().Service),
							from:  getWorkload(a[0], t),
							token: jwt.TokenIssuer1,
						},
						{
							name:  "deny service account c with JWT to jwt-and-a-only.com over mTLS",
							path:  "/",
							code:  response.StatusCodeForbidden,
							body:  "RBAC: access denied",
							host:  fmt.Sprintf("jwt-and-%s-only.com", a[0].Config().Service),
							from:  getWorkload(c[0], t),
							token: jwt.TokenIssuer1,
						},
						{
							name:  "deny service account a with wrong JWT to jwt-and-a-only.com over mTLS",
							path:  "/",
							code:  response.StatusCodeForbidden,
							body:  "RBAC: access denied",
							host:  fmt.Sprintf("jwt-and-%s-only.com", a[0].Config().Service),
							from:  getWorkload(a[0], t),
							token: jwt.TokenIssuer2,
						},
					}

					for _, tc := range cases {
						request := &epb.ForwardEchoRequest{
							// Use a fake IP to make sure the request is handled by our test.
							Url:   fmt.Sprintf("http://10.4.4.4%s", tc.path),
							Count: 1,
							Headers: []*epb.Header{
								{
									Key:   "Host",
									Value: tc.host,
								},
							},
						}
						if tc.token != "" {
							request.Headers = append(request.Headers, &epb.Header{
								Key:   "Authorization",
								Value: "Bearer " + tc.token,
							})
						}
						t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
							retry.UntilSuccessOrFail(t, func() error {
								responses, err := tc.from.ForwardEcho(context.TODO(), request)
								if err != nil {
									return err
								}
								if len(responses) < 1 {
									return fmt.Errorf("received no responses from request to %s", tc.path)
								}
								if tc.code != responses[0].Code {
									return fmt.Errorf("want status %s but got %s", tc.code, responses[0].Code)
								}
								if !strings.Contains(responses[0].Body, tc.body) {
									return fmt.Errorf("want %q in body but not found: %s", tc.body, responses[0].Body)
								}
								return nil
							}, retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
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
			newTestCase := func(from, target echo.Instances, port string, expectAllowed bool, scheme scheme.Instance) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from[0],
						Options: echo.CallOptions{
							Target:   target[0],
							PortName: port,
							Scheme:   scheme,
							Path:     "/data",
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}
			ns := apps.Namespace1
			ns2 := apps.Namespace2
			a := apps.A.Match(echo.Namespace(ns.Name()))
			b := apps.B.Match(echo.Namespace(ns.Name()))
			c := apps.C.Match(echo.Namespace(ns.Name()))
			eInNS2 := apps.E.Match(echo.Namespace(ns2.Name()))
			d := apps.D.Match(echo.Namespace(ns.Name()))
			e := apps.E.Match(echo.Namespace(ns.Name()))
			t.NewSubTest("").
				Run(func(t framework.TestContext) {
					policy := tmpl.EvaluateAllOrFail(t, map[string]string{
						"Namespace":  ns.Name(),
						"Namespace2": ns2.Name(),
						"b":          b[0].Config().Service,
						"c":          c[0].Config().Service,
						"d":          d[0].Config().Service,
						"e":          e[0].Config().Service,
						"a":          a[0].Config().Service,
					}, file.AsStringOrFail(t, "testdata/authz/v1beta1-tcp.yaml.tmpl"))
					t.ConfigIstio().ApplyYAMLOrFail(t, "", policy...)
					cases := []rbacUtil.TestCase{
						// The policy on workload b denies request with path "/data" to port 8091:
						// - request to port http-8091 should be denied because both path and port are matched.
						// - request to port http-8092 should be allowed because the port is not matched.
						// - request to port tcp-8093 should be allowed because the port is not matched.
						newTestCase(a, b, "http-8091", false, scheme.HTTP),
						newTestCase(a, b, "http-8092", true, scheme.HTTP),
						newTestCase(a, b, "tcp-8093", true, scheme.TCP),

						// The policy on workload c denies request to port 8091:
						// - request to port http-8091 should be denied because the port is matched.
						// - request to http port 8092 should be allowed because the port is not matched.
						// - request to tcp port 8093 should be allowed because the port is not matched.
						// - request from b to tcp port 8093 should be allowed by default.
						// - request from b to tcp port 8094 should be denied because the principal is matched.
						// - request from eInNS2 to tcp port 8093 should be denied because the namespace is matched.
						// - request from eInNS2 to tcp port 8094 should be allowed by default.
						newTestCase(a, c, "http-8091", false, scheme.HTTP),
						newTestCase(a, c, "http-8092", true, scheme.HTTP),
						newTestCase(a, c, "tcp-8093", true, scheme.TCP),
						newTestCase(b, c, "tcp-8093", true, scheme.TCP),
						newTestCase(b, c, "tcp-8094", false, scheme.TCP),
						newTestCase(eInNS2, c, "tcp-8093", false, scheme.TCP),
						newTestCase(eInNS2, c, "tcp-8094", true, scheme.TCP),

						// The policy on workload d denies request from service account a and workloads in namespace 2:
						// - request from a to d should be denied because it has service account a.
						// - request from b to d should be allowed.
						// - request from c to d should be allowed.
						// - request from eInNS2 to a should be allowed because there is no policy on a.
						// - request from eInNS2 to d should be denied because it's in namespace 2.
						newTestCase(a, d, "tcp-8093", false, scheme.TCP),
						newTestCase(b, d, "tcp-8093", true, scheme.TCP),
						newTestCase(c, d, "tcp-8093", true, scheme.TCP),
						newTestCase(eInNS2, a, "tcp-8093", true, scheme.TCP),
						newTestCase(eInNS2, d, "tcp-8093", false, scheme.TCP),

						// The policy on workload e denies request with path "/other":
						// - request to port http-8091 should be allowed because the path is not matched.
						// - request to port http-8092 should be allowed because the path is not matched.
						// - request to port tcp-8093 should be denied because policy uses HTTP fields.
						newTestCase(a, e, "http-8091", true, scheme.HTTP),
						newTestCase(a, e, "http-8092", true, scheme.HTTP),
						newTestCase(a, e, "tcp-8093", false, scheme.TCP),
					}

					rbacUtil.RunRBACTest(t, cases)
				})
			// TODO(JimmyCYJ): support multiple VMs and apply different security policies to each VM.
			vm := apps.VM.Match(echo.Namespace(ns.Name()))
			t.NewSubTest("").
				Run(func(t framework.TestContext) {
					policy := tmpl.EvaluateAllOrFail(t, map[string]string{
						"Namespace":  ns.Name(),
						"Namespace2": ns2.Name(),
						"b":          b[0].Config().Service,
						"c":          vm[0].Config().Service,
						"d":          d[0].Config().Service,
						"e":          e[0].Config().Service,
						"a":          a[0].Config().Service,
					}, file.AsStringOrFail(t, "testdata/authz/v1beta1-tcp.yaml.tmpl"))
					t.ConfigIstio().ApplyYAMLOrFail(t, "", policy...)
					cases := []rbacUtil.TestCase{
						// The policy on workload vm denies request to port 8091:
						// - request to port http-8091 should be denied because the port is matched.
						// - request to http port 8092 should be allowed because the port is not matched.
						// - request to tcp port 8093 should be allowed because the port is not matched.
						// - request from b to tcp port 8093 should be allowed by default.
						// - request from b to tcp port 8094 should be denied because the principal is matched.
						// - request from eInNS2 to tcp port 8093 should be denied because the namespace is matched.
						// - request from eInNS2 to tcp port 8094 should be allowed by default.
						newTestCase(a, vm, "http-8091", false, scheme.HTTP),
						newTestCase(a, vm, "http-8092", true, scheme.HTTP),
						newTestCase(a, vm, "tcp-8093", true, scheme.TCP),
						newTestCase(b, vm, "tcp-8093", true, scheme.TCP),
						newTestCase(b, vm, "tcp-8094", false, scheme.TCP),
						newTestCase(eInNS2, vm, "tcp-8093", false, scheme.TCP),
						newTestCase(eInNS2, vm, "tcp-8094", true, scheme.TCP),
					}
					rbacUtil.RunRBACTest(t, cases)
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

			c := apps.C.Match(echo.Namespace(nsC.Name()))
			vm := apps.VM.Match(echo.Namespace(nsA.Name()))
			for _, cSet := range []echo.Instances{c, vm} {
				for _, a := range apps.A.Match(echo.Namespace(nsA.Name())) {
					a, bs := a, apps.B.Match(echo.InCluster(a.Config().Cluster)).Match(echo.Namespace(nsB.Name()))
					if len(bs) < 1 {
						t.Skip()
					}
					b := bs[0]
					t.NewSubTest(fmt.Sprintf("from %s to %s in %s",
						a.Config().Cluster.StableName(), cSet[0].Config().Service, cSet[0].Config().Cluster.StableName())).
						Run(func(t framework.TestContext) {
							var ipC string
							for i := 0; i < len(cSet); i++ {
								ipC += "\"" + getWorkload(cSet[i], t).Address() + "\","
							}
							lengthC := len(ipC)
							ipC = ipC[:lengthC-1]
							args := map[string]string{
								"NamespaceA": nsA.Name(),
								"NamespaceB": nsB.Name(),
								"NamespaceC": cSet[0].Config().Namespace.Name(),
								"cSet":       cSet[0].Config().Service,
								"ipA":        getWorkload(a, t).Address(),
								"ipB":        getWorkload(b, t).Address(),
								"ipC":        ipC,
								"portC":      "8090",
								"a":          util.ASvc,
								"b":          util.BSvc,
							}

							policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, "testdata/authz/v1beta1-conditions.yaml.tmpl"))
							t.ConfigIstio().ApplyYAMLOrFail(t, "", policies...)
							callCount := 1
							if t.Clusters().IsMulticluster() {
								// so we can validate all clusters are hit
								callCount = util.CallsPerCluster * len(t.Clusters())
							}
							newTestCase := func(from echo.Instance, path string, headers map[string]string, expectAllowed bool) rbacUtil.TestCase {
								return rbacUtil.TestCase{
									Request: connection.Checker{
										From: from,
										Options: echo.CallOptions{
											Target:   cSet[0],
											PortName: "http",
											Scheme:   scheme.HTTP,
											Path:     path,
											Count:    callCount,
										},
										DestClusters: cSet.Clusters(),
									},
									Headers:       headers,
									ExpectAllowed: expectAllowed,
								}
							}
							cases := []rbacUtil.TestCase{
								newTestCase(a, "/request-headers", map[string]string{"x-foo": "foo"}, true),
								newTestCase(b, "/request-headers", map[string]string{"x-foo": "foo"}, true),
								newTestCase(a, "/request-headers", map[string]string{"x-foo": "bar"}, false),
								newTestCase(b, "/request-headers", map[string]string{"x-foo": "bar"}, false),
								newTestCase(a, "/request-headers", nil, false),
								newTestCase(b, "/request-headers", nil, false),
								newTestCase(a, "/request-headers-notValues-bar", map[string]string{"x-foo": "foo"}, true),
								newTestCase(a, "/request-headers-notValues-bar", map[string]string{"x-foo": "bar"}, false),

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
							rbacUtil.RunRBACTest(t, cases)
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
			a := apps.A.Match(echo.Namespace(apps.Namespace1.Name()))
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			c := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
			d := apps.D.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			for _, a := range []echo.Instances{a, vm} {
				for _, b := range []echo.Instances{b, vm} {
					if a[0].Config().Service == b[0].Config().Service {
						t.Skip()
					}
					t.NewSubTest(fmt.Sprintf("to %s in %s", a[0].Config().Service, a[0].Config().Cluster.StableName())).
						Run(func(t framework.TestContext) {
							args := map[string]string{
								"Namespace": ns.Name(),
								"a":         a[0].Config().Service,
								"b":         b[0].Config().Service,
								"c":         c[0].Config().Service,
								"d":         d[0].Config().Service,
							}
							policies := tmpl.EvaluateAllOrFail(t, args,
								file.AsStringOrFail(t, "testdata/authz/v1beta1-grpc.yaml.tmpl"))
							t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policies...)
							util.WaitForConfig(t, ns, policies...)
							cases := []rbacUtil.TestCase{
								{
									Request: connection.Checker{
										From: b[0],
										Options: echo.CallOptions{
											Target:   a[0],
											PortName: "grpc",
											Scheme:   scheme.GRPC,
										},
									},
									ExpectAllowed: true,
								},
								{
									Request: connection.Checker{
										From: c[0],
										Options: echo.CallOptions{
											Target:   a[0],
											PortName: "grpc",
											Scheme:   scheme.GRPC,
										},
									},
									ExpectAllowed: false,
								},
								{
									Request: connection.Checker{
										From: d[0],
										Options: echo.CallOptions{
											Target:   a[0],
											PortName: "grpc",
											Scheme:   scheme.GRPC,
										},
									},
									ExpectAllowed: true,
								},
							}

							rbacUtil.RunRBACTest(t, cases)
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
			a := apps.A.Match(echo.Namespace(ns.Name()))
			vm := apps.VM.Match(echo.Namespace(ns.Name()))
			for _, a := range []echo.Instances{a, vm} {
				for _, srcCluster := range t.Clusters() {
					t.NewSubTest(fmt.Sprintf("In %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
						b := apps.B.Match(echo.InCluster(srcCluster).And(echo.Namespace(ns.Name())))
						util.CheckExistence(t, b)
						args := map[string]string{
							"Namespace": ns.Name(),
							"a":         a[0].Config().Service,
						}
						policies := tmpl.EvaluateAllOrFail(t, args,
							file.AsStringOrFail(t, "testdata/authz/v1beta1-path.yaml.tmpl"))
						t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policies...)
						util.WaitForConfig(t, ns, policies...)

						callCount := 1
						if t.Clusters().IsMulticluster() {
							// so we can validate all clusters are hit
							callCount = util.CallsPerCluster * len(t.Clusters())
						}

						newTestCase := func(to echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
							return rbacUtil.TestCase{
								Request: connection.Checker{
									From: b[0],
									Options: echo.CallOptions{
										Target:   to[0],
										PortName: "http",
										Scheme:   scheme.HTTP,
										Path:     path,
										Count:    callCount,
									},
									DestClusters: a.Clusters(),
								},
								ExpectAllowed: expectAllowed,
							}
						}
						cases := []rbacUtil.TestCase{
							newTestCase(a, "/public", true),
							newTestCase(a, "/public/../public", true),
							newTestCase(a, "/private", false),
							newTestCase(a, "/public/../private", false),
							newTestCase(a, "/public/./../private", false),
							newTestCase(a, "/public/.././private", false),
							newTestCase(a, "/public/%2E%2E/private", false),
							newTestCase(a, "/public/%2e%2e/private", false),
							newTestCase(a, "/public/%2E/%2E%2E/private", false),
							newTestCase(a, "/public/%2e/%2e%2e/private", false),
							newTestCase(a, "/public/%2E%2E/%2E/private", false),
							newTestCase(a, "/public/%2e%2e/%2e/private", false),
						}
						rbacUtil.RunRBACTest(t, cases)
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
			a := apps.A.Match(echo.Namespace(ns.Name()))
			b := apps.B.Match(echo.Namespace(ns.Name()))
			c := apps.C.Match(echo.Namespace(ns.Name()))
			d := apps.D.Match(echo.Namespace(ns.Name()))
			vm := apps.VM.Match(echo.Namespace(ns.Name()))

			newTestCase := func(from, to echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from[0],
						Options: echo.CallOptions{
							Target:   to[0],
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}

			cases := []rbacUtil.TestCase{
				newTestCase(a, b, "/allow", true),
				newTestCase(a, b, "/audit", false),
				newTestCase(a, c, "/audit", true),
				newTestCase(a, c, "/deny", false),
				newTestCase(a, d, "/audit", true),
				newTestCase(a, d, "/other", true),
			}
			t.NewSubTest(fmt.Sprintf("from %s in %s", a[0].Config().Service, a[0].Config().Cluster.StableName())).
				Run(func(t framework.TestContext) {
					args := map[string]string{
						"b":             b[0].Config().Service,
						"c":             c[0].Config().Service,
						"d":             d[0].Config().Service,
						"Namespace":     ns.Name(),
						"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
					}
					applyPolicy := func(filename string, ns namespace.Instance) {
						policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
						t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policy...)
						util.WaitForConfig(t, ns, policy...)
					}
					applyPolicy("testdata/authz/v1beta1-audit.yaml.tmpl", ns)

					rbacUtil.RunRBACTest(t, cases)
				})

			// (TODO)JimmyCYJ: Support multiple VMs and apply audit policies to multiple VMs for testing.
			// The tests below are duplicated from above for VM workloads. With support for multiple VMs,
			// These tests will be merged to the tests above.
			vmCases := []struct {
				configFile string
				dst        echo.Instances
				subCases   []rbacUtil.TestCase
			}{
				{
					configFile: "testdata/authz/v1beta1-audit-allow.yaml.tmpl",
					dst:        vm,
					subCases: []rbacUtil.TestCase{
						newTestCase(b, vm, "/allow", true),
						newTestCase(b, vm, "/audit", false),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-audit-deny.yaml.tmpl",
					dst:        vm,
					subCases: []rbacUtil.TestCase{
						newTestCase(b, vm, "/audit", true),
						newTestCase(b, vm, "/deny", false),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-audit-default.yaml.tmpl",
					dst:        vm,
					subCases: []rbacUtil.TestCase{
						newTestCase(b, vm, "/audit", true),
						newTestCase(b, vm, "/other", true),
					},
				},
			}

			for _, tc := range vmCases {
				t.NewSubTest(fmt.Sprintf("from %s to %s in %s",
					b[0].Config().Cluster.StableName(), tc.dst[0].Config().Service, tc.dst[0].Config().Cluster.StableName())).
					Run(func(t framework.TestContext) {
						args := map[string]string{
							"Namespace": ns.Name(),
							"dst":       tc.dst[0].Config().Service,
						}
						policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, tc.configFile))
						t.ConfigIstio().ApplyYAMLOrFail(t, ns.Name(), policies...)
						util.WaitForConfig(t, ns, policies...)
						rbacUtil.RunRBACTest(t, tc.subCases)
					})
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

			applyYAML := func(filename string, namespace string) {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				t.ConfigIstio().ApplyYAMLOrFail(t, namespace, policy...)
			}

			// Deploy and wait for the ext-authz server to be ready.
			applyYAML("../../../samples/extauthz/ext-authz.yaml", ns.Name())
			if _, _, err := kube.WaitUntilServiceEndpointsAreReady(t.Clusters().Default(), ns.Name(), "ext-authz"); err != nil {
				t.Fatalf("Wait for ext-authz server failed: %v", err)
			}
			// Update the mesh config extension provider for the ext-authz service.
			extService := fmt.Sprintf("ext-authz.%s.svc.cluster.local", ns.Name())
			extServiceWithNs := fmt.Sprintf("%s/%s", ns.Name(), extService)
			istio.PatchMeshConfig(t, ist.Settings().IstioNamespace, t.Clusters(), fmt.Sprintf(`
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

			applyYAML("testdata/authz/v1beta1-custom.yaml.tmpl", "")
			ports := []echo.Port{
				{
					Name:         "tcp-8092",
					Protocol:     protocol.TCP,
					InstancePort: 8092,
				},
				{
					Name:         "tcp-8093",
					Protocol:     protocol.TCP,
					InstancePort: 8093,
				},
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
			}

			var a, b, c, d, e, f, g, x echo.Instance
			echoConfig := func(name string, includeExtAuthz bool) echo.Config {
				cfg := util.EchoConfig(name, ns, false, nil)
				cfg.IncludeExtAuthz = includeExtAuthz
				cfg.Ports = ports
				return cfg
			}
			echoboot.NewBuilder(t).
				With(&a, echoConfig("a", false)).
				With(&b, echoConfig("b", false)).
				With(&c, echoConfig("c", false)).
				With(&d, echoConfig("d", true)).
				With(&e, echoConfig("e", true)).
				With(&f, echoConfig("f", false)).
				With(&g, echoConfig("g", false)).
				With(&x, echoConfig("x", false)).
				BuildOrFail(t)

			newTestCase := func(from, target echo.Instance, path, port string, header string, expectAllowed bool,
				expectHTTPResponse []rbacUtil.ExpectContains, scheme scheme.Instance) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from,
						Options: echo.CallOptions{
							Target:   target,
							PortName: port,
							Scheme:   scheme,
							Path:     path,
						},
					},
					Headers: map[string]string{
						"x-ext-authz":                            header,
						"x-ext-authz-additional-header-override": "should-be-override",
					},
					ExpectAllowed:      expectAllowed,
					ExpectHTTPResponse: expectHTTPResponse,
				}
			}
			expectHTTPResponse := []rbacUtil.ExpectContains{
				{
					// For ext authz HTTP server, we expect the check request to include the override value because it
					// is configued in the ext-authz filter side.
					Key:       "X-Ext-Authz-Check-Received",
					Values:    []string{"additional-header-new-value", "additional-header-override-value"},
					NotValues: []string{"should-be-override"},
				},
				{
					Key:       "X-Ext-Authz-Additional-Header-Override",
					Values:    []string{"additional-header-override-value"},
					NotValues: []string{"should-be-override"},
				},
			}
			expectGRPCResponse := []rbacUtil.ExpectContains{
				{
					// For ext authz gRPC server, we expect the check request to include the original override value
					// because the override is not configurable in the ext-authz filter side.
					Key:    "X-Ext-Authz-Check-Received",
					Values: []string{"should-be-override"},
				},
				{
					Key:       "X-Ext-Authz-Additional-Header-Override",
					Values:    []string{"grpc-additional-header-override-value"},
					NotValues: []string{"should-be-override"},
				},
			}

			// Path "/custom" is protected by ext-authz service and is accessible with the header `x-ext-authz: allow`.
			// Path "/health" is not protected and is accessible to public.
			cases := []rbacUtil.TestCase{
				// workload b is using an ext-authz service in its own pod of HTTP API.
				newTestCase(x, b, "/custom", "http", "allow", true, expectHTTPResponse, scheme.HTTP),
				newTestCase(x, b, "/custom", "http", "deny", false, expectHTTPResponse, scheme.HTTP),
				newTestCase(x, b, "/health", "http", "allow", true, nil, scheme.HTTP),
				newTestCase(x, b, "/health", "http", "deny", true, nil, scheme.HTTP),

				// workload c is using an ext-authz service in its own pod of gRPC API.
				newTestCase(x, c, "/custom", "http", "allow", true, expectGRPCResponse, scheme.HTTP),
				newTestCase(x, c, "/custom", "http", "deny", false, expectGRPCResponse, scheme.HTTP),
				newTestCase(x, c, "/health", "http", "allow", true, nil, scheme.HTTP),
				newTestCase(x, c, "/health", "http", "deny", true, nil, scheme.HTTP),

				// workload d is using an local ext-authz service in the same pod as the application of HTTP API.
				newTestCase(x, d, "/custom", "http", "allow", true, expectHTTPResponse, scheme.HTTP),
				newTestCase(x, d, "/custom", "http", "deny", false, expectHTTPResponse, scheme.HTTP),
				newTestCase(x, d, "/health", "http", "allow", true, nil, scheme.HTTP),
				newTestCase(x, d, "/health", "http", "deny", true, nil, scheme.HTTP),

				// workload e is using an local ext-authz service in the same pod as the application of gRPC API.
				newTestCase(x, e, "/custom", "http", "allow", true, expectGRPCResponse, scheme.HTTP),
				newTestCase(x, e, "/custom", "http", "deny", false, expectGRPCResponse, scheme.HTTP),
				newTestCase(x, e, "/health", "http", "allow", true, nil, scheme.HTTP),
				newTestCase(x, e, "/health", "http", "deny", true, nil, scheme.HTTP),

				// workload f is using an ext-authz service in its own pod of TCP API.
				newTestCase(a, f, "", "tcp-8092", "", true, nil, scheme.TCP),
				newTestCase(x, f, "", "tcp-8092", "", false, nil, scheme.TCP),
				newTestCase(a, f, "", "tcp-8093", "", true, nil, scheme.TCP),
				newTestCase(x, f, "", "tcp-8093", "", true, nil, scheme.TCP),
			}

			rbacUtil.RunRBACTest(t, cases)

			ingr := ist.IngressFor(t.Clusters().Default())
			ingressCases := []rbacUtil.TestCase{
				// workload g is using an ext-authz service in its own pod of HTTP API.
				newTestCase(x, g, "/custom", "http", "allow", true, expectHTTPResponse, scheme.HTTP),
				newTestCase(x, g, "/custom", "http", "deny", false, expectHTTPResponse, scheme.HTTP),
				newTestCase(x, g, "/health", "http", "allow", true, nil, scheme.HTTP),
				newTestCase(x, g, "/health", "http", "deny", true, nil, scheme.HTTP),
			}
			for _, tc := range ingressCases {
				name := fmt.Sprintf("%s->%s:%s%s[%t]",
					tc.Request.From.Config().Service,
					tc.Request.Options.Target.Config().Service,
					tc.Request.Options.PortName,
					tc.Request.Options.Path,
					tc.ExpectAllowed)

				t.NewSubTest(name).Run(func(t framework.TestContext) {
					wantCode := map[bool]int{true: 200, false: 403}[tc.ExpectAllowed]
					headers := map[string][]string{
						"X-Ext-Authz": {tc.Headers["x-ext-authz"]},
					}
					authn.CheckIngressOrFail(t, ingr, "www.company.com", tc.Request.Options.Path, headers, "", wantCode)
				})
			}
		})
}
