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

package multicluster

import (
	"fmt"
	"os"
	"testing"

	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	requestCount = 20
	targetCount  = int(float32(requestCount) * .8)
)

func SecurityTest(t *testing.T, apps AppContext, features ...features.Feature) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Features(features...).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("security").
				Run(func(ctx framework.TestContext) {
					ns := namespace.NewOrFail(t, ctx, namespace.Config{
						Prefix: "security",
						Inject: true,
					})
					clusters := ctx.Environment().Clusters()
					var a1, a2, b1, b2 echo.Instance
					echoboot.NewBuilderOrFail(ctx, ctx).
						With(&a1, newEchoConfig("a", ns, clusters[0])).
						With(&a2, newEchoConfig("a", ns, clusters[1])).
						With(&b1, newEchoConfig("b", ns, clusters[0])).
						With(&b2, newEchoConfig("b", ns, clusters[1])).
						BuildOrFail(t)

					var responses client.ParsedResponses
					retry.UntilSuccessOrFail(t, func() error {
						r, err := a1.Call(echo.CallOptions{
							Count:    requestCount,
							Target:   b1,
							PortName: "grpc",
							Headers: map[string][]string{
								"Host": {b1.Config().FQDN()},
							},
							Message: t.Name(),
						})
						if err != nil {
							return err
						}
						if err := r.CheckOK(); err != nil {
							return err
						}
						responses = r
						return nil
					})

					// For non GKE clusters or ASM with Citadel, the trust domain is cluster.local.
					if (os.Getenv("GCR_PROJECT_ID") == "" && os.Getenv("GCR_PROJECT_ID_1") == "") || os.Getenv("CA") == "CITADEL" {
						checkClusterLocalPrincipal(t, responses, ns)
					} else {
						checkGCPPrincipal(t, responses, ns)
					}
				})
		})
}

func checkClusterLocalPrincipal(t *testing.T, responses client.ParsedResponses, ns namespace.Instance) {
	wantPrincipals := []string{
		fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/a", ns.Name()),
		fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/b", ns.Name()),
	}
	// Check the principal in the response to verify that mTLS is enabled and working as expected.
	for _, principal := range wantPrincipals {
		count := responses.Count(principal)
		if count < targetCount {
			t.Errorf("expected at least %d occurrences of %q in responses; got %d", targetCount, principal, count)
		}
	}
}

func checkGCPPrincipal(t *testing.T, responses client.ParsedResponses, ns namespace.Instance) {
	// TODO: Enable Principal check against specific project.
	// Currently the GCR_PROJECT_ID_* environment variable is not reliable.
	// There can be mismatch between id and cluster context.
	wantPrincipals := []string{
		fmt.Sprintf("svc.id.goog/ns/%s/sa/a", ns.Name()),
		fmt.Sprintf("svc.id.goog/ns/%s/sa/b", ns.Name()),
	}
	if os.Getenv("WIP") == "HUB" {
		wantPrincipals = []string{
			fmt.Sprintf("hub.id.goog/ns/%s/sa/a", ns.Name()),
			fmt.Sprintf("hub.id.goog/ns/%s/sa/b", ns.Name()),
		}
	}
	// Check the principal in the response to verify that mTLS is enabled and working as expected.
	for _, principal := range wantPrincipals {
		count := responses.Count(principal)
		expectCount := targetCount
		if count < expectCount {
			t.Errorf("expected at least %d occurrences of %q in responses; got %d", expectCount, principal, count)
		}
	}
}
