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
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/connection"
	"istio.io/pkg/log"
	"strconv"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

type sniffingTestCase struct {
	portName string
	from, to echo.Instance
	scheme   scheme.Instance
}

func TestSniffing(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			runTest(t, ctx)
		})
}

func runTest(t *testing.T, ctx framework.TestContext) {
	ns := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "protocolsniffing",
		Inject: true,
	})

	ports := []echo.Port{
		{
			Name:     "foo",
			Protocol: protocol.HTTP,
			// We use a port > 1024 to not require root
			InstancePort: 8090,
		},
		{
			Name:     "http",
			Protocol: protocol.HTTP,
			// We use a port > 1024 to not require root
			InstancePort: 8091,
		},
		{
			Name:     "bar",
			Protocol: protocol.TCP,
		},
		{
			Name:     "tcp",
			Protocol: protocol.TCP,
		},
		{
			Name:     "baz",
			Protocol: protocol.GRPC,
		},
		{
			Name:     "grpc",
			Protocol: protocol.GRPC,
		},
	}

	echos := echoboot.NewBuilderOrFail(t, ctx).
		WithClusters(ctx.Clusters()).
		With(nil, echo.Config{
			Service:   "from-with-sidecar-%d",
			Namespace: ns,
			Ports:     ports,
			Subsets:   []echo.SubsetConfig{{}},
			SetupFn:   setupForCluster,
		}).
		With(nil, echo.Config{
			Service:   "from-without-sidecar-%d",
			Namespace: ns,
			Ports:     ports,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarInject: {
							Value: strconv.FormatBool(false)},
					},
				},
			},
			SetupFn: setupForCluster,
		}).
		With(nil, echo.Config{
			Service:   "to-%d",
			Namespace: ns,
			Subsets:   []echo.SubsetConfig{{}},
			Ports:     ports,
			SetupFn:   setupForCluster,
		}).
		BuildOrFail(ctx)
	sourcesWithSidecar := echos.GetByServiceNamePrefix("from-with-sidecar-")
	sourcesWithoutSidecar := echos.GetByServiceNamePrefix("from-without-sidecar-")
	targets := echos.GetByServiceNamePrefix("to-")

	var testCases []sniffingTestCase

	for _, srcCluster := range ctx.Clusters() {
		for _, dstCluster := range ctx.Clusters() {
			fromWithSidecar, fromWithoutSidecar := sourcesWithSidecar[srcCluster.Index()], sourcesWithoutSidecar[srcCluster.Index()]
			to := targets[dstCluster.Index()]

			fromWithSidecar.WaitUntilCallableOrFail(t, to)
			fromWithoutSidecar.WaitUntilCallableOrFail(t, to)
			log.Infof("%s app ready: %s %s",
				ctx.Name(),
				fromWithSidecar.Config().Service,
				fromWithoutSidecar.Config().Service)

			testCases = append(testCases,
				sniffingTestCase{
					portName:
					"foo",
					from:   fromWithSidecar,
					to:     to,
					scheme: scheme.HTTP,
				},
				sniffingTestCase{
					portName:
					"http",
					from:   fromWithSidecar,
					to:     to,
					scheme: scheme.HTTP,
				},
				sniffingTestCase{
					portName:
					"baz",
					from:   fromWithSidecar,
					to:     to,
					scheme: scheme.GRPC,
				},
				sniffingTestCase{
					portName:
					"grpc",
					from:   fromWithSidecar,
					to:     to,
					scheme: scheme.GRPC,
				},
				sniffingTestCase{
					portName:
					"foo",
					from:   fromWithoutSidecar,
					to:     to,
					scheme: scheme.HTTP,
				},
				sniffingTestCase{
					portName:
					"http",
					from:   fromWithoutSidecar,
					to:     to,
					scheme: scheme.HTTP,
				},
				sniffingTestCase{
					portName:
					"baz",
					from:   fromWithoutSidecar,
					to:     to,
					scheme: scheme.GRPC,
				},
				sniffingTestCase{
					portName:
					"grpc",
					from:   fromWithoutSidecar,
					to:     to,
					scheme: scheme.GRPC,
				},
			)
		}

		for _, tc := range testCases {
			connChecker := connection.Checker{
				From: tc.from,
				Options: echo.CallOptions{
					Target:   tc.to,
					PortName: tc.portName,
					Scheme:   tc.scheme,
				},
				ExpectSuccess: true,
			}
			subTestName := fmt.Sprintf(
				"%s->%s:%s",
				tc.from.Config().Service,
				tc.to.Config().Service,
				connChecker.Options.PortName)

			ctx.NewSubTest(subTestName).
				RunParallel(func(ctx framework.TestContext) {
					retry.UntilSuccessOrFail(ctx, connChecker.Check,
						retry.Delay(time.Second),
						retry.Timeout(10*time.Second))
				})
		}
	}
}
