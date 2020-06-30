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
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/connection"
	"istio.io/pkg/log"
)

type appConnectionPair struct {
	src, dst echo.Instance
}

type callOptions struct {
	portName string
	scheme   scheme.Instance
}

func TestReachability(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		doTest(t, ctx)
	})
}

func doTest(t *testing.T, ctx framework.TestContext) {
	ns := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "inboundsplit",
		Inject: true,
	})

	ports := []echo.Port{
		{
			Name:     "foo",
			Protocol: protocol.HTTP,
			// We use a port > 1024 to not require root
			InstancePort: 8091,
		},
		{
			Name:     "http",
			Protocol: protocol.HTTP,
			// We use a port > 1024 to not require root
			InstancePort: 8090,
		},
		{
			Name:     "tcp",
			Protocol: protocol.TCP,
		},
	}

	echos := echoboot.NewBuilderOrFail(t, ctx).
		With(nil, echo.Config{
			Service:             "inoutsplitapp0",
			Namespace:           ns,
			Ports:               ports,
			Subsets:             []echo.SubsetConfig{{}},
			SetupFn:             setupForCluster,
			IncludeInboundPorts: "*",
		}).
		With(nil, echo.Config{
			Service:             "inoutsplitapp1",
			Namespace:           ns,
			Subsets:             []echo.SubsetConfig{{}},
			Ports:               ports,
			SetupFn:             setupForCluster,
			IncludeInboundPorts: "*",
		}).
		With(nil, echo.Config{
			Service:   "inoutunitedapp0",
			Namespace: ns,
			Subsets:   []echo.SubsetConfig{{}},
			Ports:     ports,
			SetupFn:   setupForCluster,
		}).
		With(nil, echo.Config{
			Service:   "inoutunitedapp1",
			Namespace: ns,
			Subsets:   []echo.SubsetConfig{{}},
			Ports:     ports,
			SetupFn:   setupForCluster,
		}).
		BuildOrFail(ctx)

	inoutSplitSources := echos.GetByServiceName("inoutsplitapp0")
	inoutSplitTargets := echos.GetByServiceName("inoutsplitapp1")
	inoutUnitedSources := echos.GetByServiceName("inoutunitedapp0")
	inoutUnitedTargets := echos.GetByServiceName("inoutunitedapp1")

	var connectivityPairs []appConnectionPair
	for _, src := range ctx.Clusters() {
		for _, dst := range ctx.Clusters() {
			inoutUnitedApp0, inoutSplitApp0 := inoutUnitedSources[src.Index()], inoutSplitSources[src.Index()]
			inoutUnitedApp1 := inoutUnitedTargets[dst.Index()]
			inoutUnitedApp0.WaitUntilCallableOrFail(t, inoutUnitedApp1)
			log.Infof("%s app ready: %s", ctx.Name(), inoutSplitApp0.Config().Service)
			inoutSplitApp0.WaitUntilCallableOrFail(t, inoutUnitedApp1)
			log.Infof("%s app ready: %s", ctx.Name(), inoutSplitApp0.Config().Service)
			inoutSplitApp1 := inoutSplitTargets[dst.Index()]

			connectivityPairs = append(connectivityPairs,
				// source is inout united
				appConnectionPair{inoutUnitedApp0, inoutUnitedApp1},
				appConnectionPair{inoutUnitedApp0, inoutSplitApp1},

				// source is inout split
				appConnectionPair{inoutSplitApp0, inoutUnitedApp1},
				appConnectionPair{inoutSplitApp0, inoutSplitApp1},

				// self connectivity (is it required?)
				appConnectionPair{inoutUnitedApp0, inoutUnitedApp0},
				appConnectionPair{inoutSplitApp0, inoutSplitApp0},
			)
		}
	}

	// TODO(yxue): support sending raw TCP traffic instead of HTTP
	callOptions := []callOptions{
		{"http", scheme.HTTP},
		{"foo", scheme.HTTP},
	}

	for _, pair := range connectivityPairs {
		for _, opt := range callOptions {
			connChecker := connection.Checker{
				From: pair.src,
				Options: echo.CallOptions{
					Target:   pair.dst,
					PortName: opt.portName,
					Scheme:   opt.scheme,
				},
				ExpectSuccess: true,
			}
			subTestName := fmt.Sprintf(
				"%s->%s:%s",
				pair.src.Config().Service,
				pair.dst.Config().Service,
				connChecker.Options.PortName)

			ctx.NewSubTest(subTestName).RunParallel(func(ctx framework.TestContext) {
				retry.UntilSuccessOrFail(ctx, connChecker.Check,
					retry.Delay(time.Second),
					retry.Timeout(10*time.Second))
			})
		}
	}
}
