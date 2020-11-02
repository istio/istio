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

	"istio.io/istio/tests/integration/security/util"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

// TestPassThroughFilterChain tests the authN and authZ policy on the pass through filter chain.
func TestPassThroughFilterChain(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.filterchain").
		Run(func(ctx framework.TestContext) {
			ns := apps.Namespace2
			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/pass-through-filter-chain.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns.Name(), policies...)

			callCount := 1
			if ctx.Clusters().IsMulticluster() {
				callCount = util.CallsPerCluster * len(ctx.Clusters())
			}

			for _, cluster := range ctx.Clusters() {
				aSet := apps.A.Match(echo.Namespace(ns.Name())).Match(echo.InCluster(cluster))
				bSet := apps.B.Match(echo.Namespace(ns.Name())).Match(echo.InCluster(cluster))
				cSet := apps.C.Match(echo.Namespace(ns.Name())).Match(echo.InCluster(cluster))
				dSet := apps.D.Match(echo.Namespace(ns.Name())).Match(echo.InCluster(cluster))

				cases := []struct {
					target echo.Instance
					port   int
					schema protocol.Instance
					want   bool
				}{
					// For workload a, there is no authN/authZ policy.
					// All requests should success, this is to verify the pass through filter chain and
					// the workload ports are working correctly.
					{
						target: aSet[0],
						port:   8085,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: aSet[0],
						port:   8086,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: aSet[0],
						port:   8087,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: aSet[0],
						port:   8088,
						schema: protocol.TCP,
						want:   true,
					},

					// For workload b, there is only authZ policy that allows access to port 8085 and 8087.
					// Only request to port 8085, 8087 should be allowed.
					{
						target: bSet[0],
						port:   8085,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: bSet[0],
						port:   8086,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: bSet[0],
						port:   8087,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: bSet[0],
						port:   8088,
						schema: protocol.TCP,
						want:   false,
					},

					// For workload c, there is only authN policy that enables mTLS (Strict).
					// The request should be denied because the x is always using plain text.
					{
						target: cSet[0],
						port:   8085,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: cSet[0],
						port:   8086,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: cSet[0],
						port:   8087,
						schema: protocol.TCP,
						want:   false,
					},
					{
						target: cSet[0],
						port:   8088,
						schema: protocol.TCP,
						want:   false,
					},

					// For workload d, there is only authN policy that enables mTLS (Permissive).
					// The request should be allowed because the x is always using plain text.
					{
						target: dSet[0],
						port:   8085,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: dSet[0],
						port:   8086,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: dSet[0],
						port:   8087,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: dSet[0],
						port:   8088,
						schema: protocol.TCP,
						want:   true,
					},
				}
				ctx.NewSubTest(fmt.Sprintf("In %s", cluster.Name())).Run(func(ctx framework.TestContext) {
					for _, tc := range cases {
						name := fmt.Sprintf("x->%s:%d[%t]", tc.target.Config().Service, tc.port, tc.want)
						a := apps.A.Match(echo.InCluster(cluster)).Match(echo.Namespace(ns.Name()))[0]
						from := getWorkload(a, t)
						// The request should be handled by the pass through filter chain.
						host := fmt.Sprintf("%s:%d", getWorkload(tc.target, t).Address(), tc.port)
						request := &epb.ForwardEchoRequest{
							Url:     fmt.Sprintf("%s://%s", tc.schema, host),
							Count:   int32(callCount),
							Message: "HelloWorld",
							Headers: []*epb.Header{
								{
									Key:   "Host",
									Value: host,
								},
							},
						}
						ctx.NewSubTest(name).Run(func(ctx framework.TestContext) {
							retry.UntilSuccessOrFail(t, func() error {
								responses, err := from.ForwardEcho(context.TODO(), request)
								if tc.want {
									if err != nil {
										return fmt.Errorf("want allow but got error: %v", err)
									}
									if len(responses) < 1 {
										return fmt.Errorf("received no responses from request to %s", host)
									}
									if tc.schema == protocol.HTTP && response.StatusCodeOK != responses[0].Code {
										return fmt.Errorf("want status %s but got %s", response.StatusCodeOK, responses[0].Code)
									}
									if ctx.Clusters().IsMulticluster() {
										responses.CheckReachedClusters(ctx.Clusters())
									}
								} else {
									// Check HTTP forbidden response
									if len(responses) >= 1 && response.StatusCodeForbidden == responses[0].Code {
										return nil
									}

									if err == nil || !strings.Contains(err.Error(), "EOF") {
										return fmt.Errorf("want error EOF but got: %v", err)
									}
								}
								return nil
							}, retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
						})
					}
				})

			}
		})
}

func getWorkload(instance echo.Instance, t *testing.T) echo.Workload {
	workloads, err := instance.Workloads()
	if err != nil {
		t.Fatalf(fmt.Sprintf("failed to get Subsets: %v", err))
	}
	if len(workloads) < 1 {
		t.Fatalf("want at least 1 workload but found 0")
	}
	return workloads[0]
}
