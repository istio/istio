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
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
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

// TestJWTHTTPS tests the requestauth policy with https jwks server.
func TestJWTHTTPS(t *testing.T) {
	payload1 := strings.Split(jwt.TokenIssuer1, ".")[1]

	framework.NewTest(t).
		Features("security.authentication.jwt").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1
			istioSystemNS := istio.ClaimSystemNamespaceOrFail(t, t)
			args := map[string]string{"Namespace": istioSystemNS.Name()}
			applyYAML := func(filename string, ns namespace.Instance) []string {
				policy := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, filename))
				for _, cluster := range t.AllClusters() {
					t.ConfigKube(cluster).ApplyYAMLOrFail(t, ns.Name(), policy...)
				}
				return policy
			}
			jwtServer := applyYAML(filepath.Join(env.IstioSrc, "samples/jwt-server", "jwt-server.yaml"), istioSystemNS)

			defer func() {
				for _, cluster := range t.AllClusters() {
					t.ConfigKube(cluster).DeleteYAMLOrFail(t, istioSystemNS.Name(), jwtServer...)
				}
			}()

			for _, cluster := range t.AllClusters() {
				fetchFn := kube.NewSinglePodFetch(cluster, istioSystemNS.Name(), "app=jwt-server")
				_, err := kube.WaitUntilPodsAreReady(fetchFn)
				if err != nil {
					t.Fatalf("pod is not getting ready : %v", err)
				}
			}

			for _, cluster := range t.AllClusters() {
				if _, _, err := kube.WaitUntilServiceEndpointsAreReady(cluster, istioSystemNS.Name(), "jwt-server"); err != nil {
					t.Fatalf("Wait for jwt-server server failed: %v", err)
				}
			}

			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}

			t.NewSubTest("jwt-authn").Run(func(t framework.TestContext) {
				testCase := authn.TestCase{
					Name:   "valid-token-forward-remote-jwks",
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
					ExpectResponseCode: http.StatusOK,
					ExpectHeaders: map[string]string{
						authHeaderKey:    "Bearer " + jwt.TokenIssuer1,
						"X-Test-Payload": payload1,
					},
					// This test does not generate cross-cluster traffic, but is flaky
					// in multicluster test. Skip in multicluster mesh.
					// TODO(JimmyCYJ): enable the test in multicluster mesh.
					SkipMultiCluster: true,
				}

				if testCase.SkipMultiCluster && t.Clusters().IsMulticluster() {
					t.Skip()
				}
				echotest.New(t, apps.All).
					SetupForDestination(func(t framework.TestContext, dst echo.Instances) error {
						if testCase.Config != "" {
							policy := yml.MustApplyNamespace(t, tmpl.MustEvaluate(
								file.AsStringOrFail(t, fmt.Sprintf("./testdata/%s.yaml.tmpl", testCase.Config)),
								map[string]string{
									"Namespace": ns.Name(),
									"dst":       dst[0].Config().Service,
								},
							), ns.Name())
							if err := t.ConfigIstio().ApplyYAML(ns.Name(), policy); err != nil {
								t.Logf("failed to apply security config %s: %v", testCase.Config, err)
								return err
							}
							t.ConfigIstio().WaitForConfigOrFail(t, t, ns.Name(), policy)
						}
						return nil
					}).
					From(
						// TODO(JimmyCYJ): enable VM for all test cases.
						util.SourceFilter(t, apps, ns.Name(), true)...).
					ConditionallyTo(echotest.ReachableDestinations).
					To(util.DestFilter(t, apps, ns.Name(), true)...).
					Run(func(t framework.TestContext, src echo.Instance, dest echo.Instances) {
						t.NewSubTest(testCase.Name).Run(func(t framework.TestContext) {
							testCase.CallOpts.Target = dest[0]
							testCase.DestClusters = dest.Match(echo.InCluster(src.Config().Cluster)).Clusters()
							testCase.CallOpts.Check = testCase.CheckAuthn
							src.CallWithRetryOrFail(t, testCase.CallOpts, echo.DefaultCallRetryOptions()...)
						})
					})
			})
		})
}
