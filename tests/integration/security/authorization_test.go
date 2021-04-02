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

var (
	// The extAuthzServiceNamespace namespace is used to deploy the sample ext-authz server.
	extAuthzServiceNamespace    namespace.Instance
	extAuthzServiceNamespaceErr error
)

type rootNS struct {
	rootNamespace string
}

func (i rootNS) Name() string {
	return i.rootNamespace
}

func (i rootNS) SetLabel(key, value string) error {
	return nil
}

func (i rootNS) RemoveLabel(key string) error {
	return nil
}

func newRootNS(ctx framework.TestContext) rootNS {
	return rootNS{
		rootNamespace: istio.GetOrFail(ctx, ctx).Settings().SystemNamespace,
	}
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
				t.Config().ApplyYAMLOrFail(t, apps.Namespace1.Name(), policies...)
				for _, cluster := range t.Clusters() {
					t.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(t framework.TestContext) {
						a := apps.A.Match(echo.InCluster(cluster).And(echo.Namespace(apps.Namespace1.Name())))
						c := apps.C.Match(echo.InCluster(cluster).And(echo.Namespace(apps.Namespace2.Name())))
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
				t.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
				for _, srcCluster := range t.Clusters() {
					t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
						a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(ns.Name())))
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
				cases := []struct {
					configDst string
					subCases  []rbacUtil.TestCase
				}{
					{
						configDst: util.BSvc,
						subCases: []rbacUtil.TestCase{
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-b", true),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-vm", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-c", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-x", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns1-all", true),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns2-c", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns2-all", false),
							newTestCase("[bInNS1]", a, bInNS1, "/policy-ns-root-c", false),
						},
					},
					{
						configDst: util.VMSvc,
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
					{
						configDst: util.CSvc,
						subCases: []rbacUtil.TestCase{
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-b", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-vm", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-c", true),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-x", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns1-all", true),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns2-c", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns2-all", false),
							newTestCase("[cInNS1]", a, cInNS1, "/policy-ns-root-c", true),
						},
					},
					{
						configDst: util.CSvc,
						subCases: []rbacUtil.TestCase{
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
				}

				for _, tc := range cases {
					t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).
						Run(func(t framework.TestContext) {
							args := map[string]string{
								"Namespace1":    ns1.Name(),
								"Namespace2":    ns2.Name(),
								"RootNamespace": rootns.rootNamespace,
								"dst":           tc.configDst,
							}
							applyPolicy := func(filename string, ns namespace.Instance) {
								policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
								t.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
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
			// TODO: Convert into multicluster support. Currently reachability does
			// not cover all clusters.
			if t.Clusters().IsMulticluster() {
				t.Skip()
			}
			ns := apps.Namespace1
			rootns := newRootNS(t)
			dst0 := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			dst1 := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootns.rootNamespace,
				"dst0":          dst0[0].Config().Service,
				"dst1":          dst1[0].Config().Service,
			}
			applyPolicy := func(filename string, ns namespace.Instance) {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				t.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
			}
			applyPolicy("testdata/authz/v1beta1-deny.yaml.tmpl", ns)
			applyPolicy("testdata/authz/v1beta1-deny-ns-root.yaml.tmpl", rootns)
			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}
			for _, srcCluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
					a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace1.Name())))
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
						newTestCase(dst0, "/deny", false),
						newTestCase(dst0, "/deny?param=value", false),
						newTestCase(dst0, "/global-deny", false),
						newTestCase(dst0, "/global-deny?param=value", false),
						newTestCase(dst0, "/other", true),
						newTestCase(dst0, "/other?param=value", true),
						newTestCase(dst0, "/allow", true),
						newTestCase(dst0, "/allow?param=value", true),
						newTestCase(dst1, "/allow/admin", false),
						newTestCase(dst1, "/allow/admin?param=value", false),
						newTestCase(dst1, "/global-deny", false),
						newTestCase(dst1, "/global-deny?param=value", false),
						newTestCase(dst1, "/other", false),
						newTestCase(dst1, "/other?param=value", false),
						newTestCase(dst1, "/allow", true),
						newTestCase(dst1, "/allow?param=value", true),
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
			dst0 := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			dst1 := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			dst2 := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
			args := map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
				"dst0":       dst0[0].Config().Service,
				"dst1":       dst1[0].Config().Service,
				"dst2":       dst2[0].Config().Service,
			}
			applyPolicy := func(filename string) {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				t.Config().ApplyYAMLOrFail(t, "", policy...)
			}
			applyPolicy("testdata/authz/v1beta1-negative-match.yaml.tmpl")
			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}
			for _, srcCluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
					srcA := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace1.Name())))
					srcBInNS2 := apps.B.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace2.Name())))
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

					// a, dst0, dst1 and dst2 are in the same namespace and another b(bInNs2) is in a different namespace.
					// a connects to dst0, dst1 and dst2 in ns1 with mTLS.
					// bInNs2 connects to dst0 and dst1 with mTLS, to dst2 with plain-text.
					cases := []rbacUtil.TestCase{
						// Test the policy with overlapped `paths` and `not_paths` on dst0.
						// a and bInNs2 should have the same results:
						// - path with prefix `/prefix` should be denied explicitly.
						// - path `/prefix/allowlist` should be excluded from the deny.
						// - path `/allow` should be allowed implicitly.
						newTestCase(srcA[0], dst0, "/prefix", false),
						newTestCase(srcA[0], dst0, "/prefix/other", false),
						newTestCase(srcA[0], dst0, "/prefix/allowlist", true),
						newTestCase(srcA[0], dst0, "/allow", true),
						newTestCase(srcBInNS2[0], dst0, "/prefix", false),
						newTestCase(srcBInNS2[0], dst0, "/prefix/other", false),
						newTestCase(srcBInNS2[0], dst0, "/prefix/allowlist", true),
						newTestCase(srcBInNS2[0], dst0, "/allow", true),

						// Test the policy that denies other namespace on dst1.
						// a should be allowed because it's from the same namespace.
						// bInNs2 should be denied because it's from a different namespace.
						newTestCase(srcA[0], dst1, "/", true),
						newTestCase(srcBInNS2[0], dst1, "/", false),

						// Test the policy that denies plain-text traffic on dst2.
						// a should be allowed because it's using mTLS.
						// bInNs2 should be denied because it's using plain-text.
						newTestCase(srcA[0], dst2, "/", true),
						newTestCase(srcBInNS2[0], dst2, "/", false),
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
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			for _, dst := range []echo.Instances{b, vm} {
				args := map[string]string{
					"Namespace":     ns.Name(),
					"RootNamespace": rootns.rootNamespace,
					"dst":           dst[0].Config().Service,
				}

				applyPolicy := func(filename string) {
					policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
					t.Config().ApplyYAMLOrFail(t, "", policy...)
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
			}
		})
}

// TestAuthorization_EgressGateway tests v1beta1 authorization on egress gateway.
func TestAuthorization_EgressGateway(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.egress-gateway").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			rootns := newRootNS(t)
			src0 := apps.A.Match(echo.Namespace(apps.Namespace1.Name()))
			src1 := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			// Workload c is accessible via egress gateway.
			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootns.rootNamespace,
				"src":           util.ASvc,
				"dst":           util.CSvc,
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/authz/v1beta1-egress-gateway.yaml.tmpl"))
			t.Config().ApplyYAMLOrFail(t, "", policies...)

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
					from: getWorkload(src0[0], t),
				},
				{
					name: "deny path to company.com",
					path: "/deny",
					code: response.StatusCodeForbidden,
					body: "RBAC: access denied",
					host: "www.company.com",
					from: getWorkload(src0[0], t),
				},
				{
					name: "allow service account src0 to src0-only.com over mTLS",
					path: "/",
					code: response.StatusCodeOK,
					body: "handled-by-egress-gateway",
					host: fmt.Sprintf("%s-only.com", src0[0].Config().Service),
					from: getWorkload(src0[0], t),
				},
				{
					name: "deny service account src1 to src0-only.com over mTLS",
					path: "/",
					code: response.StatusCodeForbidden,
					body: "RBAC: access denied",
					host: fmt.Sprintf("%s-only.com", src0[0].Config().Service),
					from: getWorkload(src1[0], t),
				},
				{
					name:  "allow src0 with JWT to jwt-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeOK,
					body:  "handled-by-egress-gateway",
					host:  "jwt-only.com",
					from:  getWorkload(src0[0], t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "allow src1 with JWT to jwt-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeOK,
					body:  "handled-by-egress-gateway",
					host:  "jwt-only.com",
					from:  getWorkload(src1[0], t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "deny src1 with wrong JWT to jwt-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeForbidden,
					body:  "RBAC: access denied",
					host:  "jwt-only.com",
					from:  getWorkload(src1[0], t),
					token: jwt.TokenIssuer2,
				},
				{
					name:  "allow service account src0 with JWT to jwt-and-src0-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeOK,
					body:  "handled-by-egress-gateway",
					host:  fmt.Sprintf("jwt-and-%s-only.com", src0[0].Config().Service),
					from:  getWorkload(src0[0], t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "deny service account src1 with JWT to jwt-and-src0-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeForbidden,
					body:  "RBAC: access denied",
					host:  fmt.Sprintf("jwt-and-%s-only.com", src0[0].Config().Service),
					from:  getWorkload(src1[0], t),
					token: jwt.TokenIssuer1,
				},
				{
					name:  "deny service account src0 with wrong JWT to jwt-and-src0-only.com over mTLS",
					path:  "/",
					code:  response.StatusCodeForbidden,
					body:  "RBAC: access denied",
					host:  fmt.Sprintf("jwt-and-%s-only.com", src0[0].Config().Service),
					from:  getWorkload(src0[0], t),
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

// TestAuthorization_TCP tests the authorization policy on workloads using the raw TCP protocol.
func TestAuthorization_TCP(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.tcp").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			ns2 := apps.Namespace2
			a := apps.A.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			c := apps.C.Match(echo.Namespace(apps.Namespace2.Name()))

			newTestCase := func(name string, from, target echo.Instances, port string, expectAllowed bool, scheme scheme.Instance) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					NamePrefix: name,
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

			cases := []struct {
				configFile string
				configSrc  string
				configDst  string
				subcases   []rbacUtil.TestCase
			}{
				{
					configFile: "testdata/authz/v1beta1-tcp-1.yaml.tmpl",
					configDst:  util.BSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload b denies request with path "/data" to port 8090:
						// - request to port http-8091 should be denied because both path and port are matched.
						// - request to port http-8092 should be allowed because the port is not matched.
						// - request to port tcp-8093 should be allowed because the port is not matched.
						newTestCase("port and path/", a, b, "http-8091", false, scheme.HTTP),
						newTestCase("port and path/", a, b, "http-8092", true, scheme.HTTP),
						newTestCase("port and path/", a, b, "tcp-8093", true, scheme.TCP),
						newTestCase("port and path/", vm, b, "http-8091", false, scheme.HTTP),
						newTestCase("port and path/", vm, b, "http-8092", true, scheme.HTTP),
						newTestCase("port and path/", vm, b, "tcp-8093", true, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-1.yaml.tmpl",
					configDst:  util.VMSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload vm denies request with path "/data" to port 8090:
						// - request to port http-8091 should be denied because both path and port are matched.
						// - request to port http-8092 should be allowed because the port is not matched.
						// - request to port tcp-8093 should be allowed because the port is not matched.
						newTestCase("port and path/", c, vm, "http-8091", false, scheme.HTTP),
						newTestCase("port and path/", c, vm, "http-8092", true, scheme.HTTP),
						newTestCase("port and path/", c, vm, "tcp-8093", true, scheme.TCP),
						newTestCase("port and path/", b, vm, "http-8091", false, scheme.HTTP),
						newTestCase("port and path/", b, vm, "http-8092", true, scheme.HTTP),
						newTestCase("port and path/", b, vm, "tcp-8093", true, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-2.yaml.tmpl",
					configDst:  util.BSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload b denies request to port 8091:
						// - request to port http-8091 should be denied because the port is matched.
						// - request to http port 8092 should be allowed because the port is not matched.
						// - request to tcp port 8093 should be allowed because the port is not matched.
						// - request from c to tcp port 8093 should be denied because the namespace is matched.
						newTestCase("port and namespace/", a, b, "http-8091", false, scheme.HTTP),
						newTestCase("port and namespace/", a, b, "http-8092", true, scheme.HTTP),
						newTestCase("port and namespace/", a, b, "tcp-8093", true, scheme.TCP),
						newTestCase("port and namespace/", vm, b, "http-8091", false, scheme.HTTP),
						newTestCase("port and namespace/", vm, b, "http-8092", true, scheme.HTTP),
						newTestCase("port and namespace/", vm, b, "tcp-8093", true, scheme.TCP),
						newTestCase("port and namespace/", c, b, "tcp-8093", false, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-2.yaml.tmpl",
					configDst:  util.VMSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload vm denies request to port 8091:
						// - request to port http-8091 should be denied because the port is matched.
						// - request to http port 8092 should be allowed because the port is not matched.
						// - request to tcp port 8093 should be allowed because the port is not matched.
						// - request from c to tcp port 8093 should be denied because the namespace is matched.
						newTestCase("port and namespace/", a, vm, "http-8091", false, scheme.HTTP),
						newTestCase("port and namespace/", a, vm, "http-8092", true, scheme.HTTP),
						newTestCase("port and namespace/", a, vm, "tcp-8093", true, scheme.TCP),
						newTestCase("port and namespace/", b, vm, "http-8091", false, scheme.HTTP),
						newTestCase("port and namespace/", b, vm, "http-8092", true, scheme.HTTP),
						newTestCase("port and namespace/", b, vm, "tcp-8093", true, scheme.TCP),
						newTestCase("port and namespace/", c, vm, "tcp-8093", false, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-5.yaml.tmpl",
					configSrc:  util.ASvc,
					configDst:  util.BSvc,
					subcases: []rbacUtil.TestCase{
						// - request from a to tcp port 8094 should be denied because the principal is matched.
						// - request from vm to tcp port 8094 should be allowed by default.
						newTestCase("port and principal/", a, b, "tcp-8094", false, scheme.TCP),
						newTestCase("port and principal/", vm, b, "tcp-8094", true, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-5.yaml.tmpl",
					configSrc:  util.VMSvc,
					configDst:  util.BSvc,
					subcases: []rbacUtil.TestCase{
						// - request from vm to tcp port 8094 should be denied because the principal is matched.
						// - request from a to tcp port 8094 should be allowed by default.
						newTestCase("port and principal/", vm, b, "tcp-8094", false, scheme.TCP),
						newTestCase("port and principal/", a, b, "tcp-8094", true, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-5.yaml.tmpl",
					configSrc:  util.ASvc,
					configDst:  util.VMSvc,
					subcases: []rbacUtil.TestCase{
						// - request from a to tcp port 8094 should be denied because the principal is matched.
						// - request from b to tcp port 8094 should be allowed by default.
						newTestCase("port and principal/", a, vm, "tcp-8094", false, scheme.TCP),
						newTestCase("port and principal/", b, vm, "tcp-8094", true, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-3.yaml.tmpl",
					configSrc:  util.ASvc,
					configDst:  util.BSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload b denies request from service account a and workloads in namespace 2:
						// - request from a to b should be denied because it has service account a.
						// - request from vm to b should be allowed.
						// - request from c to a should be allowed because there is no policy on a.
						// - request from c to vm should be allowed because there is no policy on vm.
						// - request from c to b should be denied because it's in namespace 2.
						newTestCase("namespace and principal/", a, b, "tcp-8093", false, scheme.TCP),
						newTestCase("namespace and principal/", vm, b, "tcp-8093", true, scheme.TCP),
						newTestCase("namespace and principal/", c, a, "tcp-8093", true, scheme.TCP),
						newTestCase("namespace and principal/", c, vm, "tcp-8093", true, scheme.TCP),
						newTestCase("namespace and principal/", c, b, "tcp-8093", false, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-3.yaml.tmpl",
					configSrc:  util.ASvc,
					configDst:  util.VMSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload vm denies request from service account a and workloads in namespace 2:
						// - request from a to vm should be denied because it has service account a.
						// - request from b to vm should be allowed.
						// - request from c to a should be allowed because there is no policy on a.
						// - request from c to vm should be denied because it's in namespace 2.
						newTestCase("namespace and principal/", a, vm, "tcp-8093", false, scheme.TCP),
						newTestCase("namespace and principal/", b, vm, "tcp-8093", true, scheme.TCP),
						newTestCase("namespace and principal/", c, a, "tcp-8093", true, scheme.TCP),
						newTestCase("namespace and principal/", c, vm, "tcp-8093", false, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-4.yaml.tmpl",
					configDst:  util.BSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload e denies request with path "/other":
						// - request to port http-8091 should be allowed because the path is not matched.
						// - request to port http-8092 should be allowed because the path is not matched.
						// - request to port tcp-8093 should be denied because policy uses HTTP fields.
						newTestCase("path/", a, b, "http-8091", true, scheme.HTTP),
						newTestCase("path/", a, b, "http-8092", true, scheme.HTTP),
						newTestCase("path/", a, b, "tcp-8093", false, scheme.TCP),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-tcp-4.yaml.tmpl",
					configDst:  util.VMSvc,
					subcases: []rbacUtil.TestCase{
						// The policy on workload e denies request with path "/other":
						// - request to port http-8091 should be allowed because the path is not matched.
						// - request to port http-8092 should be allowed because the path is not matched.
						// - request to port tcp-8093 should be denied because policy uses HTTP fields.
						newTestCase("path/", a, vm, "http-8091", true, scheme.HTTP),
						newTestCase("path/", a, vm, "http-8092", true, scheme.HTTP),
						newTestCase("path/", a, vm, "tcp-8093", false, scheme.TCP),
					},
				},
			}

			for _, tc := range cases {
				t.NewSubTest(tc.configDst).
					Run(func(t framework.TestContext) {
						policy := tmpl.EvaluateAllOrFail(t, map[string]string{
							"Namespace":  ns.Name(),
							"Namespace2": ns2.Name(),
							"src":        tc.configSrc,
							"dst":        tc.configDst,
						}, file.AsStringOrFail(t, tc.configFile))
						t.Config().ApplyYAMLOrFail(t, "", policy...)
						rbacUtil.RunRBACTest(t, tc.subcases)
					})
			}
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
			for _, dst := range []echo.Instances{c, vm} {
				for i := 0; i < len(t.Clusters()); i++ {
					t.NewSubTest(fmt.Sprintf("from %s to %s in %s",
						t.Clusters()[i].StableName(), dst[0].Config().Service, dst[0].Config().Cluster.Name())).
						Run(func(t framework.TestContext) {
							src1 := apps.A.Match(echo.InCluster(t.Clusters()[i])).Match(echo.Namespace(nsA.Name()))
							src2 := apps.B.Match(echo.InCluster(t.Clusters()[i])).Match(echo.Namespace(nsB.Name()))
							var ipList string
							for i := 0; i < len(dst); i++ {
								ipList += "\"" + getWorkload(dst[i], t).Address() + "\","
							}
							ipLen := len(ipList)
							ipList = ipList[:ipLen-1]
							args := map[string]string{
								"dst":           dst[0].Config().Service,
								"dstIP":         ipList,
								"dstNamespace":  dst[0].Config().Namespace.Name(),
								"dstPort":       "8090",
								"src1":          util.ASvc,
								"src2":          util.BSvc,
								"src1IP":        getWorkload(src1[0], t).Address(),
								"src2IP":        getWorkload(src2[0], t).Address(),
								"src1Namespace": nsA.Name(),
								"src2Namespace": nsB.Name(),
							}

							policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, "testdata/authz/v1beta1-conditions.yaml.tmpl"))
							t.Config().ApplyYAMLOrFail(t, "", policies...)
							callCount := 1
							if t.Clusters().IsMulticluster() {
								// so we can validate all clusters are hit
								callCount = util.CallsPerCluster * len(t.Clusters())
							}
							newTestCase := func(from echo.Instances, path string, headers map[string]string, expectAllowed bool) rbacUtil.TestCase {
								// Not all workloads are deployed in every cluster, skip the test case if there are no instances.
								if len(from) == 0 && t.Clusters().IsMulticluster() {
									return rbacUtil.TestCase{
										SkippedForMulticluster: true,
									}
								}
								return rbacUtil.TestCase{
									Request: connection.Checker{
										From: from[0],
										Options: echo.CallOptions{
											Target:   dst[0],
											PortName: "http",
											Scheme:   scheme.HTTP,
											Path:     path,
											Count:    callCount,
										},
										DestClusters: dst.Clusters(),
									},
									Headers:       headers,
									ExpectAllowed: expectAllowed,
								}
							}
							cases := []rbacUtil.TestCase{
								newTestCase(src1, "/request-headers", map[string]string{"x-foo": "foo"}, true),
								newTestCase(src2, "/request-headers", map[string]string{"x-foo": "foo"}, true),
								newTestCase(src1, "/request-headers", map[string]string{"x-foo": "bar"}, false),
								newTestCase(src2, "/request-headers", map[string]string{"x-foo": "bar"}, false),
								newTestCase(src1, "/request-headers", nil, false),
								newTestCase(src2, "/request-headers", nil, false),
								newTestCase(src1, "/request-headers-notValues-bar", map[string]string{"x-foo": "foo"}, true),
								newTestCase(src1, "/request-headers-notValues-bar", map[string]string{"x-foo": "bar"}, false),

								newTestCase(src1, fmt.Sprintf("/source-ip-%s", args["src1"]), nil, true),
								newTestCase(src2, fmt.Sprintf("/source-ip-%s", args["src1"]), nil, false),
								newTestCase(src1, fmt.Sprintf("/source-ip-%s", args["src2"]), nil, false),
								newTestCase(src2, fmt.Sprintf("/source-ip-%s", args["src2"]), nil, true),
								newTestCase(src1, fmt.Sprintf("/source-ip-notValues-%s", args["src2"]), nil, true),
								newTestCase(src2, fmt.Sprintf("/source-ip-notValues-%s", args["src2"]), nil, false),

								newTestCase(src1, fmt.Sprintf("/source-namespace-%s", args["src1"]), nil, true),
								newTestCase(src2, fmt.Sprintf("/source-namespace-%s", args["src1"]), nil, false),
								newTestCase(src1, fmt.Sprintf("/source-namespace-%s", args["src2"]), nil, false),
								newTestCase(src2, fmt.Sprintf("/source-namespace-%s", args["src2"]), nil, true),
								newTestCase(src1, fmt.Sprintf("/source-namespace-notValues-%s", args["src2"]), nil, true),
								newTestCase(src2, fmt.Sprintf("/source-namespace-notValues-%s", args["src2"]), nil, false),

								newTestCase(src1, fmt.Sprintf("/source-principal-%s", args["src1"]), nil, true),
								newTestCase(src2, fmt.Sprintf("/source-principal-%s", args["src1"]), nil, false),
								newTestCase(src1, fmt.Sprintf("/source-principal-%s", args["src2"]), nil, false),
								newTestCase(src2, fmt.Sprintf("/source-principal-%s", args["src2"]), nil, true),
								newTestCase(src1, fmt.Sprintf("/source-principal-notValues-%s", args["src2"]), nil, true),
								newTestCase(src2, fmt.Sprintf("/source-principal-notValues-%s", args["src2"]), nil, false),

								newTestCase(src1, "/destination-ip-good", nil, true),
								newTestCase(src2, "/destination-ip-good", nil, true),
								newTestCase(src1, "/destination-ip-bad", nil, false),
								newTestCase(src2, "/destination-ip-bad", nil, false),
								newTestCase(src1, fmt.Sprintf("/destination-ip-notValues-%s-or-%s", args["src1"], args["src2"]), nil, true),
								newTestCase(src1, fmt.Sprintf("/destination-ip-notValues-%s-or-%s-or-%s", args["src1"], args["src2"], args["dst"]), nil, false),

								newTestCase(src1, "/destination-port-good", nil, true),
								newTestCase(src2, "/destination-port-good", nil, true),
								newTestCase(src1, "/destination-port-bad", nil, false),
								newTestCase(src2, "/destination-port-bad", nil, false),
								newTestCase(src1, fmt.Sprintf("/destination-port-notValues-%s", args["dst"]), nil, false),
								newTestCase(src2, fmt.Sprintf("/destination-port-notValues-%s", args["dst"]), nil, false),

								newTestCase(src1, "/connection-sni-good", nil, true),
								newTestCase(src2, "/connection-sni-good", nil, true),
								newTestCase(src1, "/connection-sni-bad", nil, false),
								newTestCase(src2, "/connection-sni-bad", nil, false),
								newTestCase(src1, fmt.Sprintf("/connection-sni-notValues-%s-or-%s", args["src1"], args["src2"]), nil, true),
								newTestCase(src1, fmt.Sprintf("/connection-sni-notValues-%s-or-%s-or-%s", args["src1"], args["src2"], args["dst"]), nil, false),

								newTestCase(src1, "/other", nil, false),
								newTestCase(src2, "/other", nil, false),
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

			for _, dst := range []echo.Instances{a, vm} {
				for _, src := range [][]echo.Instances{{b, c, d}, {vm, c, d}} {
					t.NewSubTest(fmt.Sprintf("from %s to %s in %s",
						src[0][0].Config().Cluster.StableName(), dst[0].Config().Service, dst[0].Config().Cluster.Name())).
						Run(func(t framework.TestContext) {
							args := map[string]string{
								"Namespace": ns.Name(),
								"dst":       dst[0].Config().Service,
								"src0":      src[0][0].Config().Service,
								"src1":      src[1][0].Config().Service,
								"src2":      src[2][0].Config().Service,
							}
							policies := tmpl.EvaluateAllOrFail(t, args,
								file.AsStringOrFail(t, "testdata/authz/v1beta1-grpc.yaml.tmpl"))
							t.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)

							cases := []rbacUtil.TestCase{
								{
									Request: connection.Checker{
										From: src[0][0],
										Options: echo.CallOptions{
											Target:   dst[0],
											PortName: "grpc",
											Scheme:   scheme.GRPC,
										},
									},
									ExpectAllowed: true,
								},
								{
									Request: connection.Checker{
										From: src[1][0],
										Options: echo.CallOptions{
											Target:   dst[0],
											PortName: "grpc",
											Scheme:   scheme.GRPC,
										},
									},
									ExpectAllowed: false,
								},
								{
									Request: connection.Checker{
										From: src[2][0],
										Options: echo.CallOptions{
											Target:   dst[0],
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

			for _, dst := range []echo.Instances{a, vm} {
				for _, srcCluster := range t.Clusters() {
					t.NewSubTest(fmt.Sprintf("In %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
						src := apps.B.GetOrFail(t, echo.InCluster(srcCluster).And(echo.Namespace(ns.Name())))
						args := map[string]string{
							"Namespace": ns.Name(),
							"dst":       dst[0].Config().Service,
						}
						policies := tmpl.EvaluateAllOrFail(t, args,
							file.AsStringOrFail(t, "testdata/authz/v1beta1-path.yaml.tmpl"))
						t.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)

						callCount := 1
						if t.Clusters().IsMulticluster() {
							// so we can validate all clusters are hit
							callCount = util.CallsPerCluster * len(t.Clusters())
						}

						newTestCase := func(to echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
							return rbacUtil.TestCase{
								Request: connection.Checker{
									From: src,
									Options: echo.CallOptions{
										Target:   to[0],
										PortName: "http",
										Scheme:   scheme.HTTP,
										Path:     path,
										Count:    callCount,
									},
									DestClusters: dst.Clusters(),
								},
								ExpectAllowed: expectAllowed,
							}
						}
						cases := []rbacUtil.TestCase{
							newTestCase(dst, "/public", true),
							newTestCase(dst, "/public/../public", true),
							newTestCase(dst, "/private", false),
							newTestCase(dst, "/public/../private", false),
							newTestCase(dst, "/public/./../private", false),
							newTestCase(dst, "/public/.././private", false),
							newTestCase(dst, "/public/%2E%2E/private", false),
							newTestCase(dst, "/public/%2e%2e/private", false),
							newTestCase(dst, "/public/%2E/%2E%2E/private", false),
							newTestCase(dst, "/public/%2e/%2e%2e/private", false),
							newTestCase(dst, "/public/%2E%2E/%2E/private", false),
							newTestCase(dst, "/public/%2e%2e/%2e/private", false),
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
			// TODO: finish convert all authorization tests into multicluster supported
			if t.Clusters().IsMulticluster() {
				t.Skip()
			}
			ns := apps.Namespace1
			a := apps.A.Match(echo.Namespace(apps.Namespace1.Name()))
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))

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

			cases := []struct {
				configFile string
				dst        echo.Instances
				subCases   []rbacUtil.TestCase
			}{
				{
					configFile: "testdata/authz/v1beta1-audit-allow.yaml.tmpl",
					dst:        a,
					subCases: []rbacUtil.TestCase{
						newTestCase(b, a, "/allow", true),
						newTestCase(b, a, "/audit", false),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-audit-deny.yaml.tmpl",
					dst:        a,
					subCases: []rbacUtil.TestCase{
						newTestCase(b, a, "/audit", true),
						newTestCase(b, a, "/deny", false),
					},
				},
				{
					configFile: "testdata/authz/v1beta1-audit-default.yaml.tmpl",
					dst:        a,
					subCases: []rbacUtil.TestCase{
						newTestCase(b, a, "/audit", true),
						newTestCase(b, a, "/other", true),
					},
				},
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

			for _, tc := range cases {
				t.NewSubTest(fmt.Sprintf("from %s to %s in %s",
					b[0].Config().Cluster.StableName(), tc.dst[0].Config().Service, tc.dst[0].Config().Cluster.Name())).
					Run(func(t framework.TestContext) {
						args := map[string]string{
							"Namespace": ns.Name(),
							"dst":       tc.dst[0].Config().Service,
						}
						policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, tc.configFile))
						t.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
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
				t.Config().ApplyYAMLOrFail(t, namespace, policy...)
			}

			// Deploy and wait for the ext-authz server to be ready.
			if extAuthzServiceNamespace == nil {
				t.Fatalf("Failed to create namespace for ext-authz server: %v", extAuthzServiceNamespaceErr)
			}
			applyYAML("../../../samples/extauthz/ext-authz.yaml", extAuthzServiceNamespace.Name())
			if _, _, err := kube.WaitUntilServiceEndpointsAreReady(t.Clusters().Default(), extAuthzServiceNamespace.Name(), "ext-authz"); err != nil {
				t.Fatalf("Wait for ext-authz server failed: %v", err)
			}
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

			newTestCase := func(from, target echo.Instance, path, port string, header string, expectAllowed bool, scheme scheme.Instance) rbacUtil.TestCase {
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
					Headers:       map[string]string{"x-ext-authz": header},
					ExpectAllowed: expectAllowed,
				}
			}
			// Path "/custom" is protected by ext-authz service and is accessible with the header `x-ext-authz: allow`.
			// Path "/health" is not protected and is accessible to public.
			cases := []rbacUtil.TestCase{
				// workload b is using an ext-authz service in its own pod of HTTP API.
				newTestCase(x, b, "/custom", "http", "allow", true, scheme.HTTP),
				newTestCase(x, b, "/custom", "http", "deny", false, scheme.HTTP),
				newTestCase(x, b, "/health", "http", "allow", true, scheme.HTTP),
				newTestCase(x, b, "/health", "http", "deny", true, scheme.HTTP),

				// workload c is using an ext-authz service in its own pod of gRPC API.
				newTestCase(x, c, "/custom", "http", "allow", true, scheme.HTTP),
				newTestCase(x, c, "/custom", "http", "deny", false, scheme.HTTP),
				newTestCase(x, c, "/health", "http", "allow", true, scheme.HTTP),
				newTestCase(x, c, "/health", "http", "deny", true, scheme.HTTP),

				// workload d is using an local ext-authz service in the same pod as the application of HTTP API.
				newTestCase(x, d, "/custom", "http", "allow", true, scheme.HTTP),
				newTestCase(x, d, "/custom", "http", "deny", false, scheme.HTTP),
				newTestCase(x, d, "/health", "http", "allow", true, scheme.HTTP),
				newTestCase(x, d, "/health", "http", "deny", true, scheme.HTTP),

				// workload e is using an local ext-authz service in the same pod as the application of gRPC API.
				newTestCase(x, e, "/custom", "http", "allow", true, scheme.HTTP),
				newTestCase(x, e, "/custom", "http", "deny", false, scheme.HTTP),
				newTestCase(x, e, "/health", "http", "allow", true, scheme.HTTP),
				newTestCase(x, e, "/health", "http", "deny", true, scheme.HTTP),

				// workload f is using an ext-authz service in its own pod of TCP API.
				newTestCase(a, f, "", "tcp-8092", "", true, scheme.TCP),
				newTestCase(x, f, "", "tcp-8092", "", false, scheme.TCP),
				newTestCase(a, f, "", "tcp-8093", "", true, scheme.TCP),
				newTestCase(x, f, "", "tcp-8093", "", true, scheme.TCP),
			}

			rbacUtil.RunRBACTest(t, cases)

			ingr := ist.IngressFor(t.Clusters().Default())
			ingressCases := []rbacUtil.TestCase{
				// workload g is using an ext-authz service in its own pod of HTTP API.
				newTestCase(x, g, "/custom", "http", "allow", true, scheme.HTTP),
				newTestCase(x, g, "/custom", "http", "deny", false, scheme.HTTP),
				newTestCase(x, g, "/health", "http", "allow", true, scheme.HTTP),
				newTestCase(x, g, "/health", "http", "deny", true, scheme.HTTP),
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
