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

func TestOutboundSniffing(t *testing.T) {
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
		},
		{
			Name:     "http",
			Protocol: protocol.HTTP,
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

	var from, to echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&from, echo.Config{
			Service:   "from",
			Namespace: ns,
			Ports:     ports,
			Galley:    g,
			Pilot:     p,
		}).
		With(&to, echo.Config{
			Service:   "to",
			Namespace: ns,
			Ports:     ports,
			Galley:    g,
			Pilot:     p,
		}).
		BuildOrFail(ctx)

	from.WaitUntilCallableOrFail(t, to)
	log.Infof("%s app ready: %s", ctx.Name(), from.Config().Service)

	testCases := []struct {
		portName string
		scheme   scheme.Instance
	}{
		{"foo", scheme.HTTP},
		{"http", scheme.HTTP},
		{"baz", scheme.GRPC},
		{"grpc", scheme.GRPC},
	}

	for _, tc := range testCases {
		connChecker := connection.Checker{
			From: from,
			Options: echo.CallOptions{
				Target:   to,
				PortName: tc.portName,
				Scheme:   tc.scheme,
			},
			ExpectSuccess: true,
		}
		subTestName := fmt.Sprintf(
			"%s->%s:%s",
			from.Config().Service,
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
