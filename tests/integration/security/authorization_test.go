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
			for _, cluster := range t.Clusters() {
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
						// a and c send requests to b
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
			for _, srcCluster := range t.Clusters() {
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
			for _, srcCluster := range t.Clusters() {
				a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(apps.Namespace1.Name())))
				bInNS1 := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
				vmInNS1 := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
				cInNS1 := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
				cInNS2 := apps.C.Match(echo.Namespace(apps.Namespace2.Name()))
				for _, dst := range []echo.Instances{bInNS1, vmInNS1, cInNS1, cInNS2} {
					ns1 := apps.Namespace1
					ns2 := apps.Namespace2
					rootns := newRootNS(t)
					args := map[string]string{
						"Namespace1":    ns1.Name(),
						"Namespace2":    ns2.Name(),
						"RootNamespace": rootns.rootNamespace,
						"dst":           dst[0].Config().Service,
					}
					applyPolicy := func(filename string, ns namespace.Instance) {
						policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
						t.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
					}

					applyPolicy("testdata/authz/v1beta1-workload-ns1.yaml.tmpl", ns1)
					applyPolicy("testdata/authz/v1beta1-workload-ns2.yaml.tmpl", ns2)
					applyPolicy("testdata/authz/v1beta1-workload-ns-root.yaml.tmpl", rootns)
					t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
						callCount := 1
						if t.Clusters().IsMulticluster() {
							// so we can validate all clusters are hit
							callCount = util.CallsPerCluster * len(t.Clusters())
						}
						newTestCase := func(namePrefix string, target echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
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
								ExpectAllowed: expectAllowed,
							}
						}

						newTestCases := func(target echo.Instances) []rbacUtil.TestCase {
							cases := []rbacUtil.TestCase{}
							targetNS := target[0].Config().Namespace.Name()
							targetName := target[0].Config().Service
							for _, ns := range []string{ns1.Name(), ns2.Name(), rootns.rootNamespace} {
								for _, dstName := range []string{util.BSvc, util.CSvc, util.VMSvc, "x", "all"} {
									prefix := fmt.Sprintf("[%sIn%s]", targetName, targetNS)
									path := fmt.Sprintf("/policy-%s-%s", ns, dstName)
									if ns == targetNS && (dstName == targetName || dstName == "all") {
										cases = append(cases, newTestCase(prefix, target, path, true))
									} else if ns == rootns.rootNamespace && dstName == targetName {
										cases = append(cases, newTestCase(prefix, target, path, true))
									} else {
										cases = append(cases, newTestCase(prefix, target, path, false))
									}
								}
							}
							return cases
						}

						cases := newTestCases(dst)
						rbacUtil.RunRBACTest(t, cases)
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
			for _, srcCluster := range t.Clusters() {
				ns := apps.Namespace1
				rootns := newRootNS(t)
				b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
				c := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
				vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
				for _, dst := range [][]echo.Instances{{b, c}, {b, vm}, {vm, c}} {
					args := map[string]string{
						"Namespace":     ns.Name(),
						"RootNamespace": rootns.rootNamespace,
						"dst0":          dst[0][0].Config().Service,
						"dst1":          dst[1][0].Config().Service,
					}

					applyPolicy := func(filename string, ns namespace.Instance) []string {
						policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
						t.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
						return policy
					}

					policy := applyPolicy("testdata/authz/v1beta1-deny.yaml.tmpl", ns)
					util.WaitForConfig(t, policy[0], ns)
					policyNSRoot := applyPolicy("testdata/authz/v1beta1-deny-ns-root.yaml.tmpl", rootns)
					util.WaitForConfig(t, policyNSRoot[0], rootns)

					callCount := 1
					if t.Clusters().IsMulticluster() {
						// so we can validate all clusters are hit
						callCount = util.CallsPerCluster * len(t.Clusters())
					}

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
							newTestCase(dst[0], "/deny", false),
							newTestCase(dst[0], "/deny?param=value", false),
							newTestCase(dst[0], "/global-deny", false),
							newTestCase(dst[0], "/global-deny?param=value", false),
							newTestCase(dst[0], "/other", true),
							newTestCase(dst[0], "/other?param=value", true),
							newTestCase(dst[0], "/allow", true),
							newTestCase(dst[0], "/allow?param=value", true),
							newTestCase(dst[1], "/allow/admin", false),
							newTestCase(dst[1], "/allow/admin?param=value", false),
							newTestCase(dst[1], "/global-deny", false),
							newTestCase(dst[1], "/global-deny?param=value", false),
							newTestCase(dst[1], "/other", false),
							newTestCase(dst[1], "/other?param=value", false),
							newTestCase(dst[1], "/allow", true),
							newTestCase(dst[1], "/allow?param=value", true),
						}

						rbacUtil.RunRBACTest(t, cases)
					})
				}
			}
		})
}

// TestAuthorization_NegativeMatch tests the authorization policy with negative match.
func TestAuthorization_NegativeMatch(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			for _, srcCluster := range t.Clusters() {
				ns := apps.Namespace1
				ns2 := apps.Namespace2
				b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
				c := apps.C.Match(echo.Namespace(apps.Namespace1.Name()))
				d := apps.D.Match(echo.Namespace(apps.Namespace1.Name()))
				vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
				for _, dst := range [][]echo.Instances{{b, c, d}, {vm, c, d}} {
					args := map[string]string{
						"Namespace":  ns.Name(),
						"Namespace2": ns2.Name(),
						"dst0":       dst[0][0].Config().Service,
						"dst1":       dst[1][0].Config().Service,
						"dst2":       dst[2][0].Config().Service,
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
							newTestCase(srcA[0], dst[0], "/prefix", false),
							newTestCase(srcA[0], dst[0], "/prefix/other", false),
							newTestCase(srcA[0], dst[0], "/prefix/allowlist", true),
							newTestCase(srcA[0], dst[0], "/allow", true),
							newTestCase(srcBInNS2[0], dst[0], "/prefix", false),
							newTestCase(srcBInNS2[0], dst[0], "/prefix/other", false),
							newTestCase(srcBInNS2[0], dst[0], "/prefix/allowlist", true),
							newTestCase(srcBInNS2[0], dst[0], "/allow", true),

							// Test the policy that denies other namespace on dst1.
							// a should be allowed because it's from the same namespace.
							// bInNs2 should be denied because it's from a different namespace.
							newTestCase(srcA[0], dst[1], "/", true),
							newTestCase(srcBInNS2[0], dst[1], "/", false),

							// Test the policy that denies plain-text traffic on dst2.
							// a should be allowed because it's using mTLS.
							// bInNs2 should be denied because it's using plain-text.
							newTestCase(srcA[0], dst[2], "/", true),
							newTestCase(srcBInNS2[0], dst[2], "/", false),
						}

						rbacUtil.RunRBACTest(t, cases)
					})
				}
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
			a := apps.A.Match(echo.Namespace(apps.Namespace1.Name()))
			b := apps.B.Match(echo.Namespace(apps.Namespace1.Name()))
			vm := apps.VM.Match(echo.Namespace(apps.Namespace1.Name()))
			// Set up workload X that is accessible via egress gateway.
			var x echo.Instance
			XSvc := "x"
			echoboot.NewBuilder(t).
				With(&x, echo.Config{
					Service:   XSvc,
					Namespace: ns,
					Subsets:   []echo.SubsetConfig{{}},
					Ports: []echo.Port{
						{
							Name:        "http",
							Protocol:    protocol.HTTP,
							ServicePort: 8090,
						},
					},
				}).
				BuildOrFail(t)
			for _, src := range [][]echo.Instances{{a, b}, {vm, b}} {
				args := map[string]string{
					"Namespace":     ns.Name(),
					"RootNamespace": rootns.rootNamespace,
					"src":           src[0][0].Config().Service,
					"dst":           XSvc,
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
						from: getWorkload(src[0][0], t),
					},
					{
						name: "deny path to company.com",
						path: "/deny",
						code: response.StatusCodeForbidden,
						body: "RBAC: access denied",
						host: "www.company.com",
						from: getWorkload(src[0][0], t),
					},
					{
						name: "allow service account src0 to src0-only.com over mTLS",
						path: "/",
						code: response.StatusCodeOK,
						body: "handled-by-egress-gateway",
						host: fmt.Sprintf("%s-only.com", src[0][0].Config().Service),
						from: getWorkload(src[0][0], t),
					},
					{
						name: "deny service account src1 to src0-only.com over mTLS",
						path: "/",
						code: response.StatusCodeForbidden,
						body: "RBAC: access denied",
						host: fmt.Sprintf("%s-only.com", src[0][0].Config().Service),
						from: getWorkload(src[1][0], t),
					},
					{
						name:  "allow src0 with JWT to jwt-only.com over mTLS",
						path:  "/",
						code:  response.StatusCodeOK,
						body:  "handled-by-egress-gateway",
						host:  "jwt-only.com",
						from:  getWorkload(src[0][0], t),
						token: jwt.TokenIssuer1,
					},
					{
						name:  "allow src1 with JWT to jwt-only.com over mTLS",
						path:  "/",
						code:  response.StatusCodeOK,
						body:  "handled-by-egress-gateway",
						host:  "jwt-only.com",
						from:  getWorkload(src[1][0], t),
						token: jwt.TokenIssuer1,
					},
					{
						name:  "deny src1 with wrong JWT to jwt-only.com over mTLS",
						path:  "/",
						code:  response.StatusCodeForbidden,
						body:  "RBAC: access denied",
						host:  "jwt-only.com",
						from:  getWorkload(src[1][0], t),
						token: jwt.TokenIssuer2,
					},
					{
						name:  "allow service account src0 with JWT to jwt-and-src0-only.com over mTLS",
						path:  "/",
						code:  response.StatusCodeOK,
						body:  "handled-by-egress-gateway",
						host:  fmt.Sprintf("jwt-and-%s-only.com", src[0][0].Config().Service),
						from:  getWorkload(src[0][0], t),
						token: jwt.TokenIssuer1,
					},
					{
						name:  "deny service account src1 with JWT to jwt-and-src0-only.com over mTLS",
						path:  "/",
						code:  response.StatusCodeForbidden,
						body:  "RBAC: access denied",
						host:  fmt.Sprintf("jwt-and-%s-only.com", src[0][0].Config().Service),
						from:  getWorkload(src[1][0], t),
						token: jwt.TokenIssuer1,
					},
					{
						name:  "deny service account src0 with wrong JWT to jwt-and-src0-only.com over mTLS",
						path:  "/",
						code:  response.StatusCodeForbidden,
						body:  "RBAC: access denied",
						host:  fmt.Sprintf("jwt-and-%s-only.com", src[0][0].Config().Service),
						from:  getWorkload(src[0][0], t),
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
			}
		})
}

// TestAuthorization_TCP tests the authorization policy on workloads using the raw TCP protocol.
func TestAuthorization_TCP(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.tcp").
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "v1beta1-tcp-1",
				Inject: true,
			})
			ns2 := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "v1beta1-tcp-2",
				Inject: true,
			})
			policy := tmpl.EvaluateAllOrFail(t, map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
			}, file.AsStringOrFail(t, "testdata/authz/v1beta1-tcp.yaml.tmpl"))
			t.Config().ApplyYAMLOrFail(t, "", policy...)

			var a, b, c, d, e, x echo.Instance
			ports := []echo.Port{
				{
					Name:         "http-8090",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				},
				{
					Name:         "http-8091",
					Protocol:     protocol.HTTP,
					InstancePort: 8091,
				},
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
			}
			echoboot.NewBuilder(t).
				With(&x, util.EchoConfig("x", ns2, false, nil)).
				With(&a, echo.Config{
					Subsets:        []echo.SubsetConfig{{}},
					Namespace:      ns,
					Service:        "a",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&b, echo.Config{
					Namespace:      ns,
					Subsets:        []echo.SubsetConfig{{}},
					Service:        "b",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&c, echo.Config{
					Namespace:      ns,
					Subsets:        []echo.SubsetConfig{{}},
					Service:        "c",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&d, echo.Config{
					Namespace:      ns,
					Subsets:        []echo.SubsetConfig{{}},
					Service:        "d",
					Ports:          ports,
					ServiceAccount: true,
				}).
				With(&e, echo.Config{
					Namespace:      ns,
					Service:        "e",
					Ports:          ports,
					ServiceAccount: true,
				}).
				BuildOrFail(t)

			newTestCase := func(from, target echo.Instance, port string, expectAllowed bool, scheme scheme.Instance) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: from,
						Options: echo.CallOptions{
							Target:   target,
							PortName: port,
							Scheme:   scheme,
							Path:     "/data",
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}

			cases := []rbacUtil.TestCase{
				// The policy on workload b denies request with path "/data" to port 8090:
				// - request to port http-8090 should be denied because both path and port are matched.
				// - request to port http-8091 should be allowed because the port is not matched.
				// - request to port tcp-8092 should be allowed because the port is not matched.
				newTestCase(a, b, "http-8090", false, scheme.HTTP),
				newTestCase(a, b, "http-8091", true, scheme.HTTP),
				newTestCase(a, b, "tcp-8092", true, scheme.TCP),

				// The policy on workload c denies request to port 8090:
				// - request to port http-8090 should be denied because the port is matched.
				// - request to http port 8091 should be allowed because the port is not matched.
				// - request to tcp port 8092 should be allowed because the port is not matched.
				// - request from b to tcp port 8092 should be allowed by default.
				// - request from b to tcp port 8093 should be denied because the principal is matched.
				// - request from x to tcp port 8092 should be denied because the namespace is matched.
				// - request from x to tcp port 8093 should be allowed by default.
				newTestCase(a, c, "http-8090", false, scheme.HTTP),
				newTestCase(a, c, "http-8091", true, scheme.HTTP),
				newTestCase(a, c, "tcp-8092", true, scheme.TCP),
				newTestCase(b, c, "tcp-8092", true, scheme.TCP),
				newTestCase(b, c, "tcp-8093", false, scheme.TCP),
				newTestCase(x, c, "tcp-8092", false, scheme.TCP),
				newTestCase(x, c, "tcp-8093", true, scheme.TCP),

				// The policy on workload d denies request from service account a and workloads in namespace 2:
				// - request from a to d should be denied because it has service account a.
				// - request from b to d should be allowed.
				// - request from c to d should be allowed.
				// - request from x to a should be allowed because there is no policy on a.
				// - request from x to d should be denied because it's in namespace 2.
				newTestCase(a, d, "tcp-8092", false, scheme.TCP),
				newTestCase(b, d, "tcp-8092", true, scheme.TCP),
				newTestCase(c, d, "tcp-8092", true, scheme.TCP),
				newTestCase(x, a, "tcp-8092", true, scheme.TCP),
				newTestCase(x, d, "tcp-8092", false, scheme.TCP),

				// The policy on workload e denies request with path "/other":
				// - request to port http-8090 should be allowed because the path is not matched.
				// - request to port http-8091 should be allowed because the path is not matched.
				// - request to port tcp-8092 should be denied because policy uses HTTP fields.
				newTestCase(a, e, "http-8090", true, scheme.HTTP),
				newTestCase(a, e, "http-8091", true, scheme.HTTP),
				newTestCase(a, e, "tcp-8092", false, scheme.TCP),
			}

			rbacUtil.RunRBACTest(t, cases)
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

			cSet := apps.C.Match(echo.Namespace(nsC.Name()))

			var IPC string
			for i := 0; i < len(t.Clusters()); i++ {
				IPC += "\"" + getWorkload(cSet[i], t).Address() + "\","
			}
			lengthC := len(IPC)
			IPC = IPC[:lengthC-1]
			portC := 8090
			for i := 0; i < len(t.Clusters()); i++ {
				t.NewSubTest(fmt.Sprintf("IpA IpB IpC in %s", t.Clusters()[i].StableName())).Run(func(t framework.TestContext) {
					podAWithIPA := apps.A.Match(echo.InCluster(t.Clusters()[i])).Match(echo.Namespace(nsA.Name()))[0]
					podBWithIPB := apps.B.Match(echo.InCluster(t.Clusters()[i])).Match(echo.Namespace(nsB.Name()))[0]
					vm := apps.VM.Match(echo.InCluster(t.Clusters()[i])).Match(echo.Namespace(nsA.Name()))[0]

					args := map[string]string{
						"NamespaceA": nsA.Name(),
						"NamespaceB": nsB.Name(),
						"NamespaceC": nsC.Name(),
						"IpA":        getWorkload(podAWithIPA, t).Address(),
						"IpB":        getWorkload(podBWithIPB, t).Address(),
						"IpC":        IPC,
						"ipVM":       getWorkload(vm, t).Address(),
						"PortC":      fmt.Sprintf("%d", portC),
					}

					policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, "testdata/authz/v1beta1-conditions.yaml.tmpl"))
					t.Config().ApplyYAMLOrFail(t, "", policies...)

					for _, srcCluster := range t.Clusters() {
						t.NewSubTest(fmt.Sprintf("From %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
							a := apps.A.Match(echo.InCluster(srcCluster).And(echo.Namespace(nsA.Name())))
							vm := apps.VM.Match(echo.InCluster(srcCluster).And(echo.Namespace(nsA.Name())))
							b := apps.B.Match(echo.InCluster(srcCluster).And(echo.Namespace(nsB.Name())))
							callCount := 1
							if t.Clusters().IsMulticluster() {
								// so we can validate all clusters are hit
								callCount = util.CallsPerCluster * len(t.Clusters())
							}
							newTestCase := func(from echo.Instances, path string, headers map[string]string, expectAllowed bool) rbacUtil.TestCase {
								if len(from) == 0 && t.Clusters().IsMulticluster() {
									return rbacUtil.TestCase{
										SkippedForMulticluster: true,
									}
								}
								return rbacUtil.TestCase{
									Request: connection.Checker{
										From: from[0],
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
								newTestCase(vm, "/request-headers", map[string]string{"x-foo": "foo"}, true),
								newTestCase(b, "/request-headers", map[string]string{"x-foo": "foo"}, true),
								newTestCase(a, "/request-headers", map[string]string{"x-foo": "bar"}, false),
								newTestCase(vm, "/request-headers", map[string]string{"x-foo": "bar"}, false),
								newTestCase(b, "/request-headers", map[string]string{"x-foo": "bar"}, false),
								newTestCase(a, "/request-headers", nil, false),
								newTestCase(vm, "/request-headers", nil, false),
								newTestCase(b, "/request-headers", nil, false),
								newTestCase(a, "/request-headers-notValues-bar", map[string]string{"x-foo": "foo"}, true),
								newTestCase(vm, "/request-headers-notValues-bar", map[string]string{"x-foo": "foo"}, true),
								newTestCase(a, "/request-headers-notValues-bar", map[string]string{"x-foo": "bar"}, false),

								newTestCase(a, "/source-ip-a", nil, true),
								newTestCase(vm, "/source-ip-vm", nil, true),
								newTestCase(b, "/source-ip-a", nil, false),
								newTestCase(a, "/source-ip-b", nil, false),
								newTestCase(vm, "/source-ip-b", nil, false),
								newTestCase(b, "/source-ip-b", nil, true),
								newTestCase(a, "/source-ip-notValues-b", nil, true),
								newTestCase(vm, "/source-ip-notValues-b", nil, true),
								newTestCase(b, "/source-ip-notValues-b", nil, false),

								newTestCase(a, "/source-namespace-a", nil, true),
								newTestCase(vm, "/source-namespace-a", nil, true),
								newTestCase(b, "/source-namespace-a", nil, false),
								newTestCase(a, "/source-namespace-b", nil, false),
								newTestCase(vm, "/source-namespace-b", nil, false),
								newTestCase(b, "/source-namespace-b", nil, true),
								newTestCase(a, "/source-namespace-notValues-b", nil, true),
								newTestCase(vm, "/source-namespace-notValues-b", nil, true),
								newTestCase(b, "/source-namespace-notValues-b", nil, false),

								newTestCase(a, "/source-principal-a", nil, true),
								newTestCase(vm, "/source-principal-vm", nil, true),
								newTestCase(b, "/source-principal-a", nil, false),
								newTestCase(a, "/source-principal-b", nil, false),
								newTestCase(vm, "/source-principal-b", nil, false),
								newTestCase(b, "/source-principal-b", nil, true),
								newTestCase(a, "/source-principal-notValues-b", nil, true),
								newTestCase(vm, "/source-principal-notValues-b", nil, true),
								newTestCase(b, "/source-principal-notValues-b", nil, false),

								newTestCase(a, "/destination-ip-good", nil, true),
								newTestCase(vm, "/destination-ip-good", nil, true),
								newTestCase(b, "/destination-ip-good", nil, true),
								newTestCase(a, "/destination-ip-bad", nil, false),
								newTestCase(vm, "/destination-ip-bad", nil, false),
								newTestCase(b, "/destination-ip-bad", nil, false),
								newTestCase(a, "/destination-ip-notValues-a-or-b", nil, true),
								newTestCase(vm, "/destination-ip-notValues-a-or-b", nil, true),
								newTestCase(a, "/destination-ip-notValues-a-or-b-or-c", nil, false),
								newTestCase(vm, "/destination-ip-notValues-a-or-b-or-c", nil, false),

								newTestCase(a, "/destination-port-good", nil, true),
								newTestCase(vm, "/destination-port-good", nil, true),
								newTestCase(b, "/destination-port-good", nil, true),
								newTestCase(a, "/destination-port-bad", nil, false),
								newTestCase(vm, "/destination-port-bad", nil, false),
								newTestCase(b, "/destination-port-bad", nil, false),
								newTestCase(a, "/destination-port-notValues-c", nil, false),
								newTestCase(vm, "/destination-port-notValues-c", nil, false),
								newTestCase(b, "/destination-port-notValues-c", nil, false),

								newTestCase(a, "/connection-sni-good", nil, true),
								newTestCase(vm, "/connection-sni-good", nil, true),
								newTestCase(b, "/connection-sni-good", nil, true),
								newTestCase(a, "/connection-sni-bad", nil, false),
								newTestCase(vm, "/connection-sni-bad", nil, false),
								newTestCase(b, "/connection-sni-bad", nil, false),
								newTestCase(a, "/connection-sni-notValues-a-or-b", nil, true),
								newTestCase(vm, "/connection-sni-notValues-a-or-b", nil, true),
								newTestCase(a, "/connection-sni-notValues-a-or-b-or-c", nil, false),
								newTestCase(vm, "/connection-sni-notValues-a-or-b-or-c", nil, false),

								newTestCase(a, "/other", nil, false),
								newTestCase(vm, "/other", nil, false),
								newTestCase(b, "/other", nil, false),
							}
							rbacUtil.RunRBACTest(t, cases)
						})
					}
				})
			}
		})
}

// TestAuthorization_GRPC tests v1beta1 authorization with gRPC protocol.
func TestAuthorization_GRPC(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.grpc-protocol").
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "v1beta1-grpc",
				Inject: true,
			})
			var a, b, c, d echo.Instance
			echoboot.NewBuilder(t).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns, false, nil)).
				With(&d, util.EchoConfig("d", ns, false, nil)).
				BuildOrFail(t)

			cases := []rbacUtil.TestCase{
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "grpc",
							Scheme:   scheme.GRPC,
						},
					},
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "grpc",
							Scheme:   scheme.GRPC,
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "grpc",
							Scheme:   scheme.GRPC,
						},
					},
					ExpectAllowed: true,
				},
			}
			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, "testdata/authz/v1beta1-grpc.yaml.tmpl"))
			t.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)

			rbacUtil.RunRBACTest(t, cases)
		})
}

// TestAuthorization_Path tests the path is normalized before using in authorization. For example, a request
// with path "/a/../b" should be normalized to "/b" before using in authorization.
func TestAuthorization_Path(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.path-normalization").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/authz/v1beta1-path.yaml.tmpl"))
			t.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			for _, srcCluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("In %s", srcCluster.StableName())).Run(func(t framework.TestContext) {
					b := apps.B.GetOrFail(t, echo.InCluster(srcCluster).And(echo.Namespace(ns.Name())))
					a := apps.A.Match(echo.Namespace(ns.Name()))
					vm := apps.VM.Match(echo.Namespace(ns.Name()))
					callCount := 1
					if t.Clusters().IsMulticluster() {
						// so we can validate all clusters are hit
						callCount = util.CallsPerCluster * len(t.Clusters())
					}

					newTestCase := func(to echo.Instances, path string, expectAllowed bool) rbacUtil.TestCase {
						return rbacUtil.TestCase{
							Request: connection.Checker{
								From: b,
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
						newTestCase(vm, "/public", true),
						newTestCase(vm, "/private", false),
						newTestCase(vm, "/public/../private", false),
						newTestCase(vm, "/public/./../private", false),
						newTestCase(vm, "/public/.././private", false),
						newTestCase(vm, "/public/%2E%2E/private", false),
						newTestCase(vm, "/public/%2e%2e/private", false),
						newTestCase(vm, "/public/%2E/%2E%2E/private", false),
						newTestCase(vm, "/public/%2e/%2e%2e/private", false),
						newTestCase(vm, "/public/%2E%2E/%2E/private", false),
						newTestCase(vm, "/public/%2e%2e/%2e/private", false),
					}
					rbacUtil.RunRBACTest(t, cases)
				})
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
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "v1beta1-audit",
				Inject: true,
			})

			var a, b, c, d echo.Instance
			echoboot.NewBuilder(t).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&c, util.EchoConfig("c", ns, false, nil)).
				With(&d, util.EchoConfig("d", ns, false, nil)).
				BuildOrFail(t)

			newTestCase := func(target echo.Instance, path string, expectAllowed bool) rbacUtil.TestCase {
				return rbacUtil.TestCase{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   target,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     path,
						},
					},
					ExpectAllowed: expectAllowed,
				}
			}
			cases := []rbacUtil.TestCase{
				newTestCase(b, "/allow", true),
				newTestCase(b, "/audit", false),
				newTestCase(c, "/audit", true),
				newTestCase(c, "/deny", false),
				newTestCase(d, "/audit", true),
				newTestCase(d, "/other", true),
			}

			args := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
			}

			applyPolicy := func(filename string, ns namespace.Instance) {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				t.Config().ApplyYAMLOrFail(t, ns.Name(), policy...)
			}

			applyPolicy("testdata/authz/v1beta1-audit.yaml.tmpl", ns)

			rbacUtil.RunRBACTest(t, cases)
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
