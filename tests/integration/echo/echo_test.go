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
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
)

// TODO(sven): Add additional testing of the echo component, this is just the basics.
func TestEcho(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
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
				flaky    bool
			}{
				{
					testName: "Headless",
					apply: func(name string, ns namespace.Instance) echo.Config {
						cfg := baseCfg
						cfg.Service = name
						cfg.Namespace = ns
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
						return cfg
					},
				},
				{
					// TODO(https://github.com/istio/istio/issues/13810)
					flaky:    true,
					testName: "NoSidecar",
					apply: func(name string, ns namespace.Instance) echo.Config {
						cfg := baseCfg
						cfg.Service = name
						cfg.Namespace = ns
						cfg.Annotations = echo.NewAnnotations().
							SetBool(echo.SidecarInject, false)
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

				configTest := ctx.NewSubTest(config.testName)
				if config.flaky {
					configTest.Label(label.Flaky)
				}
				configTest.RunParallel(func(ctx framework.TestContext) {
					ns := namespace.NewOrFail(ctx, ctx, "echo", true)

					var a, b echo.Instance
					echoboot.NewBuilderOrFail(ctx, ctx).
						With(&a, config.apply("a", ns)).
						With(&b, config.apply("b", ns)).
						BuildOrFail(ctx)

					for _, o := range callOptions {
						// Make a copy of the options for the test.
						opts := o
						opts.Target = b

						testName := opts.PortName
						if opts.PortName != string(opts.Scheme) {
							testName = fmt.Sprintf("%s over %s", opts.Scheme, opts.PortName)
						}

						ctx.NewSubTest(testName).
							RunParallel(func(ctx framework.TestContext) {
								ctx.Environment().Case(environment.Native, func() {
									if config.testName != "NoSidecar" {
										switch opts.Scheme {
										case scheme.WebSocket, scheme.GRPC:
											// TODO(https://github.com/istio/istio/issues/13754)
											ctx.Skipf("https://github.com/istio/istio/issues/13754")
										}
									}
								})
								a.CallOrFail(ctx, opts).
									CheckOKOrFail(ctx).
									CheckHostOrFail(ctx, "b")
							})
					}
				})
			}
		})
}
