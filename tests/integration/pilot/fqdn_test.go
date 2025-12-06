//go:build integ

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

package pilot

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
)

// TestAbsoluteFQDNWithTrailingDot verifies requests with trailing dots in Host headers
// (e.g., `foo.svc.cluster.local.`) are routed correctly.
func TestAbsoluteFQDNWithTrailingDot(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		src := match.ServiceName(apps.A.NamespacedName()).GetMatches(apps.A.Instances())[0]
		dstB := apps.B.Instances()
		dstC := apps.C.Instances()

		t.NewSubTest("FQDN with trailing dot").Run(func(t framework.TestContext) {
			src.CallOrFail(t, echo.CallOptions{
				To:   dstB,
				Port: echo.Port{Name: "http"},
				HTTP: echo.HTTP{
					Path: "/",
					Headers: map[string][]string{
						"Host": {dstB[0].Config().ClusterLocalFQDN() + "."},
					},
				},
				Check: check.And(check.OK(), check.Host(dstB[0].Config().Service)),
			})
		})

		t.NewSubTest("with port").Run(func(t framework.TestContext) {
			port := dstB[0].Config().Ports.MustForName("http")
			src.CallOrFail(t, echo.CallOptions{
				To:   dstB,
				Port: echo.Port{Name: "http"},
				HTTP: echo.HTTP{
					Path: "/",
					Headers: map[string][]string{
						"Host": {dstB[0].Config().ClusterLocalFQDN() + ".:" + fmt.Sprintf("%d", port.ServicePort)},
					},
				},
				Check: check.And(check.OK(), check.Host(dstB[0].Config().Service)),
			})
		})

		t.NewSubTest("service disambiguation").Run(func(t framework.TestContext) {
			src.CallOrFail(t, echo.CallOptions{
				To:   dstB,
				Port: echo.Port{Name: "http"},
				HTTP: echo.HTTP{
					Headers: map[string][]string{"Host": {dstB[0].Config().ClusterLocalFQDN() + "."}},
				},
				Check: check.And(check.OK(), check.Host(dstB[0].Config().Service)),
			})
			src.CallOrFail(t, echo.CallOptions{
				To:   dstC,
				Port: echo.Port{Name: "http"},
				HTTP: echo.HTTP{
					Headers: map[string][]string{"Host": {dstC[0].Config().ClusterLocalFQDN() + "."}},
				},
				Check: check.And(check.OK(), check.Host(dstC[0].Config().Service)),
			})
		})
	})
}
