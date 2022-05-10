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
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/scheck"
)

// TestJWTHTTPS tests the requestauth policy with https jwks server.
func TestJWTHTTPS(t *testing.T) {
	payload1 := strings.Split(jwt.TokenIssuer1, ".")[1]

	framework.NewTest(t).
		Features("security.authentication.jwt").
		Run(func(t framework.TestContext) {
			if t.Clusters().IsMulticluster() {
				t.Skip("https://github.com/istio/istio/issues/37307")
			}

			ns := apps.Namespace1
			istioSystemNS := istio.ClaimSystemNamespaceOrFail(t, t)

			t.ConfigKube().EvalFile(istioSystemNS.Name(), map[string]string{
				"Namespace": istioSystemNS.Name(),
			}, filepath.Join(env.IstioSrc, "samples/jwt-server", "jwt-server.yaml")).ApplyOrFail(t)

			for _, cluster := range t.AllClusters() {
				fetchFn := kube.NewSinglePodFetch(cluster, istioSystemNS.Name(), "app=jwt-server")
				_, err := kube.WaitUntilPodsAreReady(fetchFn)
				if err != nil {
					t.Fatalf("pod is not getting ready : %v", err)
				}
			}

			for _, cluster := range t.AllClusters() {
				if _, _, err := kube.WaitUntilServiceEndpointsAreReady(cluster.Kube(), istioSystemNS.Name(), "jwt-server"); err != nil {
					t.Fatalf("Wait for jwt-server server failed: %v", err)
				}
			}

			cases := []struct {
				name          string
				policyFile    string
				customizeCall func(t resource.Context, from echo.Instance, opts *echo.CallOptions)
			}{
				{
					name:       "valid-token-forward-remote-jwks",
					policyFile: "./testdata/remotehttps.yaml.tmpl",
					customizeCall: func(t resource.Context, from echo.Instance, opts *echo.CallOptions) {
						opts.HTTP.Path = "/valid-token-forward-remote-jwks"
						opts.HTTP.Headers = headers.New().WithAuthz(jwt.TokenIssuer1).Build()
						opts.Check = check.And(
							check.OK(),
							scheck.ReachedClusters(t.AllClusters(), opts),
							check.RequestHeaders(map[string]string{
								headers.Authorization: "Bearer " + jwt.TokenIssuer1,
								"X-Test-Payload":      payload1,
							}))
					},
				},
			}

			for _, c := range cases {
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							args := map[string]string{
								"Namespace": ns.Name(),
								"dst":       to.Config().Service,
							}
							return t.ConfigIstio().EvalFile(ns.Name(), args, c.policyFile).Apply(apply.Wait)
						}).
						FromMatch(
							// TODO(JimmyCYJ): enable VM for all test cases.
							util.SourceMatcher(ns, true)).
						ConditionallyTo(echotest.ReachableDestinations).
						ToMatch(util.DestMatcher(ns, true)).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							opts := echo.CallOptions{
								To: to,
								Port: echo.Port{
									Name: "http",
								},
								Count: util.CallsPerCluster * to.WorkloadsOrFail(t).Len(),
							}

							c.customizeCall(t, from, &opts)

							from.CallOrFail(t, opts)
						})
				})
			}
		})
}
