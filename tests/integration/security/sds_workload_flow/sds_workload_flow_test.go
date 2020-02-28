// Copyright 2019 Istio Authors
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

package sds_workload_test

import (
	"testing"
	"time"

	"istio.io/istio/tests/integration/security/util"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/connection"
)

func TestSdsWorkloadFlow(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			istioCfg := istio.DefaultConfigOrFail(t, ctx)

			namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "sds-workload-flow",
				Inject: true,
			})

			var a, b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				BuildOrFail(t)

			checkers := []connection.Checker{
				{
					From: a,
					Options: echo.CallOptions{
						Target:   b,
						PortName: "http",
						Scheme:   scheme.HTTP,
					},
					ExpectSuccess: true,
				},
			}

			for _, checker := range checkers {
				retry.UntilSuccessOrFail(t, checker.Check, retry.Delay(time.Second), retry.Timeout(10*time.Second))
			}
		})
}
