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

package upgrade

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

func TestTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			skipIfK8sVersionUnsupported(ctx)
			testAllEchoCalls(ctx, apps.All)
		})
}

func testAllEchoCalls(ctx framework.TestContext, echoInstances echo.Instances) {
	trafficTypes := []string{"http", "tcp", "grpc"}
	for _, source := range echoInstances {
		for _, dest := range echoInstances {
			if source == dest {
				continue
			}
			for _, trafficType := range trafficTypes {
				ctx.NewSubTest(fmt.Sprintf("%s-%s->%s", trafficType, source.Config().Service, dest.Config().Service)).
					Run(func(ctx framework.TestContext) {
						resps := source.CallWithRetryOrFail(ctx, echo.CallOptions{
							Target:   dest,
							PortName: trafficType,
						}, retry.Delay(time.Millisecond*150))
						resps.CheckOKOrFail(ctx)
					})
			}
		}
	}
}

// skipIfK8sVersionUnsupported skips the test if we're running on a k8s version that is not expected to work
// with any of the revision versions included in the test (i.e. istio 1.7 not supported on k8s 1.15)
func skipIfK8sVersionUnsupported(ctx framework.TestContext) {
	ver, err := ctx.Clusters().Default().GetKubernetesVersion()
	if err != nil {
		ctx.Fatalf("failed to get Kubernetes version: %v", err)
	}
	serverVersion := fmt.Sprintf("%s.%s", ver.Major, ver.Minor)
	ctx.Name()
	if serverVersion < "1.16" {
		ctx.Skipf("k8s version %s not supported for %s (<%s)", serverVersion, ctx.Name(), "1.16")
	}
}
