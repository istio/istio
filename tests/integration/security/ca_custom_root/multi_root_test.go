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
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/tests/integration/security/util/scheck"
)

func TestMultiRootSetup(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.multiple-root").
		Run(func(t framework.TestContext) {
			testNS := apps.Namespace

			t.ConfigIstio().YAML(testNS.Name(), POLICY).ApplyOrFail(t)

			for _, cluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(t framework.TestContext) {
					verify := func(ctx framework.TestContext, from echo.Instance, to echo.Instances, s scheme.Instance, success bool) {
						want := "success"
						if !success {
							want = "fail"
						}
						name := fmt.Sprintf("server:%s[%s]", to[0].Config().Service, want)
						ctx.NewSubTest(name).Run(func(t framework.TestContext) {
							t.Helper()
							opts := echo.CallOptions{
								To:    to,
								Count: 1,
								Port: echo.Port{
									Name: "https",
								},
								Address: to.Config().Service,
								Scheme:  s,
							}
							opts.Check = check.And(check.OK(), scheck.ReachedClusters(t.AllClusters(), &opts))

							from.CallOrFail(t, opts)
						})
					}

					client := match.Cluster(cluster).FirstOrFail(t, apps.Client)
					cases := []struct {
						from   echo.Instance
						to     echo.Instances
						expect bool
					}{
						{
							from:   client,
							to:     apps.ServerNakedFooAlt,
							expect: true,
						},
					}

					for _, tc := range cases {
						verify(t, tc.from, tc.to, scheme.HTTP, tc.expect)
					}
				})
			}
		})
}
