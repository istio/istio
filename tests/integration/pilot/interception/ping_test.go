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

package interception

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/connection"
	"istio.io/pkg/log"
)

var (
	ist istio.Instance
	g   galley.Instance
	p   pilot.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("inbound_split_test", m).
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()

}

type appConnectionPair struct {
	src, dst echo.Instance
}

func TestReachability(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		doTest(t, ctx)
	})
}

func doTest(t *testing.T, ctx framework.TestContext) {
	ns := namespace.NewOrFail(t, ctx, "inboundsplit", true)

	ports := []echo.Port{
		{
			Name:     "http",
			Protocol: model.ProtocolHTTP,
		},
		{
			Name:     "tcp",
			Protocol: model.ProtocolTCP,
		},
	}

	var inoutUnitedApp0, inoutUnitedApp1, inoutSplitApp0, inoutSplitApp1 echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&inoutSplitApp0, echo.Config{
			Service:             "inoutsplitapp0",
			Namespace:           ns,
			Ports:               ports,
			Galley:              g,
			Pilot:               p,
			IncludeInboundPorts: "*",
		}).
		With(&inoutSplitApp1, echo.Config{
			Service:             "inoutsplitapp1",
			Namespace:           ns,
			Ports:               ports,
			Galley:              g,
			Pilot:               p,
			IncludeInboundPorts: "*",
		}).
		With(
			&inoutUnitedApp0, echo.Config{
				Service:   "inoutunitedapp0",
				Namespace: ns,
				Ports:     ports,
				Galley:    g,
				Pilot:     p,
			}).
		With(&inoutUnitedApp1, echo.Config{
			Service:   "inoutunitedapp1",
			Namespace: ns,
			Ports:     ports,
			Galley:    g,
			Pilot:     p,
		}).
		BuildOrFail(ctx)

	inoutUnitedApp0.WaitUntilCallableOrFail(t, inoutUnitedApp1)
	log.Infof("%s app ready: %s", ctx.Name(), inoutSplitApp0.Config().Service)
	inoutSplitApp0.WaitUntilCallableOrFail(t, inoutUnitedApp1)
	log.Infof("%s app ready: %s", ctx.Name(), inoutSplitApp0.Config().Service)

	connectivityPairs := []appConnectionPair{
		// source is inout united
		{inoutUnitedApp0, inoutUnitedApp1},
		{inoutUnitedApp0, inoutSplitApp1},

		// source is inout split
		{inoutSplitApp0, inoutUnitedApp1},
		{inoutSplitApp0, inoutSplitApp1},

		// self connectivity (is it required?)
		{inoutUnitedApp0, inoutUnitedApp0},
		{inoutSplitApp0, inoutSplitApp0},
	}

	for _, pair := range connectivityPairs {
		connChecker := connection.Checker{
			From: pair.src,
			Options: echo.CallOptions{
				Target:   pair.dst,
				PortName: strings.ToLower(string(scheme.HTTP)),
				Scheme:   scheme.HTTP,
			},
			ExpectSuccess: true,
		}
		subTestName := fmt.Sprintf(
			"%s->%s:%s",
			pair.src.Config().Service,
			pair.dst.Config().Service,
			connChecker.Options.PortName)

		t.Run(subTestName,
			func(t *testing.T) {
				retry.UntilSuccessOrFail(t, connChecker.Check,
					retry.Delay(time.Second),
					retry.Timeout(10*time.Second))
			})
	}
}
