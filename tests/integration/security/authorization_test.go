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
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
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
			a := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.A)
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			cInNS2 := match.Namespace(apps.Namespace2.Name()).GetMatches(apps.C)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			for _, to := range []echo.Instances{b, vm} {
				to := to
				t.ConfigIstio().EvalFile(map[string]string{
					"Namespace":  apps.Namespace1.Name(),
					"Namespace2": apps.Namespace2.Name(),
					"dst":        to.Config().Service,
				}, "testdata/authz/v1beta1-mtls.yaml.tmpl").ApplyOrFail(t, apps.Namespace1.Name(), resource.Wait)

				runAuthzTests(t, []authzTestCase{
					{
						from:          a,
						to:            to,
						path:          "/principal-a",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            to,
						path:          "/namespace-2",
						expectAllowed: false,
					},
					{
						from:          cInNS2,
						to:            to,
						path:          "/principal-a",
						expectAllowed: false,
					},
					{
						from:          cInNS2,
						to:            to,
						path:          "/namespace-2",
						expectAllowed: true,
					},
				})
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
			a := match.Namespace(ns.Name()).GetMatches(apps.A)
			b := match.Namespace(ns.Name()).GetMatches(apps.B)
			c := match.Namespace(ns.Name()).GetMatches(apps.C)
			vm := match.Namespace(ns.Name()).GetMatches(apps.VM)
			for _, to := range []echo.Instances{b, vm} {
				args := map[string]string{
					"Namespace":  apps.Namespace1.Name(),
					"Namespace2": apps.Namespace2.Name(),
					"dst":        to.Config().Service,
				}
				t.ConfigIstio().EvalFile(args, "testdata/authz/v1beta1-jwt.yaml.tmpl").ApplyOrFail(t, ns.Name(), resource.Wait)

				runAuthzTests(t, []authzTestCase{
					{
						namePrefix:    "[NoJWT]",
						from:          a,
						to:            to,
						path:          "/token1",
						expectAllowed: false,
					},
					{
						namePrefix:    "[NoJWT]",
						from:          a,
						to:            to,
						path:          "/token2",
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1]",
						from:          a,
						to:            to,
						path:          "/token1",
						token:         jwt.TokenIssuer1,
						expectAllowed: true,
					},
					{
						namePrefix:    "[Token1]",
						from:          a,
						to:            to,
						path:          "/token2",
						token:         jwt.TokenIssuer1,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token2]",
						from:          a,
						to:            to,
						path:          "/token1",
						token:         jwt.TokenIssuer2,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token2]",
						from:          a,
						to:            to,
						path:          "/token2",
						token:         jwt.TokenIssuer2,
						expectAllowed: true,
					},
					{
						namePrefix:    "[Token1]",
						from:          a,
						to:            to,
						path:          "/tokenAny",
						token:         jwt.TokenIssuer1,
						expectAllowed: true,
					},
					{
						namePrefix:    "[Token2]",
						from:          a,
						to:            to,
						path:          "/tokenAny",
						token:         jwt.TokenIssuer2,
						expectAllowed: true,
					},
					{
						namePrefix:    "[PermissionToken1]",
						from:          a,
						to:            to,
						path:          "/permission",
						token:         jwt.TokenIssuer1,
						expectAllowed: false,
					},
					{
						namePrefix:    "[PermissionToken2]",
						from:          a,
						to:            to,
						path:          "/permission",
						token:         jwt.TokenIssuer2,
						expectAllowed: false,
					},
					{
						namePrefix:    "[PermissionTokenWithSpaceDelimitedScope]",
						from:          a,
						to:            to,
						path:          "/permission",
						token:         jwt.TokenIssuer2WithSpaceDelimitedScope,
						expectAllowed: true,
					},
					{
						namePrefix:    "[NestedToken1]",
						from:          a,
						to:            to,
						path:          "/nested-key1",
						token:         jwt.TokenIssuer1WithNestedClaims1,
						expectAllowed: true,
					},
					{
						namePrefix:    "[NestedToken2]",
						from:          a,
						to:            to,
						path:          "/nested-key1",
						token:         jwt.TokenIssuer1WithNestedClaims2,
						expectAllowed: false,
					},
					{
						namePrefix:    "[NestedToken1]",
						from:          a,
						to:            to,
						path:          "/nested-key2",
						token:         jwt.TokenIssuer1WithNestedClaims1,
						expectAllowed: false,
					},
					{
						namePrefix:    "[NestedToken2]",
						from:          a,
						to:            to,
						path:          "/nested-key2",
						token:         jwt.TokenIssuer1WithNestedClaims2,
						expectAllowed: true,
					},
					{
						namePrefix:    "[NestedToken1]",
						from:          a,
						to:            to,
						path:          "/nested-2-key1",
						token:         jwt.TokenIssuer1WithNestedClaims1,
						expectAllowed: true,
					},
					{
						namePrefix:    "[NestedToken2]",
						from:          a,
						to:            to,
						path:          "/nested-2-key1",
						token:         jwt.TokenIssuer1WithNestedClaims2,
						expectAllowed: false,
					},
					{
						namePrefix:    "[NestedToken1]",
						from:          a,
						to:            to,
						path:          "/nested-non-exist",
						token:         jwt.TokenIssuer1WithNestedClaims1,
						expectAllowed: false,
					},
					{
						namePrefix:    "[NestedToken2]",
						from:          a,
						to:            to,
						path:          "/nested-non-exist",
						token:         jwt.TokenIssuer1WithNestedClaims2,
						expectAllowed: false,
					},
					{
						namePrefix:    "[NoJWT]",
						from:          a,
						to:            to,
						path:          "/tokenAny",
						expectAllowed: false,
					},
					{
						namePrefix:    "[NoJWT]",
						from:          a,
						to:            c,
						path:          "/somePath",
						expectAllowed: true,
					},

					// Test condition "request.auth.principal" on path "/valid-jwt".
					{
						namePrefix:    "[NoJWT]",
						from:          a,
						to:            to,
						path:          "/valid-jwt",
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1]",
						from:          a,
						to:            to,
						path:          "/valid-jwt",
						token:         jwt.TokenIssuer1,
						expectAllowed: true,
					},
					{
						namePrefix:    "[Token1WithAzp]",
						from:          a,
						to:            to,
						path:          "/valid-jwt",
						token:         jwt.TokenIssuer1WithAzp,
						expectAllowed: true,
					},
					{
						namePrefix:    "[Token1WithAud]",
						from:          a,
						to:            to,
						path:          "/valid-jwt",
						token:         jwt.TokenIssuer1WithAud,
						expectAllowed: true,
					},

					// Test condition "request.auth.presenter" on suffix "/presenter".
					{
						namePrefix:    "[Token1]",
						from:          a,
						to:            to,
						path:          "/request/presenter",
						token:         jwt.TokenIssuer1,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1WithAud]",
						from:          a,
						to:            to,
						path:          "/request/presenter",
						token:         jwt.TokenIssuer1WithAud,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1WithAzp]",
						from:          a,
						to:            to,
						path:          "/request/presenter-x",
						token:         jwt.TokenIssuer1WithAzp,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1WithAzp]",
						from:          a,
						to:            to,
						path:          "/request/presenter",
						token:         jwt.TokenIssuer1WithAzp,
						expectAllowed: true,
					},

					// Test condition "request.auth.audiences" on suffix "/audiences".
					{
						namePrefix:    "[Token1]",
						from:          a,
						to:            to,
						path:          "/request/audiences",
						token:         jwt.TokenIssuer1,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1WithAzp]",
						from:          a,
						to:            to,
						path:          "/request/audiences",
						token:         jwt.TokenIssuer1WithAzp,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1WithAud]",
						from:          a,
						to:            to,
						path:          "/request/audiences-x",
						token:         jwt.TokenIssuer1WithAud,
						expectAllowed: false,
					},
					{
						namePrefix:    "[Token1WithAud]",
						from:          a,
						to:            to,
						path:          "/request/audiences",
						token:         jwt.TokenIssuer1WithAud,
						expectAllowed: true,
					},
				})
			}
		})
}

// TestAuthorization_WorkloadSelector tests the workload selector for the v1beta1 policy in two namespaces.
func TestAuthorization_WorkloadSelector(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.workload-selector").
		Run(func(t framework.TestContext) {
			aInNS1 := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.A)
			bInNS1 := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			vmInNS1 := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			cInNS1 := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			cInNS2 := match.Namespace(apps.Namespace2.Name()).GetMatches(apps.C)
			ns1 := apps.Namespace1
			ns2 := apps.Namespace2
			rootns := newRootNS(t)

			t.NewSubTest("non-vms").Run(func(t framework.TestContext) {
				applyPolicy := func(filename string, ns namespace.Instance) {
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace1":    ns1.Name(),
						"Namespace2":    ns2.Name(),
						"RootNamespace": rootns.Name(),
						"b":             util.BSvc,
						"c":             util.CSvc,
					}, filename).ApplyOrFail(t, ns.Name(), resource.Wait)
				}
				applyPolicy("testdata/authz/v1beta1-workload-ns1.yaml.tmpl", ns1)
				applyPolicy("testdata/authz/v1beta1-workload-ns2.yaml.tmpl", ns2)
				applyPolicy("testdata/authz/v1beta1-workload-ns-root.yaml.tmpl", rootns)

				runAuthzTests(t, []authzTestCase{
					// TO: ns1.b
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns1-b",
						expectAllowed: true,
					},
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns1-vm",
						expectAllowed: false,
					},
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns1-c",
						expectAllowed: false,
					},
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns1-x",
						expectAllowed: false,
					},
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns1-all",
						expectAllowed: true,
					},
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns2-c",
						expectAllowed: false,
					},
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns2-all",
						expectAllowed: false,
					},
					{
						namePrefix:    "[bInNS1]",
						from:          aInNS1,
						to:            bInNS1,
						path:          "/policy-ns-root-c",
						expectAllowed: false,
					},

					// TO: ns1.c
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns1-b",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns1-vm",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns1-c",
						expectAllowed: true,
					},
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns1-x",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns1-all",
						expectAllowed: true,
					},
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns2-c",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns2-all",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS1]",
						from:          aInNS1,
						to:            cInNS1,
						path:          "/policy-ns-root-c",
						expectAllowed: true,
					},

					// TO: ns2.c
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns1-b",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns1-vm",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns1-c",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns1-x",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns1-all",
						expectAllowed: false,
					},
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns2-c",
						expectAllowed: true,
					},
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns2-all",
						expectAllowed: true,
					},
					{
						namePrefix:    "[cInNS2]",
						from:          aInNS1,
						to:            cInNS2,
						path:          "/policy-ns-root-c",
						expectAllowed: true,
					},
				})
			})

			t.NewSubTest("vms").Run(func(t framework.TestContext) {
				applyPolicy := func(filename string, ns namespace.Instance) {
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace1":    ns1.Name(),
						"Namespace2":    ns2.Name(),
						"RootNamespace": rootns.Name(),
						"b":             util.VMSvc, // This is the only difference from standard args.
						"c":             util.CSvc,
					}, filename).ApplyOrFail(t, ns.Name(), resource.Wait)
				}
				applyPolicy("testdata/authz/v1beta1-workload-ns1.yaml.tmpl", ns1)
				applyPolicy("testdata/authz/v1beta1-workload-ns2.yaml.tmpl", ns2)
				applyPolicy("testdata/authz/v1beta1-workload-ns-root.yaml.tmpl", rootns)

				runAuthzTests(t, []authzTestCase{
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns1-b",
						expectAllowed: false,
					},
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns1-vm",
						expectAllowed: true,
					},
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns1-c",
						expectAllowed: false,
					},
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns1-x",
						expectAllowed: false,
					},
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns1-all",
						expectAllowed: true,
					},
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns2-b",
						expectAllowed: false,
					},
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns2-all",
						expectAllowed: false,
					},
					{
						namePrefix:    "[vmInNS1]",
						from:          aInNS1,
						to:            vmInNS1,
						path:          "/policy-ns-root-c",
						expectAllowed: false,
					},
				})
			})
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
			rootNS := newRootNS(t)
			a := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.A)
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)

			applyPolicy := func(filename string, ns namespace.Instance) {
				t.ConfigIstio().EvalFile(map[string]string{
					"Namespace":     ns.Name(),
					"RootNamespace": rootNS.Name(),
					"b":             b[0].Config().Service,
					"c":             c[0].Config().Service,
					"vm":            vm[0].Config().Service,
				}, filename).ApplyOrFail(t, ns.Name(), resource.Wait)
			}
			applyPolicy("testdata/authz/v1beta1-deny.yaml.tmpl", ns)
			applyPolicy("testdata/authz/v1beta1-deny-ns-root.yaml.tmpl", rootNS)

			runAuthzTests(t, []authzTestCase{
				// TO: b
				{
					from:          a,
					to:            b,
					path:          "/deny",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            b,
					path:          "/deny?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            b,
					path:          "/global-deny",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            b,
					path:          "/global-deny?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            b,
					path:          "/other",
					expectAllowed: true,
				},
				{
					from:          a,
					to:            b,
					path:          "/other?param=value",
					expectAllowed: true,
				},
				{
					from:          a,
					to:            b,
					path:          "/allow",
					expectAllowed: true,
				},
				{
					from:          a,
					to:            b,
					path:          "/allow?param=value",
					expectAllowed: true,
				},

				// TO: c
				{
					from:          a,
					to:            c,
					path:          "/allow/admin",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            c,
					path:          "/allow/admin?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            c,
					path:          "/global-deny",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            c,
					path:          "/global-deny?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            c,
					path:          "/other",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            c,
					path:          "/other?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            c,
					path:          "/allow",
					expectAllowed: true,
				},
				{
					from:          a,
					to:            c,
					path:          "/allow?param=value",
					expectAllowed: true,
				},

				// TO: vm
				{
					from:          a,
					to:            vm,
					path:          "/allow/admin",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/allow/admin?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/global-deny",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/global-deny?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/other",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/other?param=value",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/allow",
					expectAllowed: true,
				},
				{
					from:          a,
					to:            vm,
					path:          "/allow?param=value",
					expectAllowed: true,
				},
			})
		})
}

// TestAuthorization_NegativeMatch tests the authorization policy with negative match.
func TestAuthorization_NegativeMatch(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.negative-match").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			ns2 := apps.Namespace2
			a := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.A)
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			bInNS2 := match.Namespace(apps.Namespace2.Name()).GetMatches(apps.B)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			d := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.D)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)
			t.ConfigIstio().EvalFile(map[string]string{
				"Namespace":  ns.Name(),
				"Namespace2": ns2.Name(),
				"b":          b.Config().Service,
				"c":          c.Config().Service,
				"d":          d.Config().Service,
				"vm":         vm.Config().Service,
			}, "testdata/authz/v1beta1-negative-match.yaml.tmpl").ApplyOrFail(t, "")

			runAuthzTests(t, []authzTestCase{
				// Test the policy with overlapped `paths` and `not_paths` on b.
				// a and bInNs2 should have the same results:
				// - path with prefix `/prefix` should be denied explicitly.
				// - path `/prefix/allowlist` should be excluded from the deny.
				// - path `/allow` should be allowed implicitly.
				{
					from:          a,
					to:            b,
					path:          "/prefix",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            b,
					path:          "/prefix/other",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            b,
					path:          "/prefix/allowlist",
					expectAllowed: true,
				},
				{
					from:          a,
					to:            b,
					path:          "/allow",
					expectAllowed: true,
				},
				{
					from:          bInNS2,
					to:            b,
					path:          "/prefix",
					expectAllowed: false,
				},
				{
					from:          bInNS2,
					to:            b,
					path:          "/prefix/other",
					expectAllowed: false,
				},
				{
					from:          bInNS2,
					to:            b,
					path:          "/prefix/allowlist",
					expectAllowed: true,
				},
				{
					from:          bInNS2,
					to:            b,
					path:          "/allow",
					expectAllowed: true,
				},

				// Test the policy that denies other namespace on c.
				// a should be allowed because it's from the same namespace.
				// bInNs2 should be denied because it's from a different namespace.
				{
					from:          a,
					to:            c,
					path:          "/",
					expectAllowed: true,
				},
				{
					from:          bInNS2,
					to:            c,
					path:          "/",
					expectAllowed: false,
				},

				// Test the policy that denies plain-text traffic on d.
				// a should be allowed because it's using mTLS.
				// bInNs2 should be denied because it's using plain-text.
				{
					from:          a,
					to:            d,
					path:          "/",
					expectAllowed: true,
				},
				{
					from:          bInNS2,
					to:            d,
					path:          "/",
					expectAllowed: false,
				},

				// Test the policy with overlapped `paths` and `not_paths` on vm.
				// a and bInNs2 should have the same results:
				// - path with prefix `/prefix` should be denied explicitly.
				// - path `/prefix/allowlist` should be excluded from the deny.
				// - path `/allow` should be allowed implicitly.
				// TODO(JimmyCYJ): support multiple VMs and test negative match on multiple VMs.
				{
					from:          a,
					to:            vm,
					path:          "/prefix",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/prefix/other",
					expectAllowed: false,
				},
				{
					from:          a,
					to:            vm,
					path:          "/prefix/allowlist",
					expectAllowed: true,
				},
				{
					from:          a,
					to:            vm,
					path:          "/allow",
					expectAllowed: true,
				},
				{
					from:          bInNS2,
					to:            vm,
					path:          "/prefix",
					expectAllowed: false,
				},
				{
					from:          bInNS2,
					to:            vm,
					path:          "/prefix/other",
					expectAllowed: false,
				},
				{
					from:          bInNS2,
					to:            vm,
					path:          "/prefix/allowlist",
					expectAllowed: true,
				},
				{
					from:          bInNS2,
					to:            vm,
					path:          "/allow",
					expectAllowed: true,
				},
			})
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
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace":     ns.Name(),
						"RootNamespace": rootns.Name(),
						"dst":           dst[0].Config().Service,
					}, "testdata/authz/v1beta1-ingress-gateway.yaml.tmpl").ApplyOrFail(t, "")

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
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace":     ns.Name(),
						"RootNamespace": rootns.Name(),
						"a":             a[0].Config().Service,
					}, "testdata/authz/v1beta1-egress-gateway.yaml.tmpl").ApplyOrFail(t, "")

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
			ns := apps.Namespace1
			ns2 := apps.Namespace2
			a := match.Namespace(ns.Name()).GetMatches(apps.A)
			b := match.Namespace(ns.Name()).GetMatches(apps.B)
			c := match.Namespace(ns.Name()).GetMatches(apps.C)
			eInNS2 := match.Namespace(ns2.Name()).GetMatches(apps.E)
			d := match.Namespace(ns.Name()).GetMatches(apps.D)
			e := match.Namespace(ns.Name()).GetMatches(apps.E)

			t.NewSubTest("non-vms").Run(func(t framework.TestContext) {
				t.ConfigIstio().EvalFile(map[string]string{
					"Namespace":  ns.Name(),
					"Namespace2": ns2.Name(),
					"b":          b[0].Config().Service,
					"c":          c[0].Config().Service,
					"d":          d[0].Config().Service,
					"e":          e[0].Config().Service,
					"a":          a[0].Config().Service,
				}, "testdata/authz/v1beta1-tcp.yaml.tmpl").ApplyOrFail(t, "")

				runAuthzTests(t, []authzTestCase{
					// The policy on workload b denies request with path "/data" to port 8091:
					// - request to port http-8091 should be denied because both path and port are matched.
					// - request to port http-8092 should be allowed because the port is not matched.
					// - request to port tcp-8093 should be allowed because the port is not matched.
					{
						from:          a,
						to:            b,
						scheme:        scheme.HTTP,
						portName:      "http-8091",
						path:          "/data",
						expectAllowed: false,
					},
					{
						from:          a,
						to:            b,
						scheme:        scheme.HTTP,
						portName:      "http-8092",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            b,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: true,
					},

					// The policy on workload c denies request to port 8091:
					// - request to port http-8091 should be denied because the port is matched.
					// - request to http port 8092 should be allowed because the port is not matched.
					// - request to tcp port 8093 should be allowed because the port is not matched.
					// - request from b to tcp port 8093 should be allowed by default.
					// - request from b to tcp port 8094 should be denied because the principal is matched.
					// - request from eInNS2 to tcp port 8093 should be denied because the namespace is matched.
					// - request from eInNS2 to tcp port 8094 should be allowed by default.
					{
						from:          a,
						to:            c,
						scheme:        scheme.HTTP,
						portName:      "http-8091",
						path:          "/data",
						expectAllowed: false,
					},
					{
						from:          a,
						to:            c,
						scheme:        scheme.HTTP,
						portName:      "http-8092",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            c,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          b,
						to:            c,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          b,
						to:            c,
						scheme:        scheme.TCP,
						portName:      "tcp-8094",
						path:          "/data",
						expectAllowed: false,
					},
					{
						from:          eInNS2,
						to:            c,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: false,
					},
					{
						from:          eInNS2,
						to:            c,
						scheme:        scheme.TCP,
						portName:      "tcp-8094",
						path:          "/data",
						expectAllowed: true,
					},

					// The policy on workload d denies request from service account a and workloads in namespace 2:
					// - request from a to d should be denied because it has service account a.
					// - request from b to d should be allowed.
					// - request from c to d should be allowed.
					// - request from eInNS2 to a should be allowed because there is no policy on a.
					// - request from eInNS2 to d should be denied because it's in namespace 2.
					{
						from:          a,
						to:            d,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: false,
					},
					{
						from:          b,
						to:            d,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          c,
						to:            d,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          eInNS2,
						to:            a,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          eInNS2,
						to:            d,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: false,
					},

					// The policy on workload e denies request with path "/other":
					// - request to port http-8091 should be allowed because the path is not matched.
					// - request to port http-8092 should be allowed because the path is not matched.
					// - request to port tcp-8093 should be denied because policy uses HTTP fields.
					{
						from:          a,
						to:            e,
						scheme:        scheme.HTTP,
						portName:      "http-8091",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            e,
						scheme:        scheme.HTTP,
						portName:      "http-8092",
						path:          "/data",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            e,
						scheme:        scheme.TCP,
						portName:      "tcp-8093",
						path:          "/data",
						expectAllowed: false,
					},
				})
			})
			// TODO(JimmyCYJ): support multiple VMs and apply different security policies to each VM.
			vm := match.Namespace(ns.Name()).GetMatches(apps.VM)
			t.NewSubTest("vms").
				Run(func(t framework.TestContext) {
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace":  ns.Name(),
						"Namespace2": ns2.Name(),
						"b":          b[0].Config().Service,
						"c":          vm[0].Config().Service,
						"d":          d[0].Config().Service,
						"e":          e[0].Config().Service,
						"a":          a[0].Config().Service,
					}, "testdata/authz/v1beta1-tcp.yaml.tmpl").ApplyOrFail(t, "")

					runAuthzTests(t, []authzTestCase{
						// The policy on workload vm denies request to port 8091:
						// - request to port http-8091 should be denied because the port is matched.
						// - request to http port 8092 should be allowed because the port is not matched.
						// - request to tcp port 8093 should be allowed because the port is not matched.
						// - request from b to tcp port 8093 should be allowed by default.
						// - request from b to tcp port 8094 should be denied because the principal is matched.
						// - request from eInNS2 to tcp port 8093 should be denied because the namespace is matched.
						// - request from eInNS2 to tcp port 8094 should be allowed by default.
						{
							from:          a,
							to:            vm,
							scheme:        scheme.HTTP,
							portName:      "http-8091",
							path:          "/data",
							expectAllowed: false,
						},
						{
							from:          a,
							to:            vm,
							scheme:        scheme.HTTP,
							portName:      "http-8092",
							path:          "/data",
							expectAllowed: true,
						},
						{
							from:          a,
							to:            vm,
							scheme:        scheme.TCP,
							portName:      "tcp-8093",
							path:          "/data",
							expectAllowed: true,
						},
						{
							from:          b,
							to:            vm,
							scheme:        scheme.TCP,
							portName:      "tcp-8093",
							path:          "/data",
							expectAllowed: true,
						},
						{
							from:          b,
							to:            vm,
							scheme:        scheme.TCP,
							portName:      "tcp-8094",
							path:          "/data",
							expectAllowed: false,
						},
						{
							from:          eInNS2,
							to:            vm,
							scheme:        scheme.TCP,
							portName:      "tcp-8093",
							path:          "/data",
							expectAllowed: false,
						},
						{
							from:          eInNS2,
							to:            vm,
							scheme:        scheme.TCP,
							portName:      "tcp-8094",
							path:          "/data",
							expectAllowed: true,
						},
					})
				})
		})
}

// TestAuthorization_Conditions tests v1beta1 authorization with conditions.
func TestAuthorization_Conditions(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.conditions").
		Run(func(t framework.TestContext) {
			ns1 := apps.Namespace1
			ns2 := apps.Namespace2
			ns3 := apps.Namespace3

			aInNS1 := match.Namespace(ns1.Name()).GetMatches(apps.A)
			bInNS2 := match.Namespace(ns2.Name()).GetMatches(apps.B)
			cInNS3 := match.Namespace(ns3.Name()).GetMatches(apps.C)
			vmInNS1 := match.Namespace(ns1.Name()).GetMatches(apps.VM)
			for _, to := range []echo.Instances{cInNS3, vmInNS1} {
				to := to

				addresses := func(to echo.Target) string {
					var out []string
					for _, w := range to.WorkloadsOrFail(t) {
						out = append(out, "\""+w.Address()+"\"")
					}
					return strings.Join(out, ",")
				}

				args := map[string]string{
					"NamespaceA": ns1.Name(),
					"NamespaceB": ns2.Name(),
					"NamespaceC": to.Config().Namespace.Name(),
					"cSet":       to.Config().Service,
					"ipA":        addresses(aInNS1),
					"ipB":        addresses(bInNS2),
					"ipC":        addresses(to),
					"portC":      "8090",
					"a":          util.ASvc,
					"b":          util.BSvc,
				}
				t.ConfigIstio().EvalFile(args, "testdata/authz/v1beta1-conditions.yaml.tmpl").ApplyOrFail(t, "")

				runAuthzTests(t, []authzTestCase{
					{
						from:          aInNS1,
						to:            to,
						path:          "/request-headers",
						headers:       headers.New().With("x-foo", "foo"),
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/request-headers",
						headers:       headers.New().With("x-foo", "foo"),
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          "/request-headers",
						headers:       headers.New().With("x-foo", "bar"),
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          "/request-headers",
						headers:       nil,
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/request-headers",
						headers:       nil,
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          "/request-headers-notValues-bar",
						headers:       headers.New().With("x-foo", "foo"),
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          "/request-headers-notValues-bar",
						headers:       headers.New().With("x-foo", "bar"),
						expectAllowed: false,
					},

					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-ip-%s", args["a"]),
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-ip-%s", args["a"]),
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-ip-%s", args["b"]),
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-ip-%s", args["b"]),
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-ip-notValues-%s", args["b"]),
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-ip-notValues-%s", args["b"]),
						expectAllowed: false,
					},

					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-namespace-%s", args["a"]),
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-namespace-%s", args["a"]),
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-namespace-%s", args["b"]),
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-namespace-%s", args["b"]),
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-namespace-notValues-%s", args["b"]),
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-namespace-notValues-%s", args["b"]),
						expectAllowed: false,
					},

					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-principal-%s", args["a"]),
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-principal-%s", args["a"]),
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-principal-%s", args["b"]),
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-principal-%s", args["b"]),
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/source-principal-notValues-%s", args["b"]),
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/source-principal-notValues-%s", args["b"]),
						expectAllowed: false,
					},

					{
						from:          aInNS1,
						to:            to,
						path:          "/destination-ip-good",
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/destination-ip-good",
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          "/destination-ip-bad",
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/destination-ip-bad",
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/destination-ip-notValues-%s-or-%s", args["a"], args["b"]),
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/destination-ip-notValues-%s-or-%s-or-%s", args["a"], args["b"], args["cSet"]),
						expectAllowed: false,
					},

					{
						from:          aInNS1,
						to:            to,
						path:          "/destination-port-good",
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/destination-port-good",
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          "/destination-port-bad",
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/destination-port-bad",
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/destination-port-notValues-%s", args["cSet"]),
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          fmt.Sprintf("/destination-port-notValues-%s", args["cSet"]),
						expectAllowed: false,
					},

					{
						from:          aInNS1,
						to:            to,
						path:          "/connection-sni-good",
						expectAllowed: true,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/connection-sni-good",
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          "/connection-sni-bad",
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/connection-sni-bad",
						expectAllowed: false,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/connection-sni-notValues-%s-or-%s", args["a"], args["b"]),
						expectAllowed: true,
					},
					{
						from:          aInNS1,
						to:            to,
						path:          fmt.Sprintf("/connection-sni-notValues-%s-or-%s-or-%s", args["a"], args["b"], args["cSet"]),
						expectAllowed: false,
					},

					{
						from:          aInNS1,
						to:            to,
						path:          "/other",
						expectAllowed: false,
					},
					{
						from:          bInNS2,
						to:            to,
						path:          "/other",
						expectAllowed: false,
					},
				})
			}
		})
}

// TestAuthorization_GRPC tests v1beta1 authorization with gRPC protocol.
func TestAuthorization_GRPC(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.grpc-protocol").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1

			newTest := func(a, b, c, d echo.Instances) func(t framework.TestContext) {
				return func(t framework.TestContext) {
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace": ns.Name(),
						"a":         a.Config().Service,
						"b":         b.Config().Service,
						"c":         c.Config().Service,
						"d":         d.Config().Service,
					}, "testdata/authz/v1beta1-grpc.yaml.tmpl").ApplyOrFail(t, ns.Name(), resource.Wait)

					runAuthzTests(t, []authzTestCase{
						{
							from:          b,
							to:            a,
							portName:      "grpc",
							expectAllowed: true,
						},
						{
							from:          c,
							to:            a,
							portName:      "grpc",
							expectAllowed: false,
						},
						{
							from:          d,
							to:            a,
							portName:      "grpc",
							expectAllowed: true,
						},
					})
				}
			}

			a := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.A)
			b := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.B)
			c := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.C)
			d := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.D)
			vm := match.Namespace(apps.Namespace1.Name()).GetMatches(apps.VM)

			t.NewSubTest("non-vms").Run(newTest(a, b, c, d))
			t.NewSubTest("vm-for-a").Run(newTest(vm, b, c, d))
			t.NewSubTest("vm-for-b").Run(newTest(a, vm, c, d))
		})
}

// TestAuthorization_Path tests the path is normalized before using in authorization. For example, a request
// with path "/a/../b" should be normalized to "/b" before using in authorization.
func TestAuthorization_Path(t *testing.T) {
	framework.NewTest(t).
		Features("security.authorization.path-normalization").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1

			newTest := func(a, b echo.Instances) func(framework.TestContext) {
				return func(t framework.TestContext) {
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace": ns.Name(),
						"a":         a.Config().Service,
					}, "testdata/authz/v1beta1-path.yaml.tmpl").ApplyOrFail(t, ns.Name(), resource.Wait)

					runAuthzTests(t, []authzTestCase{
						{
							from:          b,
							to:            a,
							path:          "/public",
							expectAllowed: true,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/../public",
							expectAllowed: true,
						},
						{
							from:          b,
							to:            a,
							path:          "/private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/../private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/./../private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/.././private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/%2E%2E/private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/%2e%2e/private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/%2E/%2E%2E/private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/%2e/%2e%2e/private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/%2E%2E/%2E/private",
							expectAllowed: false,
						},
						{
							from:          b,
							to:            a,
							path:          "/public/%2e%2e/%2e/private",
							expectAllowed: false,
						},
					})
				}
			}

			a := match.Namespace(ns.Name()).GetMatches(apps.A)
			b := match.Namespace(ns.Name()).GetMatches(apps.B)
			vm := match.Namespace(ns.Name()).GetMatches(apps.VM)

			t.NewSubTest("non-vms").Run(newTest(a, b))
			t.NewSubTest("vms").Run(newTest(vm, b))
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

			t.NewSubTest("non-vm").Run(func(t framework.TestContext) {
				t.ConfigIstio().EvalFile(map[string]string{
					"b":             b.Config().Service,
					"c":             c.Config().Service,
					"d":             d.Config().Service,
					"Namespace":     ns.Name(),
					"RootNamespace": istio.GetOrFail(t, t).Settings().SystemNamespace,
				}, "testdata/authz/v1beta1-audit.yaml.tmpl").ApplyOrFail(t, ns.Name(), resource.Wait)

				runAuthzTests(t, []authzTestCase{
					{
						from:          a,
						to:            b,
						path:          "/allow",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            b,
						path:          "/audit",
						expectAllowed: false,
					},
					{
						from:          a,
						to:            c,
						path:          "/audit",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            c,
						path:          "/deny",
						expectAllowed: false,
					},
					{
						from:          a,
						to:            d,
						path:          "/audit",
						expectAllowed: true,
					},
					{
						from:          a,
						to:            d,
						path:          "/other",
						expectAllowed: true,
					},
				})
			})

			vmPolicy := func(filename string) func(t framework.TestContext) {
				return func(t framework.TestContext) {
					t.ConfigIstio().EvalFile(map[string]string{
						"Namespace": ns.Name(),
						"dst":       vm.Config().Service,
					}, filename).ApplyOrFail(t, ns.Name(), resource.Wait)
				}
			}

			// (TODO)JimmyCYJ: Support multiple VMs and apply audit policies to multiple VMs for testing.
			// The tests below are duplicated from above for VM workloads. With support for multiple VMs,
			// These tests will be merged to the tests above.

			t.NewSubTest("vm-audit-allow").Run(func(t framework.TestContext) {
				vmPolicy("testdata/authz/v1beta1-audit-allow.yaml.tmpl")(t)
				runAuthzTests(t, []authzTestCase{
					{
						from:          b,
						to:            vm,
						path:          "/allow",
						expectAllowed: true,
					},
					{
						from:          b,
						to:            vm,
						path:          "/audit",
						expectAllowed: false,
					},
				})
			})

			t.NewSubTest("vm-audit-deny").Run(func(t framework.TestContext) {
				vmPolicy("testdata/authz/v1beta1-audit-deny.yaml.tmpl")(t)
				runAuthzTests(t, []authzTestCase{
					{
						from:          b,
						to:            vm,
						path:          "/audit",
						expectAllowed: true,
					},
					{
						from:          b,
						to:            vm,
						path:          "/deny",
						expectAllowed: false,
					},
				})
			})

			t.NewSubTest("vm-audit-default").Run(func(t framework.TestContext) {
				vmPolicy("testdata/authz/v1beta1-audit-default.yaml.tmpl")(t)
				runAuthzTests(t, []authzTestCase{
					{
						from:          b,
						to:            vm,
						path:          "/audit",
						expectAllowed: true,
					},
					{
						from:          b,
						to:            vm,
						path:          "/other",
						expectAllowed: true,
					},
				})
			})
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
			t.ConfigIstio().EvalFile(args, "../../../samples/extauthz/ext-authz.yaml").ApplyOrFail(t, ns.Name())
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

			t.ConfigIstio().EvalFile(args, "testdata/authz/v1beta1-custom.yaml.tmpl").ApplyOrFail(t, "")
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

			// TODO(nmittler): Can we find a way to make this test work with the existing deployment?
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

			authzHeaders := func(h string) *headers.Builder {
				return headers.New().With("x-ext-authz", h).With("x-ext-authz-additional-header-override", "should-be-override")
			}

			// Path "/custom" is protected by ext-authz service and is accessible with the header `x-ext-authz: allow`.
			// Path "/health" is not protected and is accessible to public.
			runAuthzTests(t, []authzTestCase{
				// workload b is using an ext-authz service in its own pod of HTTP API.
				{
					from:          x.Instances(),
					to:            b.Instances(),
					path:          "/custom",
					headers:       authzHeaders("allow"),
					checker:       checkHTTPHeaders(echoClient.RequestHeader),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            b.Instances(),
					path:          "/custom",
					headers:       authzHeaders("deny"),
					checker:       checkHTTPHeaders(echoClient.ResponseHeader),
					expectAllowed: false,
				},
				{
					from:          x.Instances(),
					to:            b.Instances(),
					path:          "/health",
					headers:       authzHeaders("allow"),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            b.Instances(),
					path:          "/health",
					headers:       authzHeaders("deny"),
					expectAllowed: true,
				},

				// workload c is using an ext-authz service in its own pod of gRPC API.
				{
					from:          x.Instances(),
					to:            c.Instances(),
					path:          "/custom",
					headers:       authzHeaders("allow"),
					checker:       checkGRPCHeaders(echoClient.RequestHeader),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            c.Instances(),
					path:          "/custom",
					headers:       authzHeaders("deny"),
					checker:       checkGRPCHeaders(echoClient.ResponseHeader),
					expectAllowed: false,
				},
				{
					from:          x.Instances(),
					to:            c.Instances(),
					path:          "/health",
					headers:       authzHeaders("allow"),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            c.Instances(),
					path:          "/health",
					headers:       authzHeaders("deny"),
					expectAllowed: true,
				},

				// workload d is using an local ext-authz service in the same pod as the application of HTTP API.
				{
					from:          x.Instances(),
					to:            d.Instances(),
					path:          "/custom",
					headers:       authzHeaders("allow"),
					checker:       checkHTTPHeaders(echoClient.RequestHeader),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            d.Instances(),
					path:          "/custom",
					headers:       authzHeaders("deny"),
					checker:       checkHTTPHeaders(echoClient.ResponseHeader),
					expectAllowed: false,
				},
				{
					from:          x.Instances(),
					to:            d.Instances(),
					path:          "/health",
					headers:       authzHeaders("allow"),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            d.Instances(),
					path:          "/health",
					headers:       authzHeaders("deny"),
					expectAllowed: true,
				},

				// workload e is using an local ext-authz service in the same pod as the application of gRPC API.
				{
					from:          x.Instances(),
					to:            e.Instances(),
					path:          "/custom",
					headers:       authzHeaders("allow"),
					checker:       checkGRPCHeaders(echoClient.RequestHeader),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            e.Instances(),
					path:          "/custom",
					headers:       authzHeaders("deny"),
					checker:       checkGRPCHeaders(echoClient.ResponseHeader),
					expectAllowed: false,
				},
				{
					from:          x.Instances(),
					to:            e.Instances(),
					path:          "/health",
					headers:       authzHeaders("allow"),
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            e.Instances(),
					path:          "/health",
					headers:       authzHeaders("deny"),
					expectAllowed: true,
				},

				// workload f is using an ext-authz service in its own pod of TCP API.
				{
					from:          a.Instances(),
					to:            f.Instances(),
					scheme:        scheme.TCP,
					portName:      "tcp-8092",
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            f.Instances(),
					scheme:        scheme.TCP,
					portName:      "tcp-8092",
					expectAllowed: false,
				},
				{
					from:          a.Instances(),
					to:            f.Instances(),
					scheme:        scheme.TCP,
					portName:      "tcp-8093",
					expectAllowed: true,
				},
				{
					from:          x.Instances(),
					to:            f.Instances(),
					scheme:        scheme.TCP,
					portName:      "tcp-8093",
					expectAllowed: true,
				},
			})

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
					newIngressTestCase(x, g, "/custom", authzHeaders("allow").Build(), checkHTTPHeaders(echoClient.RequestHeader), true),
					newIngressTestCase(x, g, "/custom", authzHeaders("deny").Build(), checkHTTPHeaders(echoClient.ResponseHeader), false),
					newIngressTestCase(x, g, "/health", authzHeaders("allow").Build(), nil, true),
					newIngressTestCase(x, g, "/health", authzHeaders("deny").Build(), nil, true),
				}
				for _, c := range ingressCases {
					c(t)
				}
			})
		})
}

type authzTestCase struct {
	namePrefix    string
	from          echo.Instances
	to            echo.Target
	portName      string
	path          string
	headers       *headers.Builder
	scheme        scheme.Instance
	token         string
	checker       check.Checker
	expectAllowed bool
}

func runAuthzTests(t framework.TestContext, cases []authzTestCase) {
	t.Helper()
	for _, c := range cases {
		c := c

		if c.portName == "" {
			c.portName = "http"
		}

		if c.headers == nil {
			c.headers = headers.New()
		}

		want := "deny"
		if c.expectAllowed {
			want = "allow"
		}
		name := fmt.Sprintf("%s%s->%s:%s%s[%s]",
			c.namePrefix,
			c.from.Config().Service,
			c.to.Config().Service,
			c.portName,
			c.path,
			want)

		t.NewSubTest(name).Run(func(t framework.TestContext) {
			if strings.Contains(name, "source-ip") && t.Clusters().IsMulticluster() {
				t.Skip("https://github.com/istio/istio/issues/37307: " +
					"Current source ip based authz test cases are not required in multicluster setup because " +
					"cross-network traffic will lose the origin source ip info")
			}

			echotest.New(t, append(c.from.Instances(), c.to.Instances()...)).
				FromMatch(match.NamespacedName(c.from.Config().NamespacedName())).
				ToMatch(match.NamespacedName(c.to.Config().NamespacedName())).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					opts := echo.CallOptions{
						To:     to,
						Scheme: c.scheme,
						Port: echo.Port{
							Name: c.portName,
						},
						HTTP: echo.HTTP{
							Path:    c.path,
							Headers: c.headers.WithAuthz(c.token).Build(),
						},
						Count: util.CallsPerCluster * to.WorkloadsOrFail(t).Len(),
					}
					if c.expectAllowed {
						opts.Check = check.And(check.OK(), scheck.ReachedClusters(&opts))
					} else {
						opts.Check = scheck.RBACFailure(&opts)
					}

					if c.checker != nil {
						opts.Check = check.And(opts.Check, c.checker)
					}

					from.CallOrFail(t, opts)
				})
		})
	}
}
