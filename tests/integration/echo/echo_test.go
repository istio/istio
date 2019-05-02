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
	"fmt"
	"testing"

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

		baseCfg := echo.Config{
			Pilot:  p,
			Galley: g,
			Ports: []echo.Port{
				{
					Name:        "http",
					Protocol:    model.ProtocolHTTP,
					ServicePort: 80,
				},
				{
					Name:        "tcp",
					Protocol:    model.ProtocolTCP,
					ServicePort: 90,
				},
				{
					Name:        "grpc",
					Protocol:    model.ProtocolGRPC,
					ServicePort: 70,
				},
			},
		}

		configs := []struct {
			testName string
			apply    func(name string, ns namespace.Instance) echo.Config
		}{
			{
				testName: "Headless",
				apply: func(name string, ns namespace.Instance) echo.Config {
					cfg := baseCfg
					cfg.Service = name
					cfg.Namespace = ns
					cfg.Sidecar = true
					cfg.Headless = true
					return cfg
				},
			},
			{
				testName: "Sidecar",
				apply: func(name string, ns namespace.Instance) echo.Config {
					cfg := baseCfg
					cfg.Service = name
					cfg.Namespace = ns
					cfg.Sidecar = true
					return cfg
				},
			},
			{
				testName: "NoSidecar",
				apply: func(name string, ns namespace.Instance) echo.Config {
					cfg := baseCfg
					cfg.Service = name
					cfg.Namespace = ns
					cfg.Sidecar = false
					return cfg
				},
			},
		}

		callOptions := []echo.CallOptions{
			{
				PortName: "http",
				Scheme:   scheme.HTTP,
			},
			{
				PortName: "http",
				Scheme:   scheme.WebSocket,
			},
			{
				PortName: "tcp",
				Scheme:   scheme.HTTP,
			},
			{
				PortName: "grpc",
				Scheme:   scheme.GRPC,
			},
		}

		for _, config := range configs {
			t.Run(config.testName, func(t *testing.T) {
				framework.Run(t, func(ctx framework.TestContext) {
					ns := namespace.NewOrFail(t, ctx, "echo", true)

					a := echoboot.NewOrFail(t, ctx, config.apply("a", ns))
					b := echoboot.NewOrFail(t, ctx, config.apply("b", ns))

					a.WaitUntilReadyOrFail(t, b)

					for _, opts := range callOptions {
						opts.Target = b

						testName := opts.PortName
						if opts.PortName != string(opts.Scheme) {
							testName = fmt.Sprintf("%s over %s", opts.Scheme, opts.PortName)
						}
						t.Run(testName, func(t *testing.T) {
							ctx.Environment().Case(environment.Native, func() {
								if config.testName != "NoSidecar" {
									switch opts.Scheme {
									case scheme.WebSocket, scheme.GRPC:
										// TODO(https://github.com/istio/istio/issues/13754)
										t.Skipf("https://github.com/istio/istio/issues/13754")
									}
								}
							})
							a.CallOrFail(t, opts).
								CheckOKOrFail(t).
								CheckHostOrFail(t, "b")
						})
					}
				})
			})
		}
	})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("echo_test", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		Run()

}
