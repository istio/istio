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

package pilot

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/connection"
)

func TestSniffing(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
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

	var fromWithSidecar, fromWithoutSidecar, to echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&fromWithSidecar, echo.Config{
			Service:   "from-with-sidecar",
			Namespace: ns,
			Ports:     ports,
			Galley:    g,
			Pilot:     p,
		}).
		With(&fromWithoutSidecar, echo.Config{
			Service:   "from-without-sidecar",
			Namespace: ns,
			Ports:     ports,
			Galley:    g,
			Pilot:     p,
			Annotations: map[echo.Annotation]*echo.AnnotationValue{
				echo.SidecarInject: {
					Value: strconv.FormatBool(false)},
			},
		}).
		With(&to, echo.Config{
			Service:   "to",
			Namespace: ns,
			Ports:     ports,
			Galley:    g,
			Pilot:     p,
		}).
		BuildOrFail(ctx)

	fromWithSidecar.WaitUntilCallableOrFail(t, to)
	fromWithoutSidecar.WaitUntilCallableOrFail(t, to)
	log.Infof("%s app ready: %s %s",
		ctx.Name(),
		fromWithSidecar.Config().Service,
		fromWithoutSidecar.Config().Service)

	testCases := []struct {
		portName string
		from     echo.Instance
		scheme   scheme.Instance
	}{
		{
			portName: "foo",
			from:     fromWithSidecar,
			scheme:   scheme.HTTP,
		},
		{
			portName: "http",
			from:     fromWithSidecar,
			scheme:   scheme.HTTP,
		},
		{
			portName: "baz",
			from:     fromWithSidecar,
			scheme:   scheme.GRPC,
		},
		{
			portName: "grpc",
			from:     fromWithSidecar,
			scheme:   scheme.GRPC,
		},
		{
			portName: "foo",
			from:     fromWithoutSidecar,
			scheme:   scheme.HTTP,
		},
		{
			portName: "http",
			from:     fromWithoutSidecar,
			scheme:   scheme.HTTP,
		},
		{
			portName: "baz",
			from:     fromWithoutSidecar,
			scheme:   scheme.GRPC,
		},
		{
			portName: "grpc",
			from:     fromWithoutSidecar,
			scheme:   scheme.GRPC,
		},
	}

	for _, tc := range testCases {
		connChecker := connection.Checker{
			From: tc.from,
			Options: echo.CallOptions{
				Target:   to,
				PortName: tc.portName,
				Scheme:   tc.scheme,
			},
			ExpectSuccess: true,
		}
		subTestName := fmt.Sprintf(
			"%s->%s:%s",
			tc.from.Config().Service,
			to.Config().Service,
			connChecker.Options.PortName)

		t.Run(subTestName,
			func(t *testing.T) {
				retry.UntilSuccessOrFail(t, connChecker.Check,
					retry.Delay(time.Second),
					retry.Timeout(10*time.Second))
			})
	}
}
