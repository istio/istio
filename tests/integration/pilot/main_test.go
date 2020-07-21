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
	"strconv"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i istio.Instance

	// Namespace echo apps will be deployed
	echoNamespace namespace.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	// Standard echo app to be used by tests
	a echo.Instance
	// Standard echo app to be used by tests
	b echo.Instance
	// Echo app to be used by tests, with no sidecar injected
	naked echo.Instance
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(&i, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  global:
    meshExpansion:
      enabled: true`
		})).
		Setup(func(ctx resource.Context) error {
			var err error
			// TODO: allow using an existing namespace to allow repeated runs with 0 setup
			echoNamespace, err = namespace.New(ctx, namespace.Config{
				Prefix: "echo",
				Inject: true,
			})
			if err != nil {
				return err
			}
			ports := []echo.Port{
				{Name: "http", Protocol: protocol.HTTP},
				{Name: "grpc", Protocol: protocol.GRPC},
				{Name: "tcp", Protocol: protocol.TCP},
				{Name: "auto-tcp", Protocol: protocol.TCP},
				{Name: "auto-http", Protocol: protocol.HTTP},
				{Name: "auto-grpc", Protocol: protocol.GRPC},
			}
			if err := echoboot.NewBuilder(ctx).
				With(&a, echo.Config{
					Service:   "a",
					Namespace: echoNamespace,
					Ports:     ports,
					Subsets:   []echo.SubsetConfig{{}},
				}).
				With(&b, echo.Config{
					Service:   "b",
					Namespace: echoNamespace,
					Ports:     ports,
					Subsets:   []echo.SubsetConfig{{}},
				}).
				With(&naked, echo.Config{
					Service:   "naked",
					Namespace: echoNamespace,
					Ports:     ports,
					Subsets: []echo.SubsetConfig{
						{
							Annotations: map[echo.Annotation]*echo.AnnotationValue{
								echo.SidecarInject: {
									Value: strconv.FormatBool(false)},
							},
						},
					},
				}).
				Build(); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func echoConfig(ns namespace.Instance, name string) echo.Config {
	return echo.Config{
		Service:   name,
		Namespace: ns,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
		},
		Subsets: []echo.SubsetConfig{{}},
	}
}
