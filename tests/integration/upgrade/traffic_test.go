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

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/framework"
)

func TestTraffic(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.routing", "traffic.reachability", "traffic.shifting").
		Run(func(ctx framework.TestContext) {
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
						retry.UntilSuccessOrFail(ctx, func() error {
							resp, err := source.Call(echo.CallOptions{
								Target:   dest,
								PortName: trafficType,
							})
							if err != nil {
								return err
							}
							return resp.CheckOK()
						}, retry.Delay(time.Millisecond*150))
					})
			}
		}
	}
}
