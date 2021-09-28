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

package cacustomroot

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/tests/integration/security/util/connection"
)

func TestMultiRootSetup(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.multiple-root").
		Run(func(t framework.TestContext) {
			testNS := apps.Namespace

			t.Config().ApplyYAMLOrFail(t, testNS.Name(), POLICY)

			for _, cluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(t framework.TestContext) {
					verify := func(ctx framework.TestContext, src echo.Instance, dest echo.Instance, s scheme.Instance, success bool) {
						want := "success"
						if !success {
							want = "fail"
						}
						name := fmt.Sprintf("server:%s[%s]", dest.Config().Service, want)
						ctx.NewSubTest(name).Run(func(t framework.TestContext) {
							t.Helper()
							opt := echo.CallOptions{
								Target:   dest,
								PortName: HTTPS,
								Address:  dest.Config().Service,
								Scheme:   s,
							}
							checker := connection.Checker{
								From:          src,
								Options:       opt,
								ExpectSuccess: success,
								DestClusters:  t.Clusters(),
							}
							checker.CheckOrFail(t)
						})
					}

					client := apps.Client.GetOrFail(t, echo.InCluster(cluster))
					serverNakedFooAlt := apps.ServerNakedFooAlt.GetOrFail(t, echo.InCluster(cluster))
					cases := []struct {
						src    echo.Instance
						dest   echo.Instance
						expect bool
					}{
						{
							src:    client,
							dest:   serverNakedFooAlt,
							expect: false,
						},
					}

					for _, tc := range cases {
						verify(t, tc.src, tc.dest, scheme.HTTP, tc.expect)
					}
				})
			}
		})
}
