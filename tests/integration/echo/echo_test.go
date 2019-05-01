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

package echo

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

var (
	ist istio.Instance
)

// TODO(sven): Add additional testing of the echo component, this is just the basics.
func TestEcho(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		g := galley.NewOrFail(t, ctx, galley.Config{})
		p := pilot.NewOrFail(t, ctx, pilot.Config{
			Galley: g,
		})

		ns := namespace.NewOrFail(t, ctx, "test", true)
		a := echoboot.NewOrFail(ctx, t, echo.Config{
			Pilot:     p,
			Galley:    g,
			Namespace: ns,
			Sidecar:   true,
			Service:   "a",
			Version:   "v1",
		})
		b := echoboot.NewOrFail(ctx, t, echo.Config{
			Pilot:     p,
			Galley:    g,
			Namespace: ns,
			Sidecar:   true,
			Service:   "b",
			Version:   "v2",
			Ports: []echo.Port{
				{
					Name:     "http",
					Protocol: model.ProtocolHTTP,
				},
			}})

		a.WaitUntilReadyOrFail(t, b)

		_ = a.CallOrFail(t, echo.CallOptions{
			Target:   b,
			PortName: "http",
		}).CheckOKOrFail(t)
	})
}

func TestEchoNoSidecar(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		g := galley.NewOrFail(t, ctx, galley.Config{})
		p := pilot.NewOrFail(t, ctx, pilot.Config{
			Galley: g,
		})

		ns := namespace.NewOrFail(t, ctx, "test", true)
		a := echoboot.NewOrFail(ctx, t, echo.Config{
			Pilot:     p,
			Galley:    g,
			Namespace: ns,
			Service:   "a",
			Version:   "v1",
		})
		b := echoboot.NewOrFail(ctx, t, echo.Config{
			Pilot:     p,
			Galley:    g,
			Namespace: ns,
			Service:   "b",
			Version:   "v2",
			Ports: []echo.Port{
				{
					Name:     "http",
					Protocol: model.ProtocolHTTP,
				},
			}})

		a.WaitUntilReadyOrFail(t, b)

		_ = a.CallOrFail(t, echo.CallOptions{
			Target:   b,
			PortName: "http",
		}).CheckOKOrFail(t)
	})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("echo_test", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		Run()

}
